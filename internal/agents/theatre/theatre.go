package theatre

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

// DefaultCooldown is the minimum wall time between two generations. Without
// it, a handful of page refreshes would each trigger a full production.
const DefaultCooldown = 10 * time.Minute

// Theatre is the facade the rest of kinoview sees: it prepares the intro
// splash story by running a theatre production — a director superagent
// orchestrating mini-agent subagents over a shared board — and hands out the
// current story. It implements the agents.Teller contract (Next/Prepare/Warm),
// the house home of agent contracts since phase 9.
//
// Cooldown, single-flight, Warm's two-step seed-then-upgrade, Next's
// synchronous compose fallback, loadFromDisk validation and mtime-seeded
// cooldown are all preserved from the pre-migration intro-story agent the
// theatre replaced, unchanged in behaviour (decision D12). With no model
// configured the theatre never builds a director: Next/Warm/Prepare are
// composer-only, byte-identical to the pre-migration floor (the phase-9
// snapshot test proves it).
type Theatre struct {
	// muse supplies what the next play should be about.
	muse Muse

	rnd   *rand.Rand
	rndMu sync.Mutex // guards rnd: every draw is serialized — compose paths and the generation-id draw
	path  string

	cooldown time.Duration

	model     string
	configDir string
	cacheDir  string

	// company is the theatre's persistent paperwork (the board, the working
	// file, the ledger, the transcript and the seven company docs). The facade
	// owns it — New opens it once and every subsystem reads and writes through
	// it (R2-01): a fresh Company per call would not serialize its
	// load-modify-save paths across calls.
	company *Company

	directorMax int
	globalMax   int
	wallClock   time.Duration

	logSink func(model.LogMessage)

	// registry pins the canonical look per cast id (pin_identity, decision
	// D7): the permanent cast carries its canonical coat from generation one
	// (phase 6) and the book round-trips through registry.json.
	registry *Registry

	// writeMu serialises disk writes independently of mu, so persisting never
	// holds the lock that serving a story needs.
	writeMu sync.Mutex

	// writeWG tracks Next's fire-and-forget story writes, so tests can wait
	// for the background persist before tearing down their temp cache dirs.
	writeWG sync.WaitGroup

	mu       sync.Mutex
	current  *model.Story
	lastGen  time.Time
	inFlight bool

	// runLLM overrides the runner's LLM seam when set. Production leaves it
	// nil — the runner's own clai path builds the director and the subagents
	// from the shared model config. Tests inject a scripted fake, so a whole
	// production runs without a model configured.
	runLLM func(ctx context.Context, p llmParams) (llmOutcome, error)
}

// Muse supplies what the next play should be about.
//
// The theatre asks it at generation time rather than being handed a value,
// because preparation happens long after the story was requested — by then
// the household may have watched something else. The migrated muse (phase 9)
// also carries LatestTheme, the house default implementation.
type Muse interface {
	// Theme returns a short description of what to riff on, or "" for none.
	Theme() string
}

// MuseFunc adapts a plain function to a Muse.
type MuseFunc func() string

// Theme implements Muse.
func (f MuseFunc) Theme() string { return f() }

// Option configures a Theatre.
type Option func(*Theatre)

// WithMuse gives the theatre something to riff on — normally the most
// recently watched title.
func WithMuse(m Muse) Option {
	return func(t *Theatre) { t.muse = m }
}

// WithCallBudgets sets the generation's call budgets: the director's own cap
// and the global cap across every role (decision D8 — flags, tuned from
// telemetry).
func WithCallBudgets(directorMax, globalMax int) Option {
	return func(t *Theatre) {
		t.directorMax = directorMax
		t.globalMax = globalMax
	}
}

// WithWallClock caps one generation's wall clock; the broker refuses spawns
// past it and the director's loop is cancelled when it expires.
func WithWallClock(d time.Duration) Option {
	return func(t *Theatre) { t.wallClock = d }
}

// WithSessionSink streams mini-agent session lines to the house loghandler
// (or anywhere else that accepts model.LogMessage), the same shape the stage's
// WithLogSink takes.
func WithSessionSink(sink func(model.LogMessage)) Option {
	return func(t *Theatre) { t.logSink = sink }
}

// New builds a Theatre. Pass an empty model name to run composer-only, which
// reproduces the pre-migration composer-only behaviour exactly (the phase-9
// snapshot test proves it).
func New(c models.Configurations, cacheDir string, cooldown time.Duration, opts ...Option) *Theatre {
	t := &Theatre{
		rnd:         rand.New(rand.NewSource(time.Now().UnixNano())),
		path:        filepath.Join(cacheDir, "intro_story.json"),
		cooldown:    cooldown,
		model:       c.Model,
		configDir:   c.ConfigDir,
		cacheDir:    cacheDir,
		directorMax: DefaultDirectorBudget,
		globalMax:   DefaultGlobalBudget,
		wallClock:   DefaultWallClock,
		registry:    newRegistry(),
	}
	if t.cooldown <= 0 {
		t.cooldown = DefaultCooldown
	}
	for _, o := range opts {
		o(t)
	}
	t.company = Open(cacheDir)
	t.loadFromDisk()
	t.loadLibrary()
	return t
}

// loadLibrary picks up the company's durable memory at startup (phase 6):
// the registry document seeds the costumer's book, so characters canonized
// in an earlier generation survive the restart. A corrupt document is logged
// and degrades to the empty one — the server starts (the acceptance
// criterion).
func (t *Theatre) loadLibrary() {
	lib := t.company.LoadLibrary()
	t.registry.LoadDoc(lib.Registry)
}

// theme asks the muse what to riff on, tolerating a nil or panicking muse: a
// splash story is never worth taking the server down for.
func (t *Theatre) theme() (out string) {
	if t.muse == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			ancli.Errf("theatre: muse panicked: %v", r)
			out = ""
		}
	}()
	return strings.TrimSpace(t.muse.Theme())
}

// compose renders a story through the deterministic floor. The random source
// is guarded by its own mutex, because the compose paths run under different
// locks: Next composes under t.mu while Prepare and Warm compose outside it
// (Prepare must not hold t.mu across an LLM production). A shared rand.Rand
// drawn from two goroutines races (review 1, R1-01); the lock serializes the
// access without touching the draw order, so the composer-only path stays
// byte-identical to the frozen snapshot. The production path draws the same
// source for the generation id (openProduction) and goes through the same
// lock — newGenID (review 2, R2-01).
func (t *Theatre) compose(theme string) model.Story {
	t.rndMu.Lock()
	defer t.rndMu.Unlock()
	return ComposeThemed(t.rnd, theme)
}

// newGenID draws the next generation id from the theatre's random source.
// openProduction draws it before the runner starts and outside t.mu, so it is
// serialized through the same rndMu as every compose draw — the facade owns
// exactly two draws from t.rnd and both are guarded. The lock is leaf-ordered
// and never held across the production.
func (t *Theatre) newGenID() string {
	t.rndMu.Lock()
	defer t.rndMu.Unlock()
	return newID(t.rnd)
}

// Next hands out the prepared story, or composes one on the spot.
func (t *Theatre) Next() model.Story {
	theme := t.theme()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		return *t.current
	}
	// Nothing was prepared — compose one and persist it, so the store stops
	// being empty even if this process dies straight afterwards. The write
	// is tracked in writeWG so tests can wait for it; the failure is logged
	// and never fatal — the splash must not depend on disk health.
	s := t.compose(theme)
	t.current = &s
	t.writeWG.Go(func() {
		if err := t.saveStory(s); err != nil {
			ancli.Errf("theatre: persist composed story: %v", err)
		}
	})
	return s
}

// Prepare generates the next story, honouring the cooldown and single-flight.
func (t *Theatre) Prepare(ctx context.Context, reason string) bool {
	t.mu.Lock()
	if t.inFlight {
		t.mu.Unlock()
		return false
	}
	if since := time.Since(t.lastGen); !t.lastGen.IsZero() && since < t.cooldown {
		t.mu.Unlock()
		ancli.Noticef("theatre: skipping prep (%v, %v left of cooldown)",
			reason, (t.cooldown - since).Truncate(time.Second))
		return false
	}
	t.inFlight = true
	t.lastGen = time.Now()
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.inFlight = false
		t.mu.Unlock()
	}()

	theme := t.theme()
	story, err := t.generate(ctx, theme)
	if err != nil {
		ancli.Errf("theatre: production failed, composing instead: %v", err)
		story = t.compose(theme)
	}

	t.mu.Lock()
	t.current = &story
	t.mu.Unlock()
	if err := t.saveStory(story); err != nil {
		ancli.Errf("theatre: persist prepared story: %v", err)
	}
	ancli.Okf("theatre: prepared %q (%v, origin: %v)", story.Title, reason, story.Origin)
	return true
}

// generate produces the next story. With no model configured it composes
// directly — no director is built (the regression surface for the
// composer-only mode). Otherwise it runs a full production.
func (t *Theatre) generate(ctx context.Context, theme string) (model.Story, error) {
	if strings.TrimSpace(t.model) == "" {
		return t.compose(theme), nil
	}
	return t.runProduction(ctx, theme)
}

// loadFromDisk picks up a previously persisted story and, for an LLM-authored
// one, carries the cooldown across restarts: lastGen lives in memory, so
// without the mtime fallback a crash-loop would cost one API call per
// restart. A composed story never starts the clock — the cooldown limits API
// spend, and a composed story cost nothing.
func (t *Theatre) loadFromDisk() {
	b, err := os.ReadFile(t.path)
	if err != nil {
		return
	}
	var s model.Story
	if err := json.Unmarshal(b, &s); err != nil {
		ancli.Errf("theatre: cached story unreadable: %v", err)
		return
	}
	// A cache file is as untrusted as a fresh LLM reply — it was written by one.
	if err := s.Validate(); err != nil {
		ancli.Errf("theatre: cached story invalid: %v", err)
		return
	}
	t.current = &s
	if s.Origin == "llm" {
		if fi, statErr := os.Stat(t.path); statErr == nil {
			t.lastGen = fi.ModTime()
		}
	}
}

// Warm guarantees a stored story exists before the first visitor arrives.
//
// It does this in two steps, and the order matters. First it composes one
// synchronously and writes it to disk, themed on whatever was watched most
// recently — that is instant and free, so from this point on there is always
// a prepared story on disk, whatever happens next. Then, if a model is
// configured, it upgrades that story in the background.
func (t *Theatre) Warm(ctx context.Context) {
	t.mu.Lock()
	have := t.current != nil
	t.mu.Unlock()
	if have {
		// Something usable is already on disk from a previous run.
		if t.model != "" {
			go t.Prepare(ctx, "startup refresh")
		}
		return
	}

	theme := t.theme()
	seed := t.compose(theme)
	t.mu.Lock()
	t.current = &seed
	t.mu.Unlock()
	if err := t.saveStory(seed); err != nil {
		ancli.Errf("theatre: persist seeded story: %v", err)
	}
	if theme != "" {
		ancli.Okf("theatre: seeded %q from last watched %q", seed.Title, theme)
	} else {
		ancli.Okf("theatre: seeded %q (nothing watched yet)", seed.Title)
	}

	// Upgrade to an authored story in the background. The seeded one is
	// already safely stored, so a slow or failing LLM costs us nothing.
	if t.model != "" {
		go t.Prepare(ctx, "startup upgrade")
	}
}

// saveStory writes the story atomically: temp file then rename.
//
// The file is the guarantee that a story is always prepared, and it is read
// at startup. A half-written one would be rejected by Validate and leave us
// with nothing stored, so it must never be observable in a partial state.
//
// The error is returned, never swallowed: Next/Prepare/Warm log it and keep
// serving from memory, while submit_story aborts the submit — a submission
// must not claim a persistence the disk did not record (review 7, R7-02).
func (t *Theatre) saveStory(s model.Story) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal story: %w", err)
	}
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "intro_story-*.json")
	if err != nil {
		return fmt.Errorf("temp story file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp story: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp story: %w", err)
	}
	if err := os.Rename(tmpName, t.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace story: %w", err)
	}
	return nil
}

// idAlphabet and newID generate the theatre's id namespace: "stry_" plus 8
// lowercase alphanumerics, the same format the pre-migration cache used, so a
// pre-migration cache and the debug renderer keep working. The composer
// (floor.go) shares this single definition.
const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func newID(r *rand.Rand) string {
	var b strings.Builder
	b.WriteString("stry_")
	for range 8 {
		b.WriteByte(idAlphabet[r.Intn(len(idAlphabet))])
	}
	return b.String()
}

// Compile-time proof that the theatre satisfies the house Teller contract
// (internal/agents/interfaces.go) — the index wires it as agents.Teller.
var _ agents.Teller = (*Theatre)(nil)

// Compile-time proof that the theatre satisfies the house Feedbacker
// contract (internal/agents/interfaces.go) — the index type-asserts it in
// the intro feedback handler.
var _ agents.Feedbacker = (*Theatre)(nil)

// Feedback records one audience note about a story (the agents.Feedbacker
// contract). The note is appended through the facade's persistent company —
// the audience doc's single write path (decision D-5) — so the
// load-modify-save holds the company's mutex and two concurrent posts lose
// no note (R2-01). The facade is the trust boundary, like submit_story: it
// re-checks the rating and the story id even though the handler already
// validated them. The comment is truncated to its cap by the doc trim, never
// rejected (decision D-3).
func (t *Theatre) Feedback(_ context.Context, storyID string, rating int, comment string) error {
	if rating != 1 && rating != -1 {
		return fmt.Errorf("theatre: feedback rating %d out of {+1, -1}", rating)
	}
	storyID = strings.TrimSpace(storyID)
	if !artifactIDRe.MatchString(storyID) {
		return fmt.Errorf("theatre: feedback story id %q does not match %v", storyID, artifactIDRe)
	}
	return t.company.AppendAudience(AudienceNote{
		StoryID: storyID,
		Rating:  rating,
		Comment: comment,
		Date:    dateStamp(time.Now()),
	})
}

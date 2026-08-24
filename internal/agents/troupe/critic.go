package troupe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
)

// criticToolCalls is the critic's per-agent tool-call cap: the same
// hardcoded global maximum that bounds the director's loop. The critic runs
// after the generation — never inside it, no review gate — and it is not
// part of the generation budget: an exhausted generation (stoploss or call
// max spent) must not silence the empty-stage review, so the critic carries
// its own bound instead of admitting against the budget.
const criticToolCalls = maxGenerationCalls

const (
	// maxCriticismBodyLen caps the advisory body of one criticism note, the
	// same cap as a role note's prompt: room for a spicy, evidence-cited
	// review, bounded so one note cannot balloon the notebook.
	maxCriticismBodyLen = 8000
	// maxCriticismGenerationLen caps the generation id a criticism note
	// names. The stage stamps it; the cap keeps the note envelope honest.
	maxCriticismGenerationLen = 128
)

// NoteTypeCriticism is the feedback note type the critic writes: the
// server-side criticism of one generation, never audience feedback. Phase 9's
// unified feedback directory serves it beside the audience types.
const NoteTypeCriticism = "criticism"

// CriticismData is the kind-specific data of a criticism note: the
// generation the critic reviewed, the note paths the criticism is grounded
// in (cites), and the advisory body.
type CriticismData struct {
	GenerationID string   `json:"generationId"`
	Cites        []string `json:"cites"`
	Body         string   `json:"body"`
}

// CriticismNote is one feedback/ note with type criticism: the uniform
// feedback envelope (playId/type/ts/data, decision 21). The writer stamps ts
// server-side — the critic never does. playId is the play the criticism
// reviews, pinned by the stage from the generation's outcome; it is empty
// for the empty stage (a generation that shipped nothing).
type CriticismNote struct {
	PlayID string        `json:"playId"`
	Type   string        `json:"type"`
	TS     string        `json:"ts"`
	Data   CriticismData `json:"data"`
}

// Criticism is one criticism note request: the play id (empty when the
// generation shipped nothing — the empty-stage note), the generation id, the
// evidence paths the note is grounded in, and the advisory body.
type Criticism struct {
	PlayID     string
	Generation string
	Cites      []string
	Body       string
}

// criticismNoteDirs are the notebook directories whose .json files a
// criticism note may cite as evidence. Feedback notes are the canonical
// evidence; the rest cover the empty-stage case, where the critic cites
// whatever notes exist (the swarm's work) for why nothing shipped.
// plays/index.json is bookkeeping, never a note, and is excluded.
var criticismNoteDirs = map[string]bool{
	"feedback": true, "plays": true, "roles": true,
	"models": true, "clips": true, "voices": true, "sounds": true, "gags": true,
}

// CriticismWriter is the critic's append-only gate: the only writer of
// criticism notes in feedback/. It validates the note — cites must be real
// notebook note paths, the body non-empty, a named play on disk — stamps the
// server time, derives the filename (<playId>_criticism_<utc>.json) and
// persists the note atomically. It only appends: an existing file at the
// derived name is refused, never overwritten. The commit half of the
// write+commit unit is the WithCriticismCommit seam, wired by phase 9's
// serve setup.
type CriticismWriter struct {
	mu       sync.Mutex                  // the critic runs once per generation, but appends must serialize
	worktree string                      // the materialised notebook worktree
	commit   func(filename string) error // nil: no commit (tests)
	now      func() time.Time            // test seam; production stamps time.Now
}

// CriticismWriterOption configures one CriticismWriter.
type CriticismWriterOption func(*CriticismWriter)

// WithCriticismClock overrides the writer's clock — a test seam only, never
// an operator surface: the note's ts is always the server time in
// production, and the filename derives from it.
func WithCriticismClock(now func() time.Time) CriticismWriterOption {
	return func(w *CriticismWriter) { w.now = now }
}

// WithCriticismCommit sets the commit half of the write+commit unit: a
// function committing the worktree through the shared notebook after a note
// is written. Production wires the slivingdoc commit; tests leave it unset.
// A commit failure surfaces from Write, so the facade logs the loss — a
// criticism note is never silently dropped.
func WithCriticismCommit(fn func(filename string) error) CriticismWriterOption {
	return func(w *CriticismWriter) { w.commit = fn }
}

// NewCriticismWriter builds the criticism gate over a materialised notebook
// worktree — the same directory the critic reads feedback/ and plays/ from.
func NewCriticismWriter(worktree string, opts ...CriticismWriterOption) (*CriticismWriter, error) {
	if worktree == "" {
		return nil, errors.New("troupe: criticism writer: worktree can't be empty")
	}
	w := &CriticismWriter{worktree: worktree}
	for _, o := range opts {
		o(w)
	}
	return w, nil
}

// Write validates and persists one criticism note. Exact errors return
// wherever the evidence is missing — a fabricated cite, an empty body, a
// play that is not on disk — and nothing is written. The note's ts is
// stamped here, server-side, and the filename derives from it, so the
// filename and the body never drift.
func (w *CriticismWriter) Write(c Criticism) (CriticismNote, error) {
	// The append-only gate serialises whole writes: the exists-check and the
	// atomic rename must not interleave with another writer, or a second
	// note in the same second could overwrite the first (D-8-6, the same
	// single-writer rule the submitter enforces under -race).
	w.mu.Lock()
	defer w.mu.Unlock()

	e := &errs{}
	if c.PlayID != "" {
		if !playIDRe.MatchString(c.PlayID) {
			e.addf("play %q must match story_YYYYMMDDTHHMMSSZ or be empty (the empty stage)", c.PlayID)
		} else if _, err := os.Stat(filepath.Join(w.worktree, "plays", c.PlayID+".json")); err != nil {
			e.addf("play %s is not on disk in plays/ — never fabricate a play", c.PlayID)
		}
	}
	if c.Generation == "" {
		e.addf("generation: must not be empty")
	} else if n := utf8.RuneCountInString(c.Generation); n > maxCriticismGenerationLen {
		e.addf("generation: %d runes exceeds the %d cap", n, maxCriticismGenerationLen)
	}
	if len(c.Cites) == 0 {
		e.addf("cites: must cite at least one note path")
	}
	for _, cite := range c.Cites {
		if err := w.checkCite(cite); err != nil {
			e.addf("%v", err)
		}
	}
	if c.Body == "" {
		e.addf("body: must not be empty")
	} else if n := utf8.RuneCountInString(c.Body); n > maxCriticismBodyLen {
		e.addf("body: %d runes exceeds the %d cap", n, maxCriticismBodyLen)
	}
	if err := e.err(); err != nil {
		return CriticismNote{}, fmt.Errorf("troupe: criticism: %w", err)
	}

	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	note := CriticismNote{
		PlayID: c.PlayID,
		Type:   NoteTypeCriticism,
		TS:     now.Format(time.RFC3339),
		Data: CriticismData{
			GenerationID: c.Generation,
			Cites:        c.Cites,
			Body:         c.Body,
		},
	}
	filename := criticismFilename(c.PlayID, now)
	target := filepath.Join(w.worktree, "feedback", filename)
	if _, err := os.Stat(target); err == nil {
		return CriticismNote{}, fmt.Errorf("troupe: criticism: %s already exists — notes are append-only", filename)
	}
	data, err := json.Marshal(note)
	if err != nil {
		return CriticismNote{}, fmt.Errorf("troupe: criticism: marshal: %w", err)
	}
	if err := writeFileAtomic(target, data); err != nil {
		return CriticismNote{}, fmt.Errorf("troupe: criticism: %w", err)
	}
	if w.commit != nil {
		if err := w.commit(filename); err != nil {
			return CriticismNote{}, fmt.Errorf("troupe: criticism: %s: commit: %w", filename, err)
		}
	}
	return note, nil
}

// checkCite validates one cite: a worktree-relative path to a real .json
// note in a notebook directory. A cite naming anything else — a missing
// file, a path that escapes the notebook, the plays/ index — is fabricated
// evidence and refused with an exact error.
func (w *CriticismWriter) checkCite(cite string) error {
	clean := path.Clean(cite)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("cites: %q escapes the notebook", cite)
	}
	dir, base := path.Split(clean)
	dir = strings.TrimSuffix(dir, "/")
	if !criticismNoteDirs[dir] {
		return fmt.Errorf("cites: %q is not a notebook note path", cite)
	}
	if !strings.HasSuffix(base, ".json") {
		return fmt.Errorf("cites: %q must name a .json note", cite)
	}
	if dir == "plays" && base == "index.json" {
		return fmt.Errorf("cites: %q is bookkeeping, never a note", cite)
	}
	info, err := os.Stat(filepath.Join(w.worktree, filepath.FromSlash(clean)))
	if err != nil {
		return fmt.Errorf("cites: %q is not on disk", cite)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cites: %q is not a file", cite)
	}
	return nil
}

// criticismFilename derives the feedback/ filename of a criticism note:
// <playId>_criticism_<utc>.json, or criticism_<utc>.json for the empty
// stage (no play id to lead the name). The <utc> is the compact form of the
// stamped ts, so the filename and the body never drift. The shared
// feedbackFilename helper owns the derivation; this wrapper pins the type.
func criticismFilename(playID string, ts time.Time) string {
	return feedbackFilename(playID, NoteTypeCriticism, ts)
}

// criticPrompt is the fixed critic prompt: the reflective pole of the stage,
// short-imperative like every other fixed role note. It names the read paths
// and the write_criticism gate; the tool description carries the argument
// list. The generation id and the generation's outcome are stamped at run
// time — the critic must review the right generation and know honestly
// whether the stage is empty.
const criticPrompt = `You are the critic of a tiny self-contained theatre on a media server. You are
the reflective pole of the stage: after a generation you review what it
shipped — or failed to ship — against the viewer ground truth, and you leave
evidence-cited, spicy comments for the next generation's director. You have
opinions, never authority: you never drive, never block, never edit. Your
only write path is one criticism note appended to feedback/.

WORKFLOW — run these phases in order:

1. READ — read the viewer feedback in feedback/ (ls feedback/, cat the
   notes). Read the submitted play in plays/ (cat the newest story_*.json).
   Read whatever the swarm left in the repertoire.
2. JUDGE — form an opinion: did the play serve the viewers' ground truth?
   What worked, what did not, what should the next generation try? Be spicy
   and specific — evidence, not vibes.
3. COMMENT — call write_criticism with the generation id, the note paths you
   are grounded in (cites) and your body. Every cite must be a real notebook
   note path you read — feedback/ notes first; never fabricate a note. The
   play id is pinned by the stage from this generation's outcome: when
   nothing was submitted, write honestly about the empty stage and never
   invent a play.`

// Critic is the fixed advisory role: after a generation it reads viewer
// feedback, the submitted play and the notes the swarm left, and emits
// evidence-cited, spicy comments for the next generation's director. It has
// opinions, never authority: it never drives (there is no review gate the
// director must pass), never vetoes, never edits — its only write path is
// one criticism note appended to feedback/ through the write_criticism gate.
// An exhausted generation is still reviewed: the critic comments on why
// nothing shipped, citing whatever notes exist, and never fabricates a play.
type Critic struct {
	model     string
	configDir string
	logger    *slog.Logger
	writer    *CriticismWriter // the append-only gate

	// runCritic is the seam between the critic and clai: production builds
	// the bounded clai agent (Setup + Run) with the read-only tool set and
	// the gate; tests inject a scripted fake that drives the tool objects
	// directly, so a whole review runs without a model configured.
	runCritic func(ctx context.Context, p criticParams) (string, error)
}

// criticParams is everything one critic agent loop needs: the fixed prompt
// (stamped with the generation id and outcome) and the tool objects.
type criticParams struct {
	prompt string
	globs  []string
	tools  []models.LLMTool
}

// CriticOption configures one Critic.
type CriticOption func(*Critic)

// WithCriticismWriter sets the append-only gate the critic's write_criticism
// tool persists through. It must operate on the same worktree the critic
// reads.
func WithCriticismWriter(w *CriticismWriter) CriticOption {
	return func(c *Critic) { c.writer = w }
}

// WithCriticModel sets the critic's clai model (the -troupeModel flag).
func WithCriticModel(m string) CriticOption {
	return func(c *Critic) { c.model = m }
}

// WithCriticConfigDir sets the clai config dir the critic's agent uses.
func WithCriticConfigDir(dir string) CriticOption {
	return func(c *Critic) { c.configDir = dir }
}

// WithCriticLogger attaches a slog logger to the critic's agent, so the
// review's model steps land in a log sink instead of racing on stdout.
func WithCriticLogger(l *slog.Logger) CriticOption {
	return func(c *Critic) { c.logger = l }
}

// WithRunCritic overrides the critic's agent seam (tests): the scripted fake
// receives the assembled prompt, globs and tools and drives them directly,
// so a whole review runs without an LLM. Production leaves it unset and the
// critic builds and runs the bounded clai agent.
func WithRunCritic(fn func(ctx context.Context, p criticParams) (string, error)) CriticOption {
	return func(c *Critic) { c.runCritic = fn }
}

// NewCritic builds the fixed advisory role. The writer, the model and the
// config dir are required.
func NewCritic(opts ...CriticOption) (*Critic, error) {
	c := &Critic{}
	for _, o := range opts {
		o(c)
	}
	switch {
	case c.writer == nil:
		return nil, errors.New("troupe: critic: criticism writer can't be nil")
	case c.model == "":
		return nil, errors.New("troupe: critic: model can't be empty")
	case c.configDir == "":
		return nil, errors.New("troupe: critic: config dir can't be empty")
	}
	if c.runCritic == nil {
		c.runCritic = c.runAgent
	}
	return c, nil
}

// Run reviews one generation: the critic's bounded agent loop reads the
// feedback trail, the submitted play (or the empty stage) and the notes the
// swarm left, and writes one evidence-cited criticism note through the gate.
// Run is not part of the generation — it never admits against the generation
// budget, so an exhausted generation is still reviewed — and its outcome is
// advisory: a review that writes nothing (the critic found no evidence, or
// chose not to comment) fails no one. generationID is the generation under
// review, stamped into the note; outcome tells the critic what shipped, so
// the empty stage is commented on honestly, never fabricated.
func (c *Critic) Run(ctx context.Context, generationID string, outcome Outcome) error {
	if generationID == "" {
		return errors.New("troupe: critic: generation id can't be empty")
	}
	globs, tools := c.toolSet(outcome)
	if _, err := c.runCritic(ctx, criticParams{
		prompt: c.prompt(generationID, outcome),
		globs:  globs,
		tools:  tools,
	}); err != nil {
		return fmt.Errorf("troupe: critic: %w", err)
	}
	return nil
}

// criticReadTools are the critic's instrument set: the read-only file tools
// from the closed registry. The critic never writes the repertoire — no
// write_file, no apply_patch, no mkdir, no spawn_role, no submit_play — so
// its only write path is the write_criticism gate appended below. The
// "never edits" rule is enforced by the tool set, not by the prompt.
var criticReadTools = []string{"cat", "rows_between", "ls", "rg"}

// toolSet is the critic's instrument set: the read-only registry globs plus
// write_criticism pinned to the generation's outcome — the submitted play
// id, or the honest empty stage (the critic structurally cannot claim a play
// that was not submitted). The registry remains the name→tool authority; a
// name in criticReadTools that drifts out of it shows up as a missing glob
// here, pinned by a test.
func (c *Critic) toolSet(outcome Outcome) (globs []string, tools []models.LLMTool) {
	for _, e := range toolRegistry {
		if slices.Contains(criticReadTools, e.name) {
			globs = append(globs, e.glob)
		}
	}
	tools = append(tools, c.writer.newWriteCriticismTool(outcome.PlayID))
	return globs, tools
}

// prompt assembles the critic's system prompt: the fixed workflow, the
// generation id under review, and the generation's outcome — the submitted
// play id, or the honest empty stage.
func (c *Critic) prompt(generationID string, outcome Outcome) string {
	p := criticPrompt + "\n\nGENERATION " + generationID + "\n"
	if outcome.Submitted {
		p += "PLAY " + outcome.PlayID + " — this generation submitted it; review it against the feedback.\n"
	} else {
		p += "PLAY none — this generation shipped nothing; the stage is empty. Say why, citing whatever notes exist.\n"
	}
	return p
}

// runAgent is the production critic seam: the bounded clai agent with the
// fixed prompt, the read-only tool set plus the gate, and the critic's own
// tool-call cap. No usage recorder: the critic is not part of the generation
// budget — it runs after the generation, and an exhausted generation must
// still be reviewed.
func (c *Critic) runAgent(ctx context.Context, p criticParams) (string, error) {
	opts := []agent.Option{
		agent.WithModel(c.model),
		agent.WithConfigDir(c.configDir),
		agent.WithPrompt(p.prompt),
		agent.WithTools(p.tools),
		agent.WithMaxToolCalls(criticToolCalls),
	}
	if len(p.globs) > 0 {
		opts = append(opts, agent.WithToolGlobs(p.globs...))
	}
	if c.logger != nil {
		opts = append(opts, agent.WithLogger(c.logger))
	}
	a := agent.New(opts...)
	if err := a.Setup(ctx); err != nil {
		return "", fmt.Errorf("troupe: critic setup: %w", err)
	}
	out, err := a.Run(ctx)
	if err != nil {
		return "", fmt.Errorf("troupe: critic run: %w", err)
	}
	return out, nil
}

// writeCriticismTool is the critic-only write_criticism tool: it appends one
// evidence-cited criticism note to feedback/. It is excluded from the
// general registry — roles can never select it, and no spawned agent can
// ever write a criticism note — and phase 8 grants it to the critic's fixed
// tool set. The play id is pinned by the stage from the generation's outcome
// at tool construction; the model never names a play.
type writeCriticismTool struct {
	writer *CriticismWriter
	playID string // the submitted play id, or "" for the empty stage
}

// newWriteCriticismTool builds the write_criticism tool bound to this writer
// and pinned to the play the reviewed generation shipped (empty when it
// shipped nothing).
func (w *CriticismWriter) newWriteCriticismTool(playID string) models.LLMTool {
	return &writeCriticismTool{writer: w, playID: playID}
}

func (t *writeCriticismTool) Call(input models.Input) (string, error) {
	generation, ok := input["generation"].(string)
	if !ok || generation == "" {
		return "", errors.New("write_criticism: generation must be a non-empty string (the generation id)")
	}
	cites, err := toStrings(input["cites"])
	if err != nil {
		return "", errors.New("write_criticism: cites " + err.Error())
	}
	body, ok := input["body"].(string)
	if !ok || body == "" {
		return "", errors.New("write_criticism: body must be a non-empty string")
	}
	note, err := t.writer.Write(Criticism{
		PlayID:     t.playID,
		Generation: generation,
		Cites:      cites,
		Body:       body,
	})
	if err != nil {
		return "", err
	}
	ts, err := time.Parse(time.RFC3339, note.TS)
	if err != nil {
		return "", fmt.Errorf("write_criticism: stamp ts: %w", err)
	}
	return fmt.Sprintf("criticism written: feedback/%s", criticismFilename(note.PlayID, ts)), nil
}

func (t *writeCriticismTool) Specification() models.Specification {
	return models.Specification{
		Name:        "write_criticism",
		Description: "Append one evidence-cited criticism note to feedback/ — the review of this generation for the next director. generation is the generation id; cites are the real notebook note paths this criticism is grounded in (feedback/ notes first; every cite must be an existing note path); body is the advisory text. The play id is pinned by the stage from this generation's outcome and the note's ts is stamped server-side; the filename derives from them. This is the critic's only write path: it never edits the play or the repertoire, only appends.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"generation": {
					Type:        "string",
					Description: "The generation id under review",
				},
				"cites": {
					Type:        "array",
					Description: "Real notebook note paths this criticism is grounded in, e.g. feedback/story_20260820T161500Z_rating_20260820T170000Z.json",
					Items:       &models.ParameterObject{Type: "string"},
				},
				"body": {
					Type:        "string",
					Description: "The advisory criticism text, evidence-cited and specific",
				},
			},
			Required: []string{"generation", "cites", "body"},
		},
	}
}

// toStrings converts a clai tool input value into a []string: the JSON array
// the model passes arrives as []any of strings. Anything else is an exact
// error the critic fixes.
func toStrings(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("must be an array of note paths")
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, errors.New("must be an array of note paths (strings)")
		}
		out = append(out, s)
	}
	return out, nil
}

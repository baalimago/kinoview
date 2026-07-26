package storyteller

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

	"github.com/baalimago/clai/pkg/text"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// DefaultCooldown is the minimum wall time between two generations. Without it,
// a handful of page refreshes would each trigger an LLM call.
const DefaultCooldown = 10 * time.Minute

// Teller prepares a short story for the next visit and hands out the current one.
type Teller interface {
	// Next returns the story to play now. It never fails: if nothing has been
	// prepared it composes one synchronously.
	Next() model.Story

	// Prepare generates the story for the *next* visit. Subject to the cooldown
	// and to single-flight; returns true if a generation actually ran.
	Prepare(ctx context.Context, reason string) bool

	// Warm makes sure something good is ready before the first visitor arrives.
	Warm(ctx context.Context)
}

type teller struct {
	// llm is nil when no model is configured — the composer covers that.
	llm  text.FullResponse
	rnd  *rand.Rand
	path string

	cooldown time.Duration

	muse Muse

	mu       sync.Mutex
	current  *model.Story
	lastGen  time.Time
	inFlight bool
}

const systemPrompt = `You write tiny wordless slapstick scenes for a media-server splash screen.
The scene is acted out by simple cartoon animals over about 10 seconds, then the app opens.

The permanent cast:
  - "ina"    : a cat  (character "cat")
  - "freija" : a dog  (character "dog")
  - "mouse1" : a mouse they like to hunt (character "mouse")

You do not have to use all of them. Two characters is usually funnier than three.

Respond with ONLY a JSON object, no prose and no code fences:
{
  "title": "<short, charming, max 60 chars>",
  "durationMs": 9500,
  "scene":  { "backdrop": "livingroom" },
  "cast":  [ { "id": "ina", "character": "cat", "lane": 0, "x": 0.35, "scale": 1.0 } ],
  "props": [ { "id": "yarn1", "prop": "yarn", "lane": 0, "x": 0.5 } ],
  "beats": [ { "t": 0, "actor": "ina", "action": "enter", "from": "left", "ms": 1100 } ]
}

Rules:
  * "scene".backdrop must be one of: night, livingroom, garden, theatre, sunset
  * "character" must be one of: cat, dog, mouse
  * "prop" must be one of: yarn, box            (props are optional)
  * "action" must be one of: enter, exit, walkTo, vocalize, sit, stretch, blink,
    pounce, chase, greet, stareoff, nap, bat
  * pounce, chase, greet, stareoff and bat REQUIRE a "target" naming another id
  * "x" is a fraction of screen width, 0.05 to 0.95. "t" and "ms" are milliseconds.
  * every character must "enter" before it does anything else
  * "t" must be between 0 and durationMs, and durationMs at most 10000
  * at most 5 cast, 4 props, 44 beats
  * aim for 9000-9500ms and 12-20 beats: a real little scene, not one gag
  * "vocalize" makes the character speak (meow / bark / squeak) — use it 3-5 times, spread out
  * Titles like "Ina & Freija in: The Great Mouse Hunt" are welcome but keep them short

Write a scene in three acts: a setup, a complication, and a resolution.
Leave breathing room — a beat of stillness between actions reads better than constant motion.`

// Option configures a Teller.
type Option func(*teller)

// WithMuse gives the storyteller something to riff on — normally the most
// recently watched title.
func WithMuse(m Muse) Option {
	return func(t *teller) { t.muse = m }
}

// New builds a Teller. Pass an empty model name to run composer-only.
func New(c models.Configurations, cacheDir string, cooldown time.Duration, opts ...Option) Teller {
	t := &teller{
		rnd:      rand.New(rand.NewSource(time.Now().UnixNano())),
		path:     filepath.Join(cacheDir, "intro_story.json"),
		cooldown: cooldown,
	}
	if t.cooldown <= 0 {
		t.cooldown = DefaultCooldown
	}
	if strings.TrimSpace(c.Model) != "" {
		c.SystemPrompt = systemPrompt
		t.llm = text.NewFullResponseQuerier(c)
	}
	for _, o := range opts {
		o(t)
	}
	t.loadFromDisk()
	return t
}

// theme asks the muse what to riff on, tolerating a nil or panicking muse: a
// splash story is never worth taking the server down for.
func (t *teller) theme() (out string) {
	if t.muse == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			ancli.Errf("storyteller: muse panicked: %v", r)
			out = ""
		}
	}()
	return strings.TrimSpace(t.muse.Theme())
}

// Next hands out the prepared story, or composes one on the spot.
func (t *teller) Next() model.Story {
	theme := t.theme()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		return *t.current
	}
	s := ComposeThemed(t.rnd, theme)
	t.current = &s
	return s
}

// Prepare generates the next story, honouring the cooldown and single-flight.
func (t *teller) Prepare(ctx context.Context, reason string) bool {
	t.mu.Lock()
	if t.inFlight {
		t.mu.Unlock()
		return false
	}
	if since := time.Since(t.lastGen); !t.lastGen.IsZero() && since < t.cooldown {
		t.mu.Unlock()
		ancli.Noticef("storyteller: skipping prep (%v, %v left of cooldown)",
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
		ancli.Errf("storyteller: llm failed, composing instead: %v", err)
		story = ComposeThemed(t.rnd, theme)
	}

	t.mu.Lock()
	t.current = &story
	t.mu.Unlock()
	t.saveToDisk(story)
	ancli.Okf("storyteller: prepared %q (%v, origin: %v)", story.Title, reason, story.Origin)
	return true
}

// generate asks the LLM for a story and validates it. Any problem is an error,
// which the caller answers with the composer.
func (t *teller) generate(ctx context.Context, theme string) (model.Story, error) {
	if t.llm == nil {
		return ComposeThemed(t.rnd, theme), nil
	}

	chat := models.Chat{
		Messages: []models.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(theme)},
		},
	}

	resp, err := t.llm.Query(ctx, chat)
	if err != nil {
		return model.Story{}, fmt.Errorf("llm query: %w", err)
	}

	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		if len(resp.Messages) == 0 {
			return model.Story{}, fmt.Errorf("empty response from llm")
		}
		lastMsg = resp.Messages[len(resp.Messages)-1]
	}

	raw := extractJSON(lastMsg.Content)
	if raw == "" {
		return model.Story{}, fmt.Errorf("no JSON object found in llm reply")
	}

	var s model.Story
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return model.Story{}, fmt.Errorf("unmarshal story: %w", err)
	}
	// The LLM does not pick ids; we own them so they always validate.
	s.ID = newID(t.rnd)
	s.Origin = "llm"
	s.Theme = theme
	if err := s.Validate(); err != nil {
		return model.Story{}, fmt.Errorf("llm story invalid: %w", err)
	}
	return s, nil
}

// userPrompt asks for the next scene, themed on whatever was last watched.
func userPrompt(theme string) string {
	if theme == "" {
		return "Write the next scene. Surprise me — do not repeat the most obvious idea."
	}
	return "The household just finished watching: \"" + theme + "\".\n\n" +
		"Write the next scene as a wordless animal homage to it — borrow its MOOD and " +
		"SHAPE (a heist, a chase, a slow sad goodbye, a standoff), not its plot. The " +
		"animals cannot speak or hold objects, so translate it into things a cat, a dog " +
		"and a mouse could actually do on a bare stage. Pick the backdrop that suits it. " +
		"The title may play on the original."
}

// extractJSON pulls the first balanced {...} out of a reply, tolerating code
// fences and any stray prose an LLM decides to add.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// skip
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func (t *teller) loadFromDisk() {
	b, err := os.ReadFile(t.path)
	if err != nil {
		return
	}
	var s model.Story
	if err := json.Unmarshal(b, &s); err != nil {
		ancli.Errf("storyteller: cached story unreadable: %v", err)
		return
	}
	// A cache file is as untrusted as a fresh LLM reply — it was written by one.
	if err := s.Validate(); err != nil {
		ancli.Errf("storyteller: cached story invalid: %v", err)
		return
	}
	t.current = &s

	// Carry the cooldown across restarts. lastGen lives in memory, so without
	// this a restart resets it and the next visit triggers a fresh generation —
	// a crash-loop or a few manual restarts would each cost an LLM call, which
	// is exactly what the cooldown exists to prevent. The file's mtime is when
	// we last generated, so it needs no extra state.
	if fi, statErr := os.Stat(t.path); statErr == nil {
		t.lastGen = fi.ModTime()
	}
}

// Warm prepares a story at startup so the first visitor gets a real one.
//
// Without it the first ever visit is always composer-authored: Next() composes
// synchronously rather than blocking the splash on an LLM call, and the LLM only
// runs afterwards. Warming in the background fixes that without ever making the
// splash wait. It is a no-op when a usable story is already cached, and it goes
// through the same cooldown and single-flight as any other preparation.
func (t *teller) Warm(ctx context.Context) {
	t.mu.Lock()
	have := t.current != nil
	t.mu.Unlock()
	if have {
		return
	}
	go t.Prepare(ctx, "startup warm-up")
}

func (t *teller) saveToDisk(s model.Story) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		ancli.Errf("storyteller: marshal story: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		ancli.Errf("storyteller: mkdir cache: %v", err)
		return
	}
	if err := os.WriteFile(t.path, b, 0o644); err != nil {
		ancli.Errf("storyteller: write story: %v", err)
	}
}

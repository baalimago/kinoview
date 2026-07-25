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
}

type teller struct {
	// llm is nil when no model is configured — the composer covers that.
	llm  text.FullResponse
	rnd  *rand.Rand
	path string

	cooldown time.Duration

	mu       sync.Mutex
	current  *model.Story
	lastGen  time.Time
	inFlight bool
}

const systemPrompt = `You write tiny wordless slapstick scenes for a media-server splash screen.
The scene is acted out by simple cartoon animals in about 4 seconds, then the app opens.

The permanent cast:
  - "ina"    : a cat  (character "cat")
  - "freija" : a dog  (character "dog")
  - "mouse1" : a mouse they like to hunt (character "mouse")

You do not have to use all of them. Two characters is usually funnier than three.

Respond with ONLY a JSON object, no prose and no code fences:
{
  "title": "<short, charming, max 60 chars>",
  "durationMs": 4000,
  "cast":  [ { "id": "ina", "character": "cat", "lane": 0, "x": 0.35, "scale": 1.0 } ],
  "props": [ { "id": "yarn1", "prop": "yarn", "lane": 0, "x": 0.5 } ],
  "beats": [ { "t": 0, "actor": "ina", "action": "enter", "from": "left", "ms": 1100 } ]
}

Rules:
  * "character" must be one of: cat, dog, mouse
  * "prop" must be one of: yarn, box            (props are optional)
  * "action" must be one of: enter, exit, walkTo, vocalize, sit, stretch, blink,
    pounce, chase, greet, stareoff, nap, bat
  * pounce, chase, greet, stareoff and bat REQUIRE a "target" naming another id
  * "x" is a fraction of screen width, 0.05 to 0.95. "t" and "ms" are milliseconds.
  * every character must "enter" before it does anything else
  * "t" must be between 0 and durationMs, and durationMs at most 5000
  * at most 4 cast, 3 props, 24 beats
  * "vocalize" makes the character speak (meow / bark / squeak) — use it 2-3 times
  * Titles like "Ina & Freija in: The Great Mouse Hunt" are welcome but keep them short

Write a scene with a beginning, a small surprise, and an ending.`

// New builds a Teller. Pass an empty model name to run composer-only.
func New(c models.Configurations, cacheDir string, cooldown time.Duration) Teller {
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
	t.loadFromDisk()
	return t
}

// Next hands out the prepared story, or composes one on the spot.
func (t *teller) Next() model.Story {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		return *t.current
	}
	s := Compose(t.rnd)
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

	story, err := t.generate(ctx)
	if err != nil {
		ancli.Errf("storyteller: llm failed, composing instead: %v", err)
		story = Compose(t.rnd)
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
func (t *teller) generate(ctx context.Context) (model.Story, error) {
	if t.llm == nil {
		return Compose(t.rnd), nil
	}

	chat := models.Chat{
		Messages: []models.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Write the next scene. Surprise me — do not repeat the most obvious idea."},
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
	if err := s.Validate(); err != nil {
		return model.Story{}, fmt.Errorf("llm story invalid: %w", err)
	}
	return s, nil
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

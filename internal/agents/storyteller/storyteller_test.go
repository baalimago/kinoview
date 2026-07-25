package storyteller

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

// Every composer template must produce a story that survives validation, for
// any seed. A template regression would otherwise silently degrade to
// minimalStory in production.
func TestCompose_AllScenesValidateAcrossSeeds(t *testing.T) {
	seen := map[string]bool{}
	for seed := int64(0); seed < 400; seed++ {
		r := rand.New(rand.NewSource(seed))
		s := Compose(r)
		if err := s.Validate(); err != nil {
			t.Fatalf("seed %d produced invalid story: %v", seed, err)
		}
		if s.Origin != "composer" {
			t.Errorf("seed %d: origin = %q", seed, s.Origin)
		}
		if s.Title == "" {
			t.Errorf("seed %d: empty title", seed)
		}
		if len(s.Cast) == 0 || len(s.Beats) == 0 {
			t.Errorf("seed %d: empty cast or beats", seed)
		}
		seen[s.Title] = true
	}
	// Sanity: the composer should not be emitting a single story forever.
	if len(seen) < 5 {
		t.Errorf("expected varied titles across seeds, got %d distinct", len(seen))
	}
}

// Anyone who acts must have entered first, otherwise the player would be asked
// to animate a character that is not on stage.
func TestCompose_EveryActorEntersBeforeActing(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		r := rand.New(rand.NewSource(seed))
		s := Compose(r)
		entered := map[string]int{}
		for _, b := range s.Beats {
			if b.Action == "enter" {
				if _, ok := entered[b.Actor]; !ok {
					entered[b.Actor] = b.T
				}
			}
		}
		for _, b := range s.Beats {
			if b.Action == "enter" {
				continue
			}
			at, ok := entered[b.Actor]
			if !ok {
				t.Fatalf("seed %d: %q acts (%s) without entering", seed, b.Actor, b.Action)
			}
			if b.T < at {
				t.Fatalf("seed %d: %q does %s at %d, before entering at %d",
					seed, b.Actor, b.Action, b.T, at)
			}
		}
	}
}

func TestCompose_BeatsWithinDuration(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, b := range s.Beats {
			if b.T > s.DurationMs {
				t.Fatalf("seed %d: beat at %d exceeds duration %d", seed, b.T, s.DurationMs)
			}
		}
	}
}

func newTestTeller(t *testing.T, cooldown time.Duration) *teller {
	t.Helper()
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, cooldown).(*teller)
	return tl
}

func TestNext_NeverEmpty(t *testing.T) {
	tl := newTestTeller(t, time.Minute)
	s := tl.Next()
	if len(s.Cast) == 0 || len(s.Beats) == 0 {
		t.Fatalf("Next returned an unplayable story: %+v", s)
	}
}

// The cooldown is the cost control the user explicitly asked for: repeated
// refreshes must not each trigger a generation.
func TestPrepare_CooldownBlocksRepeats(t *testing.T) {
	tl := newTestTeller(t, time.Hour)
	ctx := context.Background()

	if !tl.Prepare(ctx, "first") {
		t.Fatal("first Prepare should run")
	}
	for i := 0; i < 5; i++ {
		if tl.Prepare(ctx, "refresh") {
			t.Fatalf("Prepare ran again within the cooldown (attempt %d)", i)
		}
	}
}

func TestPrepare_RunsAgainAfterCooldown(t *testing.T) {
	tl := newTestTeller(t, 10*time.Millisecond)
	ctx := context.Background()
	if !tl.Prepare(ctx, "first") {
		t.Fatal("first Prepare should run")
	}
	time.Sleep(20 * time.Millisecond)
	if !tl.Prepare(ctx, "second") {
		t.Fatal("Prepare should run once the cooldown has elapsed")
	}
}

func TestPrepare_PersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, time.Hour).(*teller)
	if !tl.Prepare(context.Background(), "test") {
		t.Fatal("Prepare should run")
	}
	want := tl.Next()

	path := filepath.Join(dir, "intro_story.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("story not written to disk: %v", err)
	}

	// A fresh teller over the same cache dir must pick the story back up.
	reloaded := New(models.Configurations{}, dir, time.Hour).(*teller)
	got := reloaded.Next()
	if got.ID != want.ID {
		t.Errorf("reloaded story ID = %q, want %q", got.ID, want.ID)
	}
}

// A corrupt or hostile cache file must not reach the player.
func TestLoadFromDisk_RejectsInvalidCache(t *testing.T) {
	dir := t.TempDir()
	bad := model.Story{
		ID:    "../../escape",
		Title: "nope",
		Cast:  []model.Cast{{ID: "x", Character: "dragon"}},
	}
	b, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "intro_story.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	tl := New(models.Configurations{}, dir, time.Hour).(*teller)
	s := tl.Next()
	if s.ID == "../../escape" {
		t.Fatal("invalid cached story was served")
	}
	if len(s.Cast) == 0 {
		t.Fatal("expected a composed fallback story")
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"bare", `{"a":1}`, `{"a":1}`},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"prose around", `Sure! {"a":1} Hope that helps.`, `{"a":1}`},
		{"nested", `{"a":{"b":2}}`, `{"a":{"b":2}}`},
		{"brace in string", `{"a":"}"}`, `{"a":"}"}`},
		{"escaped quote", `{"a":"\""}`, `{"a":"\""}`},
		{"none", `no json here`, ``},
	}
	for _, c := range cases {
		if got := extractJSON(c.in); got != c.want {
			t.Errorf("%s: extractJSON(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestSceneNames_NonEmpty(t *testing.T) {
	if len(SceneNames()) == 0 {
		t.Fatal("expected composer scenes")
	}
}

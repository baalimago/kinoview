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
	for seed := range int64(400) {
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
	for seed := range int64(200) {
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
			// Scene beats dress the set and have no actor to enter.
			if b.Actor == "" {
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
	for seed := range int64(200) {
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
	for i := range 5 {
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

// The cooldown must survive a restart. lastGen is in-memory, so a fresh teller
// over a recently-written cache must NOT immediately regenerate — otherwise a
// crash-loop costs one LLM call per restart.
func TestCooldown_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first := New(models.Configurations{}, dir, time.Hour).(*teller)
	if !first.Prepare(context.Background(), "initial") {
		t.Fatal("first Prepare should run")
	}

	restarted := New(models.Configurations{}, dir, time.Hour).(*teller)
	if restarted.Prepare(context.Background(), "after restart") {
		t.Error("a restart reset the cooldown; generation ran again immediately")
	}
}

// An old cache is fair game again.
func TestCooldown_ExpiredCacheAllowsRegeneration(t *testing.T) {
	dir := t.TempDir()
	seed := New(models.Configurations{}, dir, time.Hour).(*teller)
	if !seed.Prepare(context.Background(), "initial") {
		t.Fatal("first Prepare should run")
	}
	// Backdate the cache well beyond the cooldown.
	path := filepath.Join(dir, "intro_story.json")
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	restarted := New(models.Configurations{}, dir, time.Hour).(*teller)
	if !restarted.Prepare(context.Background(), "stale cache") {
		t.Error("expected regeneration once the cached story aged past the cooldown")
	}
}

// Warm is what stops the very first visitor getting a composer story while the
// configured LLM sits idle.
func TestWarm_PreparesWhenNothingCached(t *testing.T) {
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, time.Hour).(*teller)

	tl.Warm(context.Background())

	path := filepath.Join(dir, "intro_story.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Warm did not prepare a story")
}

// ...but it must not burn a generation when a good story is already cached.
func TestWarm_NoopWhenCached(t *testing.T) {
	dir := t.TempDir()
	seed := New(models.Configurations{}, dir, time.Hour).(*teller)
	if !seed.Prepare(context.Background(), "initial") {
		t.Fatal("seed Prepare should run")
	}
	want := seed.Next().ID

	restarted := New(models.Configurations{}, dir, time.Hour).(*teller)
	restarted.Warm(context.Background())
	time.Sleep(150 * time.Millisecond)

	if got := restarted.Next().ID; got != want {
		t.Errorf("Warm regenerated over a cached story: got %q, want %q", got, want)
	}
}

// The composer must actually use the set it dresses, otherwise the cell system
// is dead weight: every scene needs a backdrop and pieces standing on it.
func TestCompose_DressesTheSet(t *testing.T) {
	withCells := 0
	for seed := range int64(120) {
		s := Compose(rand.New(rand.NewSource(seed)))
		if s.Scene.Backdrop == "" {
			t.Fatalf("seed %d: no backdrop", seed)
		}
		if !model.ValidBackdrops[s.Scene.Backdrop] {
			t.Fatalf("seed %d: invalid backdrop %q", seed, s.Scene.Backdrop)
		}
		if len(s.Scene.Cells) > 0 {
			withCells++
		}
		for _, c := range s.Scene.Cells {
			if !model.ValidRows[c.Row] {
				t.Errorf("seed %d: cell %q has bad row %q", seed, c.ID, c.Row)
			}
			if c.Piece != "" && !model.ValidPieces[c.Piece] {
				t.Errorf("seed %d: cell %q has bad piece %q", seed, c.ID, c.Piece)
			}
		}
	}
	if withCells == 0 {
		t.Error("no composed scene dressed the set with cells")
	}
}

// Scene beats must address a cell that exists, or the swap silently does nothing.
func TestCompose_SceneBeatsAddressRealCells(t *testing.T) {
	sawSceneBeat := false
	for seed := range int64(200) {
		s := Compose(rand.New(rand.NewSource(seed)))
		cells := map[string]bool{}
		for _, c := range s.Scene.Cells {
			cells[c.ID] = true
		}
		for _, b := range s.Beats {
			switch b.Action {
			case "setCell":
				sawSceneBeat = true
				if !cells[b.Target] {
					t.Fatalf("seed %d: setCell targets unknown cell %q", seed, b.Target)
				}
				if b.Actor != "" {
					t.Errorf("seed %d: scene beat carries actor %q", seed, b.Actor)
				}
			case "setBackdrop":
				sawSceneBeat = true
				if !model.ValidBackdrops[b.Piece] {
					t.Fatalf("seed %d: setBackdrop to %q", seed, b.Piece)
				}
			}
		}
	}
	if !sawSceneBeat {
		t.Error("no composed scene ever changed the set mid-play")
	}
}

// TestDumpStories writes composed stories to disk for visual inspection.
// Skipped unless DUMP_STORIES is set.
func TestDumpStories(t *testing.T) {
	dir := os.Getenv("DUMP_STORIES")
	if dir == "" {
		t.Skip("set DUMP_STORIES=<dir> to dump")
	}
	theme := os.Getenv("DUMP_THEME")
	for i, seed := range []int64{3, 7, 11, 19, 23} {
		s := ComposeThemed(rand.New(rand.NewSource(seed)), theme)
		b, err := json.MarshalIndent(s, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/story_"+string(rune('a'+i))+".json", b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s -> %q [%s] cells=%d beats=%d", string(rune('a'+i)), s.Title, s.Scene.Backdrop, len(s.Scene.Cells), len(s.Beats))
	}
}

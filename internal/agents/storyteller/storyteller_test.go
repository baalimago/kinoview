package storyteller

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// writeCachedStory puts a story of a given origin on disk, as a previous run
// would have left it.
func writeCachedStory(t *testing.T, dir, origin string, age time.Duration) string {
	t.Helper()
	s := ComposeThemed(newRand(9), "")
	s.Origin = origin
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "intro_story.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// The cooldown must survive a restart for LLM stories: lastGen is in-memory, so
// without the mtime fallback a crash-loop would cost one API call per restart.
func TestCooldown_SurvivesRestartForLLMStories(t *testing.T) {
	dir := t.TempDir()
	writeCachedStory(t, dir, "llm", 0)

	restarted := New(models.Configurations{}, dir, time.Hour).(*teller)
	if restarted.Prepare(context.Background(), "after restart") {
		t.Error("a restart reset the cooldown; generation ran again immediately")
	}
}

// An old cache is fair game again.
func TestCooldown_ExpiredCacheAllowsRegeneration(t *testing.T) {
	dir := t.TempDir()
	writeCachedStory(t, dir, "llm", 3*time.Hour)

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
	for i, seed := range []int64{3, 7, 11, 19, 23, 31} {
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

// A story must be on disk by the time Warm returns — not eventually. The whole
// point is that no visitor can arrive before one is prepared.
func TestWarm_StoresSynchronously(t *testing.T) {
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, time.Hour).(*teller)

	tl.Warm(context.Background())

	// No polling, no sleeping: it is either there now or the guarantee is broken.
	if _, err := os.Stat(filepath.Join(dir, "intro_story.json")); err != nil {
		t.Fatalf("no story on disk when Warm returned: %v", err)
	}
	if s := tl.Next(); len(s.Beats) == 0 {
		t.Error("Next returned an unplayable story after Warm")
	}
}

// The seeded story must be themed on the most recently viewed item.
func TestWarm_SeedsFromLastViewed(t *testing.T) {
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, time.Hour,
		WithMuse(MuseFunc(func() string { return "Solaris 1972" }))).(*teller)

	tl.Warm(context.Background())

	s := tl.Next()
	if s.Theme != "Solaris 1972" {
		t.Errorf("seeded story theme = %q, want %q", s.Theme, "Solaris 1972")
	}
	if !strings.Contains(s.Title, "Solaris 1972") {
		t.Errorf("seeded title %q does not mention the last watched item", s.Title)
	}

	// And it must be the persisted copy that carries the theme, not just memory.
	b, err := os.ReadFile(filepath.Join(dir, "intro_story.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk model.Story
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Theme != "Solaris 1972" {
		t.Errorf("persisted theme = %q, want %q", onDisk.Theme, "Solaris 1972")
	}
}

// A composed story on disk must NOT hold the cooldown shut: the cooldown limits
// API spend, and a composed story cost nothing. Otherwise the seed Warm writes
// would block the very LLM upgrade it stands in for.
func TestCooldown_ComposerStoryDoesNotGateLLM(t *testing.T) {
	dir := t.TempDir()
	seeder := New(models.Configurations{}, dir, time.Hour).(*teller)
	seeder.Warm(context.Background())

	reloaded := New(models.Configurations{}, dir, time.Hour).(*teller)
	if !reloaded.lastGen.IsZero() {
		t.Errorf("a composer story started the cooldown (lastGen=%v)", reloaded.lastGen)
	}
	if !reloaded.Prepare(context.Background(), "upgrade") {
		t.Error("Prepare was blocked by a cooldown that a composed story should not have started")
	}
}

// An LLM story, on the other hand, must hold it.
func TestCooldown_LLMStoryGatesRegeneration(t *testing.T) {
	dir := t.TempDir()
	writeCachedStory(t, dir, "llm", 0)

	tl := New(models.Configurations{}, dir, time.Hour).(*teller)
	if tl.lastGen.IsZero() {
		t.Error("an llm story did not start the cooldown")
	}
	if tl.Prepare(context.Background(), "too soon") {
		t.Error("Prepare ran despite a recent llm generation")
	}
}

// Even the last-resort synchronous compose inside Next must end up on disk, so
// the store stops being empty after the very first visit.
func TestNext_PersistsWhatItInvents(t *testing.T) {
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, time.Hour).(*teller)

	_ = tl.Next() // nothing cached, nothing warmed

	path := filepath.Join(dir, "intro_story.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Next composed a story but never persisted it")
}

// Concurrent writers must never leave a partially written file behind.
func TestSaveToDisk_IsAtomic(t *testing.T) {
	dir := t.TempDir()
	tl := New(models.Configurations{}, dir, time.Hour).(*teller)
	path := filepath.Join(dir, "intro_story.json")

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tl.saveToDisk(ComposeThemed(newRand(int64(n)), ""))
		}(i)
	}
	// Read continuously while the writers churn; every observable state must
	// parse and validate.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			b, err := os.ReadFile(path)
			if err != nil {
				continue // not created yet; a missing file is fine, a torn one is not
			}
			var s model.Story
			if err := json.Unmarshal(b, &s); err != nil {
				t.Errorf("observed a torn story file: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	<-done
}

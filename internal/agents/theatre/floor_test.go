package theatre

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"os"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

// Every composer template must produce a story that survives validation, for
// any seed. A template regression would otherwise silently degrade to
// minimalStory in production.
func TestCompose_AllScenesValidateAcrossSeeds(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	for seed := range int64(200) {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, b := range s.Beats {
			if b.T > s.DurationMs {
				t.Fatalf("seed %d: beat at %d exceeds duration %d", seed, b.T, s.DurationMs)
			}
		}
	}
}

func TestSceneNames_NonEmpty(t *testing.T) {
	t.Parallel()
	if len(SceneNames()) == 0 {
		t.Fatal("expected composer scenes")
	}
}

// The theme must reach the marquee even with no LLM configured.
func TestComposeThemed_BillsTheTheme(t *testing.T) {
	t.Parallel()
	s := ComposeThemed(newRand(1), "The Godfather 1972")
	if s.Theme != "The Godfather 1972" {
		t.Errorf("theme not recorded: %q", s.Theme)
	}
	if s.Title == "" {
		t.Fatal("empty title")
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("themed story invalid: %v", err)
	}
}

// The composer must actually use the set it dresses, otherwise the cell system
// is dead weight: every scene needs a backdrop and pieces standing on it.
func TestCompose_DressesTheSet(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// The phase-7 templates must actually show up, and the new vocabulary must
// reach the stage: every template is reachable, and the new pieces, props,
// backdrops and actions appear across seeds.
func TestCompose_Phase7VocabularyReachable(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, n := range SceneNames() {
		names[n] = true
	}
	for _, want := range []string{"midnightsnack", "birdwatching", "snowed-in"} {
		if !names[want] {
			t.Fatalf("phase-7 template %q missing from the composer", want)
		}
	}

	pieces := map[string]bool{}
	props := map[string]bool{}
	backdrops := map[string]bool{}
	actions := map[string]bool{}
	for seed := range int64(400) {
		s := Compose(rand.New(rand.NewSource(seed)))
		backdrops[s.Scene.Backdrop] = true
		for _, c := range s.Scene.Cells {
			if c.Piece != "" {
				pieces[c.Piece] = true
			}
		}
		for _, p := range s.Props {
			props[p.Prop] = true
		}
		for _, b := range s.Beats {
			actions[b.Action] = true
		}
	}
	for _, p := range []string{"fireplace", "bookshelf", "door", "log"} {
		if !pieces[p] {
			t.Errorf("piece %q never dressed a composed set", p)
		}
	}
	for _, p := range []string{"ball", "bone", "cushion", "bowl"} {
		if !props[p] {
			t.Errorf("prop %q never appeared in a composed story", p)
		}
	}
	for _, b := range []string{"kitchen", "forest", "rain"} {
		if !backdrops[b] {
			t.Errorf("backdrop %q never picked", b)
		}
	}
	for _, a := range []string{"yawn", "sniff", "jump"} {
		if !actions[a] {
			t.Errorf("action %q never composed", a)
		}
	}
}

// A jump beat must always carry a target — the model drops targetless jumps,
// so the composer must never emit one.
func TestCompose_JumpAlwaysTargeted(t *testing.T) {
	t.Parallel()
	for seed := range int64(400) {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, b := range s.Beats {
			if b.Action == "jump" && b.Target == "" {
				t.Fatalf("seed %d: jump without a target", seed)
			}
		}
	}
}

// The log stands clear of the cast: a composed scene never puts it through a
// performer (the staging rules staging_test.go exists to protect).
func TestCompose_LogNeverThroughABody(t *testing.T) {
	t.Parallel()
	withLog := 0
	for seed := range int64(300) {
		s := Compose(rand.New(rand.NewSource(seed)))
		occ := occupiedCols(planFromCast(s.Cast))
		for _, c := range s.Scene.Cells {
			if c.Piece == "log" {
				withLog++
				if occ[c.Col] {
					t.Fatalf("seed %d: log at column %d stands through a performer", seed, c.Col)
				}
			}
		}
	}
	if withLog == 0 {
		t.Error("the log never appeared in any composed scene")
	}
}

// The birdwatching and snowed-in shapes target their signature pieces when
// those stand: a jump onto the log and a sniff at the door. The targeted
// beats must reference a cell that actually exists.
func TestCompose_Phase7PieceBeatsAddressRealCells(t *testing.T) {
	t.Parallel()
	for seed := range int64(400) {
		s := Compose(rand.New(rand.NewSource(seed)))
		cells := map[string]bool{}
		for _, c := range s.Scene.Cells {
			cells[c.ID] = true
		}
		for _, b := range s.Beats {
			if b.Action == "jump" && b.Target != "" && !cells[b.Target] {
				// A jump may target a prop or a cast member too.
				if !targetKnown(s, b.Target) {
					t.Fatalf("seed %d: jump targets unknown %q", seed, b.Target)
				}
			}
			if b.Action == "sniff" && b.Target != "" && !cells[b.Target] && !targetKnown(s, b.Target) {
				t.Fatalf("seed %d: sniff targets unknown %q", seed, b.Target)
			}
		}
	}
}

// The phase-8 bird: the birdvisit template is reachable, its bird is always
// the canonical entry (pip / chaffinch), and the bird's beats stay inside the
// closed vocabulary. The 400-seed sweep in
// TestCompose_AllScenesValidateAcrossSeeds covers the template's validity.
func TestCompose_BirdSceneReachable(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, n := range SceneNames() {
		names[n] = true
	}
	if !names["birdvisit"] {
		t.Fatal("phase-8 template \"birdvisit\" missing from the composer")
	}

	sawBird := false
	for seed := range int64(400) {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, c := range s.Cast {
			if c.Character != "bird" {
				continue
			}
			sawBird = true
			if c.ID != "pip" || c.Coat != "chaffinch" {
				t.Fatalf("seed %d: bird cast = %+v, want pip/chaffinch (the canonical entry)", seed, c)
			}
		}
	}
	if !sawBird {
		t.Error("the bird never appeared in any composed story")
	}
}

// The bird's scene never asks the bird to do something its art cannot: the
// bird only enters, exits, vocalizes, stares and jumps (a hop at the cat —
// the swat-miss tease). No sit, no pounce, no chase.
func TestCompose_BirdBeatsStayInBirdVocabulary(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{"enter": true, "exit": true, "vocalize": true, "stareoff": true, "jump": true}
	for seed := range int64(200) {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, b := range s.Beats {
			if b.Actor != "pip" {
				continue
			}
			if !allowed[b.Action] {
				t.Fatalf("seed %d: bird asked to %s — its art cannot", seed, b.Action)
			}
		}
	}
}

// The frozen pre-migration proof (phase 9): the composer-only path must be
// byte-identical before and after the removal of the old intro-story agent.
// The golden file was captured from the pre-migration floor before the move;
// any drift in the migrated floor fails here, not just moves.
//
// The comparison is on the JSON bytes the player consumes, not the in-memory
// structs: an empty Props slice marshals away and round-trips as nil, so
// struct equality would flag a JSON artifact as a drift.
func TestCompose_SnapshotMatchesFrozenPreMigrationOutput(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("testdata/composer_snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Theme   string        `json:"theme"`
		Seeds   []int64       `json:"seeds"`
		Stories []model.Story `json:"stories"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Seeds) == 0 || len(snap.Seeds) != len(snap.Stories) {
		t.Fatalf("frozen snapshot malformed: %d seeds, %d stories", len(snap.Seeds), len(snap.Stories))
	}

	for i, seed := range snap.Seeds {
		got := ComposeThemed(rand.New(rand.NewSource(seed)), snap.Theme)
		gb, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		wb, err := json.Marshal(snap.Stories[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gb, wb) {
			t.Errorf("seed %d drifted from the frozen pre-migration story:\n got  %s\n want %s",
				seed, gb, wb)
		}
	}
}

func targetKnown(s model.Story, id string) bool {
	for _, c := range s.Cast {
		if c.ID == id {
			return true
		}
	}
	for _, p := range s.Props {
		if p.ID == id {
			return true
		}
	}
	return false
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

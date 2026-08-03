package theatre

import (
	"math/rand"
	"slices"
	"strings"

	"github.com/baalimago/kinoview/internal/model"
)

// Compose builds a story without any LLM involvement.
//
// This is the floor under the whole feature: the splash must never depend on an
// API key being present or a network call succeeding, so every failure path ends
// here. It recombines hand-authored scene templates with randomised casting,
// marks, coats and timings, which gives plenty of variety on its own.
func Compose(r *rand.Rand) model.Story { return ComposeThemed(r, "") }

// ComposeThemed is Compose with a subject to riff on — normally the title of
// the last thing watched. The composer cannot invent new choreography for it,
// so it bills the existing scene under the theme instead: "Ina & Freija in:
// <whatever you just watched>". That is enough to make the splash feel like it
// noticed, without an LLM.
func ComposeThemed(r *rand.Rand, theme string) model.Story {
	scene := scenes[r.Intn(len(scenes))]
	s := scene.build(r)
	s.ID = newID(r)
	s.Origin = "composer"
	if theme != "" {
		s.Theme = theme
		s.Title = billing(r, theme)
	}
	if s.DurationMs == 0 {
		s.DurationMs = 4000
	}
	// The composer is trusted, but running the same gate keeps the two
	// producers honest about the same limits.
	if err := s.Validate(); err != nil {
		// A template that cannot validate is a programming error; fall back to
		// the simplest possible scene rather than shipping nothing.
		return minimalStory(r)
	}
	return s
}

const (
	ina    = "ina"    // the cat
	freija = "freija" // the dog
	mouse  = "mouse1" // the mouse
	pip    = "pip"    // the bird (phase 8 — registry entry, canonical chaffinch)
)

var catCoats = []string{"ginger", "grey", "cream", "tuxedo", "char", "siamese"}

var dogCoats = []string{"tan", "cocoa", "cloud", "slate"}

func pick(r *rand.Rand, ss []string) string { return ss[r.Intn(len(ss))] }

func jitter(r *rand.Rand, base, spread int) int {
	return base + r.Intn(spread*2+1) - spread
}

func catCast(r *rand.Rand, p plan) model.Cast {
	return model.Cast{
		ID: ina, Character: "cat", Coat: pick(r, catCoats),
		Lane: p.laneOf(ina), X: p.markOf(ina), Scale: 0.95 + r.Float64()*0.2,
	}
}

func dogCast(r *rand.Rand, p plan) model.Cast {
	return model.Cast{
		ID: freija, Character: "dog", Coat: pick(r, dogCoats),
		Lane: p.laneOf(freija), X: p.markOf(freija), Scale: 1.0 + r.Float64()*0.15,
	}
}

// Backdrops that suit each scene, so the set matches the action rather than
// being picked at random against it.
var (
	indoorSets  = []string{"livingroom", "theatre", "kitchen"}
	outdoorSets = []string{"garden", "night", "sunset", "forest", "rain"}
)

func cellAt(id, row string, col int, piece string) model.Cell {
	return model.Cell{ID: id, Row: row, Col: col, Piece: piece}
}

// indoorSet and outdoorSet dress a room or a garden with a little variation, so
// two runs of the same scene template do not look identical.
// skyPieceFor keeps the sky honest: a moon belongs over a night, not over a
// midday garden.
func skyPieceFor(backdrop string, r *rand.Rand) string {
	switch backdrop {
	case "night":
		return "moon"
	case "sunset":
		return pick(r, []string{"moon", "cloud"})
	default:
		return "cloud"
	}
}

// dresser fills the free columns of a plan with pieces, in priority order.
//
// The set has to be dressed AROUND the cast, not to a fixed recipe: marks are
// randomised now, so a hardcoded "scenery lives in columns 0 and 5" rule puts a
// tree straight through whoever happens to be standing there. Pieces are laid
// into whatever columns nobody occupies, outermost first, and any that do not
// fit are simply left out — a sparser set beats a tree growing out of a dog.
type dresser struct {
	row   string
	piece func(r *rand.Rand) string
	// clear means this piece needs a column with nobody standing in it.
	//
	// Whether overlap looks wrong depends on the piece, not on the column: a
	// tree trunk or a lamp rising past an actor reads as depth, and a flat rug
	// under their feet reads as floor. What looks broken is a SHORT piece whose
	// silhouette ends at body height — a bush that appears to grow out of the
	// dog. Only those need a clear column; demanding one for everything left
	// three-character scenes with a bare stage.
	clear bool
}

func dressPlan(backdrop string, r *rand.Rand, p plan, want []dresser) model.Scene {
	free := freeCols(p)
	cells := make([]model.Cell, 0, len(want))
	used := map[string]bool{} // row:col, so two pieces never stack in one slot
	next := 0
	for i, d := range want {
		if d.row == "sky" {
			// The sky is above every head; it can go anywhere.
			cells = append(cells, cellAt(cellID(i), "sky", 2+r.Intn(3), d.piece(r)))
			continue
		}
		// Tall and flat pieces may share a column with a performer; short ones
		// may not. Either way a slot is never used twice.
		cols := free
		if !d.clear {
			cols = append(append([]int{}, free...), otherCols(free)...)
		}
		for off := 0; off < len(cols); off++ {
			col := cols[(next+off)%len(cols)]
			key := d.row + ":" + itoa(col)
			if used[key] {
				continue
			}
			used[key] = true
			cells = append(cells, cellAt(cellID(i), d.row, col, d.piece(r)))
			next = (next + off + 1) % maxInt(1, len(cols))
			break
		}
	}
	return model.Scene{Backdrop: backdrop, Cells: cells}
}

func cellID(i int) string { return "set_" + string(rune('a'+i)) }

// otherCols returns the columns absent from free, innermost last, so a piece
// allowed to overlap still prefers the edges of the stage.
func otherCols(free []int) []int {
	isFree := map[int]bool{}
	for _, c := range free {
		isFree[c] = true
	}
	var out []int
	for _, c := range []int{0, 5, 1, 4, 2, 3} {
		if !isFree[c] {
			out = append(out, c)
		}
	}
	return out
}

func itoa(i int) string { return string(rune('0' + i)) }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fixed(name string) func(*rand.Rand) string {
	return func(*rand.Rand) string { return name }
}

func indoorSet(r *rand.Rand, p plan) model.Scene {
	backdrop := pick(r, indoorSets)
	return dressPlan(backdrop, r, p, indoorDressers(backdrop))
}

func outdoorSet(r *rand.Rand, p plan) model.Scene {
	backdrop := pick(r, outdoorSets)
	return dressPlan(backdrop, r, p, outdoorDressers(backdrop))
}

// kitchenSet dresses the kitchen specifically, so the midnight raid always
// happens in the room the template's shape calls for.
func kitchenSet(r *rand.Rand, p plan) model.Scene {
	return dressPlan("kitchen", r, p, indoorDressers("kitchen"))
}

// indoorDressers lists the pieces a room gets, in the order dressPlan lays
// them. Extracted from indoorSet so the scenographer's deterministic floor
// (DressDraft) can dress a room around an existing cast. Each room dresses
// differently so the phase-7 pieces actually appear: a kitchen gets the door
// and the bookshelf, a living room the hearth (whose fire can go out
// mid-play), the theatre keeps its lamp and curtains.
func indoorDressers(backdrop string) []dresser {
	switch backdrop {
	case "kitchen":
		return []dresser{
			{row: "far", piece: fixed("door")},      // tall — a way out
			{row: "far", piece: fixed("bookshelf")}, // tall, on the back wall
			{row: "mid", piece: fixed("fireplace")}, // tall hearth
			{row: "near", piece: fixed("rug")},      // flat, underfoot
		}
	case "theatre":
		return []dresser{
			{row: "far", piece: fixed("window")},              // tall, on the back wall
			{row: "mid", piece: fixed("lamp")},                // tall, rises past a body
			{row: "far", piece: fixed("sofa"), clear: true},   // low
			{row: "near", piece: fixed("plant"), clear: true}, // low, foreground
			{row: "near", piece: fixed("rug")},                // flat, underfoot
		}
	default: // livingroom
		return []dresser{
			{row: "far", piece: fixed("window")},              // tall, on the back wall
			{row: "mid", piece: fixed("fireplace")},           // tall hearth
			{row: "far", piece: fixed("sofa"), clear: true},   // low
			{row: "near", piece: fixed("plant"), clear: true}, // low, foreground
			{row: "near", piece: fixed("rug")},                // flat, underfoot
		}
	}
}

// outdoorDressers lists the pieces an outdoor set gets. The sky piece depends
// on the backdrop (a moon belongs over a night), so the backdrop is a
// parameter. A forest leads with the log — it is the point of a forest floor —
// and a rainy set with the door, the shelter the cast reacts to.
func outdoorDressers(backdrop string) []dresser {
	switch backdrop {
	case "forest":
		return []dresser{
			{row: "near", piece: fixed("log"), clear: true}, // low — the forest floor
			{row: "far", piece: fixed("tree")},              // tall
			{row: "far", piece: fixed("tree")},              // tall
			{row: "sky", piece: fixed("cloud")},
			{row: "mid", piece: fixed("bush"), clear: true}, // low
		}
	case "rain":
		return []dresser{
			{row: "far", piece: fixed("door")},               // tall — the shelter's door
			{row: "far", piece: fixed("tree")},               // tall
			{row: "sky", piece: fixed("cloud")},              // the storm sky
			{row: "mid", piece: fixed("fence"), clear: true}, // low
			{row: "mid", piece: fixed("log"), clear: true},   // low
		}
	default: // garden, night, sunset
		return []dresser{
			{row: "far", piece: fixed("tree")}, // tall
			{row: "far", piece: fixed("tree")}, // tall
			{row: "sky", piece: func(rr *rand.Rand) string { return skyPieceFor(backdrop, rr) }},
			{row: "mid", piece: fixed("bush"), clear: true},  // low
			{row: "mid", piece: fixed("fence"), clear: true}, // low
			{row: "near", piece: fixed("log"), clear: true},  // low
		}
	}
}

// DressDraft is the deterministic scenographer floor (phase 5): it dresses a
// draft's set around wherever the playwright put the cast — the draft's
// backdrop is kept when valid, and pieces are laid into the columns nobody
// occupies (the staging rules from staging_test.go). Migrated into the
// theatre in phase 9; the scenographer fallback uses it.
func DressDraft(r *rand.Rand, s model.Story) model.Scene {
	backdrop := s.Scene.Backdrop
	if !model.ValidBackdrops[backdrop] {
		backdrop = model.DefaultBackdrop
	}
	p := planFromCast(s.Cast)
	if indoorBackdrop(backdrop) {
		return dressPlan(backdrop, r, p, indoorDressers(backdrop))
	}
	return dressPlan(backdrop, r, p, outdoorDressers(backdrop))
}

// cellWithPiece returns the id of the first cell holding a piece, or "" when
// the dressing left it out (a clear piece with no free column). Templates that
// need to target a specific piece — the log, the door — look it up this way
// and skip the targeted beats when it is not standing.
func cellWithPiece(sc model.Scene, piece string) string {
	for _, c := range sc.Cells {
		if c.Piece == piece {
			return c.ID
		}
	}
	return ""
}

// indoorBackdrop reports whether a backdrop belongs to the indoor sets.
func indoorBackdrop(backdrop string) bool {
	return slices.Contains(indoorSets, backdrop)
}

func mouseCast(p plan) model.Cast {
	return model.Cast{
		ID: mouse, Character: "mouse",
		// Species size lives in the frontend character registry; the story
		// scale only nudges it.
		Lane: p.laneOf(mouse), X: p.markOf(mouse), Scale: 1,
	}
}

// birdCast casts the permanent bird (phase 8): the registry's canonical
// look, like ina/ginger — pin_identity stamps it regardless.
func birdCast(p plan) model.Cast {
	return model.Cast{
		ID: pip, Character: "bird", Coat: "chaffinch",
		Lane: p.laneOf(pip), X: p.markOf(pip), Scale: 1,
	}
}

// enter builds an entrance using the staged side, so the same template can send
// a character on from either wing.
func enter(p plan, id string, t, ms int) model.Beat {
	return model.Beat{T: t, Actor: id, Action: "enter", From: p.sideOf(id), Ms: ms}
}

// leave sends a character off the nearest side, which is not necessarily the
// one they arrived from.
func leave(r *rand.Rand, p plan, id string, t, ms int) model.Beat {
	side := "left"
	if p.markOf(id) > 0.5 {
		side = "right"
	}
	if r.Intn(4) == 0 {
		// Occasionally bolt the "wrong" way, straight past the other characters.
		if side == "left" {
			side = "right"
		} else {
			side = "left"
		}
	}
	return model.Beat{T: t, Actor: id, Action: "exit", From: side, Ms: ms}
}

type scene struct {
	name  string
	build func(r *rand.Rand) model.Story
}

// Each scene is a three-act piece over ~9.5s: a setup, a complication and a
// resolution, with deliberate stillness between actions.
//
// Templates describe the SHAPE of a scene, never the staging. Marks, entry
// sides and lanes come from stage()/solo(), so the same shape plays out
// differently every run instead of six templates all looking like "Ina from the
// left, Freija from the right, meet in the middle".
var scenes = []scene{
	{"mousehunt", func(r *rand.Rand) model.Story {
		p := stage(r, ina, freija, mouse)
		return model.Story{
			Title:      pick(r, []string{"The Great Mouse Hunt", "A Mouse Appears", "The Pursuit"}),
			Scene:      outdoorSet(r, p),
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p), mouseCast(p)},
			Beats: []model.Beat{
				enter(p, ina, 0, jitter(r, 1500, 150)),
				{T: jitter(r, 1700, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 2350, 150), Actor: ina, Action: "sit", Ms: 1400},
				{T: jitter(r, 3000, 150), Actor: ina, Action: "blink"},
				enter(p, mouse, jitter(r, 3700, 150), 1200),
				{T: jitter(r, 4900, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 5200, 120), Actor: ina, Action: "stareoff", Target: mouse, Ms: 900},
				enter(p, freija, jitter(r, 5900, 150), 1200),
				{T: jitter(r, 6600, 120), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 7100, 120), Actor: ina, Action: "pounce", Target: mouse},
				leave(r, p, mouse, jitter(r, 7450, 120), 900),
				{T: jitter(r, 7700, 120), Actor: ina, Action: "chase", Target: mouse, Ms: 1100},
				{T: jitter(r, 7950, 120), Actor: freija, Action: "chase", Target: mouse, Ms: 1100},
				{T: jitter(r, 8200, 100), Actor: freija, Action: "vocalize"},
			},
		}
	}},
	{"greeting", func(r *rand.Rand) model.Story {
		// Either animal can be the one who arrives first and takes the lead.
		lead, foil := ina, freija
		if r.Intn(2) == 0 {
			lead, foil = freija, ina
		}
		p := stage(r, lead, foil, "")
		return model.Story{
			Title:      pick(r, []string{"Ina & Freija Say Hello", "An Introduction", "Old Friends"}),
			Scene:      outdoorSet(r, p),
			DurationMs: 9200,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p)},
			// A peace offering between the two marks: whichever of them notices
			// it first investigates before the greeting.
			Props: []model.Prop{{ID: "bone1", Prop: "bone", Lane: 1, X: clamp01((p.markOf(lead) + p.markOf(foil)) / 2)}},
			Beats: []model.Beat{
				enter(p, lead, 0, jitter(r, 1600, 150)),
				enter(p, foil, jitter(r, 700, 200), jitter(r, 1700, 150)),
				{T: jitter(r, 2400, 150), Actor: lead, Action: "stareoff", Target: foil, Ms: 800},
				{T: jitter(r, 3300, 150), Actor: lead, Action: "greet", Target: foil, Ms: 600},
				{T: jitter(r, 3900, 120), Actor: lead, Action: "vocalize"},
				{T: jitter(r, 4250, 150), Actor: lead, Action: "sniff", Target: "bone1", Ms: 800},
				{T: jitter(r, 4600, 120), Actor: foil, Action: "vocalize"},
				{T: jitter(r, 5300, 150), Actor: foil, Action: "greet", Target: lead, Ms: 600},
				{T: jitter(r, 6100, 150), Actor: lead, Action: "stretch"},
				{T: jitter(r, 6900, 150), Actor: foil, Action: "sit", Ms: 2000},
				{T: jitter(r, 7500, 150), Actor: lead, Action: "sit", Ms: 1600},
				{T: jitter(r, 8300, 150), Actor: lead, Action: "blink"},
				{T: jitter(r, 8700, 120), Actor: foil, Action: "vocalize"},
			},
		}
	}},
	{"yarn", func(r *rand.Rand) model.Story {
		p := stage(r, ina, freija, "")
		return model.Story{
			Title:      pick(r, []string{"Ina and the Yarn", "A Ball of Yarn", "Batting Practice"}),
			Scene:      indoorSet(r, p),
			DurationMs: 9400,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p)},
			// The yarn lands wherever Ina ends up, not always mid-stage.
			Props: []model.Prop{{ID: "yarn1", Prop: "yarn", Lane: 0, X: clamp01(p.markOf(ina) + 0.10)}},
			Beats: []model.Beat{
				enter(p, ina, 0, jitter(r, 1600, 150)),
				enter(p, freija, jitter(r, 900, 250), 1500),
				{T: jitter(r, 2300, 150), Actor: ina, Action: "stareoff", Target: "yarn1", Ms: 700},
				{T: jitter(r, 3100, 120), Actor: ina, Action: "bat", Target: "yarn1"},
				{T: jitter(r, 3600, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 4300, 150), Actor: freija, Action: "sit", Ms: 2400},
				{T: jitter(r, 4900, 120), Actor: ina, Action: "bat", Target: "yarn1"},
				{T: jitter(r, 5500, 120), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 6200, 120), Actor: ina, Action: "pounce", Target: "yarn1"},
				{T: jitter(r, 6900, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 7600, 150), Actor: ina, Action: "stretch"},
				{T: jitter(r, 8400, 150), Actor: ina, Action: "nap", Ms: 1100},
				{T: jitter(r, 8600, 120), Action: "setCell", Target: "set_b", Piece: ""},
			},
		}
	}},
	{"boxnap", func(r *rand.Rand) model.Story {
		// Whoever gets there first claims the box; the other one objects.
		claimer, objector := ina, freija
		if r.Intn(3) == 0 {
			claimer, objector = freija, ina
		}
		p := stage(r, claimer, objector, "")
		return model.Story{
			Title:      pick(r, []string{"If I Fits", "The Box", "A Question of Ownership"}),
			Scene:      indoorSet(r, p),
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p)},
			Props:      []model.Prop{{ID: "box1", Prop: "box", Lane: 0, X: p.markOf(claimer)}},
			Beats: []model.Beat{
				enter(p, claimer, 0, jitter(r, 1600, 150)),
				{T: jitter(r, 1900, 150), Actor: claimer, Action: "stareoff", Target: "box1", Ms: 700},
				{T: jitter(r, 2700, 120), Actor: claimer, Action: "vocalize"},
				{T: jitter(r, 3300, 120), Actor: claimer, Action: "bat", Target: "box1"},
				{T: jitter(r, 3900, 150), Actor: claimer, Action: "nap", Ms: 5000},
				enter(p, objector, jitter(r, 4900, 200), 1500),
				{T: jitter(r, 6600, 120), Actor: objector, Action: "stareoff", Target: claimer, Ms: 1100},
				{T: jitter(r, 7300, 120), Actor: objector, Action: "vocalize"},
				{T: jitter(r, 8100, 150), Actor: objector, Action: "sit", Ms: 1200},
				{T: jitter(r, 8800, 120), Actor: objector, Action: "vocalize"},
				{T: jitter(r, 5600, 150), Action: "setCell", Target: "set_c", Piece: "lamp"},
			},
		}
	}},
	{"standoff", func(r *rand.Rand) model.Story {
		p := stage(r, ina, freija, mouse)
		return model.Story{
			Title:      pick(r, []string{"The Standoff", "Who Blinks First", "A Difference of Opinion"}),
			Scene:      outdoorSet(r, p),
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p), mouseCast(p)},
			Beats: []model.Beat{
				enter(p, ina, 0, 1500),
				enter(p, freija, jitter(r, 400, 250), 1600),
				{T: jitter(r, 2200, 150), Actor: ina, Action: "stareoff", Target: freija, Ms: 1600},
				{T: jitter(r, 2300, 150), Actor: freija, Action: "stareoff", Target: ina, Ms: 1600},
				{T: jitter(r, 3200, 120), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 3900, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 4700, 150), Actor: ina, Action: "blink"},
				enter(p, mouse, jitter(r, 5300, 200), 1400),
				{T: jitter(r, 6600, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 7000, 120), Actor: ina, Action: "stareoff", Target: mouse, Ms: 600},
				{T: jitter(r, 7400, 120), Actor: ina, Action: "pounce", Target: mouse},
				leave(r, p, mouse, jitter(r, 7800, 120), 900),
				{T: jitter(r, 8100, 120), Actor: freija, Action: "chase", Target: mouse, Ms: 1100},
				{T: jitter(r, 8500, 100), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 4900, 200), Action: "setBackdrop", Piece: "sunset"},
			},
		}
	}},
	{"crossing", func(r *rand.Rand) model.Story {
		// Two animals cross paths in opposite directions and barely acknowledge
		// each other. A different shape entirely: no meeting in the middle.
		a, b := ina, freija
		if r.Intn(2) == 0 {
			a, b = freija, ina
		}
		p := plan{x: map[string]float64{}, from: map[string]string{}, lane: map[string]int{}}
		p.x[a], p.from[a], p.lane[a] = 0.78, "left", 0
		p.x[b], p.from[b], p.lane[b] = 0.20, "right", 1
		return model.Story{
			Title:      pick(r, []string{"Passing Through", "Two Ships", "Somewhere Else To Be"}),
			Scene:      outdoorSet(r, p),
			DurationMs: 9200,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p)},
			Beats: []model.Beat{
				enter(p, a, 0, jitter(r, 3200, 250)),
				enter(p, b, jitter(r, 900, 250), jitter(r, 3400, 250)),
				{T: jitter(r, 3600, 150), Actor: a, Action: "vocalize"},
				{T: jitter(r, 4300, 150), Actor: b, Action: "stareoff", Target: a, Ms: 700},
				{T: jitter(r, 5200, 150), Actor: b, Action: "vocalize"},
				{T: jitter(r, 6000, 200), Actor: a, Action: "sit", Ms: 1800},
				{T: jitter(r, 6800, 200), Actor: b, Action: "stretch"},
				{T: jitter(r, 7800, 200), Actor: a, Action: "blink"},
				{T: jitter(r, 8400, 150), Actor: b, Action: "sit", Ms: 1000},
			},
		}
	}},
	{"stakeout", func(r *rand.Rand) model.Story {
		// The mouse is out first and the predators arrive to find it already
		// there — the reverse of the hunt, and it ends in a draw.
		p := stage(r, ina, freija, mouse)
		return model.Story{
			Title:      pick(r, []string{"The Stakeout", "Bold of You", "An Uninvited Guest"}),
			Scene:      indoorSet(r, p),
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p), mouseCast(p)},
			Beats: []model.Beat{
				enter(p, mouse, 0, jitter(r, 1300, 200)),
				{T: jitter(r, 1500, 120), Actor: mouse, Action: "vocalize"},
				enter(p, ina, jitter(r, 2200, 250), jitter(r, 1600, 150)),
				{T: jitter(r, 4000, 150), Actor: ina, Action: "stareoff", Target: mouse, Ms: 1400},
				{T: jitter(r, 4600, 120), Actor: ina, Action: "vocalize"},
				enter(p, freija, jitter(r, 5200, 250), 1500),
				{T: jitter(r, 6900, 150), Actor: freija, Action: "stareoff", Target: mouse, Ms: 1200},
				{T: jitter(r, 7400, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 7900, 150), Actor: ina, Action: "sit", Ms: 1500},
				{T: jitter(r, 8300, 150), Actor: freija, Action: "sit", Ms: 1200},
				{T: jitter(r, 8900, 120), Actor: mouse, Action: "vocalize"},
			},
		}
	}},
	{"soloina", func(r *rand.Rand) model.Story {
		// Sometimes it is the dog's night instead.
		who := ina
		if r.Intn(3) == 0 {
			who = freija
		}
		p := solo(r, who)
		var cast []model.Cast
		var title []string
		if who == freija {
			cast = []model.Cast{dogCast(r, p)}
			title = []string{"Freija Arrives", "Good Evening", "Just Freija"}
		} else {
			cast = []model.Cast{catCast(r, p)}
			title = []string{"Ina Arrives", "Good Evening", "Just Ina"}
		}
		// The solo performer brings its own company: a bone for the dog's
		// night, a cushion for the cat's nap.
		target, propName := "bone1", "bone"
		if who == ina {
			target, propName = "cush1", "cushion"
		}
		return model.Story{
			Title:      pick(r, title),
			Scene:      indoorSet(r, p),
			DurationMs: 8200,
			Cast:       cast,
			Props:      []model.Prop{{ID: target, Prop: propName, Lane: 0, X: clamp01(p.markOf(who) + 0.14)}},
			Beats: []model.Beat{
				enter(p, who, 0, jitter(r, 1700, 200)),
				{T: jitter(r, 2100, 150), Actor: who, Action: "vocalize"},
				{T: jitter(r, 2900, 150), Actor: who, Action: "stretch"},
				{T: jitter(r, 3500, 150), Actor: who, Action: "sniff", Target: target, Ms: 800},
				{T: jitter(r, 3800, 150), Actor: who, Action: "blink"},
				{T: jitter(r, 4400, 150), Actor: who, Action: "sit", Ms: 2200},
				{T: jitter(r, 5400, 120), Actor: who, Action: "vocalize"},
				{T: jitter(r, 6300, 150), Actor: who, Action: "blink"},
				{T: jitter(r, 7000, 150), Actor: who, Action: "nap", Ms: 1200},
			},
		}
	}},
	{"midnightsnack", func(r *rand.Rand) model.Story {
		// Ina raids the kitchen at midnight: the bowl draws her, the ball
		// distracts her, the mouse defends the bowl, and the fire goes out
		// as she settles into a victory nap. The new vocabulary in one scene:
		// yawn, sniff, jump, bowl, ball, the kitchen set.
		p := stage(r, ina, mouse, "")
		return model.Story{
			Title:      pick(r, []string{"Midnight Snack", "The Kitchen Raid", "Bowl Watching"}),
			Scene:      kitchenSet(r, p),
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), mouseCast(p)},
			Props: []model.Prop{
				{ID: "bowl1", Prop: "bowl", Lane: 1, X: clamp01(p.markOf(mouse) + 0.04)},
				{ID: "ball1", Prop: "ball", Lane: 0, X: clamp01(p.markOf(ina) + 0.16)},
			},
			Beats: []model.Beat{
				enter(p, ina, 0, jitter(r, 1500, 150)),
				{T: jitter(r, 1700, 150), Actor: ina, Action: "yawn", Ms: 1200},
				{T: jitter(r, 3000, 150), Actor: ina, Action: "sniff", Target: "bowl1", Ms: 900},
				{T: jitter(r, 3900, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 4600, 120), Actor: ina, Action: "bat", Target: "ball1"},
				enter(p, mouse, jitter(r, 5200, 200), 1200),
				{T: jitter(r, 6500, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 7000, 150), Actor: ina, Action: "stareoff", Target: mouse, Ms: 900},
				{T: jitter(r, 7700, 150), Actor: ina, Action: "jump", Target: "ball1", Ms: 700},
				leave(r, p, mouse, jitter(r, 8200, 120), 900),
				{T: jitter(r, 8600, 120), Actor: ina, Action: "sit", Ms: 1100},
				{T: jitter(r, 8900, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 9000, 150), Action: "setCell", Target: "set_c", Piece: ""},
			},
		}
	}},
	{"birdwatching", func(r *rand.Rand) model.Story {
		// The trio gathers on the forest floor and watches the sky — a quiet
		// shape that foreshadows the bird. Ina hops onto the log for a better
		// view when one stands; the jump is the only movement in the piece.
		p := stage(r, ina, freija, mouse)
		scene := dressPlan("forest", r, p, outdoorDressers("forest"))
		beats := []model.Beat{
			enter(p, ina, 0, jitter(r, 1500, 150)),
			{T: jitter(r, 1700, 150), Actor: ina, Action: "yawn", Ms: 1200},
			enter(p, freija, jitter(r, 2700, 200), jitter(r, 1500, 150)),
			{T: jitter(r, 4300, 150), Actor: freija, Action: "sit", Ms: 2600},
			enter(p, mouse, jitter(r, 5000, 200), 1300),
			{T: jitter(r, 6300, 120), Actor: mouse, Action: "vocalize"},
			{T: jitter(r, 6900, 150), Actor: ina, Action: "vocalize"},
			{T: jitter(r, 7600, 150), Actor: freija, Action: "stareoff", Target: ina, Ms: 900},
			{T: jitter(r, 8200, 150), Actor: ina, Action: "sit", Ms: 1300},
			{T: jitter(r, 8700, 120), Actor: mouse, Action: "vocalize"},
		}
		if logID := cellWithPiece(scene, "log"); logID != "" {
			beats = append(beats, model.Beat{T: jitter(r, 5900, 150), Actor: ina, Action: "jump", Target: logID, Ms: 700})
		}
		return model.Story{
			Title:      pick(r, []string{"Birdwatching", "Something in the Sky", "The Skywatchers"}),
			Scene:      scene,
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p), mouseCast(p)},
			Beats:      beats,
		}
	}},
	{"snowed-in", func(r *rand.Rand) model.Story {
		// The intruder shape under a storm: ina and freija shelter by the
		// door, freija smells something out there, the mouse arrives, the dog
		// jumps at it, and the rain clears to the forest as the storm passes.
		p := stage(r, ina, freija, mouse)
		scene := dressPlan("rain", r, p, outdoorDressers("rain"))
		beats := []model.Beat{
			enter(p, ina, 0, jitter(r, 1500, 150)),
			enter(p, freija, jitter(r, 700, 200), jitter(r, 1500, 150)),
			{T: jitter(r, 2300, 150), Actor: ina, Action: "sit", Ms: 1800},
			{T: jitter(r, 3100, 150), Actor: freija, Action: "vocalize"},
			{T: jitter(r, 3900, 150), Actor: ina, Action: "yawn", Ms: 1200},
			enter(p, mouse, jitter(r, 5600, 200), 1300),
			{T: jitter(r, 7000, 150), Actor: mouse, Action: "vocalize"},
			{T: jitter(r, 7500, 150), Actor: ina, Action: "stareoff", Target: mouse, Ms: 900},
			{T: jitter(r, 8000, 150), Actor: freija, Action: "jump", Target: mouse, Ms: 700},
			leave(r, p, mouse, jitter(r, 8500, 120), 900),
			{T: jitter(r, 8800, 120), Actor: freija, Action: "vocalize"},
			{T: jitter(r, 4900, 200), Action: "setBackdrop", Piece: "forest"},
		}
		if doorID := cellWithPiece(scene, "door"); doorID != "" {
			beats = append(beats, model.Beat{T: jitter(r, 3400, 150), Actor: freija, Action: "sniff", Target: doorID, Ms: 900})
		}
		return model.Story{
			Title:      pick(r, []string{"Snowed In", "The Storm", "Someone at the Door"}),
			Scene:      scene,
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), dogCast(r, p), mouseCast(p)},
			Beats:      beats,
		}
	}},
	{"birdvisit", func(r *rand.Rand) model.Story {
		// The bird comes to call (phase 8): ina settles in, pip perches above
		// her, chirps, teases her with a hop that lands just short — a swat
		// that misses — and flies off, leaving her meowing at the sky. The
		// perch is a species trait in the player, so the story only sets the
		// lane and the marks.
		p := stage(r, ina, pip, "")
		backdrop := pick(r, []string{"forest", "garden", "rain"})
		beats := []model.Beat{
			enter(p, ina, 0, jitter(r, 1500, 150)),
			{T: jitter(r, 2000, 150), Actor: ina, Action: "sit", Ms: 1800},
			{T: jitter(r, 2600, 120), Actor: ina, Action: "vocalize"},
			enter(p, pip, jitter(r, 3400, 200), 1300),
			{T: jitter(r, 4600, 120), Actor: pip, Action: "vocalize"},
			{T: jitter(r, 5200, 150), Actor: ina, Action: "stareoff", Target: pip, Ms: 900},
			{T: jitter(r, 6100, 150), Actor: pip, Action: "stareoff", Target: ina, Ms: 800},
			{T: jitter(r, 6800, 150), Actor: pip, Action: "jump", Target: ina, Ms: 700},
			{T: jitter(r, 7500, 120), Actor: pip, Action: "vocalize"},
			leave(r, p, pip, jitter(r, 8100, 150), 1000),
			{T: jitter(r, 8700, 120), Actor: ina, Action: "vocalize"},
		}
		return model.Story{
			Title:      pick(r, []string{"The Visitor", "Pip Calls By", "A Bird in the House"}),
			Scene:      dressPlan(backdrop, r, p, outdoorDressers(backdrop)),
			DurationMs: 9500,
			Cast:       []model.Cast{catCast(r, p), birdCast(p)},
			Beats:      beats,
		}
	}},
}

func minimalStory(r *rand.Rand) model.Story {
	s := model.Story{
		ID:         newID(r),
		Title:      "Just Ina",
		Origin:     "composer",
		DurationMs: 3000,
		Cast:       []model.Cast{{ID: ina, Character: "cat", Lane: 0, X: 0.45, Scale: 1}},
		Beats: []model.Beat{
			{T: 0, Actor: ina, Action: "enter", From: "left", Ms: 1200},
			{T: 1600, Actor: ina, Action: "vocalize"},
		},
	}
	// Ignore the error: this literal is known-good, and there is nothing left to
	// fall back to.
	_ = s.Validate()
	return s
}

// billing puts the theme on the marquee. Kept short so it survives the title
// length cap with the cast names intact.
func billing(r *rand.Rand, theme string) string {
	if len(theme) > 38 {
		theme = strings.TrimSpace(theme[:38])
	}
	return pick(r, []string{
		"Ina & Freija in: " + theme,
		theme + ", Reenacted",
		"Tonight: " + theme,
		"A Tribute to " + theme,
	})
}

// SceneNames lists the composer's templates, for logging and tests.
func SceneNames() []string {
	out := make([]string, 0, len(scenes))
	for _, s := range scenes {
		out = append(out, s.name)
	}
	return out
}

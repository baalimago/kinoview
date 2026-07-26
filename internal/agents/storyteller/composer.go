package storyteller

import (
	"math/rand"
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
	mouse  = "mouse1"
)

var catCoats = []string{"ginger", "grey", "cream", "tuxedo", "char", "siamese"}

var dogCoats = []string{"tan", "cocoa", "cloud", "slate"}

func pick(r *rand.Rand, ss []string) string { return ss[r.Intn(len(ss))] }

func jitter(r *rand.Rand, base, spread int) int {
	return base + r.Intn(spread*2+1) - spread
}

func catCast(r *rand.Rand, x float64) model.Cast {
	return model.Cast{
		ID: ina, Character: "cat", Coat: pick(r, catCoats),
		Lane: 0, X: x, Scale: 0.95 + r.Float64()*0.2,
	}
}

func dogCast(r *rand.Rand, x float64) model.Cast {
	return model.Cast{
		ID: freija, Character: "dog", Coat: pick(r, dogCoats),
		Lane: 0, X: x, Scale: 1.0 + r.Float64()*0.15,
	}
}

// Backdrops that suit each scene, so the set matches the action rather than
// being picked at random against it.
var (
	indoorSets  = []string{"livingroom", "theatre"}
	outdoorSets = []string{"garden", "night", "sunset"}
	anySets     = []string{"night", "livingroom", "garden", "theatre", "sunset"}
)

func setOf(r *rand.Rand, choices []string) model.Scene {
	return model.Scene{Backdrop: pick(r, choices)}
}

// dress puts pieces on the set. Cells are addressed so a later setCell beat can
// swap what stands in one part way through the play.
func dress(backdrop string, r *rand.Rand, cells ...model.Cell) model.Scene {
	return model.Scene{Backdrop: backdrop, Cells: cells}
}

func cellAt(id, row string, col int, piece string) model.Cell {
	return model.Cell{ID: id, Row: row, Col: col, Piece: piece}
}

// indoorSet and outdoorSet dress a room or a garden with a little variation, so
// two runs of the same scene template do not look identical.
// The cast performs between x=0.34 and x=0.76, which spans columns 2, 3 and 4.
// Scenery therefore lives in the WINGS — columns 0 and 5 — exactly as it would
// on a real stage. Anything else ends up growing out of somebody's back. The
// sky row is exempt: it sits above every head.
const (
	wingLeft  = 0
	wingRight = 5
)

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

func indoorSet(r *rand.Rand) model.Scene {
	backdrop := pick(r, indoorSets)
	return dress(backdrop, r,
		cellAt("set_a", "far", wingLeft, pick(r, []string{"window", "sofa"})),
		cellAt("set_b", "mid", wingRight, "lamp"),
		// Near row, not far: a plant in FRONT of the lamp reads as depth. Behind
		// it, in the same column, it just grows through the lampshade.
		cellAt("set_c", "near", wingRight, "plant"),
		cellAt("set_d", "near", wingLeft, "rug"),
	)
}

func outdoorSet(r *rand.Rand) model.Scene {
	backdrop := pick(r, outdoorSets)
	return dress(backdrop, r,
		cellAt("set_a", "far", wingLeft, "tree"),
		cellAt("set_b", "far", wingRight, pick(r, []string{"tree", "bush"})),
		cellAt("set_c", "mid", wingLeft, "bush"),
		cellAt("set_d", "sky", 2+r.Intn(3), skyPieceFor(backdrop, r)),
		cellAt("set_e", "mid", wingRight, "fence"),
	)
}

func mouseCast(x float64) model.Cast {
	return model.Cast{ID: mouse, Character: "mouse", Lane: 0, X: x, Scale: 1}
}

type scene struct {
	name  string
	build func(r *rand.Rand) model.Story
}

// Each scene is a three-act piece over ~9.5s: a setup, a complication and a
// resolution, with deliberate stillness between actions. Constant motion for ten
// seconds reads as noise; the pauses are what make it look authored.
var scenes = []scene{
	{"mousehunt", func(r *rand.Rand) model.Story {
		// Act 1 Ina settles. Act 2 a mouse. Act 3 everyone leaves at speed.
		return model.Story{
			Title:      pick(r, []string{"The Great Mouse Hunt", "A Mouse Appears", "The Pursuit"}),
			Scene:      outdoorSet(r),
			DurationMs: 9500,
			Cast: []model.Cast{
				catCast(r, 0.34),
				dogCast(r, 0.68),
				mouseCast(0.52),
			},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1500, 150)},
				{T: jitter(r, 1700, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 2350, 150), Actor: ina, Action: "sit", Ms: 1400},
				{T: jitter(r, 3000, 150), Actor: ina, Action: "blink"},
				// Act 2: something small moves.
				{T: jitter(r, 3700, 150), Actor: mouse, Action: "enter", From: "right", Ms: 1200},
				{T: jitter(r, 4900, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 5200, 120), Actor: ina, Action: "stareoff", Target: mouse, Ms: 900},
				{T: jitter(r, 5900, 150), Actor: freija, Action: "enter", From: "right", Ms: 1200},
				{T: jitter(r, 6600, 120), Actor: freija, Action: "vocalize"},
				// Act 3: the chase.
				{T: jitter(r, 7100, 120), Actor: ina, Action: "pounce", Target: mouse},
				{T: jitter(r, 7450, 120), Actor: mouse, Action: "exit", From: "left", Ms: 900},
				{T: jitter(r, 7700, 120), Actor: ina, Action: "chase", Target: mouse, Ms: 1100},
				{T: jitter(r, 7950, 120), Actor: freija, Action: "chase", Target: mouse, Ms: 1100},
				{T: jitter(r, 8200, 100), Actor: freija, Action: "vocalize"},
			},
		}
	}},
	{"greeting", func(r *rand.Rand) model.Story {
		// Two friends converge, say hello, and settle down together.
		return model.Story{
			Title:      pick(r, []string{"Ina & Freija Say Hello", "An Introduction", "Old Friends"}),
			Scene:      outdoorSet(r),
			DurationMs: 9200,
			Cast: []model.Cast{
				catCast(r, 0.40),
				dogCast(r, 0.60),
			},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1600, 150)},
				{T: jitter(r, 600, 150), Actor: freija, Action: "enter", From: "right", Ms: jitter(r, 1700, 150)},
				{T: jitter(r, 2400, 150), Actor: ina, Action: "stareoff", Target: freija, Ms: 800},
				{T: jitter(r, 3300, 150), Actor: ina, Action: "greet", Target: freija, Ms: 600},
				{T: jitter(r, 3900, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 4600, 120), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 5300, 150), Actor: freija, Action: "greet", Target: ina, Ms: 600},
				{T: jitter(r, 6100, 150), Actor: ina, Action: "stretch"},
				{T: jitter(r, 6900, 150), Actor: freija, Action: "sit", Ms: 2000},
				{T: jitter(r, 7500, 150), Actor: ina, Action: "sit", Ms: 1600},
				{T: jitter(r, 8300, 150), Actor: ina, Action: "blink"},
				{T: jitter(r, 8700, 120), Actor: freija, Action: "vocalize"},
			},
		}
	}},
	{"yarn", func(r *rand.Rand) model.Story {
		// Ina discovers the yarn and will not leave it alone.
		return model.Story{
			Title:      pick(r, []string{"Ina and the Yarn", "A Ball of Yarn", "Batting Practice"}),
			Scene:      indoorSet(r),
			DurationMs: 9400,
			Cast: []model.Cast{
				catCast(r, 0.40),
				dogCast(r, 0.76),
			},
			Props: []model.Prop{{ID: "yarn1", Prop: "yarn", Lane: 0, X: 0.52}},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1600, 150)},
				{T: jitter(r, 900, 150), Actor: freija, Action: "enter", From: "right", Ms: 1500},
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
				// The lamp goes out as she settles: the set reacting to the story.
				{T: jitter(r, 8600, 120), Action: "setCell", Target: "set_b", Piece: ""},
			},
		}
	}},
	{"boxnap", func(r *rand.Rand) model.Story {
		// The cat claims the box. The dog registers a complaint. Nothing changes.
		return model.Story{
			Title:      pick(r, []string{"If I Fits", "The Box", "Ina Claims the Box"}),
			Scene:      indoorSet(r),
			DurationMs: 9500,
			Cast: []model.Cast{
				catCast(r, 0.44),
				dogCast(r, 0.74),
			},
			Props: []model.Prop{{ID: "box1", Prop: "box", Lane: 0, X: 0.44}},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1600, 150)},
				{T: jitter(r, 1900, 150), Actor: ina, Action: "stareoff", Target: "box1", Ms: 700},
				{T: jitter(r, 2700, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 3300, 120), Actor: ina, Action: "bat", Target: "box1"},
				{T: jitter(r, 3900, 150), Actor: ina, Action: "nap", Ms: 5000},
				{T: jitter(r, 4900, 150), Actor: freija, Action: "enter", From: "right", Ms: 1500},
				{T: jitter(r, 6600, 120), Actor: freija, Action: "stareoff", Target: ina, Ms: 1100},
				{T: jitter(r, 7300, 120), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 8100, 150), Actor: freija, Action: "sit", Ms: 1200},
				{T: jitter(r, 8800, 120), Actor: freija, Action: "vocalize"},
				// Someone switches a lamp on over the sleeping cat.
				{T: jitter(r, 5600, 150), Action: "setCell", Target: "set_c", Piece: "lamp"},
			},
		}
	}},
	{"standoff", func(r *rand.Rand) model.Story {
		// A staring contest, settled by a third party wandering through.
		return model.Story{
			Title:      pick(r, []string{"The Standoff", "Who Blinks First", "A Difference of Opinion"}),
			Scene:      outdoorSet(r),
			DurationMs: 9500,
			Cast: []model.Cast{
				catCast(r, 0.36),
				dogCast(r, 0.64),
				mouseCast(0.5),
			},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: 1500},
				{T: jitter(r, 300, 120), Actor: freija, Action: "enter", From: "right", Ms: 1600},
				{T: jitter(r, 2200, 150), Actor: ina, Action: "stareoff", Target: freija, Ms: 1600},
				{T: jitter(r, 2300, 150), Actor: freija, Action: "stareoff", Target: ina, Ms: 1600},
				{T: jitter(r, 3200, 120), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 3900, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 4700, 150), Actor: ina, Action: "blink"},
				// The interruption.
				{T: jitter(r, 5300, 150), Actor: mouse, Action: "enter", From: "left", Ms: 1400},
				{T: jitter(r, 6600, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 7000, 120), Actor: ina, Action: "stareoff", Target: mouse, Ms: 600},
				{T: jitter(r, 7400, 120), Actor: ina, Action: "pounce", Target: mouse},
				{T: jitter(r, 7800, 120), Actor: mouse, Action: "exit", From: "right", Ms: 900},
				{T: jitter(r, 8100, 120), Actor: freija, Action: "chase", Target: mouse, Ms: 1100},
				{T: jitter(r, 8500, 100), Actor: freija, Action: "vocalize"},
				// Dusk falls over the standoff.
				{T: jitter(r, 4900, 200), Action: "setBackdrop", Piece: "sunset"},
			},
		}
	}},
	{"soloina", func(r *rand.Rand) model.Story {
		// The quiet one. Keeps the rotation calm instead of always staging a
		// full production.
		return model.Story{
			Title:      pick(r, []string{"Ina Arrives", "Good Evening", "Just Ina"}),
			Scene:      indoorSet(r),
			DurationMs: 8200,
			Cast:       []model.Cast{catCast(r, 0.46)},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: pick(r, []string{"left", "right"}), Ms: jitter(r, 1700, 200)},
				{T: jitter(r, 2100, 150), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 2900, 150), Actor: ina, Action: "stretch"},
				{T: jitter(r, 3800, 150), Actor: ina, Action: "blink"},
				{T: jitter(r, 4400, 150), Actor: ina, Action: "sit", Ms: 2200},
				{T: jitter(r, 5400, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 6300, 150), Actor: ina, Action: "blink"},
				{T: jitter(r, 7000, 150), Actor: ina, Action: "nap", Ms: 1200},
			},
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

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func newID(r *rand.Rand) string {
	var b strings.Builder
	b.WriteString("stry_")
	for range 8 {
		b.WriteByte(idAlphabet[r.Intn(len(idAlphabet))])
	}
	return b.String()
}

// SceneNames lists the composer's templates, for logging and tests.
func SceneNames() []string {
	out := make([]string, 0, len(scenes))
	for _, s := range scenes {
		out = append(out, s.name)
	}
	return out
}

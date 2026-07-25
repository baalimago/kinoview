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
func Compose(r *rand.Rand) model.Story {
	scene := scenes[r.Intn(len(scenes))]
	s := scene.build(r)
	s.ID = newID(r)
	s.Origin = "composer"
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

func mouseCast(x float64) model.Cast {
	return model.Cast{ID: mouse, Character: "mouse", Lane: 0, X: x, Scale: 1}
}

type scene struct {
	name  string
	build func(r *rand.Rand) model.Story
}

// Each scene is a little three-act shape: someone arrives, something happens,
// it resolves. Titles are picked per scene so the card matches the action.
var scenes = []scene{
	{"mousehunt", func(r *rand.Rand) model.Story {
		// The mouse scurries through; Ina spots it, Freija joins the chase.
		return model.Story{
			Title:      pick(r, []string{"The Great Mouse Hunt", "A Mouse Appears", "The Pursuit"}),
			DurationMs: 4000,
			Cast: []model.Cast{
				catCast(r, 0.34),
				dogCast(r, 0.66),
				mouseCast(0.5),
			},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1100, 150)},
				{T: jitter(r, 500, 100), Actor: mouse, Action: "enter", From: "right", Ms: 700},
				{T: jitter(r, 1150, 100), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 1300, 120), Actor: freija, Action: "enter", From: "right", Ms: 900},
				{T: jitter(r, 2000, 100), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 2350, 120), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 2500, 100), Actor: ina, Action: "pounce", Target: mouse},
				{T: jitter(r, 2700, 100), Actor: mouse, Action: "exit", From: "left", Ms: 700},
				{T: jitter(r, 2900, 100), Actor: ina, Action: "chase", Target: mouse, Ms: 800},
				{T: jitter(r, 3050, 100), Actor: freija, Action: "chase", Target: mouse, Ms: 800},
			},
		}
	}},
	{"greeting", func(r *rand.Rand) model.Story {
		// Cat and dog meet in the middle and say hello.
		return model.Story{
			Title:      pick(r, []string{"Ina & Freija Say Hello", "An Introduction", "Old Friends"}),
			DurationMs: 3800,
			Cast: []model.Cast{
				catCast(r, 0.40),
				dogCast(r, 0.60),
			},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1150, 150)},
				{T: jitter(r, 200, 100), Actor: freija, Action: "enter", From: "right", Ms: jitter(r, 1200, 150)},
				{T: jitter(r, 1500, 100), Actor: ina, Action: "greet", Target: freija},
				{T: jitter(r, 1700, 100), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 2150, 100), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 2600, 150), Actor: freija, Action: "sit"},
				{T: jitter(r, 2800, 150), Actor: ina, Action: "blink"},
			},
		}
	}},
	{"yarn", func(r *rand.Rand) model.Story {
		// Ina finds the yarn. Freija watches, unimpressed.
		return model.Story{
			Title:      pick(r, []string{"Ina and the Yarn", "A Ball of Yarn", "Batting Practice"}),
			DurationMs: 3900,
			Cast: []model.Cast{
				catCast(r, 0.42),
				dogCast(r, 0.74),
			},
			Props: []model.Prop{{ID: "yarn1", Prop: "yarn", Lane: 0, X: 0.52}},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1200, 150)},
				{T: jitter(r, 400, 150), Actor: freija, Action: "enter", From: "right", Ms: 1100},
				{T: jitter(r, 1550, 100), Actor: ina, Action: "bat", Target: "yarn1"},
				{T: jitter(r, 1900, 100), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 2250, 100), Actor: ina, Action: "bat", Target: "yarn1"},
				{T: jitter(r, 2500, 150), Actor: freija, Action: "sit"},
				{T: jitter(r, 3000, 150), Actor: ina, Action: "stretch"},
			},
		}
	}},
	{"boxnap", func(r *rand.Rand) model.Story {
		// The cat claims the box. The dog arrives too late.
		return model.Story{
			Title:      pick(r, []string{"If I Fits", "The Box", "Ina Claims the Box"}),
			DurationMs: 4000,
			Cast: []model.Cast{
				catCast(r, 0.46),
				dogCast(r, 0.72),
			},
			Props: []model.Prop{{ID: "box1", Prop: "box", Lane: 0, X: 0.46}},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: jitter(r, 1150, 150)},
				{T: jitter(r, 1400, 100), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 1750, 150), Actor: ina, Action: "nap"},
				{T: jitter(r, 2100, 150), Actor: freija, Action: "enter", From: "right", Ms: 1000},
				{T: jitter(r, 3150, 100), Actor: freija, Action: "stareoff", Target: ina},
				{T: jitter(r, 3350, 100), Actor: freija, Action: "vocalize"},
			},
		}
	}},
	{"standoff", func(r *rand.Rand) model.Story {
		// A brief staring contest, settled by the mouse walking past.
		return model.Story{
			Title:      pick(r, []string{"The Standoff", "Who Blinks First", "A Difference of Opinion"}),
			DurationMs: 4000,
			Cast: []model.Cast{
				catCast(r, 0.38),
				dogCast(r, 0.62),
				mouseCast(0.5),
			},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: "left", Ms: 1000},
				{T: jitter(r, 150, 80), Actor: freija, Action: "enter", From: "right", Ms: 1050},
				{T: jitter(r, 1300, 100), Actor: ina, Action: "stareoff", Target: freija},
				{T: jitter(r, 1350, 100), Actor: freija, Action: "stareoff", Target: ina},
				{T: jitter(r, 1900, 100), Actor: freija, Action: "vocalize"},
				{T: jitter(r, 2200, 100), Actor: mouse, Action: "enter", From: "left", Ms: 900},
				{T: jitter(r, 2900, 100), Actor: mouse, Action: "vocalize"},
				{T: jitter(r, 3100, 100), Actor: ina, Action: "pounce", Target: mouse},
				{T: jitter(r, 3300, 100), Actor: mouse, Action: "exit", From: "right", Ms: 600},
			},
		}
	}},
	{"soloina", func(r *rand.Rand) model.Story {
		// The quiet one: just the cat, as before. Keeps the rotation calm
		// sometimes instead of always staging a full production.
		return model.Story{
			Title:      pick(r, []string{"Ina Arrives", "Good Evening", "Just Ina"}),
			DurationMs: 3200,
			Cast:       []model.Cast{catCast(r, 0.44)},
			Beats: []model.Beat{
				{T: 0, Actor: ina, Action: "enter", From: pick(r, []string{"left", "right"}), Ms: jitter(r, 1250, 200)},
				{T: jitter(r, 1600, 120), Actor: ina, Action: "vocalize"},
				{T: jitter(r, 2300, 150), Actor: ina, Action: "stretch"},
				{T: jitter(r, 2700, 150), Actor: ina, Action: "blink"},
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

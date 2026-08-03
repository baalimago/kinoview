package theatre

import (
	"math/rand"

	"github.com/baalimago/kinoview/internal/model"
)

// plan is where everybody stands and which side they come on from.
//
// This exists because the first version of the composer hardcoded the staging
// into every template — Ina always walked on from the left, Freija always from
// the right, and they always met in the middle. Six different templates all
// looked like the same scene. Marks, entry sides and lanes are now decided per
// run, independently of what the scene is about.
type plan struct {
	x    map[string]float64
	from map[string]string
	lane map[string]int
}

func (p plan) markOf(id string) float64 { return p.x[id] }
func (p plan) sideOf(id string) string  { return p.from[id] }
func (p plan) laneOf(id string) int     { return p.lane[id] }

// planFromCast recovers a staging plan from a story's cast — the marks and
// lanes the playwright chose — so the deterministic scenographer floor can
// dress around them. Entry sides are unknown from the cast; dressPlan does
// not use them.
func planFromCast(cast []model.Cast) plan {
	p := plan{x: map[string]float64{}, from: map[string]string{}, lane: map[string]int{}}
	for _, c := range cast {
		p.x[c.ID] = c.X
		p.lane[c.ID] = c.Lane
	}
	return p
}

// layouts describe where two leads end up. The interesting part is that a mark
// is chosen independently of the side walked on from: a character whose mark is
// on the far side crosses the whole stage to reach it, which looks completely
// different from a short walk-on even though the beats are identical.
var layouts = []func(r *rand.Rand) (float64, float64){
	// Converge — the classic. Kept, but now only one option among many.
	func(r *rand.Rand) (float64, float64) { return rand2(r, 0.26, 0.36, 0.62, 0.74) },
	// Wide — they stay apart and the stage feels big.
	func(r *rand.Rand) (float64, float64) { return rand2(r, 0.16, 0.24, 0.76, 0.84) },
	// Close — as close as they can stand, resolved into separate lanes below.
	func(r *rand.Rand) (float64, float64) { return rand2(r, 0.34, 0.42, 0.50, 0.58) },
	// Off-centre left — both stage-left, leaving the right of the stage empty.
	func(r *rand.Rand) (float64, float64) { return rand2(r, 0.16, 0.24, 0.48, 0.58) },
	// Off-centre right.
	func(r *rand.Rand) (float64, float64) { return rand2(r, 0.42, 0.52, 0.76, 0.84) },
}

func rand2(r *rand.Rand, aLo, aHi, bLo, bHi float64) (float64, float64) {
	return aLo + r.Float64()*(aHi-aLo), bLo + r.Float64()*(bHi-bLo)
}

// A character occupies roughly a quarter of the stage width at any viewport (the
// art scales with the stage), so marks closer than this overlap outright.
const (
	// Measured by looking: at 0.26 a cat and a dog still touch, because the dog
	// is the wider of the two. 0.32 clears them.
	minGap = 0.32
	// Below this they are close enough that stacking them in separate lanes
	// reads better than standing them side by side.
	nearGap = 0.40
	// Keep everyone off the extreme edges: past this they are half in the wings
	// where the scenery lives, and clipped by the frame.
	stageLo = 0.16
	stageHi = 0.84
)

// separate pushes two marks apart until they no longer overlap, keeping their
// midpoint so the layout's intent (left-heavy, wide, centred) survives.
func separate(a, b float64) (float64, float64) {
	if absf(a-b) >= minGap {
		return a, b
	}
	mid := (a + b) / 2
	lo, hi := mid-minGap/2, mid+minGap/2
	// Nudge back inside the stage if centring pushed us off an edge.
	if lo < stageLo {
		lo, hi = stageLo, stageLo+minGap
	}
	if hi > stageHi {
		lo, hi = stageHi-minGap, stageHi
	}
	if a <= b {
		return lo, hi
	}
	return hi, lo
}

// occupiedCols marks the cell columns the cast stands in, plus their immediate
// neighbours: a body is wider than one sixth of the stage.
func occupiedCols(p plan) map[int]bool {
	occ := map[int]bool{}
	for _, x := range p.x {
		c := int(x * 6)
		for d := -1; d <= 1; d++ {
			if c+d >= 0 && c+d < 6 {
				occ[c+d] = true
			}
		}
	}
	return occ
}

// FreeCols returns the columns no performer is standing in, outermost first —
// scenery reads best at the edges of the stage.
func freeCols(p plan) []int {
	occ := occupiedCols(p)
	order := []int{0, 5, 1, 4, 2, 3}
	var out []int
	for _, c := range order {
		if !occ[c] {
			out = append(out, c)
		}
	}
	return out
}

// stage lays out up to three performers: two leads plus an optional critter.
func stage(r *rand.Rand, lead, foil, critter string) plan {
	p := plan{
		x:    map[string]float64{},
		from: map[string]string{},
		lane: map[string]int{},
	}

	a, b := layouts[r.Intn(len(layouts))](r)
	a, b = separate(a, b)
	// Which lead takes which mark is itself a coin flip, so the same layout
	// reads differently depending on who ends up where.
	if r.Intn(2) == 0 {
		a, b = b, a
	}
	p.x[lead], p.x[foil] = a, b

	for _, id := range []string{lead, foil} {
		p.from[id] = entrySide(r, p.x[id])
		p.lane[id] = 0
	}

	// If they end up close, put the foil a lane back so the overlap reads as one
	// animal standing behind the other rather than two bodies merging into a
	// single creature.
	if absf(p.x[lead]-p.x[foil]) < nearGap {
		p.lane[foil] = 1
	} else if critter == "" && r.Intn(3) == 0 {
		p.lane[foil] = 1
	}

	if critter != "" {
		// The critter takes the space the leads left, not automatically the
		// middle. Whichever gap is biggest is where it turns up.
		p.x[critter] = widestGap(r, p.x[lead], p.x[foil])
		p.from[critter] = entrySide(r, p.x[critter])
		p.lane[critter] = 0
	}
	return p
}

// solo lays out a single performer.
func solo(r *rand.Rand, id string) plan {
	p := plan{x: map[string]float64{}, from: map[string]string{}, lane: map[string]int{}}
	p.x[id] = stageLo + 0.08 + r.Float64()*(stageHi-stageLo-0.16)
	p.from[id] = entrySide(r, p.x[id])
	p.lane[id] = 0
	return p
}

// entrySide picks which wing to walk on from. Mostly the near side (a short,
// natural entrance), but a third of the time the far side, which sends the
// character across the whole stage and changes the shape of the scene.
func entrySide(r *rand.Rand, mark float64) string {
	near := "left"
	if mark > 0.5 {
		near = "right"
	}
	if r.Intn(3) == 0 {
		if near == "left" {
			return "right"
		}
		return "left"
	}
	return near
}

// widestGap returns a mark inside the largest empty stretch of stage.
func widestGap(r *rand.Rand, a, b float64) float64 {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	gaps := [][2]float64{{stageLo, lo}, {lo, hi}, {hi, stageHi}}
	best := gaps[0]
	for _, g := range gaps[1:] {
		if g[1]-g[0] > best[1]-best[0] {
			best = g
		}
	}
	span := best[1] - best[0]
	if span < 0.12 {
		// Nowhere roomy: tuck it just off one of the leads.
		if r.Intn(2) == 0 {
			return clamp01(lo - 0.14)
		}
		return clamp01(hi + 0.14)
	}
	return best[0] + 0.2*span + r.Float64()*0.6*span
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func clamp01(f float64) float64 {
	if f < stageLo {
		return stageLo
	}
	if f > stageHi {
		return stageHi
	}
	return f
}

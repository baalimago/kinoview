package theatre

import (
	"fmt"
	"math/rand"
	"testing"
)

// The original composer hardcoded staging into every template, so six different
// scenes all played as "Ina from the left, Freija from the right, meet in the
// middle". These tests make that a measurable regression rather than a matter
// of opinion.

func TestStaging_EntrySidesVary(t *testing.T) {
	t.Parallel()
	counts := map[string]int{}
	for seed := range int64(300) {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, b := range s.Beats {
			if b.Action == "enter" {
				counts[b.Actor+":"+b.From]++
			}
		}
	}
	// Every principal must be seen entering from BOTH wings.
	for _, id := range []string{ina, freija} {
		l, r := counts[id+":left"], counts[id+":right"]
		if l == 0 || r == 0 {
			t.Errorf("%s only ever enters from one side (left=%d right=%d)", id, l, r)
		}
		// And neither side should dominate absurdly.
		ratio := float64(l) / float64(l+r)
		if ratio < 0.2 || ratio > 0.8 {
			t.Errorf("%s entry sides are lopsided: left=%d right=%d", id, l, r)
		}
	}
}

func TestStaging_MarksVary(t *testing.T) {
	t.Parallel()
	// Bucket each principal's mark; a monotonous composer collapses into one or
	// two buckets.
	buckets := map[string]map[int]bool{ina: {}, freija: {}}
	for seed := range int64(300) {
		s := Compose(rand.New(rand.NewSource(seed)))
		for _, c := range s.Cast {
			if b, ok := buckets[c.ID]; ok {
				b[int(c.X*10)] = true
			}
		}
	}
	for id, b := range buckets {
		if len(b) < 5 {
			t.Errorf("%s only stands in %d distinct positions across 300 runs", id, len(b))
		}
	}
}

// Nobody should be permanently stage-left or stage-right of the other.
func TestStaging_LeadsSwapSides(t *testing.T) {
	t.Parallel()
	catLeft, dogLeft := 0, 0
	for seed := range int64(300) {
		s := Compose(rand.New(rand.NewSource(seed)))
		var catX, dogX float64
		var haveCat, haveDog bool
		for _, c := range s.Cast {
			switch c.ID {
			case ina:
				catX, haveCat = c.X, true
			case freija:
				dogX, haveDog = c.X, true
			}
		}
		if !haveCat || !haveDog {
			continue
		}
		if catX < dogX {
			catLeft++
		} else {
			dogLeft++
		}
	}
	if catLeft == 0 || dogLeft == 0 {
		t.Errorf("the leads never swap sides (cat-left=%d dog-left=%d)", catLeft, dogLeft)
	}
}

// A character whose mark is on the far side crosses the stage; that shape must
// actually occur.
func TestStaging_SomeoneCrossesTheStage(t *testing.T) {
	t.Parallel()
	crossings := 0
	for seed := range int64(300) {
		s := Compose(rand.New(rand.NewSource(seed)))
		marks := map[string]float64{}
		for _, c := range s.Cast {
			marks[c.ID] = c.X
		}
		for _, b := range s.Beats {
			if b.Action != "enter" {
				continue
			}
			x, ok := marks[b.Actor]
			if !ok {
				continue
			}
			if (b.From == "left" && x > 0.6) || (b.From == "right" && x < 0.4) {
				crossings++
			}
		}
	}
	if crossings == 0 {
		t.Error("no character ever crosses the stage to reach its mark")
	}
}

// Distinct scene shapes must all be reachable.
func TestCompose_AllTemplatesReachable(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for seed := range int64(500) {
		s := Compose(rand.New(rand.NewSource(seed)))
		seen[fmt.Sprintf("%d-cast/%d-beats/%v", len(s.Cast), len(s.Beats), s.Scene.Backdrop != "")] = true
	}
	if len(seen) < 4 {
		t.Errorf("only %d distinct scene footprints across 500 runs", len(seen))
	}
}

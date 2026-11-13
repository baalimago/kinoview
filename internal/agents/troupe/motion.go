package troupe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// instanceMotion is one instance's performance: the model, the stage
// placement and the instance's timeline entries in authored order. Two films
// that share an instance with the same motion are the same performance,
// whatever the set behind them.
type instanceMotion struct {
	model   Ref
	scale   float64
	x, y    *float64
	entries []TimelineEntry
}

// motions buckets a play's timeline by instance, in authored order. An
// instance with no entry is not in the map: a backdrop that only stands there
// has no performance to repeat.
func motions(spec PlaySpec) map[string]instanceMotion {
	byInstance := make(map[string]instanceMotion, len(spec.Instances))
	for _, instance := range spec.Instances {
		byInstance[instance.ID] = instanceMotion{
			model: instance.Model,
			scale: instance.Scale,
			x:     instance.X,
			y:     instance.Y,
		}
	}
	for _, entry := range spec.Timeline {
		motion, ok := byInstance[entry.On]
		if !ok {
			continue // a dangling instance is the resolver's refusal, not ours
		}
		motion.entries = append(motion.entries, entry)
		byInstance[entry.On] = motion
	}
	for id, motion := range byInstance {
		if len(motion.entries) == 0 {
			delete(byInstance, id)
		}
	}
	return byInstance
}

// equal reports whether two motions are the same performance: the same model,
// the same placement and the same entries, in the same order. The entries are
// compared through their encoding, which is stable for one type in one order.
func (m instanceMotion) equal(other instanceMotion) bool {
	if m.model != other.model || m.scale != other.scale || !sameCoord(m.x, other.x) || !sameCoord(m.y, other.y) {
		return false
	}
	a, errA := json.Marshal(m.entries)
	b, errB := json.Marshal(other.entries)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(a, b)
}

// sameCoord compares two optional stage coordinates: absent equals absent, and
// absent never equals zero.
func sameCoord(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// frozenInstances names the instances whose motion in next repeats prev entry
// for entry, sorted by id so the refusal reads the same every time. An
// instance only one play has, or one that moved differently, is not a repeat.
func repeatedInstances(prev, next PlaySpec) []string {
	before, after := motions(prev), motions(next)
	var repeated []string
	for id, motion := range after {
		if earlier, ok := before[id]; ok && motion.equal(earlier) {
			repeated = append(repeated, id)
		}
	}
	slices.Sort(repeated)
	return repeated
}

// repeatedMotion names the instances the submitted play repeats from the play
// before it. The troupe's one recorded complaint is a fresh set behind the
// same performance (2026-11-13: four generations of the identical cat chain),
// and the critic must see the repeat as a fact, not infer it from prose. No
// previous submitted play, no repeats.
func repeatedMotion(lib *PlayLibrary, playID string) ([]string, error) {
	prev, ok, err := previousSubmittedPlay(lib, playID)
	if err != nil || !ok {
		return nil, err
	}
	current, err := lib.Get(playID)
	if err != nil {
		return nil, err
	}
	prevSpec, err := decodePlaySpec(prev.Play)
	if err != nil {
		return nil, err
	}
	currentSpec, err := decodePlaySpec(current.Play)
	if err != nil {
		return nil, err
	}
	return repeatedInstances(prevSpec, currentSpec), nil
}

// previousSubmittedPlay returns the submitted play authored immediately before
// id: the next entry of the newest-first index. The play that starts the
// history has no predecessor.
func previousSubmittedPlay(lib *PlayLibrary, playID string) (ResolvedPlay, bool, error) {
	page, err := lib.Page(PlayPageQuery{Limit: maxPlayPageSize})
	if err != nil {
		return ResolvedPlay{}, false, err
	}
	for i, entry := range page.Index {
		if entry.ID != playID {
			continue
		}
		if i+1 >= len(page.Index) {
			return ResolvedPlay{}, false, nil
		}
		prev, err := lib.Get(page.Index[i+1].ID)
		if err != nil {
			return ResolvedPlay{}, false, err
		}
		return prev, true, nil
	}
	return ResolvedPlay{}, false, nil
}

// decodePlaySpec reads one envelope's spec as a play.
func decodePlaySpec(env Envelope) (PlaySpec, error) {
	var spec PlaySpec
	if err := json.Unmarshal(env.Spec, &spec); err != nil {
		return PlaySpec{}, fmt.Errorf("troupe: play %s: decode spec: %w", env.ID, err)
	}
	return spec, nil
}

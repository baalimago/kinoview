package troupe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
)

// Snapshot is one immutable copy of the materialised notebook worktree:
// relative paths ("models/cat@1.json") to file bytes. The resolver performs
// no I/O of its own — the caller hands it the snapshot — so the same
// snapshot always yields the same resolved play or the same exact errors.
type Snapshot map[string][]byte

// ExpansionBudget is the arithmetic expansion budget. The resolver never
// expands; it sanity-checks the declared numbers a play's closure declares,
// because the engine may expand a whole forest from one scatter spec. Each
// cap is a per-verb limit; MaxTotal caps the sum of every structural verb's
// declared size across the closure. A zero value disables the cap it names.
type ExpansionBudget struct {
	MaxCount  int // scatter count per verb
	MaxDepth  int // recurse depth per verb
	MaxBranch int // recurse branch per verb
	MaxTotal  int // declared nodes across the play's closure
}

// DefaultExpansionBudget is the operational guardrail: generous for a small
// gallery stage, cheap for a TV. The engine's floor is webOS TV 4.x (Chromium
// 53); a play declaring tens of thousands of nodes would not render smoothly
// there anyway. Agents never author against it.
func DefaultExpansionBudget() ExpansionBudget {
	return ExpansionBudget{MaxCount: 500, MaxDepth: 6, MaxBranch: 8, MaxTotal: 10_000}
}

// resolver is one ResolvePlay run: the snapshot, the budget and the parsed
// worktree library.
type resolver struct {
	snap   Snapshot
	budget ExpansionBudget
	lib    library
}

// ResolveOption configures one ResolvePlay run.
type ResolveOption func(*resolver)

// WithExpansionBudget overrides the arithmetic expansion budget.
// DefaultExpansionBudget is used when no option is given.
func WithExpansionBudget(b ExpansionBudget) ResolveOption {
	return func(r *resolver) { r.budget = b }
}

// library is the parsed worktree, indexed by id@version within each kind
// (plays by their bare story_<UTC> id, because plays carry no version).
type library struct {
	models map[string]parsed
	voices map[string]parsed
	sounds map[string]parsed
	clips  map[string]parsed
	gags   map[string]parsed
	plays  map[string]parsed
}

// parsed is one validated notebook file: the envelope (spec byte-preserved
// for the served artifact) plus the typed spec the closure walks for refs.
type parsed struct {
	env  Envelope
	spec Spec
}

// add indexes one parsed asset by its identity: id@version for the versioned
// kinds, the bare story id for plays.
func (l *library) add(env Envelope, spec Spec) {
	ref := env.ID
	if env.Version != 0 {
		ref += "@" + strconv.Itoa(env.Version)
	}
	switch s := spec.(type) {
	case *ModelSpec:
		l.models[ref] = parsed{env, s}
	case *ClipSpec:
		l.clips[ref] = parsed{env, s}
	case *VoiceSpec:
		l.voices[ref] = parsed{env, s}
	case *SoundSpec:
		l.sounds[ref] = parsed{env, s}
	case *GagSpec:
		l.gags[ref] = parsed{env, s}
	case *PlaySpec:
		l.plays[ref] = parsed{env, s}
	}
}

// walkDirs are the notebook directories the resolver walks. Six hold grammar
// assets; roles/ and feedback/ hold other note envelopes (phases 4 and 8)
// that the resolver conflict-flags but never parses as assets.
var walkDirs = map[string]bool{
	"models": true, "clips": true, "voices": true, "sounds": true,
	"gags": true, "plays": true, "roles": true, "feedback": true,
}

// playIndex is the plays/ metadata index: bookkeeping for the play API, never
// a grammar asset. The resolver reads play files by their datetime id, never
// this index.
const playIndex = "plays/index.json"

// conflictMarkers are the slivingdoc (git-style) merge conflict markers. A
// shared file carrying them is a merge state, not a document: it is flagged,
// never parsed, and the director must reconcile it before a play serves.
var conflictMarkers = [][]byte{
	[]byte("<<<<<<<"),
	[]byte("======="),
	[]byte(">>>>>>>"),
}

// isResolvedPlay reports whether a plays/ file is an already-submitted
// resolved play — the served artifact {play, assets}, never an authored
// note. Only submit_play writes this shape (phase 6 replaces the authored
// raw play with it at submit time). The walk skips it; the submit gate
// refuses a second submit on it.
func isResolvedPlay(data []byte) bool {
	var probe struct {
		Play json.RawMessage `json:"play"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return len(probe.Play) > 0
}

// conflictMarked reports whether a shared file carries slivingdoc (git-style)
// merge conflict markers.
func conflictMarked(data []byte) bool {
	for _, m := range conflictMarkers {
		if bytes.Contains(data, m) {
			return true
		}
	}
	return false
}

// ResolvePlay validates every asset file in the snapshot, closes the named
// play's cross-file id@version closure transitively, enforces the arithmetic
// expansion budget and emits the resolved play — the play with its
// references intact plus the flattened asset table. The play is resolved by
// its bare story_<UTC> id, never through plays/index.json. A self-referential
// recurse closes bounded by the visited set: each id@version is added once,
// so the closure always terminates. It does not touch anything inside a spec
// — bone ids, slot ids and selectors resolve in the engine at render time.
func ResolvePlay(snap Snapshot, playID string, opts ...ResolveOption) (ResolvedPlay, error) {
	r := &resolver{snap: snap, budget: DefaultExpansionBudget()}
	for _, opt := range opts {
		opt(r)
	}
	lib, err := r.walk()
	if err != nil {
		return ResolvedPlay{}, err
	}
	r.lib = lib

	p, ok := lib.plays[playID]
	if !ok {
		return ResolvedPlay{}, fmt.Errorf("troupe: no such play %s in plays/", playID)
	}

	c := &closure{lib: lib, out: newAssets()}
	if err := c.play(p.spec.(*PlaySpec), "plays/"+playID+".json"); err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: %w", err)
	}
	if err := r.checkBudget(c.out); err != nil {
		return ResolvedPlay{}, err
	}
	return ResolvedPlay{Play: p.env, Assets: c.out}, nil
}

// walk validates the snapshot in deterministic (sorted) order: conflict-
// marked files are flagged, grammar files are parsed against the frozen
// grammar, and roles/, feedback/, plays/index.json and the resolved served
// artifacts a submit history accumulates are walked for conflict markers
// but never parsed as assets.
func (r *resolver) walk() (library, error) {
	lib := library{
		models: map[string]parsed{}, voices: map[string]parsed{}, sounds: map[string]parsed{},
		clips: map[string]parsed{}, gags: map[string]parsed{}, plays: map[string]parsed{},
	}
	for _, name := range slices.Sorted(maps.Keys(r.snap)) {
		dir := filepath.Dir(name)
		if !walkDirs[dir] {
			continue
		}
		data := r.snap[name]
		if conflictMarked(data) {
			return library{}, fmt.Errorf("troupe: %s: conflict-marked file: reconcile before a play serves", name)
		}
		if _, isAsset := kindDirs[dir]; !isAsset || name == playIndex {
			continue
		}
		// A submitted play sits in plays/ as the resolved served artifact
		// {play, assets} — never an authored note. The walk skips it the way
		// it skips index.json: walked for conflict markers, never parsed.
		if dir == "plays" && isResolvedPlay(data) {
			continue
		}
		env, spec, err := Parse(name, data)
		if err != nil {
			return library{}, err
		}
		lib.add(env, spec)
	}
	return lib, nil
}

// ── The cross-file closure ───────────────────────────────────────────────

// closure closes the cross-file id@version closure of one play: typed by
// field (model → models/, voice → voices/, clip → clips/, gag → gags/,
// sound → sounds/), transitively, with a visited set per kind so a
// self-referential recurse closes in one pass.
type closure struct {
	lib library
	out ResolvedAssets
}

// newAssets returns the resolved asset table with every kind present, so the
// served artifact carries {} — never null — for an untouched kind.
func newAssets() ResolvedAssets {
	return ResolvedAssets{
		Models: map[string]Envelope{},
		Voices: map[string]Envelope{},
		Sounds: map[string]Envelope{},
		Clips:  map[string]Envelope{},
		Gags:   map[string]Envelope{},
	}
}

// add inserts one asset envelope into the resolved table, reporting whether
// it was newly closed. A ref already closed is skipped; a ref absent from
// the worktree library is an exact, reference-shaped error the director
// fixes.
func add(table map[string]Envelope, lib map[string]parsed, ref Ref, kind Kind) (parsed, bool, error) {
	key := string(ref)
	if _, ok := table[key]; ok {
		return parsed{}, false, nil
	}
	a, ok := lib[key]
	if !ok {
		return parsed{}, false, fmt.Errorf("no such %s %s in %ss/", kind, ref, kind)
	}
	table[key] = a.env
	return a, true, nil
}

// play closes the play itself: instance model/voice/sound refs and timeline
// clip/gag refs.
func (c *closure) play(ps *PlaySpec, where string) error {
	for i, inst := range ps.Instances {
		w := fmt.Sprintf("%s: instances[%d]", where, i)
		if err := c.model(inst.Model, w+".model"); err != nil {
			return err
		}
		if inst.Voice != nil {
			if err := c.voice(*inst.Voice, w+".voice"); err != nil {
				return err
			}
		}
		if inst.Sound != nil {
			if err := c.sound(*inst.Sound, w+".sound"); err != nil {
				return err
			}
		}
	}
	for i, e := range ps.Timeline {
		w := fmt.Sprintf("%s: timeline[%d]", where, i)
		if e.Clip != nil {
			if err := c.clip(*e.Clip, w+".clip"); err != nil {
				return err
			}
		}
		if e.Gag != nil {
			if err := c.gag(*e.Gag, w+".gag"); err != nil {
				return err
			}
		}
	}
	return nil
}

// model closes one model: its optional voice/sound refs and every structural
// verb's model and tip refs.
func (c *closure) model(ref Ref, where string) error {
	a, added, err := add(c.out.Models, c.lib.models, ref, KindModel)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if !added {
		return nil
	}
	ms := a.spec.(*ModelSpec)
	base := fmt.Sprintf("%s: model %s", where, ref)
	if ms.Voice != nil {
		if err := c.voice(*ms.Voice, base+": spec.voice"); err != nil {
			return err
		}
	}
	if ms.Sound != nil {
		if err := c.sound(*ms.Sound, base+": spec.sound"); err != nil {
			return err
		}
	}
	for i, st := range ms.Structure {
		w := fmt.Sprintf("%s: spec.structure[%d]", base, i)
		if err := c.model(st.Model, w+".model"); err != nil {
			return err
		}
		if st.Tip != nil {
			if err := c.model(*st.Tip, w+".tip"); err != nil {
				return err
			}
		}
	}
	return nil
}

// clip closes one clip: its events' sound refs. A clip references bones only
// (the validator rejects a clip referencing a clip), so this is the whole
// clip dependency set.
func (c *closure) clip(ref Ref, where string) error {
	a, added, err := add(c.out.Clips, c.lib.clips, ref, KindClip)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if !added {
		return nil
	}
	cs := a.spec.(*ClipSpec)
	base := fmt.Sprintf("%s: clip %s", where, ref)
	for i, ev := range cs.Events {
		if ev.Sound != nil {
			if err := c.sound(*ev.Sound, fmt.Sprintf("%s: spec.events[%d].sound", base, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// gag closes one gag: its sequential clips. A gag references clips only
// (never another gag — the validator rejects it).
func (c *closure) gag(ref Ref, where string) error {
	a, added, err := add(c.out.Gags, c.lib.gags, ref, KindGag)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if !added {
		return nil
	}
	gs := a.spec.(*GagSpec)
	base := fmt.Sprintf("%s: gag %s", where, ref)
	for i, cl := range gs.Clips {
		if err := c.clip(cl, fmt.Sprintf("%s: spec.clips[%d]", base, i)); err != nil {
			return err
		}
	}
	return nil
}

// voice closes one voice. A voice spec is pure synth parameters — it carries
// no cross-file refs, so this is the whole voice closure.
func (c *closure) voice(ref Ref, where string) error {
	if _, _, err := add(c.out.Voices, c.lib.voices, ref, KindVoice); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	return nil
}

// sound closes one sound. A sound spec is pure synthesis parameters — it
// carries no cross-file refs, so this is the whole sound closure.
func (c *closure) sound(ref Ref, where string) error {
	if _, _, err := add(c.out.Sounds, c.lib.sounds, ref, KindSound); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	return nil
}

// ── The arithmetic expansion budget ──────────────────────────────────────

// checkBudget enforces the arithmetic expansion budget over the play's
// closure: per-verb caps on the declared scatter count and recurse
// depth/branch, and MaxTotal on the sum of every structural verb's declared
// size. It never expands — the engine expands at render time, and may
// optimise the expansion away.
func (r *resolver) checkBudget(out ResolvedAssets) error {
	total := 0
	for _, ref := range slices.Sorted(maps.Keys(out.Models)) {
		ms := r.lib.models[ref].spec.(*ModelSpec)
		for i, st := range ms.Structure {
			where := fmt.Sprintf("models/%s.json: spec.structure[%d]", ref, i)
			switch st.Type {
			case StructureAttach:
				total++
			case StructureScatter:
				if r.budget.MaxCount > 0 && st.Count > r.budget.MaxCount {
					return fmt.Errorf("troupe: %s: scatter count %d exceeds the %d cap", where, st.Count, r.budget.MaxCount)
				}
				total += st.Count
			case StructureRecurse:
				if r.budget.MaxDepth > 0 && st.Depth > r.budget.MaxDepth {
					return fmt.Errorf("troupe: %s: recurse depth %d exceeds the %d cap", where, st.Depth, r.budget.MaxDepth)
				}
				if r.budget.MaxBranch > 0 && st.Branch > r.budget.MaxBranch {
					return fmt.Errorf("troupe: %s: recurse branch %d exceeds the %d cap", where, st.Branch, r.budget.MaxBranch)
				}
				total += recurseNodes(st.Depth, st.Branch)
			}
			if r.budget.MaxTotal > 0 && total > r.budget.MaxTotal {
				return fmt.Errorf("troupe: %s: declared expansion of %d nodes exceeds the %d total budget", where, total, r.budget.MaxTotal)
			}
		}
	}
	return nil
}

// recurseNodes is the closed-form node count one recurse declares at every
// level: sum_{i=0}^{depth} branch^i. The resolver never expands; this is the
// arithmetic the budget approximates.
func recurseNodes(depth, branch int) int {
	if branch <= 1 {
		return depth + 1
	}
	pow := 1
	for i := 0; i <= depth; i++ {
		pow *= branch
	}
	return (pow - 1) / (branch - 1)
}

package troupe

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// snapshotFromTestdata reads the fixture worktree into a resolver snapshot.
func snapshotFromTestdata(t *testing.T) Snapshot {
	t.Helper()
	snap := Snapshot{}
	err := filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		rel, err := filepath.Rel("testdata", path)
		if err != nil {
			return err
		}
		snap[rel] = mustRead(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk testdata: %v", err)
	}
	return snap
}

// mutateSnapshot returns the conformance snapshot with one file's content
// replaced.
func mutateSnapshot(t *testing.T, path, content string) Snapshot {
	t.Helper()
	snap := snapshotFromTestdata(t)
	snap[path] = []byte(content)
	return snap
}

// conformancePlayID is the fixture play every resolve test resolves.
const conformancePlayID = "story_20260820T161500Z"

// TestResolvePlay_Conformance resolves the fixture play and pins the
// flattened, transitively-closed asset table against the README's resolved-
// play shape: every id@version the play's references reach is present exactly
// once, the specs are preserved byte-for-byte from the authored files, and
// the play envelope is the authored play.
func TestResolvePlay_Conformance(t *testing.T) {
	rp, err := ResolvePlay(snapshotFromTestdata(t), conformancePlayID)
	if err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}

	want := map[string][]string{
		"models": {"branch@1", "cat@1", "dog@1", "forest@1", "leaf@1", "tree@1"},
		"voices": {"cat@1", "dog@1"},
		"sounds": {"rustle@1"},
		"clips":  {"blink@1", "pounce@1", "walk@1"},
		"gags":   {"doubletake@1"},
	}
	got := map[string]map[string]Envelope{
		"models": rp.Assets.Models,
		"voices": rp.Assets.Voices,
		"sounds": rp.Assets.Sounds,
		"clips":  rp.Assets.Clips,
		"gags":   rp.Assets.Gags,
	}
	for kind, refs := range want {
		m := got[kind]
		if len(m) != len(refs) {
			t.Errorf("assets.%s has %d entries, want %d", kind, len(m), len(refs))
		}
		for _, ref := range refs {
			if _, ok := m[ref]; !ok {
				t.Errorf("assets.%s is missing %s", kind, ref)
			}
		}
	}

	// Every resolved spec is byte-identical to its authored file — the served
	// artifact preserves the notebook bytes, never re-marshals them.
	for kind, refs := range want {
		for _, ref := range refs {
			authored := mustRead(t, "testdata/"+kind+"/"+ref+".json")
			var raw struct {
				Spec json.RawMessage `json:"spec"`
			}
			if err := json.Unmarshal(authored, &raw); err != nil {
				t.Fatalf("unmarshal %s/%s.json: %v", kind, ref, err)
			}
			if !bytes.Equal(got[kind][ref].Spec, raw.Spec) {
				t.Errorf("assets.%s[%s]: spec bytes differ from the authored file", kind, ref)
			}
		}
	}

	if rp.Play.ID != conformancePlayID || rp.Play.Kind != KindPlay {
		t.Errorf("play envelope = %s/%s, want play/%s", rp.Play.Kind, rp.Play.ID, conformancePlayID)
	}
	// id@version keys are kind-scoped: cat@1 the model and cat@1 the voice
	// are distinct assets.
	if rp.Assets.Models["cat@1"].Kind != KindModel || rp.Assets.Voices["cat@1"].Kind != KindVoice {
		t.Error("cat@1 must resolve as both a model and a voice, distinctly")
	}
}

// TestResolvePlay_MatchesEngineFixture cross-checks the resolver against the
// hand-resolved fixture the engine tests and the lab consume: same shape,
// same content. The fixture is hand-formatted, so the comparison is
// semantic — both documents unmarshal to the same JSON tree.
func TestResolvePlay_MatchesEngineFixture(t *testing.T) {
	rp, err := ResolvePlay(snapshotFromTestdata(t), conformancePlayID)
	if err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}

	var got, want any
	if err := json.Unmarshal(mustMarshal(t, rp), &got); err != nil {
		t.Fatalf("unmarshal resolved play: %v", err)
	}
	if err := json.Unmarshal(mustRead(t, "../../../lab/fixtures/"+conformancePlayID+".resolved.json"), &want); err != nil {
		t.Fatalf("unmarshal engine fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("resolved play differs from the engine fixture")
	}
}

// TestResolvePlay_Deterministic pins the resolver's purity: the same snapshot
// resolved twice yields byte-identical output.
func TestResolvePlay_Deterministic(t *testing.T) {
	snap := snapshotFromTestdata(t)
	a, err := ResolvePlay(snap, conformancePlayID)
	if err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}
	b, err := ResolvePlay(snap, conformancePlayID)
	if err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}
	if !bytes.Equal(mustMarshal(t, a), mustMarshal(t, b)) {
		t.Error("resolving the same snapshot twice produced different output")
	}
}

// TestResolvePlay_MissingPlay covers an absent play id.
func TestResolvePlay_MissingPlay(t *testing.T) {
	_, err := ResolvePlay(snapshotFromTestdata(t), "story_20260901T000000Z")
	if err == nil || !strings.Contains(err.Error(), "no such play story_20260901T000000Z in plays/") {
		t.Fatalf("err = %v, want a missing-play error", err)
	}
}

// TestResolvePlay_MissingReference covers the typed closure failures: every
// id@version reference must resolve inside its own kind's directory, and the
// error names the reference exactly so the director can fix it.
func TestResolvePlay_MissingReference(t *testing.T) {
	newPlayID := "story_20260821T090000Z"
	play := func(model string) string {
		return `{"kind":"play","id":"story_20260821T090000Z","status":"draft","author":"fixture","provenance":"fixture","spec":{"instances":[{"id":"cat","model":` + strconv.Quote(model) + `,"role":"actor","scale":1}],"timeline":[]}}`
	}
	cases := []struct {
		name   string
		snap   Snapshot
		playID string
		want   string
	}{
		{
			"missing model",
			mutateSnapshot(t, "plays/"+newPlayID+".json", play("ghost@1")),
			newPlayID,
			"no such model ghost@1 in models/",
		},
		{
			"cross-kind reference (model→clip)",
			mutateSnapshot(t, "plays/"+newPlayID+".json", play("walk@1")),
			newPlayID,
			"no such model walk@1 in models/",
		},
		{
			"missing voice via model spec",
			mutateSnapshot(t, "models/cat@1.json",
				strings.Replace(string(mustRead(t, "testdata/models/cat@1.json")), `"voice": "cat@1"`, `"voice": "meow@1"`, 1)),
			conformancePlayID,
			"no such voice meow@1 in voices/",
		},
		{
			"missing clip via gag",
			mutateSnapshot(t, "gags/doubletake@1.json",
				strings.Replace(string(mustRead(t, "testdata/gags/doubletake@1.json")), `"pounce@1"`, `"pounce@2"`, 1)),
			conformancePlayID,
			"no such clip pounce@2 in clips/",
		},
		{
			"missing sound via clip event",
			mutateSnapshot(t, "clips/pounce@1.json",
				strings.Replace(string(mustRead(t, "testdata/clips/pounce@1.json")), `"sound": "rustle@1"`, `"sound": "rustle@2"`, 1)),
			conformancePlayID,
			"no such sound rustle@2 in sounds/",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePlay(tt.snap, tt.playID)
			if err == nil {
				t.Fatalf("ResolvePlay succeeded, want a rejection mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// TestResolvePlay_RejectsWorktree covers the worktree-level rejections: a
// filename/envelope mismatch anywhere blocks the play, and a conflict-marked
// file — asset, note or index — is flagged, never parsed.
func TestResolvePlay_RejectsWorktree(t *testing.T) {
	conflicted := "<<<<<<< HEAD\n" + string(mustRead(t, "testdata/models/leaf@1.json")) + "\n=======\n>>>>>>> troupe\n"
	cases := []struct {
		name string
		snap Snapshot
		want string
	}{
		{
			"filename/envelope mismatch",
			mutateSnapshot(t, "models/dog@1.json", string(mustRead(t, "testdata/models/cat@1.json"))),
			`id "cat" does not match the filename id "dog"`,
		},
		{
			"conflict-marked asset",
			mutateSnapshot(t, "models/leaf@1.json", conflicted),
			"conflict-marked",
		},
		{
			"conflict-marked note",
			mutateSnapshot(t, "feedback/story_x_rating_y.json", conflicted),
			"conflict-marked",
		},
		{
			"conflict-marked play index",
			mutateSnapshot(t, "plays/index.json", conflicted),
			"conflict-marked",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePlay(tt.snap, conformancePlayID)
			if err == nil {
				t.Fatalf("ResolvePlay succeeded, want a rejection mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// TestResolvePlay_SkipsResolvedPlays pins the phase-6 integration: a
// submitted play sits in plays/ as the resolved served artifact {play,
// assets} — never an authored note. The walk skips it the way it skips
// index.json (walked for conflict markers, never parsed), so an authored
// play resolves alongside the history of submitted ones. A conflict-marked
// resolved play is still flagged: the merge state blocks a play serving,
// artifact or note alike.
func TestResolvePlay_SkipsResolvedPlays(t *testing.T) {
	resolved := `{"play":{"kind":"play","id":"story_20260901T000000Z","status":"submitted","author":"director","provenance":"g","spec":{"instances":[],"timeline":[]}},"assets":{"models":{},"voices":{},"sounds":{},"clips":{},"gags":{}}}`
	if _, err := ResolvePlay(mutateSnapshot(t, "plays/story_20260901T000000Z.json", resolved), conformancePlayID); err != nil {
		t.Fatalf("ResolvePlay with a submitted play in plays/: %v", err)
	}

	conflicted := "<<<<<<< HEAD\n" + resolved + "\n=======\n>>>>>>> troupe\n"
	_, err := ResolvePlay(mutateSnapshot(t, "plays/story_20260901T000000Z.json", conflicted), conformancePlayID)
	if err == nil || !strings.Contains(err.Error(), "conflict-marked") {
		t.Fatalf("conflict-marked resolved play: err = %v, want a conflict flag", err)
	}
}

// TestResolvePlay_IgnoresNonAssetNotes pins what the walk skips: roles/ and
// feedback/ notes, the plays/ metadata index and files outside the notebook
// directories are walked for conflict markers but never parsed as assets.
func TestResolvePlay_IgnoresNonAssetNotes(t *testing.T) {
	snap := snapshotFromTestdata(t)
	snap["roles/clown.json"] = []byte(`{"id":"clown","prompt":"You are the clown.","tools":["cat"],"budget":8}`)
	snap["feedback/story_x_rating_y.json"] = []byte(`{"playId":"story_x","type":"rating","ts":"2026-08-20T16:15:04Z","data":{"rating":1}}`)
	snap["plays/index.json"] = []byte(`{"index":[{"id":"story_20260820T161500Z","status":"submitted"}]}`)
	snap["notes/scratch.json"] = []byte(`not a grammar file`)

	if _, err := ResolvePlay(snap, conformancePlayID); err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}
}

// TestResolvePlay_Budget covers the arithmetic expansion budget: per-verb
// caps on scatter count and recurse depth/branch, the total cap over the
// closure, the zero-value disable and the closure-only scope.
func TestResolvePlay_Budget(t *testing.T) {
	replace := func(path, old, new string) Snapshot {
		return mutateSnapshot(t, path, strings.Replace(string(mustRead(t, "testdata/"+path)), old, new, 1))
	}
	cases := []struct {
		name string
		snap Snapshot
		opt  ResolveOption
		want string
	}{
		{
			"scatter count cap",
			replace("models/forest@1.json", `"count": 20`, `"count": 600`),
			nil,
			"scatter count 600 exceeds the 500 cap",
		},
		{
			"recurse depth cap",
			replace("models/branch@1.json", `"depth": 3`, `"depth": 7`),
			nil,
			"recurse depth 7 exceeds the 6 cap",
		},
		{
			"recurse branch cap",
			replace("models/branch@1.json", `"branch": 2`, `"branch": 9`),
			nil,
			"recurse branch 9 exceeds the 8 cap",
		},
		{
			"total cap over the closure",
			snapshotFromTestdata(t),
			WithExpansionBudget(ExpansionBudget{MaxCount: 500, MaxDepth: 6, MaxBranch: 8, MaxTotal: 10}),
			"total budget",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			opts := []ResolveOption{}
			if tt.opt != nil {
				opts = append(opts, tt.opt)
			}
			_, err := ResolvePlay(tt.snap, conformancePlayID, opts...)
			if err == nil {
				t.Fatalf("ResolvePlay succeeded, want a rejection mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}

	// A zero-value budget disables every cap: the conformance play passes.
	if _, err := ResolvePlay(snapshotFromTestdata(t), conformancePlayID, WithExpansionBudget(ExpansionBudget{})); err != nil {
		t.Errorf("zero-value budget rejected the conformance play: %v", err)
	}
}

// TestResolvePlay_BudgetAppliesToClosureOnly pins the budget's scope: only
// the play's closure is checked. An unreferenced model declaring an enormous
// scatter never expands, so it must not block the play.
func TestResolvePlay_BudgetAppliesToClosureOnly(t *testing.T) {
	huge := `{"kind":"model","id":"huge","version":1,"status":"draft","author":"fixture","provenance":"fixture","spec":{"bones":[{"id":"root","parent":null,"x":0,"y":0,"rot":0,"length":0}],"structure":[{"type":"scatter","model":"leaf@1","count":100000,"over":{"type":"band","w":10,"h":10},"seed":1}]}}`
	snap := mutateSnapshot(t, "models/huge@1.json", huge)

	if _, err := ResolvePlay(snap, conformancePlayID); err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}
}

// TestResolvePlay_EmptyKindsMarshalAsObjects pins the served shape: an
// untouched kind marshals as {} — the engine reads assets.<kind> as a table,
// never null.
func TestResolvePlay_EmptyKindsMarshalAsObjects(t *testing.T) {
	play := `{"kind":"play","id":"story_20260820T161500Z","status":"draft","author":"fixture","provenance":"fixture","spec":{"instances":[{"id":"leaf","model":"leaf@1","role":"prop","scale":1}],"timeline":[]}}`
	snap := Snapshot{
		"models/leaf@1.json":                mustRead(t, "testdata/models/leaf@1.json"),
		"plays/story_20260820T161500Z.json": []byte(play),
	}
	rp, err := ResolvePlay(snap, conformancePlayID)
	if err != nil {
		t.Fatalf("ResolvePlay: %v", err)
	}

	var doc struct {
		Assets struct {
			Models json.RawMessage `json:"models"`
			Voices json.RawMessage `json:"voices"`
			Sounds json.RawMessage `json:"sounds"`
			Clips  json.RawMessage `json:"clips"`
			Gags   json.RawMessage `json:"gags"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(mustMarshal(t, rp), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(doc.Assets.Models) == "null" {
		t.Error("assets.models marshals as null")
	}
	for kind, raw := range map[string]json.RawMessage{
		"voices": doc.Assets.Voices, "sounds": doc.Assets.Sounds,
		"clips": doc.Assets.Clips, "gags": doc.Assets.Gags,
	} {
		if string(raw) != "{}" {
			t.Errorf("assets.%s = %s, want {}", kind, raw)
		}
	}
}

// mustMarshal marshals v, failing the test on error.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

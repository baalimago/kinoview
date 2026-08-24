package troupe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// seedWorktree copies the conformance fixture worktree into a fresh temp dir
// and returns its root: the materialised notebook a submitter reads and
// writes.
func seedWorktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for rel, data := range snapshotFromTestdata(t) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

// draftPlay is a fresh, director-authored play over the conformance assets:
// the raw authored form, references intact, status draft. The %s is the play
// id — each authoring is a new UTC datetime, never a rewrite.
const draftPlay = `{"kind":"play","id":"%s","status":"draft","author":"director","provenance":"generation g_01j9x","spec":{"instances":[{"id":"forest","model":"forest@1","role":"backdrop","scale":2.4,"y":-0.2},{"id":"cat","model":"cat@1","role":"actor","voice":"cat@1","scale":1,"x":0.1}],"timeline":[{"at":0,"on":"cat","clip":"walk@1"},{"at":0,"on":"cat","tween":{"to":{"x":0.5},"over":3000,"easing":"ease-in-out"}}]}}`

// writeDraft authors one draft play into the seeded worktree.
func writeDraft(t *testing.T, worktree, playID string) {
	t.Helper()
	path := filepath.Join(worktree, "plays", playID+".json")
	if err := os.WriteFile(path, fmt.Appendf(nil, draftPlay, playID), 0o644); err != nil {
		t.Fatalf("write draft play: %v", err)
	}
}

// newTestSubmitter builds a submitter over a seeded temp worktree.
func newTestSubmitter(t *testing.T) *Submitter {
	t.Helper()
	s, err := NewSubmitter(seedWorktree(t))
	if err != nil {
		t.Fatalf("NewSubmitter: %v", err)
	}
	return s
}

// TestNewSubmitter_Errors pins the required option: a submitter without a
// worktree is refused before anything runs.
func TestNewSubmitter_Errors(t *testing.T) {
	if _, err := NewSubmitter(""); err == nil || !strings.Contains(err.Error(), "worktree can't be empty") {
		t.Fatalf("err = %v, want an empty-worktree error", err)
	}
}

// TestSubmit_RejectsBadID pins the id gate: submit_play accepts only a
// story_<UTC> play id, so a malformed id returns an exact error before any
// work happens.
func TestSubmit_RejectsBadID(t *testing.T) {
	s := newTestSubmitter(t)
	for _, id := range []string{"", "cat@1", "story_x", "story_20260821"} {
		if _, err := s.Submit(id); err == nil || !strings.Contains(err.Error(), "must match story_YYYYMMDDTHHMMSSZ") {
			t.Errorf("Submit(%q): err = %v, want an id-shape error", id, err)
		}
	}
}

// TestSubmit_PersistsResolvedPlay covers the resolve-then-persist path: a
// valid play is resolved, stamped submitted and durably persisted — exactly
// one play file (the authored raw play replaced by the resolved served
// artifact under the same datetime id) and exactly one index entry.
func TestSubmit_PersistsResolvedPlay(t *testing.T) {
	s := newTestSubmitter(t)
	const id = "story_20260821T093000Z"
	writeDraft(t, s.worktree, id)

	rp, err := s.Submit(id)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if rp.Play.Status != StatusSubmitted {
		t.Errorf("resolved play status = %q, want submitted", rp.Play.Status)
	}

	playPath := filepath.Join(s.worktree, "plays", id+".json")
	data, err := os.ReadFile(playPath)
	if err != nil {
		t.Fatalf("read persisted play: %v", err)
	}
	if !isResolvedPlay(data) {
		t.Error("persisted play is not the resolved served artifact {play, assets}")
	}
	// The persisted file is exactly the resolved play the resolver emitted:
	// the play with references intact, the flattened closure, status stamped.
	if !bytes.Equal(data, mustMarshal(t, rp)) {
		t.Error("persisted play differs from the resolved play")
	}

	idx, err := readIndex(filepath.Join(s.worktree, playIndex))
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx.Index) != 1 {
		t.Fatalf("index has %d entries, want exactly 1", len(idx.Index))
	}
	entry := idx.Index[0]
	if entry.ID != id || entry.Status != StatusSubmitted {
		t.Errorf("index entry = %s/%s, want %s/submitted", entry.ID, entry.Status, id)
	}
	if entry.Author != "director" || entry.Provenance != "generation g_01j9x" {
		t.Errorf("index entry author/provenance = %q/%q, want the play envelope's", entry.Author, entry.Provenance)
	}
	if entry.Created == "" {
		t.Error("index entry has no created stamp")
	}

	// No stray temp files: the atomic writes consumed their temps.
	entries, err := os.ReadDir(filepath.Join(s.worktree, "plays"))
	if err != nil {
		t.Fatalf("read plays dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("stray temp file %s left behind", e.Name())
		}
	}
}

// TestSubmit_FailingResolutionWritesNothing covers the exact-error path:
// wherever the resolution fails — a missing model, a conflict-marked asset,
// a budget overrun — the exact error returns to the director and nothing is
// written: the authored play stays raw and no index file exists.
func TestSubmit_FailingResolutionWritesNothing(t *testing.T) {
	const id = "story_20260821T093000Z"
	cases := []struct {
		name string
		mut  func(t *testing.T, worktree string)
		want string
	}{
		{
			"missing model",
			func(t *testing.T, worktree string) {
				writeDraft(t, worktree, id)
				path := filepath.Join(worktree, "plays", id+".json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				broken := strings.Replace(string(data), `"model":"forest@1"`, `"model":"ghost@1"`, 1)
				if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			"no such model ghost@1 in models/",
		},
		{
			"conflict-marked asset",
			func(t *testing.T, worktree string) {
				writeDraft(t, worktree, id)
				path := filepath.Join(worktree, "models", "leaf@1.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				conflicted := "<<<<<<< HEAD\n" + string(data) + "\n=======\n>>>>>>> troupe\n"
				if err := os.WriteFile(path, []byte(conflicted), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			"conflict-marked",
		},
		{
			"budget overrun",
			func(t *testing.T, worktree string) {
				writeDraft(t, worktree, id)
				path := filepath.Join(worktree, "models", "forest@1.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				huge := strings.Replace(string(data), `"count": 20`, `"count": 600`, 1)
				if err := os.WriteFile(path, []byte(huge), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			"scatter count 600 exceeds the 500 cap",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			worktree := seedWorktree(t)
			tt.mut(t, worktree)
			s, err := NewSubmitter(worktree)
			if err != nil {
				t.Fatalf("NewSubmitter: %v", err)
			}
			_, err = s.Submit(id)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			data, err := os.ReadFile(filepath.Join(worktree, "plays", id+".json"))
			if err != nil {
				t.Fatalf("authored play vanished: %v", err)
			}
			if isResolvedPlay(data) {
				t.Error("authored play was replaced despite the failed resolution")
			}
			if _, err := os.Stat(filepath.Join(worktree, playIndex)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("index exists despite the failed resolution (stat err = %v)", err)
			}
		})
	}
}

// TestSubmit_SecondSubmitRefused pins the persistence boundary: a second
// submit of the same play id is refused once the resolved play is durably on
// disk, and the refusal rewrites nothing — neither the play file nor the
// index.
func TestSubmit_SecondSubmitRefused(t *testing.T) {
	s := newTestSubmitter(t)
	const id = "story_20260821T093000Z"
	writeDraft(t, s.worktree, id)
	if _, err := s.Submit(id); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	playPath := filepath.Join(s.worktree, "plays", id+".json")
	indexPath := filepath.Join(s.worktree, playIndex)
	playBefore, err := os.ReadFile(playPath)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.Submit(id)
	if !errors.Is(err, ErrAlreadySubmitted) {
		t.Fatalf("second submit err = %v, want ErrAlreadySubmitted", err)
	}
	playAfter, _ := os.ReadFile(playPath)
	indexAfter, _ := os.ReadFile(indexPath)
	if !bytes.Equal(playBefore, playAfter) {
		t.Error("second submit rewrote the play file")
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Error("second submit rewrote the index")
	}
}

// TestSubmit_PersistFailure pins the abort-on-failure rule: a persist
// failure returns the atomic-write error and the paper trail never claims a
// success the disk did not record.
func TestSubmit_PersistFailure(t *testing.T) {
	const id = "story_20260821T093000Z"

	t.Run("play write fails, index untouched", func(t *testing.T) {
		worktree := seedWorktree(t)
		writeDraft(t, worktree, id)
		playsDir := filepath.Join(worktree, "plays")
		if err := os.Chmod(playsDir, 0o555); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		defer func() { _ = os.Chmod(playsDir, 0o755) }()

		s, err := NewSubmitter(worktree)
		if err != nil {
			t.Fatalf("NewSubmitter: %v", err)
		}
		_, err = s.Submit(id)
		if err == nil || !strings.Contains(err.Error(), "persist play") {
			t.Fatalf("err = %v, want a persist-play failure", err)
		}
		if _, err := os.Stat(filepath.Join(worktree, playIndex)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("index exists despite the persist failure (stat err = %v)", err)
		}
		data, err := os.ReadFile(filepath.Join(worktree, "plays", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if isResolvedPlay(data) {
			t.Error("play was replaced despite the persist failure")
		}
	})

	t.Run("index step fails after the play write", func(t *testing.T) {
		worktree := seedWorktree(t)
		writeDraft(t, worktree, id)
		// A directory at plays/index.json makes the index step fail after
		// the play file was written — pinning the ordering: the play first,
		// the paper trail after, so a crash can never leave an index entry
		// pointing at a missing play.
		indexPath := filepath.Join(worktree, playIndex)
		if err := os.Mkdir(indexPath, 0o755); err != nil {
			t.Fatalf("mkdir index: %v", err)
		}

		s, err := NewSubmitter(worktree)
		if err != nil {
			t.Fatalf("NewSubmitter: %v", err)
		}
		_, err = s.Submit(id)
		if err == nil || !strings.Contains(err.Error(), "index play") {
			t.Fatalf("err = %v, want an index-step failure", err)
		}
		data, err := os.ReadFile(filepath.Join(worktree, "plays", id+".json"))
		if err != nil {
			t.Fatalf("read play: %v", err)
		}
		if !isResolvedPlay(data) {
			t.Error("play was not durably written before the index step")
		}
	})
}

// TestSubmit_IndexOrdering pins the index invariant: newest-first is the
// plain lexicographic sort of the datetime ids.
func TestSubmit_IndexOrdering(t *testing.T) {
	s := newTestSubmitter(t)
	const oldID = "story_20260821T093000Z"
	const newID = "story_20260821T100000Z"
	writeDraft(t, s.worktree, oldID)
	writeDraft(t, s.worktree, newID)

	if _, err := s.Submit(oldID); err != nil {
		t.Fatalf("submit %s: %v", oldID, err)
	}
	if _, err := s.Submit(newID); err != nil {
		t.Fatalf("submit %s: %v", newID, err)
	}

	idx, err := readIndex(filepath.Join(s.worktree, playIndex))
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	got := make([]string, len(idx.Index))
	for i, e := range idx.Index {
		got[i] = e.ID
	}
	if want := []string{newID, oldID}; !slicesEqual(got, want) {
		t.Errorf("index order = %v, want newest-first %v", got, want)
	}
}

// TestSubmit_IndexSortsNewestFirst pins the append's defensive re-sort: an
// out-of-order entry from any earlier state can never persist, because every
// append re-sorts the whole list by the datetime ids.
func TestSubmit_IndexSortsNewestFirst(t *testing.T) {
	worktree := seedWorktree(t)
	const id = "story_20260821T100000Z"
	writeDraft(t, worktree, id)
	indexPath := filepath.Join(worktree, playIndex)
	seed := `{"index":[{"id":"story_20260821T093000Z","status":"submitted","author":"director","provenance":"g","created":"2026-08-21T09:30:01Z"},{"id":"story_20260820T161500Z","status":"submitted","author":"director","provenance":"g","created":"2026-08-20T16:15:01Z"}]}`
	if err := os.WriteFile(indexPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	s, err := NewSubmitter(worktree)
	if err != nil {
		t.Fatalf("NewSubmitter: %v", err)
	}
	if _, err := s.Submit(id); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	idx, err := readIndex(indexPath)
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	got := make([]string, len(idx.Index))
	for i, e := range idx.Index {
		got[i] = e.ID
	}
	want := []string{id, "story_20260821T093000Z", "story_20260820T161500Z"}
	if !slicesEqual(got, want) {
		t.Errorf("index order = %v, want %v", got, want)
	}
}

// TestSubmit_Concurrent pins the single-writer gate: concurrent submits
// serialize through the submitter, so no index update is lost.
func TestSubmit_Concurrent(t *testing.T) {
	s := newTestSubmitter(t)
	ids := []string{"story_20260821T093000Z", "story_20260821T100000Z"}
	for _, id := range ids {
		writeDraft(t, s.worktree, id)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, errs[i] = s.Submit(id)
		}(i, id)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent submit %s: %v", ids[i], err)
		}
	}

	idx, err := readIndex(filepath.Join(s.worktree, playIndex))
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx.Index) != len(ids) {
		t.Errorf("index has %d entries, want %d — the single-writer gate lost an update", len(idx.Index), len(ids))
	}
}

// TestSubmitPlayTool_Call pins the tool's surface: play is a required
// non-empty string, the happy path confirms the submission, and the spec
// names the tool and its input.
func TestSubmitPlayTool_Call(t *testing.T) {
	s := newTestSubmitter(t)
	const id = "story_20260821T093000Z"
	writeDraft(t, s.worktree, id)
	tool := s.newSubmitPlayTool()

	if _, err := tool.Call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "play must be a non-empty string") {
		t.Errorf("Call without play: err = %v, want a play error", err)
	}
	if _, err := tool.Call(map[string]any{"play": 7}); err == nil || !strings.Contains(err.Error(), "play must be a non-empty string") {
		t.Errorf("Call with a non-string play: err = %v, want a play error", err)
	}
	out, err := tool.Call(map[string]any{"play": id})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, "submitted "+id) {
		t.Errorf("out = %q, want a submitted confirmation", out)
	}

	spec := tool.Specification()
	if spec.Name != "submit_play" || spec.Inputs == nil || len(spec.Inputs.Required) != 1 || spec.Inputs.Required[0] != "play" {
		t.Errorf("Specification = %s / required %v, want submit_play requiring play", spec.Name, spec.Inputs)
	}
}

// TestSubmitPlay_NotInGeneralRegistry pins the director-only rule: roles
// select from the general registry, and submit_play is granted to the
// director's fixed tool set (phase 7) — never to a role note.
func TestSubmitPlay_NotInGeneralRegistry(t *testing.T) {
	if registryHas("submit_play") {
		t.Error("submit_play must not be in the general tool registry")
	}
}

// TestSubmit_ResolvedPlayInWorktree pins the cross-generation loop: after
// one play is submitted, the worktree carries its resolved served artifact
// in plays/, and a later submit of a fresh play still resolves — the walk
// skips the served artifact and reads the authored play.
func TestSubmit_ResolvedPlayInWorktree(t *testing.T) {
	s := newTestSubmitter(t)
	const first = "story_20260821T093000Z"
	const second = "story_20260821T100000Z"
	writeDraft(t, s.worktree, first)
	if _, err := s.Submit(first); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	writeDraft(t, s.worktree, second)
	if _, err := s.Submit(second); err != nil {
		t.Fatalf("second submit over a worktree with a submitted play: %v", err)
	}

	idx, err := readIndex(filepath.Join(s.worktree, playIndex))
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx.Index) != 2 {
		t.Errorf("index has %d entries, want 2", len(idx.Index))
	}
}

// TestReadIndex_PinsShape pins the index contract the API reads: the
// plays/index.json envelope is {"index": [...]}, newest-first.
func TestReadIndex_PinsShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), playIndex)
	if _, err := readIndex(path); err != nil {
		t.Fatalf("absent index must read as empty: %v", err)
	}
	idx, err := readIndex(path)
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx.Index) != 0 {
		t.Errorf("absent index = %v, want an empty list", idx.Index)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(mustMarshal(t, PlayIndex{Index: []PlayIndexEntry{{ID: "story_20260820T161500Z", Status: StatusSubmitted}}}), &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["index"]; !ok {
		t.Error(`play index must marshal as {"index": [...]}`)
	}
}

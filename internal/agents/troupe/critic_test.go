package troupe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestWriter builds a criticism writer over a seeded temp worktree.
func newTestWriter(t *testing.T) *CriticismWriter {
	t.Helper()
	w, err := NewCriticismWriter(seedWorktree(t))
	if err != nil {
		t.Fatalf("NewCriticismWriter: %v", err)
	}
	return w
}

// newTestCritic builds a critic over a seeded worktree and a scripted run
// seam.
func newTestCritic(t *testing.T, opts ...CriticOption) (*Critic, *CriticismWriter) {
	t.Helper()
	writer := newTestWriter(t)
	all := append([]CriticOption{
		WithCriticismWriter(writer),
		WithCriticModel("gpt-5"),
		WithCriticConfigDir(t.TempDir()),
	}, opts...)
	c, err := NewCritic(all...)
	if err != nil {
		t.Fatalf("NewCritic: %v", err)
	}
	return c, writer
}

// seedFeedback writes one audience feedback note into the worktree: the
// uniform feedback envelope under its <playId>_<type>_<utc>.json filename —
// the evidence a critic run cites.
func seedFeedback(t *testing.T, worktree string) {
	t.Helper()
	path := filepath.Join(worktree, "feedback", "story_20260820T161500Z_rating_20260820T170000Z.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir feedback: %v", err)
	}
	note := `{"playId":"story_20260820T161500Z","type":"rating","ts":"2026-08-20T17:00:00Z","data":{"rating":1,"comment":"more dog"}}`
	if err := os.WriteFile(path, []byte(note), 0o644); err != nil {
		t.Fatalf("write feedback note: %v", err)
	}
}

// criticismOnDisk is one criticism note read back from the worktree: the
// filename the writer derived and the parsed note.
type criticismOnDisk struct {
	file string
	note CriticismNote
}

// criticismNotesOnDisk parses every criticism note a worktree's feedback/
// holds, in filename order.
func criticismNotesOnDisk(t *testing.T, worktree string) []criticismOnDisk {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(worktree, "feedback"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read feedback dir: %v", err)
	}
	var notes []criticismOnDisk
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(worktree, "feedback", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var note CriticismNote
		if err := json.Unmarshal(data, &note); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		if note.Type == NoteTypeCriticism {
			notes = append(notes, criticismOnDisk{file: e.Name(), note: note})
		}
	}
	return notes
}

// snapshotWorktree returns every file in a worktree keyed by its relative
// path — the byte-level picture a "never edits" test diffs across a run.
func snapshotWorktree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snap[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return snap
}

// TestNewCritic_Errors pins the required options: a critic without a writer,
// a model or a config dir is refused before anything runs.
func TestNewCritic_Errors(t *testing.T) {
	cases := []struct {
		name string
		opts []CriticOption
		want string
	}{
		{"no writer", []CriticOption{WithCriticModel("m"), WithCriticConfigDir(t.TempDir())}, "criticism writer can't be nil"},
		{"no model", []CriticOption{WithCriticismWriter(newTestWriter(t)), WithCriticConfigDir(t.TempDir())}, "model can't be empty"},
		{"no config dir", []CriticOption{WithCriticismWriter(newTestWriter(t)), WithCriticModel("m")}, "config dir can't be empty"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCritic(tt.opts...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestCritic_ToolSet pins the critic's instrument set: the read-only file
// tools from the closed registry and nothing else — no write_file, no
// apply_patch, no mkdir, no notebook tools, no spawn_role, no submit_play —
// plus write_criticism pinned to the reviewed generation's outcome. The
// "never edits" rule is enforced by the tool set, not by the prompt.
func TestCritic_ToolSet(t *testing.T) {
	c, _ := newTestCritic(t)

	globs, tools := c.toolSet(Outcome{Submitted: true, PlayID: "story_20260820T161500Z"})
	for _, want := range []string{"cat", "rows_between", "ls", "rg"} {
		if !slices.Contains(globs, want) {
			t.Errorf("globs missing the read tool %q: %v", want, globs)
		}
	}
	for _, absent := range []string{"write_file", "apply_patch", "mkdir", "mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit"} {
		if slices.Contains(globs, absent) {
			t.Errorf("globs carry the write/notebook tool %q: %v", absent, globs)
		}
	}
	got := make([]string, len(tools))
	for i, tool := range tools {
		got[i] = tool.Specification().Name
	}
	if !slices.Contains(got, "write_criticism") {
		t.Errorf("tools = %v, want write_criticism", got)
	}

	t.Run("the gate is pinned to the outcome's play", func(t *testing.T) {
		_, tools := c.toolSet(Outcome{Submitted: true, PlayID: "story_20260820T161500Z"})
		if inner := findTool(t, tools, "write_criticism").(*writeCriticismTool); inner.playID != "story_20260820T161500Z" {
			t.Errorf("submitted playID = %q, want the outcome's play", inner.playID)
		}
		_, tools = c.toolSet(Outcome{})
		if inner := findTool(t, tools, "write_criticism").(*writeCriticismTool); inner.playID != "" {
			t.Errorf("empty-stage playID = %q, want empty — the critic cannot claim a play", inner.playID)
		}
	})
}

// TestCritic_Prompt pins the assembled prompt: the fixed workflow naming
// the write_criticism gate, the generation id stamped, and the outcome —
// the submitted play id, or the honest empty stage naming no play.
func TestCritic_Prompt(t *testing.T) {
	c, _ := newTestCritic(t)
	p := c.prompt("g_01j8x", Outcome{Submitted: true, PlayID: "story_20260820T161500Z"})
	for _, want := range []string{"WORKFLOW", "write_criticism", "GENERATION g_01j8x", "PLAY story_20260820T161500Z"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	empty := c.prompt("g_01j9y", Outcome{})
	if !strings.Contains(empty, "PLAY none") || strings.Contains(empty, "PLAY story_") {
		t.Errorf("empty-stage prompt must say PLAY none and stamp no play id:\n%s", empty)
	}
}

// TestCritic_Run_WritesCriticismNote is the phase-8 headline: a critic run
// over a submitted play and a feedback trail writes one evidence-cited
// criticism note into feedback/ — the uniform envelope, ts stamped
// server-side, cites the real feedback note paths, filename derived from the
// note.
func TestCritic_Run_WritesCriticismNote(t *testing.T) {
	const generation = "g_01j8x"
	const playID = "story_20260820T161500Z"
	c, writer := newTestCritic(t)
	seedFeedback(t, writer.worktree)

	c.runCritic = func(_ context.Context, p criticParams) (string, error) {
		if p.prompt == "" || len(p.globs) == 0 || len(p.tools) == 0 {
			t.Fatal("the critic loop must receive the prompt, the globs and the tools")
		}
		tool := findTool(t, p.tools, "write_criticism")
		out, err := tool.Call(map[string]any{
			"generation": generation,
			"cites":      []any{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
			"body":       "the cat's walk reads stiff and the doubletake gag landed; the viewers want more dog.",
		})
		if err != nil {
			t.Fatalf("write_criticism: %v", err)
		}
		if !strings.Contains(out, "criticism written: feedback/") {
			t.Errorf("out = %q, want a written confirmation", out)
		}
		return "reviewed", nil
	}

	if err := c.Run(t.Context(), generation, Outcome{Submitted: true, PlayID: playID}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	notes := criticismNotesOnDisk(t, writer.worktree)
	if len(notes) != 1 {
		t.Fatalf("criticism notes = %d, want exactly 1", len(notes))
	}
	note := notes[0].note
	if note.PlayID != playID || note.Type != NoteTypeCriticism {
		t.Errorf("note = %s/%s, want %s/criticism", note.PlayID, note.Type, playID)
	}
	if note.TS == "" {
		t.Error("note has no server-stamped ts")
	}
	if note.Data.GenerationID != generation {
		t.Errorf("generationId = %q, want %q", note.Data.GenerationID, generation)
	}
	if len(note.Data.Cites) != 1 || note.Data.Cites[0] != "feedback/story_20260820T161500Z_rating_20260820T170000Z.json" {
		t.Errorf("cites = %v, want the real feedback note path", note.Data.Cites)
	}
	if !strings.HasPrefix(notes[0].file, playID+"_criticism_") {
		t.Errorf("filename = %q, want %s_criticism_<utc>.json", notes[0].file, playID)
	}
}

// TestCritic_Run_EmptyStage pins the empty-stage honesty: a generation that
// shipped nothing is still reviewed — the note carries an empty playId, a
// filename with no play id, and cites whatever notes exist. The critic never
// fabricates a play.
func TestCritic_Run_EmptyStage(t *testing.T) {
	const generation = "g_01j9y"
	c, writer := newTestCritic(t)
	seedFeedback(t, writer.worktree)

	c.runCritic = func(_ context.Context, p criticParams) (string, error) {
		if !strings.Contains(p.prompt, "PLAY none") {
			t.Errorf("empty-stage prompt must say PLAY none:\n%s", p.prompt)
		}
		tool := findTool(t, p.tools, "write_criticism")
		_, err := tool.Call(map[string]any{
			"generation": generation,
			"cites":      []any{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
			"body":       "nothing shipped: the swarm produced nothing and the director never submitted. The stage stays empty.",
		})
		return "", err
	}

	if err := c.Run(t.Context(), generation, Outcome{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	notes := criticismNotesOnDisk(t, writer.worktree)
	if len(notes) != 1 {
		t.Fatalf("criticism notes = %d, want exactly 1 — the empty stage is still reviewed", len(notes))
	}
	if notes[0].note.PlayID != "" {
		t.Errorf("playId = %q, want empty — never fabricate a play", notes[0].note.PlayID)
	}
	if !strings.HasPrefix(notes[0].file, "criticism_") || strings.Contains(notes[0].file, "story_") {
		t.Errorf("filename = %q, want criticism_<utc>.json with no play id", notes[0].file)
	}
}

// TestCritic_Run_RequiresGenerationID pins the run gate: a review without a
// generation id is refused before the agent loop.
func TestCritic_Run_RequiresGenerationID(t *testing.T) {
	c, _ := newTestCritic(t)
	err := c.Run(t.Context(), "", Outcome{})
	if err == nil || !strings.Contains(err.Error(), "generation id can't be empty") {
		t.Fatalf("err = %v, want a generation-id error", err)
	}
}

// TestCritic_Run_AgentError pins that a failing agent loop fails the review:
// the run error propagates wrapped.
func TestCritic_Run_AgentError(t *testing.T) {
	c, _ := newTestCritic(t, WithRunCritic(func(_ context.Context, _ criticParams) (string, error) {
		return "", errors.New("the model exploded")
	}))
	err := c.Run(t.Context(), "g_01j8x", Outcome{})
	if err == nil || !strings.Contains(err.Error(), "the model exploded") {
		t.Fatalf("err = %v, want the agent-loop error", err)
	}
}

// TestCritic_NeverEdits pins the advisory contract end to end: after a full
// review, the only change in the worktree is the appended criticism note —
// the critic never edits the play or the repertoire, it only appends.
func TestCritic_NeverEdits(t *testing.T) {
	c, writer := newTestCritic(t)
	seedFeedback(t, writer.worktree)
	before := snapshotWorktree(t, writer.worktree)

	c.runCritic = func(_ context.Context, p criticParams) (string, error) {
		tool := findTool(t, p.tools, "write_criticism")
		_, err := tool.Call(map[string]any{
			"generation": "g_01j8x",
			"cites":      []any{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
			"body":       "more dog",
		})
		return "", err
	}
	if err := c.Run(t.Context(), "g_01j8x", Outcome{Submitted: true, PlayID: "story_20260820T161500Z"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	added := 0
	for rel, data := range snapshotWorktree(t, writer.worktree) {
		prev, existed := before[rel]
		if !existed {
			added++
			if !strings.HasPrefix(rel, "feedback/") || !strings.Contains(rel, "criticism_") {
				t.Errorf("the critic added an unexpected file %s", rel)
			}
			continue
		}
		if !bytes.Equal(prev, data) {
			t.Errorf("the critic edited %s — it must never edit, only append", rel)
		}
	}
	if added != 1 {
		t.Errorf("files added = %d, want exactly the one criticism note", added)
	}
}

// TestCriticismWriter_Commit pins the phase-9 commit seam: the commit runs
// with the derived filename after the note is on disk, and a commit failure
// surfaces — a criticism note is never silently dropped.
func TestCriticismWriter_Commit(t *testing.T) {
	frozen := time.Date(2026, 8, 21, 9, 30, 0, 0, time.UTC)
	criticism := Criticism{
		PlayID:     "story_20260820T161500Z",
		Generation: "g_01j9x",
		Cites:      []string{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
		Body:       "The cat's walk is stiff; more dog next time.",
	}

	var committed []string
	worktree := seedWorktree(t)
	seedFeedback(t, worktree)
	w, err := NewCriticismWriter(worktree,
		WithCriticismClock(func() time.Time { return frozen }),
		WithCriticismCommit(func(filename string) error {
			committed = append(committed, filename)
			return nil
		}))
	if err != nil {
		t.Fatalf("NewCriticismWriter: %v", err)
	}
	if _, err := w.Write(criticism); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(committed) != 1 || committed[0] != "story_20260820T161500Z_criticism_20260821T093000Z.json" {
		t.Errorf("committed = %v, want the derived filename", committed)
	}

	wantErr := errors.New("bucket unreachable")
	failingWorktree := seedWorktree(t)
	seedFeedback(t, failingWorktree)
	failing, err := NewCriticismWriter(failingWorktree,
		WithCriticismClock(func() time.Time { return frozen }),
		WithCriticismCommit(func(string) error { return wantErr }))
	if err != nil {
		t.Fatalf("NewCriticismWriter: %v", err)
	}
	if _, err := failing.Write(criticism); !errors.Is(err, wantErr) {
		t.Fatalf("Write = %v, want the commit failure", err)
	}
}

// are missing, fabricated, escaping the notebook or naming bookkeeping, an
// empty body or generation, a malformed play id or a play not on disk — all
// return their exact error and write nothing.
func TestCriticismWriter_Validation(t *testing.T) {
	base := Criticism{
		PlayID:     "story_20260820T161500Z",
		Generation: "g_01j8x",
		Cites:      []string{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
		Body:       "more dog",
	}
	cases := []struct {
		name string
		mut  func(*Criticism)
		want string
	}{
		{"no cites", func(c *Criticism) { c.Cites = nil }, "must cite at least one note path"},
		{"fabricated cite", func(c *Criticism) { c.Cites = []string{"feedback/ghost.json"} }, "is not on disk"},
		{"cite escapes the notebook", func(c *Criticism) { c.Cites = []string{"../secret.json"} }, "escapes the notebook"},
		{"cite outside the notebook", func(c *Criticism) { c.Cites = []string{"tmp/x.json"} }, "is not a notebook note path"},
		{"cite is bookkeeping", func(c *Criticism) { c.Cites = []string{"plays/index.json"} }, "is bookkeeping"},
		{"cite is not a note", func(c *Criticism) { c.Cites = []string{"feedback/notes.txt"} }, "must name a .json note"},
		{"empty body", func(c *Criticism) { c.Body = "" }, "body: must not be empty"},
		{"empty generation", func(c *Criticism) { c.Generation = "" }, "generation: must not be empty"},
		{"malformed play id", func(c *Criticism) { c.PlayID = "cat@1" }, "must match story_YYYYMMDDTHHMMSSZ"},
		{"play not on disk", func(c *Criticism) { c.PlayID = "story_20260822T000000Z" }, "not on disk in plays/"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestWriter(t)
			seedFeedback(t, w.worktree)
			c := base
			tt.mut(&c)
			_, err := w.Write(c)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			if notes := criticismNotesOnDisk(t, w.worktree); len(notes) != 0 {
				t.Error("a refused note must write nothing")
			}
		})
	}
}

// TestCriticismWriter_AppendOnly pins the append-only contract: a note whose
// derived filename already exists is refused — never overwritten. The clock
// is frozen so the two writes derive the same name deterministically.
func TestCriticismWriter_AppendOnly(t *testing.T) {
	frozen := time.Date(2026, 8, 21, 9, 30, 0, 0, time.UTC)
	w, err := NewCriticismWriter(seedWorktree(t), WithCriticismClock(func() time.Time { return frozen }))
	if err != nil {
		t.Fatalf("NewCriticismWriter: %v", err)
	}
	seedFeedback(t, w.worktree)
	c := Criticism{
		PlayID:     "story_20260820T161500Z",
		Generation: "g_01j8x",
		Cites:      []string{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
		Body:       "more dog",
	}
	if _, err := w.Write(c); err != nil {
		t.Fatalf("first write: %v", err)
	}
	_, err = w.Write(c)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second write err = %v, want an append-only refusal", err)
	}
	if notes := criticismNotesOnDisk(t, w.worktree); len(notes) != 1 {
		t.Errorf("notes on disk = %d, want exactly 1 — the refusal rewrote nothing", len(notes))
	}
}

// TestCriticismWriter_Concurrent pins the single-writer gate: concurrent
// writes of the same-named note serialize through the writer, so exactly one
// lands and the other is refused — append-only holds under -race regardless
// of caller.
func TestCriticismWriter_Concurrent(t *testing.T) {
	frozen := time.Date(2026, 8, 21, 9, 30, 0, 0, time.UTC)
	w, err := NewCriticismWriter(seedWorktree(t), WithCriticismClock(func() time.Time { return frozen }))
	if err != nil {
		t.Fatalf("NewCriticismWriter: %v", err)
	}
	seedFeedback(t, w.worktree)
	c := Criticism{
		PlayID:     "story_20260820T161500Z",
		Generation: "g_01j8x",
		Cites:      []string{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
		Body:       "more dog",
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = w.Write(c)
		}(i)
	}
	wg.Wait()

	landed := 0
	for _, err := range results {
		if err == nil {
			landed++
		}
	}
	if landed != 1 {
		t.Errorf("writes landed = %d, want exactly 1 — the gate lost the append-only rule", landed)
	}
	if notes := criticismNotesOnDisk(t, w.worktree); len(notes) != 1 {
		t.Errorf("notes on disk = %d, want exactly 1", len(notes))
	}
}

// TestCriticismWriter_Filename pins the filename derivation: the submitted
// note is <playId>_criticism_<utc>.json, the empty-stage note drops the play
// id lead — criticism_<utc>.json.
func TestCriticismWriter_Filename(t *testing.T) {
	ts := time.Date(2026, 8, 21, 9, 30, 5, 0, time.UTC)
	if got := criticismFilename("story_20260820T161500Z", ts); got != "story_20260820T161500Z_criticism_20260821T093005Z.json" {
		t.Errorf("submitted filename = %q", got)
	}
	if got := criticismFilename("", ts); got != "criticism_20260821T093005Z.json" {
		t.Errorf("empty-stage filename = %q", got)
	}
}

// TestWriteCriticismTool_Call pins the tool's surface: generation and body
// are required non-empty strings, cites an array of note paths, the happy
// path writes the note and confirms, and the spec names the tool and its
// required inputs.
func TestWriteCriticismTool_Call(t *testing.T) {
	w := newTestWriter(t)
	seedFeedback(t, w.worktree)
	tool := w.newWriteCriticismTool("story_20260820T161500Z")

	if _, err := tool.Call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "generation must be a non-empty string") {
		t.Errorf("Call without generation: err = %v, want a generation error", err)
	}
	if _, err := tool.Call(map[string]any{"generation": "g_01j8x", "cites": "not-a-list", "body": "x"}); err == nil || !strings.Contains(err.Error(), "cites must be an array") {
		t.Errorf("Call with a non-array cites: err = %v, want a cites error", err)
	}
	if _, err := tool.Call(map[string]any{"generation": "g_01j8x", "cites": []any{"feedback/x.json"}, "body": ""}); err == nil || !strings.Contains(err.Error(), "body must be a non-empty string") {
		t.Errorf("Call without body: err = %v, want a body error", err)
	}

	out, err := tool.Call(map[string]any{
		"generation": "g_01j8x",
		"cites":      []any{"feedback/story_20260820T161500Z_rating_20260820T170000Z.json"},
		"body":       "more dog",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, "criticism written: feedback/") {
		t.Errorf("out = %q, want a written confirmation", out)
	}

	spec := tool.Specification()
	if spec.Name != "write_criticism" || spec.Inputs == nil {
		t.Fatalf("Specification = %s / %v, want write_criticism with inputs", spec.Name, spec.Inputs)
	}
	if len(spec.Inputs.Required) != 3 || !slices.Contains(spec.Inputs.Required, "cites") {
		t.Errorf("required = %v, want generation/cites/body", spec.Inputs.Required)
	}
}

// TestWriteCriticism_NotInGeneralRegistry pins the critic-only rule: roles
// select from the general registry, and write_criticism is granted to the
// critic's fixed tool set (phase 8) — never to a role note.
func TestWriteCriticism_NotInGeneralRegistry(t *testing.T) {
	if registryHas("write_criticism") {
		t.Error("write_criticism must not be in the general tool registry")
	}
}

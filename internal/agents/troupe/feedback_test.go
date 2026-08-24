package troupe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// frozenFeedback is the deterministic server time the writer stamps: the
// filename derives from it, so a frozen clock makes append-only refusals
// deterministic.
var frozenFeedback = time.Date(2026, 8, 21, 9, 30, 4, 0, time.UTC)

// newTestFeedbackWriter builds an audience note writer over a seeded
// worktree with a frozen clock.
func newTestFeedbackWriter(t *testing.T, opts ...FeedbackWriterOption) *FeedbackWriter {
	t.Helper()
	all := append([]FeedbackWriterOption{WithFeedbackClock(func() time.Time { return frozenFeedback })}, opts...)
	w, err := NewFeedbackWriter(seedWorktree(t), all...)
	if err != nil {
		t.Fatalf("NewFeedbackWriter: %v", err)
	}
	return w
}

// ratingReq is a valid rating request over the seeded conformance play.
var ratingReq = FeedbackRequest{
	PlayID: "story_20260820T161500Z",
	Type:   NoteTypeRating,
	Data:   json.RawMessage(`{"rating":1,"comment":"more dog"}`),
}

// TestFeedbackWriter_Write pins the headline: a valid note lands as one file
// in feedback/ named <playId>_<type>_<utc>.json, with the server-stamped ts
// in the body and the data byte-preserved.
func TestFeedbackWriter_Write(t *testing.T) {
	w := newTestFeedbackWriter(t)
	note, err := w.Write(ratingReq)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if note.TS != "2026-08-21T09:30:04Z" {
		t.Errorf("ts = %q, want the server stamp", note.TS)
	}
	data, err := os.ReadFile(filepath.Join(w.worktree, "feedback", "story_20260820T161500Z_rating_20260821T093004Z.json"))
	if err != nil {
		t.Fatalf("the note is on disk: %v", err)
	}
	var persisted FeedbackNote
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("persisted note: %v", err)
	}
	if persisted.PlayID != ratingReq.PlayID || persisted.Type != ratingReq.Type || persisted.TS != note.TS {
		t.Errorf("persisted envelope = %+v, want the uniform playId/type/ts", persisted)
	}
	if string(persisted.Data) != string(ratingReq.Data) {
		t.Errorf("persisted data = %s, want the byte-preserved data", persisted.Data)
	}
}

// TestFeedbackWriter_EveryType pins the closed type set: each audience type
// writes its own note with its own data shape.
func TestFeedbackWriter_EveryType(t *testing.T) {
	cases := []struct {
		noteType string
		data     string
	}{
		{NoteTypeRating, `{"rating":-1,"comment":"sleepy"}`},
		{NoteTypeDismissal, `{"atMs":42000}`},
		{NoteTypeCompletion, `{"durationMs":180000}`},
		{NoteTypeReplay, `{"count":3}`},
		{NoteTypeContinuity, `{"history":[{"id":"x"}]}`},
	}
	for _, tt := range cases {
		t.Run(tt.noteType, func(t *testing.T) {
			w := newTestFeedbackWriter(t)
			if _, err := w.Write(FeedbackRequest{PlayID: ratingReq.PlayID, Type: tt.noteType, Data: json.RawMessage(tt.data)}); err != nil {
				t.Fatalf("Write %s: %v", tt.noteType, err)
			}
			path := filepath.Join(w.worktree, "feedback", "story_20260820T161500Z_"+tt.noteType+"_20260821T093004Z.json")
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s note not at %s: %v", tt.noteType, path, err)
			}
		})
	}
}

// TestFeedbackWriter_Validation pins the exact refusals: a malformed play
// id, a play not on disk, an unknown type, and each type's invalid data are
// refused with ErrFeedbackInvalid and nothing is written.
func TestFeedbackWriter_Validation(t *testing.T) {
	base := ratingReq
	cases := []struct {
		name string
		mut  func(*FeedbackRequest)
		want string
	}{
		{"malformed play id", func(r *FeedbackRequest) { r.PlayID = "cat@1" }, "must match story_YYYYMMDDTHHMMSSZ"},
		{"play not on disk", func(r *FeedbackRequest) { r.PlayID = "story_20260822T000000Z" }, "not on disk in plays/"},
		{"unknown type", func(r *FeedbackRequest) { r.Type = "criticism" }, "not an audience note type"},
		{"rating must be ±1", func(r *FeedbackRequest) { r.Data = json.RawMessage(`{"rating":0}`) }, "rating must be +1 or -1"},
		{"rating unknown field", func(r *FeedbackRequest) { r.Data = json.RawMessage(`{"rating":1,"vibe":"bad"}`) }, "unknown field"},
		{"rating missing field", func(r *FeedbackRequest) { r.Data = json.RawMessage(`{}`) }, "rating must be +1 or -1"},
		{"dismissal negative", func(r *FeedbackRequest) { r.Type = NoteTypeDismissal; r.Data = json.RawMessage(`{"atMs":-5}`) }, "atMs must not be negative"},
		{"completion negative", func(r *FeedbackRequest) { r.Type = NoteTypeCompletion; r.Data = json.RawMessage(`{"durationMs":-1}`) }, "durationMs must not be negative"},
		{"replay zero", func(r *FeedbackRequest) { r.Type = NoteTypeReplay; r.Data = json.RawMessage(`{"count":0}`) }, "count must be a positive number"},
		{"continuity empty history", func(r *FeedbackRequest) { r.Type = NoteTypeContinuity; r.Data = json.RawMessage(`{"history":[]}`) }, "history must not be empty"},
		{"continuity missing history", func(r *FeedbackRequest) { r.Type = NoteTypeContinuity; r.Data = json.RawMessage(`{}`) }, "history must not be empty"},
		{"data is a string", func(r *FeedbackRequest) { r.Data = json.RawMessage(`"hello"`) }, "cannot unmarshal string"},
		{"missing data", func(r *FeedbackRequest) { r.Data = nil }, "EOF"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestFeedbackWriter(t)
			req := base
			tt.mut(&req)
			_, err := w.Write(req)
			if !errors.Is(err, ErrFeedbackInvalid) {
				t.Fatalf("err = %v, want ErrFeedbackInvalid", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
			assertNoFeedbackNotes(t, w.worktree, "a refused note must write nothing")
		})
	}
}

// TestFeedbackWriter_AppendOnly pins the append-only rule: a second note of
// the same type for the same play in the same second collides on the derived
// filename and is refused, never overwriting the first.
func TestFeedbackWriter_AppendOnly(t *testing.T) {
	w := newTestFeedbackWriter(t)
	if _, err := w.Write(ratingReq); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	_, err := w.Write(ratingReq)
	if !errors.Is(err, ErrFeedbackInvalid) {
		t.Fatalf("second Write = %v, want ErrFeedbackInvalid", err)
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("err = %v, want the append-only refusal", err)
	}
	// A different type in the same second is a different filename: allowed.
	if _, err := w.Write(FeedbackRequest{PlayID: ratingReq.PlayID, Type: NoteTypeReplay, Data: json.RawMessage(`{"count":2}`)}); err != nil {
		t.Errorf("a same-second note of another type must write: %v", err)
	}
}

// TestFeedbackWriter_Commit pins the write+commit unit: the commit seam runs
// with the derived filename after the note is on disk, and a commit failure
// surfaces — the note is never silently dropped.
func TestFeedbackWriter_Commit(t *testing.T) {
	var committed []string
	w := newTestFeedbackWriter(t, WithFeedbackCommit(func(filename string) error {
		committed = append(committed, filename)
		return nil
	}))
	if _, err := w.Write(ratingReq); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(committed) != 1 || committed[0] != "story_20260820T161500Z_rating_20260821T093004Z.json" {
		t.Errorf("committed = %v, want the derived filename", committed)
	}

	wantErr := errors.New("bucket unreachable")
	failing := newTestFeedbackWriter(t, WithFeedbackCommit(func(string) error { return wantErr }))
	if _, err := failing.Write(ratingReq); !errors.Is(err, wantErr) {
		t.Fatalf("Write = %v, want the commit failure", err)
	}
}

// TestFeedbackWriter_Concurrent pins the single-writer gate: concurrent
// notes of different types serialize on the writer's lock, and every note
// lands — no lost writes under -race.
func TestFeedbackWriter_Concurrent(t *testing.T) {
	w := newTestFeedbackWriter(t)
	types := []string{NoteTypeRating, NoteTypeDismissal, NoteTypeCompletion, NoteTypeReplay, NoteTypeContinuity}
	var wg sync.WaitGroup
	for i, noteType := range types {
		wg.Add(1)
		go func(noteType string, i int) {
			defer wg.Done()
			data := json.RawMessage(`{"rating":1}`)
			if noteType == NoteTypeDismissal {
				data = json.RawMessage(`{"atMs":100}`)
			} else if noteType == NoteTypeCompletion {
				data = json.RawMessage(`{"durationMs":200}`)
			} else if noteType == NoteTypeReplay {
				data = json.RawMessage(`{"count":1}`)
			} else if noteType == NoteTypeContinuity {
				data = json.RawMessage(fmt.Sprintf(`{"history":[{"i":%d}]}`, i))
			}
			if _, err := w.Write(FeedbackRequest{PlayID: ratingReq.PlayID, Type: noteType, Data: data}); err != nil {
				t.Errorf("concurrent Write %s: %v", noteType, err)
			}
		}(noteType, i)
	}
	wg.Wait()
	// Five different types, five different filenames: five notes on disk.
	entries, err := os.ReadDir(filepath.Join(w.worktree, "feedback"))
	if err != nil {
		t.Fatalf("read feedback/: %v", err)
	}
	if len(entries) != len(types) {
		t.Errorf("notes on disk = %d, want %d", len(entries), len(types))
	}
}

// TestFeedbackWriter_Filename pins the shared derivation: the audience
// filename is <playId>_<type>_<utc>.json, and the playId-less form (the
// critic's empty stage) is <type>_<utc>.json.
func TestFeedbackWriter_Filename(t *testing.T) {
	ts := frozenFeedback
	if got := feedbackFilename("story_20260820T161500Z", NoteTypeRating, ts); got != "story_20260820T161500Z_rating_20260821T093004Z.json" {
		t.Errorf("filename = %q", got)
	}
	if got := feedbackFilename("", NoteTypeCriticism, ts); got != "criticism_20260821T093004Z.json" {
		t.Errorf("playId-less filename = %q", got)
	}
}

// assertNoFeedbackNotes fails the test if any note exists in feedback/ — a
// refused write must leave the directory empty (or absent).
func assertNoFeedbackNotes(t *testing.T, worktree, msg string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(worktree, "feedback"))
	if errors.Is(err, os.ErrNotExist) {
		return // never wrote anything — the directory was not even created
	}
	if err != nil {
		t.Fatalf("%s: read feedback/: %v", msg, err)
	}
	if len(entries) > 0 {
		t.Errorf("%s: feedback/ has %d notes", msg, len(entries))
	}
}

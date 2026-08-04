package theatre

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A transcript that has grown past the cap trims its oldest lines on the next
// append, keeping the newest TranscriptMaxLines.
func TestAppendTranscript_TrimsOldestPastCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	path := filepath.Join(dir, CompanyDir, transcriptFileName)
	// Seed a transcript already at the cap, as a previous run would leave it.
	var sb strings.Builder
	for i := range TranscriptMaxLines {
		ev := TranscriptEvent{Gen: "stry_test1", Seq: i + 1, Kind: "note", From: "stage", Body: fmt.Sprintf("line %d", i)}
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := co.AppendTranscript(TranscriptEvent{Gen: "stry_test1", Kind: "submit", From: "stage", Body: "done"}); err != nil {
		t.Fatal(err)
	}

	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != TranscriptMaxLines {
		t.Fatalf("lines = %d, want %d", len(lines), TranscriptMaxLines)
	}
	var first, last TranscriptEvent
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[len(lines)-1], &last); err != nil {
		t.Fatal(err)
	}
	if first.Body != "line 1" {
		t.Errorf("oldest kept = %q, want the pre-cap head trimmed", first.Body)
	}
	if last.Body != "done" || last.Seq != TranscriptMaxLines+1 {
		t.Errorf("newest = %q seq %d, want %q seq %d", last.Body, last.Seq, "done", TranscriptMaxLines+1)
	}
}

// Corrupt lines drop, semantically invalid events drop, the readable rest
// survives.
func TestLoadTranscript_CorruptLinesDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, transcriptFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "garbage line\n" +
		`{"gen":"stry_test1","seq":2,"kind":"note","from":"stage","body":"keep"}` + "\n" +
		`{"gen":"stry_test1","seq":3,"kind":"alien","from":"stage","body":"drop"}` + "\n" +
		`{"gen":"stry_test1","seq":4,"kind":"note","from":"alien","body":"drop too"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := Open(dir).LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 1 {
		t.Fatalf("events = %d, want 1: %+v", len(tr.Events), tr.Events)
	}
	if tr.Events[0].Body != "keep" || tr.Events[0].Seq != 2 {
		t.Errorf("kept event = %+v", tr.Events[0])
	}
}

// Untrusted events record nothing: no gen, alien kind, or missing author.
func TestAppendTranscript_DropsInvalidEvents(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	for _, ev := range []TranscriptEvent{
		{Gen: "", Kind: "post", From: "director"},
		{Gen: "stry_test1", Kind: "alien", From: "stage"},
		{Gen: "stry_test1", Kind: "note", From: "alien"},
		{Gen: "stry_test1", Kind: "note", From: "", Body: "no author"},
	} {
		if err := co.AppendTranscript(ev); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 0 {
		t.Fatalf("invalid events recorded: %+v", tr.Events)
	}
}

// Seq continues from the last readable line even when the file was trimmed.
func TestAppendTranscript_SeqContinuesAcrossAppends(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	for i := range 3 {
		if err := co.AppendTranscript(TranscriptEvent{Gen: "g", Kind: "note", From: "stage", Body: fmt.Sprintf("n%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 3 || tr.Events[2].Seq != 3 {
		t.Fatalf("events = %+v, want seqs 1..3", tr.Events)
	}
}

// A zero timestamp is stamped by the writer, so events are always ordered.
func TestAppendTranscript_StampsZeroTime(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	if err := co.AppendTranscript(TranscriptEvent{Gen: "g", Kind: "note", From: "stage", Body: "now"}); err != nil {
		t.Fatal(err)
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 1 || tr.Events[0].T.IsZero() {
		t.Fatalf("event time not stamped: %+v", tr.Events)
	}
}

// A corrupt trailing line cannot supply the next seq; the append falls back to
// the line count and still succeeds.
func TestAppendTranscript_CorruptTrailingLineSeqFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	path := filepath.Join(dir, CompanyDir, transcriptFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := co.AppendTranscript(TranscriptEvent{Gen: "g", Kind: "note", From: "stage", Body: "after"}); err != nil {
		t.Fatal(err)
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 1 || tr.Events[0].Seq != 2 {
		t.Fatalf("events = %+v, want one event with seq 2", tr.Events)
	}
}

// An unreadable transcript file is reported, never fatal.
func TestLoadTranscript_UnreadableFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, transcriptFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory where the file should be cannot be read as a file.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	tr, err := Open(dir).LoadTranscript()
	if err == nil {
		t.Fatal("expected an error for an unreadable transcript")
	}
	if len(tr.Events) != 0 {
		t.Errorf("unreadable transcript leaked events: %+v", tr.Events)
	}
}

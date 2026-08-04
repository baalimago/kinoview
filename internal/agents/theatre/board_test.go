package theatre

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBoard_MissingIsEmpty(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	b, err := co.LoadBoard()
	if err != nil {
		t.Fatalf("missing board must not error: %v", err)
	}
	if len(b.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(b.Entries))
	}
}

// A corrupt board degrades to the empty board, never a crash.
func TestLoadBoard_CorruptFileFallsBackToEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, boardFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	co := Open(dir)
	b, err := co.LoadBoard()
	if err == nil {
		t.Fatal("expected an error for a corrupt board")
	}
	if len(b.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(b.Entries))
	}
}

// Entries with unknown kinds or roles drop, over-long bodies truncate, invalid
// addressees clear, and seqs renumber — never a wholesale rejection.
func TestLoadBoard_DropsUnknownsAndTruncates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	long := strings.Repeat("x", EntryMaxBody+50)
	if err := os.MkdirAll(filepath.Join(dir, CompanyDir), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{
		"generation": "stry_test1",
		"theme": "t",
		"entries": [
			{"author": "playwright", "kind": "alien", "to": "director", "body": "drop me"},
			{"author": "alien", "kind": "note", "body": "drop me too"},
			{"author": "director", "kind": "brief", "to": "playwright", "body": %q},
			{"author": "playwright", "kind": "note", "to": "alien", "body": "cleared"},
			{"author": "scenographer", "kind": "note", "body": "keep me"}
		]
	}`, long)
	if err := os.WriteFile(filepath.Join(dir, CompanyDir, boardFileName), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := co.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(got.Entries), got.Entries)
	}
	first := got.Entries[0]
	if first.Author != "director" || first.Kind != "brief" {
		t.Errorf("first entry = %+v", first)
	}
	if len(first.Body) != EntryMaxBody {
		t.Errorf("body length = %d, want %d", len(first.Body), EntryMaxBody)
	}
	if got.Entries[1].To != "" {
		t.Errorf("invalid addressee not cleared: %+v", got.Entries[1])
	}
	for i, e := range got.Entries {
		if e.Seq != i+1 {
			t.Errorf("entry %d seq = %d, want %d", i, e.Seq, i+1)
		}
	}
}

func TestBoard_AppendDropsInvalid(t *testing.T) {
	t.Parallel()
	var b Board
	b.Append(Entry{Author: "playwright", Kind: "alien", Body: "nope"})
	b.Append(Entry{Author: "alien", Kind: "note", Body: "nope"})
	b.Append(Entry{Author: "playwright", Kind: "note", To: "alien", Body: "cleared"})
	b.Append(Entry{Author: "director", Kind: "brief", To: "dramaturg", Body: strings.Repeat("x", EntryMaxBody+10)})
	if len(b.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(b.Entries))
	}
	if b.Entries[0].To != "" {
		t.Errorf("invalid addressee survived: %+v", b.Entries[0])
	}
	if len(b.Entries[1].Body) != EntryMaxBody {
		t.Errorf("body length = %d, want %d", len(b.Entries[1].Body), EntryMaxBody)
	}
	if b.Entries[0].Seq != 1 || b.Entries[1].Seq != 2 {
		t.Errorf("seqs = %d, %d; want 1, 2", b.Entries[0].Seq, b.Entries[1].Seq)
	}
}

// The board is bounded at write time: past BoardMaxEntries the oldest entries
// drop, and seq stays contiguous.
func TestBoard_AppendCapsAtSixty(t *testing.T) {
	t.Parallel()
	var b Board
	for i := range BoardMaxEntries + 10 {
		b.Append(Entry{Author: "stage", Kind: "note", Body: fmt.Sprintf("note %d", i)})
	}
	if len(b.Entries) != BoardMaxEntries {
		t.Fatalf("entries = %d, want %d", len(b.Entries), BoardMaxEntries)
	}
	if b.Entries[0].Body != "note 10" {
		t.Errorf("oldest kept = %q, want the first post-cap note", b.Entries[0].Body)
	}
	if b.Entries[len(b.Entries)-1].Seq != BoardMaxEntries {
		t.Errorf("last seq = %d, want %d", b.Entries[len(b.Entries)-1].Seq, BoardMaxEntries)
	}
}

func TestBoard_ExcerptLastN(t *testing.T) {
	t.Parallel()
	var b Board
	for i := range 25 {
		b.Append(Entry{Author: "stage", Kind: "note", Body: fmt.Sprintf("note %d", i)})
	}
	ex := b.Excerpt(BoardExcerptMax)
	if len(ex) != BoardExcerptMax {
		t.Fatalf("excerpt = %d, want %d", len(ex), BoardExcerptMax)
	}
	if ex[0].Body != "note 5" || ex[len(ex)-1].Body != "note 24" {
		t.Errorf("excerpt window wrong: first %q last %q", ex[0].Body, ex[len(ex)-1].Body)
	}
	// The excerpt must be a copy: mutating it does not touch the board.
	ex[0].Body = "tampered"
	if b.Entries[5].Body != "note 5" {
		t.Error("excerpt aliases the board")
	}
	if ex := b.Excerpt(0); ex != nil {
		t.Errorf("Excerpt(0) = %v, want nil", ex)
	}
}

package theatre

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

// A working file holding an unplayable story is rejected outright — the caller
// answers with the composer floor.
func TestLoadWorking_InvalidStoryRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, workingFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	w := Working{Story: model.Story{ID: "bad", Title: "no cast"}, Revision: 1, Status: "draft"}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	co := Open(dir)
	got, err := co.LoadWorking()
	if err == nil {
		t.Fatal("expected an invalid draft to be rejected")
	}
	if got.Story.ID != "" {
		t.Errorf("rejected draft leaked: %+v", got)
	}
}

// A missing draft is "no usable draft yet": rejected, same fallback.
func TestLoadWorking_MissingIsRejected(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	if _, err := co.LoadWorking(); err == nil {
		t.Fatal("expected a missing draft to be rejected (composer floor)")
	}
}

// The write gate refuses an unplayable story before it reaches disk.
func TestSaveWorking_RejectsUnplayableStory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	w := Working{Story: model.Story{ID: "bad", Title: "no cast"}}
	if err := co.SaveWorking(w); err == nil {
		t.Fatal("expected SaveWorking to reject an unplayable story")
	}
	if _, err := os.Stat(filepath.Join(dir, CompanyDir, workingFileName)); !os.IsNotExist(err) {
		t.Errorf("a rejected draft reached disk: %v", err)
	}
}

// An unknown status defaults rather than rejects: the draft itself may be
// perfect, only the label is foreign.
func TestLoadWorking_UnknownStatusDefaultsToDraft(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, workingFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	s := validStory()
	w := Working{Story: s, Revision: -2, Status: "finished"}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Open(dir).LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "draft" {
		t.Errorf("status = %q, want draft", got.Status)
	}
	if got.Revision != 0 {
		t.Errorf("revision = %d, want 0", got.Revision)
	}
}

// R3-02: the out-of-band distill inputs — the brief and the dressed marker —
// round-trip through the working file, and a pre-fix file without them still
// loads (both fields are omitempty).
func TestLoadWorking_BriefAndDressedRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	w := Working{
		Story:   validStory(),
		Status:  "dressed",
		Brief:   `{"mood":"standoff","shape":"mousehunt"}`,
		Dressed: true,
	}
	if err := co.SaveWorking(w); err != nil {
		t.Fatal(err)
	}
	got, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if got.Brief != w.Brief || !got.Dressed {
		t.Errorf("working = brief %q, dressed %v; want the saved brief and marker", got.Brief, got.Dressed)
	}

	// A pre-fix file (no brief/dressed keys) loads with both zeroed.
	b, err := json.Marshal(Working{Story: validStory(), Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, CompanyDir, workingFileName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if got.Brief != "" || got.Dressed {
		t.Errorf("pre-fix working file loaded with brief %q / dressed %v, want both zeroed", got.Brief, got.Dressed)
	}
}

func TestWorking_Summary(t *testing.T) {
	t.Parallel()
	s := validStory()
	// A set change mid-play reads as a second act.
	s.Beats = append(s.Beats, model.Beat{T: 2000, Action: "setBackdrop", Piece: "sunset"})
	w := Working{Story: s, Revision: 2, Status: "pinned"}
	sum := w.Summary()
	if sum.Title != "The Test Night" {
		t.Errorf("title = %q", sum.Title)
	}
	if len(sum.Cast) != 1 || sum.Cast[0] != "ina" {
		t.Errorf("cast = %v", sum.Cast)
	}
	if sum.Beats != 3 {
		t.Errorf("beats = %d, want 3", sum.Beats)
	}
	if sum.Acts != 2 {
		t.Errorf("acts = %d, want 2", sum.Acts)
	}
	if sum.Backdrop != "night" {
		t.Errorf("backdrop = %q", sum.Backdrop)
	}
	if sum.Status != "pinned" {
		t.Errorf("status = %q", sum.Status)
	}
}

// The playwright's draft report carries the author's own act structure, which
// supersedes the derived set-change count (D-P1-6), and the canon facts ride
// into the summary so every agent context sees them.
func TestWorking_ReportSupersedesDerivedActs(t *testing.T) {
	t.Parallel()
	s := validStory()
	// One set change mid-play: two acts by the derived count.
	s.Beats = append(s.Beats, model.Beat{T: 2000, Action: "setBackdrop", Piece: "sunset"})
	w := Working{
		Story: s, Revision: 2, Status: "pinned",
		Canon: []string{"the mouse got away"},
		Report: &DraftReport{Title: "The Test Night", Acts: []Act{
			{Name: "act one", Beats: 2, OneLine: "the setup"},
			{Name: "act two", Beats: 1, OneLine: "the turn"},
		}},
	}
	sum := w.Summary()
	if sum.Acts != 2 {
		t.Errorf("acts = %d, want the report's 2 (the derived count is also 2 here)", sum.Acts)
	}

	// A report with three acts overrides the derived two.
	w.Report.Acts = append(w.Report.Acts, Act{Name: "act three", Beats: 1, OneLine: "the end"})
	if sum := w.Summary(); sum.Acts != 3 {
		t.Errorf("acts = %d, want the report's 3 over the derived 2", sum.Acts)
	}
	if len(sum.Canon) != 1 || sum.Canon[0] != "the mouse got away" {
		t.Errorf("summary canon = %v, want the working file's facts", sum.Canon)
	}
}

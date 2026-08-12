package theatre

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Every company file round-trips: write → load → validate → identical
// semantics.
func TestBoard_RoundTrip(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	want := Board{
		Generation: "stry_test1",
		Theme:      "Solaris 1972",
		Entries: []Entry{
			{Author: "director", Kind: "brief", To: "dramaturg", Body: "a heist, please"},
			{Author: "playwright", Kind: "deliverable", Body: "16 beats, 3 acts"},
			{Author: "wardrobe", Kind: "answer", To: "playwright", Body: "silver reads"},
		},
	}
	if err := co.SaveBoard(want); err != nil {
		t.Fatal(err)
	}
	got, err := co.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != want.Generation || got.Theme != want.Theme {
		t.Errorf("header = %q/%q, want %q/%q", got.Generation, got.Theme, want.Generation, want.Theme)
	}
	if len(got.Entries) != len(want.Entries) {
		t.Fatalf("entries = %d, want %d", len(got.Entries), len(want.Entries))
	}
	for i, e := range got.Entries {
		w := want.Entries[i]
		if e.Author != w.Author || e.Kind != w.Kind || e.To != w.To || e.Body != w.Body {
			t.Errorf("entry %d = %+v, want %+v", i, e, w)
		}
		if e.Seq != i+1 {
			t.Errorf("entry %d seq = %d, want %d", i, e.Seq, i+1)
		}
	}
}

func TestWorking_RoundTrip(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	want := Working{Story: validStory(), Revision: 3, Status: "dressed"}
	if err := co.SaveWorking(want); err != nil {
		t.Fatal(err)
	}
	got, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 3 || got.Status != "dressed" {
		t.Errorf("bookkeeping = rev %d status %q, want 3/dressed", got.Revision, got.Status)
	}
	if got.Story.ID != want.Story.ID || got.Story.Title != want.Story.Title {
		t.Errorf("story = %q/%q, want %q/%q", got.Story.ID, got.Story.Title, want.Story.ID, want.Story.Title)
	}
	if len(got.Story.Beats) != len(want.Story.Beats) || len(got.Story.Cast) != len(want.Story.Cast) {
		t.Errorf("story size mismatch: %d beats, %d cast", len(got.Story.Beats), len(got.Story.Cast))
	}
}

// ResetWorking clears the draft between generations: a missing file is the
// normal "no draft yet" state and a second reset is a no-op.
func TestCompany_ResetWorking(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	if err := co.SaveWorking(Working{Story: validStory(), Revision: 1, Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := co.LoadWorking(); err != nil {
		t.Fatalf("seed working file: %v", err)
	}
	if err := co.ResetWorking(); err != nil {
		t.Fatalf("ResetWorking: %v", err)
	}
	if _, err := co.LoadWorking(); err == nil {
		t.Fatal("working file still loads after ResetWorking")
	}
	// A missing file is the normal state: resetting again is a no-op.
	if err := co.ResetWorking(); err != nil {
		t.Fatalf("second ResetWorking: %v", err)
	}
}

func TestLedger_RoundTrip(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	started := time.Now().Truncate(time.Second)
	want := Ledger{
		Generation:   "stry_test1",
		Phase:        "dress",
		PhaseIndex:   3,
		PhasesTotal:  6,
		Budget:       Budget{DirectorUsed: 17, DirectorMax: 50, GlobalUsed: 42, GlobalMax: 200},
		Actors:       []Actor{{Role: "playwright", Status: "working", Calls: 8, Budget: 8, LastAction: "wrote draft"}},
		StartedAt:    started,
		UpdatedAt:    started,
		WallDeadline: started.Add(time.Hour),
	}
	if err := co.SaveLedger(want); err != nil {
		t.Fatal(err)
	}
	got, err := co.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != want.Generation || got.PhaseIndex != 3 || got.PhasesTotal != 6 {
		t.Errorf("header = %+v", got)
	}
	if got.Budget != want.Budget {
		t.Errorf("budget = %+v, want %+v", got.Budget, want.Budget)
	}
	if len(got.Actors) != 1 || got.Actors[0] != want.Actors[0] {
		t.Errorf("actors = %+v, want %+v", got.Actors, want.Actors)
	}
	if !got.StartedAt.Equal(want.StartedAt) || !got.WallDeadline.Equal(want.WallDeadline) {
		t.Errorf("times = %v/%v, want %v/%v", got.StartedAt, got.WallDeadline, want.StartedAt, want.WallDeadline)
	}
}

func TestTranscript_RoundTrip(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	when := time.Now().Truncate(time.Second)
	events := []TranscriptEvent{
		{Gen: "stry_test1", Kind: "post", From: "director", To: "dramaturg", Body: "brief", T: when},
		{Gen: "stry_test1", Kind: "deliver", From: "playwright", To: "stage", Body: "16 beats", T: when.Add(time.Second)},
		{Gen: "stry_test1", Kind: "submit", From: "stage", Body: "done", T: when.Add(2 * time.Second)},
	}
	for _, ev := range events {
		if err := co.AppendTranscript(ev); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != len(events) {
		t.Fatalf("events = %d, want %d", len(tr.Events), len(events))
	}
	for i, ev := range tr.Events {
		want := events[i]
		if ev.Kind != want.Kind || ev.From != want.From || ev.To != want.To || ev.Body != want.Body {
			t.Errorf("event %d = %+v, want %+v", i, ev, want)
		}
		if ev.Seq != i+1 {
			t.Errorf("event %d seq = %d, want %d", i, ev.Seq, i+1)
		}
		if !ev.T.Equal(want.T) {
			t.Errorf("event %d time = %v, want %v", i, ev.T, want.T)
		}
	}
}

// Concurrent writers must never leave a partially written file behind — the
// same read-loop pattern as the theatre's TestTheatre_SaveStoryIsAtomic.
func TestSaveBoard_ConcurrentWritersNeverTorn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	path := filepath.Join(dir, CompanyDir, boardFileName)

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b := Board{Generation: fmt.Sprintf("gen-%d", n)}
			for j := range 5 {
				b.Append(Entry{Author: "playwright", Kind: "note", Body: fmt.Sprintf("writer %d note %d", n, j)})
			}
			if err := co.SaveBoard(b); err != nil {
				t.Errorf("save: %v", err)
			}
		}(i)
	}
	// Read the raw file while the writers churn; every observable state must
	// parse. The mutex would hide disk-level tearing, so read without it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			b, err := os.ReadFile(path)
			if err != nil {
				continue // not created yet; a missing file is fine, a torn one is not
			}
			var board Board
			if err := json.Unmarshal(b, &board); err != nil {
				t.Errorf("observed a torn board file: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	<-done
}

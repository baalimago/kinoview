package theatre

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

// The ledger records per-role call counts, token usage, consult count and hop
// depth for a fixture generation — the "analyze performance later" data.
func TestStage_LedgerRecordsTelemetry(t *testing.T) {
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_tel", WithBudgets(50, 200), WithWallDeadline(10*time.Minute))
	silenceFeed(stage)
	stage.SetActorBudget("playwright", 8)
	stage.RecordCall("director", "read_story")
	stage.RecordCall("director", "draft_story")
	stage.RecordCall("playwright", "write")
	stage.RecordTokens("playwright", 4321)
	stage.RecordConsult("playwright", 2)
	stage.RecordConsult("playwright", 1)
	stage.SetPhase("draft")
	stage.Submit("T")
	<-stage.feed.done()

	ledger, err := co.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Generation != "stry_tel" {
		t.Errorf("generation = %q", ledger.Generation)
	}
	if ledger.Phase != "submitted" || ledger.PhaseIndex != len(phaseOrder) {
		t.Errorf("final phase = %s (%d/%d)", ledger.Phase, ledger.PhaseIndex, ledger.PhasesTotal)
	}
	if ledger.Budget.DirectorUsed != 2 || ledger.Budget.GlobalUsed != 3 {
		t.Errorf("budget used = %+v, want director 2 / global 3", ledger.Budget)
	}
	if ledger.WallDeadline.IsZero() {
		t.Error("wall deadline not recorded")
	}
	if ledger.StartedAt.IsZero() || ledger.UpdatedAt.Before(ledger.StartedAt) {
		t.Error("timing not recorded")
	}

	byRole := map[string]Actor{}
	for _, a := range ledger.Actors {
		byRole[a.Role] = a
	}
	pw, ok := byRole["playwright"]
	if !ok {
		t.Fatalf("playwright missing from ledger actors: %+v", ledger.Actors)
	}
	if pw.Calls != 1 || pw.Tokens != 4321 || pw.Consults != 2 || pw.HopDepth != 2 || pw.Budget != 8 {
		t.Errorf("playwright telemetry = %+v, want 1 call, 4321 tokens, 2 consults, hop 2, budget 8", pw)
	}
	if byRole["director"].Calls != 2 || byRole["director"].LastAction != "draft_story" {
		t.Errorf("director telemetry = %+v", byRole["director"])
	}
}

// A failed transcript write must not stop the event from printing: the
// transcript is a record, the feed is the show.
func TestStage_TranscriptWriteFailureStillPrints(t *testing.T) {
	dir := t.TempDir()
	co := Open(dir)
	// A directory where the transcript file should be makes every append fail.
	badPath := filepath.Join(dir, CompanyDir, transcriptFileName)
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatal(err)
	}

	stage := OpenStage(co, "stry_t1")
	out := captureStdout(t, func(t *testing.T) {
		errOut := testboil.CaptureStderr(t, func(t *testing.T) {
			stage.Emit(TranscriptEvent{Kind: "post", From: "director", To: "stage", Body: "still printed"})
			stage.Close()
			<-stage.feed.done()
		})
		if !strings.Contains(errOut, "theatre: transcript write failed") {
			t.Errorf("stderr = %q, want a logged transcript write failure", errOut)
		}
	})
	if !strings.Contains(out, "notice: [theatre stry_t1] director→stage: still printed") {
		t.Errorf("stdout = %q, want the event printed despite the write failure", out)
	}
}

// A failed ledger write must not stop the phase line: the ledger is
// telemetry, not the show.
func TestStage_LedgerWriteFailureContinues(t *testing.T) {
	dir := t.TempDir()
	co := Open(dir)
	badPath := filepath.Join(dir, CompanyDir, ledgerFileName)
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatal(err)
	}

	stage := OpenStage(co, "stry_l1")
	out := captureStdout(t, func(t *testing.T) {
		errOut := testboil.CaptureStderr(t, func(t *testing.T) {
			stage.SetPhase("brief")
			stage.RecordCall("director", "read_story")
			stage.Close()
			<-stage.feed.done()
		})
		if !strings.Contains(errOut, "theatre: ledger write failed") {
			t.Errorf("stderr = %q, want a logged ledger write failure", errOut)
		}
	})
	if !strings.Contains(out, "─ phase 1/5 brief ─ budget 0/50") {
		t.Errorf("stdout = %q, want the phase line despite the ledger failure", out)
	}
}

// An event the transcript would reject prints nothing: the feed and the file
// never disagree, even about untrusted input.
func TestStage_EmitDropsInvalidEvents(t *testing.T) {
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_inv")
	out := captureStdout(t, func(t *testing.T) {
		stage.Emit(TranscriptEvent{Kind: "alien", From: "director", To: "stage", Body: "bad kind"})
		stage.Emit(TranscriptEvent{Kind: "post", From: "alien", To: "stage", Body: "bad author"})
		stage.Emit(TranscriptEvent{Kind: "post", From: "director", To: "stage", Body: "good"})
		stage.Close()
		<-stage.feed.done()
	})
	if ls := lines(out); len(ls) != 1 || !strings.Contains(ls[0], "good") {
		t.Errorf("feed output = %q, want only the valid event", out)
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 1 || tr.Events[0].Body != "good" {
		t.Errorf("transcript = %+v, want only the valid event", tr.Events)
	}
}

// Mini-agent sessions stream through the log sink with role and generation
// tags, ready for the house loghandler.
func TestStage_LogStreamsTagged(t *testing.T) {
	var got []model.LogMessage
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_s1", WithLogSink(func(lm model.LogMessage) { got = append(got, lm) }))
	stage.Log(model.WARNING, "dramaturg", "consult budget spent")

	if len(got) != 1 {
		t.Fatalf("sink received %d messages, want 1", len(got))
	}
	if got[0].Logger != "theatre.dramaturg" {
		t.Errorf("logger = %q, want theatre.dramaturg", got[0].Logger)
	}
	if got[0].Level != model.WARNING {
		t.Errorf("level = %v, want warning", got[0].Level)
	}
	if !strings.Contains(got[0].Message, "corrID: stry_s1") || !strings.Contains(got[0].Message, "consult budget spent") {
		t.Errorf("message = %q, want the corrID and the message", got[0].Message)
	}

	// Without a sink, Log is a quiet no-op.
	quiet := OpenStage(co, "stry_s2")
	quiet.Log(model.INFO, "director", "nothing happens")
}

// A stage that was closed never records again.
func TestStage_ClosedStageRecordsNothing(t *testing.T) {
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_cl")
	stage.Close()
	<-stage.feed.done()
	stage.Emit(TranscriptEvent{Kind: "post", From: "director", To: "stage", Body: "too late"})
	stage.SetPhase("brief")
	stage.RecordCall("director", "nope")
	stage.Submit("T")
	stage.Fail(fmt.Errorf("nope"))

	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Events) != 0 {
		t.Errorf("closed stage recorded events: %+v", tr.Events)
	}
	if _, err := co.LoadLedger(); err != nil {
		t.Fatal(err)
	}
}

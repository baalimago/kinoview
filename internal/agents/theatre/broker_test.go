package theatre

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// wiredProduction builds a silenced stage plus a stub runner with its broker
// wired — the shape every runner and broker test starts from. script selects
// the LLM outcome per prompt (nil answers "ok"); the prompts slice records
// every bounded loop the runner ran.
func wiredProduction(t *testing.T, script func(prompt string) llmOutcome, opts ...StageOption) (*Company, *Stage, *Runner, *Broker, *[]string) {
	t.Helper()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12", opts...)
	silenceFeed(stage)

	runner, prompts := stubRunner(t, stage, script)
	broker := NewBroker(co, stage, runner)
	runner.WireBroker(broker)
	return co, stage, runner, broker, prompts
}

// stubRunner wires a Runner whose LLM is a scripted fake: every bounded loop
// returns the outcome the script picks for the prompt (nil answers "ok"), and
// every prompt is recorded. This is the stub seam the phase-3 contract asks
// for — the machinery runs without a model configured.
func stubRunner(t *testing.T, stage *Stage, script func(prompt string) llmOutcome) (*Runner, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var prompts []string
	runner := NewRunner(stage.company, stage, WithCacheDir(t.TempDir()))
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		mu.Lock()
		prompts = append(prompts, p.prompt)
		mu.Unlock()
		if script == nil {
			return llmOutcome{text: "ok"}, nil
		}
		return script(p.prompt), nil
	}
	return runner, &prompts
}

// deliverable wraps a report and its collaborations as an agent's final text,
// the shape parseReport reads out of an LLM reply.
func deliverable(report string, collabs ...string) string {
	var b strings.Builder
	b.WriteString(`{"report": "` + report + `"`)
	if len(collabs) > 0 {
		b.WriteString(`, "collaborations": [`)
		for i, c := range collabs {
			if i > 0 {
				b.WriteString(",")
			}
			parts := strings.SplitN(c, "|", 2)
			b.WriteString(`{"role": "` + parts[0] + `", "question": "` + parts[1] + `"}`)
		}
		b.WriteString("]")
	}
	b.WriteString("}")
	return b.String()
}

func promptsContain(prompts *[]string, needle string) bool {
	for _, p := range *prompts {
		if strings.Contains(p, needle) {
			return true
		}
	}
	return false
}

// countPrompts counts the LLM loops whose prompt contains needle — the
// spawn-count proxy for consulted roles, whose answer roundtrips are not
// budgeted calls (review 3, R3-03).
func countPrompts(prompts *[]string, needle string) int {
	n := 0
	for _, p := range *prompts {
		if strings.Contains(p, needle) {
			n++
		}
	}
	return n
}

func ledgerCalls(t *testing.T, stage *Stage, role string) int {
	t.Helper()
	ledger, err := stage.company.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range ledger.Actors {
		if a.Role == role {
			return a.Calls
		}
	}
	return 0
}

// A consult chain of depth 3 terminates: the third consult is refused with a
// clear message, no spawn happens past the cap, and the ledger records hop
// depth 2 as the max.
func TestBroker_ConsultChainDepth3Terminates(t *testing.T) {
	_, stage, _, broker, prompts := wiredProduction(t, func(prompt string) llmOutcome {
		switch {
		case strings.Contains(prompt, "q2"):
			return llmOutcome{text: deliverable("s answer", "playwright|q3")}
		case strings.Contains(prompt, "q1"):
			return llmOutcome{text: deliverable("w answer", "scenographer|q2")}
		}
		return llmOutcome{text: "unexpected"}
	})

	answer, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "q1", 0)
	if err != nil {
		t.Fatalf("consult failed: %v", err)
	}
	if answer != "w answer" {
		t.Errorf("answer = %q, want the wardrobe's answer", answer)
	}

	// The chain: dramaturg(0) → wardrobe(1) → scenographer(2) → playwright
	// refused. The playwright's question must never reach the LLM.
	if promptsContain(prompts, "q3") {
		t.Error("the playwright was spawned for the depth-2 consult — the hop cap failed")
	}

	ledger, err := stage.company.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	hop, consults := map[string]int{}, map[string]int{}
	for _, a := range ledger.Actors {
		hop[a.Role] = a.HopDepth
		consults[a.Role] = a.Consults
	}
	if hop["dramaturg"] != 1 || hop["wardrobe"] != 2 || hop["scenographer"] != 2 {
		t.Errorf("hop depths = %v, want dramaturg 1, wardrobe 2, scenographer 2", hop)
	}
	if consults["dramaturg"] != 1 || consults["wardrobe"] != 1 || consults["scenographer"] != 1 {
		t.Errorf("consult counts = %v, want one each", consults)
	}
	if len(ledger.Actors) != 3 {
		t.Errorf("ledger actors = %v, want dramaturg, wardrobe and scenographer only", ledger.Actors)
	}
}

// A repeat consult returns the previous answer from the table without a
// second spawn: the consulted role's spawn counter stays unchanged. The
// repeat is still accounted — the ledger counts the consult and the
// transcript notes the table hit, so the telemetry sees every consultation
// (review 1, R1-03).
func TestBroker_RepeatConsultReturnsCachedAnswer(t *testing.T) {
	_, stage, _, broker, _ := wiredProduction(t, nil)

	answer, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "does silver read?", 0)
	if err != nil {
		t.Fatalf("consult failed: %v", err)
	}
	if answer != "ok" {
		t.Errorf("answer = %q, want the stub's answer", answer)
	}

	callsAfterFirst := ledgerCalls(t, stage, "wardrobe")

	again, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "does silver read?", 0)
	if err != nil {
		t.Fatalf("repeat consult failed: %v", err)
	}
	if again != answer {
		t.Errorf("repeat answer = %q, want the cached %q", again, answer)
	}
	if got := ledgerCalls(t, stage, "wardrobe"); got != callsAfterFirst {
		t.Errorf("wardrobe spawn counter = %d, want %d — the repeat consult spawned again", got, callsAfterFirst)
	}

	ledger, err := stage.company.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range ledger.Actors {
		if a.Role == "dramaturg" && a.Consults != 2 {
			t.Errorf("dramaturg consults = %d, want 2 (spawn + repeat)", a.Consults)
		}
	}
	tr, err := stage.company.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var noted bool
	for _, ev := range tr.Events {
		if ev.Kind == "note" && strings.Contains(ev.Body, "repeat consult") {
			noted = true
		}
	}
	if !noted {
		t.Error("transcript lacks the repeat-consult note")
	}
}

// The table keys on the questioner too: the same question from a different
// role is a different consultation.
func TestBroker_RepeatConsultQuestionerSpecific(t *testing.T) {
	_, _, _, broker, prompts := wiredProduction(t, nil)

	for _, questioner := range []string{"dramaturg", "playwright"} {
		if _, err := broker.Consult(context.Background(), questioner, "wardrobe", "q", 0); err != nil {
			t.Fatalf("consult from %s: %v", questioner, err)
		}
	}
	// Each consultation spawned a fresh wardrobe loop — the answer roundtrip
	// is not a budgeted call (review 3, R3-03), so the spawn count comes from
	// the LLM prompts, not the ledger's call counter.
	if got := countPrompts(prompts, "You are the wardrobe consultant."); got != 2 {
		t.Errorf("wardrobe spawns = %d, want 2 (one per questioner)", got)
	}
}

// The director and the stage are never consultable, and unknown roles are
// refused at the door with a clear message — the input schema restricts the
// role enum, and the broker enforces it as well.
func TestBroker_RefusesNonProductionRoles(t *testing.T) {
	_, _, _, broker, _ := wiredProduction(t, nil)
	for _, target := range []string{"director", "stage", "bogus", "costume"} {
		msg, err := broker.Consult(context.Background(), "dramaturg", target, "q", 0)
		if err != nil {
			t.Fatalf("consult %s: %v", target, err)
		}
		if !strings.Contains(msg, "consult refused") {
			t.Errorf("target %q: message = %q, want a refusal", target, msg)
		}
	}
	msg, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "   ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "empty question") {
		t.Errorf("empty question: message = %q", msg)
	}
}

// A spawn past the global call cap is refused and the caller is told.
func TestBroker_GlobalBudgetExhaustedRefusal(t *testing.T) {
	_, stage, _, broker, _ := wiredProduction(t, nil, WithBudgets(50, 2))
	for range 2 {
		stage.RecordCall("director", "read_story")
	}
	msg, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "q", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "call budget exhausted") {
		t.Errorf("message = %q, want the budget refusal", msg)
	}
}

// A spawn past the wall-clock deadline is refused and the caller is told.
func TestBroker_WallDeadlineExceededRefusal(t *testing.T) {
	_, _, _, broker, _ := wiredProduction(t, nil, WithWallDeadline(-time.Minute))
	msg, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "q", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "deadline exceeded") {
		t.Errorf("message = %q, want the deadline refusal", msg)
	}
}

// A consultation posts the question and the answer to the board and records
// both in the transcript.
func TestBroker_PostsQuestionAndAnswer(t *testing.T) {
	co, _, _, broker, _ := wiredProduction(t, nil)

	answer, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "does silver read?", 0)
	if err != nil {
		t.Fatalf("consult failed: %v", err)
	}
	if answer != "ok" {
		t.Fatalf("answer = %q", answer)
	}

	board, err := co.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Entries) != 2 {
		t.Fatalf("board entries = %d, want question + answer", len(board.Entries))
	}
	q, a := board.Entries[0], board.Entries[1]
	if q.Author != "dramaturg" || q.Kind != "question" || q.To != "wardrobe" || q.Body != "does silver read?" {
		t.Errorf("question entry = %+v", q)
	}
	if a.Author != "wardrobe" || a.Kind != "answer" || a.To != "dramaturg" || a.Body != "ok" {
		t.Errorf("answer entry = %+v", a)
	}

	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, ev := range tr.Events {
		kinds[ev.Kind] = true
	}
	for _, want := range []string{"consult", "answer", "deliver"} {
		if !kinds[want] {
			t.Errorf("transcript missing a %q event; kinds = %v", want, kinds)
		}
	}
}

// A board write failure is logged and the consultation continues: the board
// is context, not the show — the transcript keeps the authoritative record.
func TestBroker_BoardWriteFailureContinues(t *testing.T) {
	co, _, _, broker, _ := wiredProduction(t, nil)
	// The board path is a directory, so every board write fails.
	if err := os.MkdirAll(co.boardPath(), 0o755); err != nil {
		t.Fatal(err)
	}

	answer, err := broker.Consult(context.Background(), "dramaturg", "wardrobe", "does silver read?", 0)
	if err != nil {
		t.Fatalf("consult failed: %v", err)
	}
	if answer != "ok" {
		t.Errorf("answer = %q, want the stub's answer despite the board failure", answer)
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, ev := range tr.Events {
		kinds[ev.Kind] = true
	}
	if !kinds["consult"] || !kinds["answer"] {
		t.Errorf("transcript kinds = %v, want consult + answer even with a broken board", kinds)
	}
}

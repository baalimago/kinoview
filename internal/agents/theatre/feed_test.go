package theatre

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
)

// withAncliPlain pins the ancli globals the feed tests need — one line per
// event, no ANSI color — and restores them afterwards.
func withAncliPlain(t *testing.T) {
	t.Helper()
	prevNewline, prevColor := ancli.Newline, ancli.UseColor
	ancli.Newline, ancli.UseColor = true, false
	t.Cleanup(func() { ancli.Newline, ancli.UseColor = prevNewline, prevColor })
}

// captureStdout pins the ancli globals and captures stdout while do runs.
func captureStdout(t *testing.T, do func(t *testing.T)) string {
	t.Helper()
	withAncliPlain(t)
	return testboil.CaptureStdout(t, do)
}

// lines splits captured output into its non-empty lines.
func lines(out string) []string {
	var ls []string
	for l := range strings.SplitSeq(out, "\n") {
		if l != "" {
			ls = append(ls, l)
		}
	}
	return ls
}

// For a scripted sequence of 20 events, the feed emits exactly 20 ancli lines,
// each carrying the [theatre <gen>] prefix and the documented body format, in
// transcript order.
func TestFeed_OneLinePerEventInOrder(t *testing.T) {
	co := Open(t.TempDir())
	out := captureStdout(t, func(t *testing.T) {
		stage := scriptedProduction(t, co, "stry_ab12", true)
		<-stage.feed.done()
	})

	ls := lines(out)
	if len(ls) != 20 {
		t.Fatalf("feed emitted %d lines, want 20:\n%s", len(ls), out)
	}
	for i, l := range ls {
		if !strings.HasPrefix(l, "notice: [theatre stry_ab12] ") &&
			!strings.HasPrefix(l, "ok: [theatre stry_ab12] ") &&
			!strings.HasPrefix(l, "warning: [theatre stry_ab12] ") {
			t.Errorf("line %d lacks the theatre prefix: %q", i, l)
		}
	}

	want := []string{
		"notice: [theatre stry_ab12] ─ phase 1/6 brief ─ budget 0/50",
		`notice: [theatre stry_ab12] director→dramaturg: brief (mood=standoff, lineup=3)`,
		"notice: [theatre stry_ab12] dramaturg→playwright: note 0",
		`notice: [theatre stry_ab12] playwright→wardrobe: "does silver read on night?"`,
		`notice: [theatre stry_ab12] wardrobe→playwright: "silver reads; keep ina lane 1"`,
		`notice: [theatre stry_ab12] playwright⇉draft: 16 beats / 3 acts / "The Long Night"`,
		"warning: [theatre stry_ab12] stage: budget refusal: playwright out of calls",
		"notice: [theatre stry_ab12] ─ phase 3/6 dress ─ scenographer 2/8 calls ─ budget 3/50",
		"notice: [theatre stry_ab12] scenographer→director: dressing 0",
		"notice: [theatre stry_ab12] ─ phase 6/6 submit ─ scenographer 2/8 calls ─ budget 3/50",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("feed output missing %q\nfull output:\n%s", w, out)
		}
	}
	if last := ls[len(ls)-1]; !strings.HasPrefix(last, "ok: [theatre stry_ab12] ✓ submitted \"The Long Night\"") {
		t.Errorf("last line = %q, want the ✓ submitted summary", last)
	}
}

// Transcript and feed derive from the same events: every printed line is the
// exact rendering of the transcript event in the same position.
func TestFeed_TranscriptAndFeedAgree(t *testing.T) {
	co := Open(t.TempDir())
	out := captureStdout(t, func(t *testing.T) {
		stage := scriptedProduction(t, co, "stry_ab12", true)
		<-stage.feed.done()
	})

	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var trGen []TranscriptEvent
	for _, ev := range tr.Events {
		if ev.Gen == "stry_ab12" {
			trGen = append(trGen, ev)
		}
	}
	ls := lines(out)
	if len(ls) != len(trGen) {
		t.Fatalf("feed lines = %d, transcript events = %d", len(ls), len(trGen))
	}
	for i, ev := range trGen {
		want := fmt.Sprintf("%s: [theatre stry_ab12] %s", levelPrefix(ev), FormatEventLine(ev))
		if ls[i] != want {
			t.Errorf("line %d = %q, want %q", i, ls[i], want)
		}
	}
}

func levelPrefix(ev TranscriptEvent) string {
	switch levelOf(ev) {
	case "ok":
		return "ok"
	case "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "notice"
	}
}

// 8 concurrent agent sessions produce exactly one complete line per event, in
// the transcript's order — ancli's mutexes make lines atomic, the feed
// goroutine makes the order authoritative.
func TestFeed_ConcurrentSessionsNoInterleaving(t *testing.T) {
	// Pin the ancli globals BEFORE any feed activity: the feed goroutine reads
	// them at emit time, and swapping them while it runs is a data race (the
	// -race detector catches it). The emitting goroutines run inside the
	// stdout capture, so every line lands in the captured output.
	withAncliPlain(t)
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_conc", WithBudgets(50, 200))
	roles := []string{"director", "dramaturg", "playwright", "scenographer", "wardrobe", "stage", "director", "playwright"}
	const perGoroutine = 25

	out := testboil.CaptureStdout(t, func(t *testing.T) {
		var wg sync.WaitGroup
		for gi, role := range roles {
			wg.Add(1)
			go func(gi int, role string) {
				defer wg.Done()
				for i := range perGoroutine {
					stage.Emit(TranscriptEvent{
						Kind: "post", From: role, To: "stage",
						Body: fmt.Sprintf("event-%d", gi*perGoroutine+i),
					})
				}
			}(gi, role)
		}
		wg.Wait()
		stage.Close()
		<-stage.feed.done()
	})

	ls := lines(out)
	if len(ls) != len(roles)*perGoroutine {
		t.Fatalf("feed emitted %d lines, want %d", len(ls), len(roles)*perGoroutine)
	}
	lineRe := regexp.MustCompile(`^notice: \[theatre stry_conc\] (director|dramaturg|playwright|scenographer|wardrobe|stage)→stage: event-\d+$`)
	for i, l := range ls {
		if !lineRe.MatchString(l) {
			t.Fatalf("line %d is not a complete well-formed line: %q", i, l)
		}
	}

	// The printed bodies must be in the transcript's order, exactly.
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var trBodies []string
	for _, ev := range tr.Events {
		if ev.Gen == "stry_conc" {
			trBodies = append(trBodies, ev.Body)
		}
	}
	bodyRe := regexp.MustCompile(`event-\d+$`)
	for i, l := range ls {
		got := bodyRe.FindString(l)
		if got != trBodies[i] {
			t.Fatalf("line %d printed %q, transcript has %q — feed and file disagree", i, got, trBodies[i])
		}
	}
}

// A fail event prints as an ✗ error line on stderr, so the operator sees the
// failure even though the splash keeps working (the composer floor).
func TestFeed_FailPrintsError(t *testing.T) {
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_f1")
	errOut := testboil.CaptureStderr(t, func(t *testing.T) {
		withAncliPlain(t)
		stage.SetPhase("brief")
		stage.Fail(fmt.Errorf("llm query: boom"))
		<-stage.feed.done()
	})
	if !strings.Contains(errOut, "error: [theatre stry_f1] ✗ llm query: boom") {
		t.Errorf("stderr = %q, want the ✗ fail line", errOut)
	}
}

// A panic inside the feed's print path is recovered, logged, and the feed
// keeps consuming — the generation is unaffected.
func TestFeed_PanicRecovered(t *testing.T) {
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_p1")

	// Panic on the first event only, then record normally. The switch happens
	// before any send, so there is no race with the feed goroutine.
	panicked := false
	var mu sync.Mutex
	var printed []string
	stage.feed.print = func(level, msg string) {
		if !panicked {
			panicked = true
			panic("boom")
		}
		mu.Lock()
		printed = append(printed, msg)
		mu.Unlock()
	}

	errOut := testboil.CaptureStderr(t, func(t *testing.T) {
		withAncliPlain(t)
		stage.Emit(TranscriptEvent{Kind: "post", From: "director", To: "stage", Body: "explodes"})
		stage.Emit(TranscriptEvent{Kind: "post", From: "director", To: "stage", Body: "survives"})
		stage.Close()
		<-stage.feed.done()
	})

	if !strings.Contains(errOut, "theatre: feed recovered from panic: boom") {
		t.Errorf("stderr = %q, want the recovery logged", errOut)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(printed) != 1 || !strings.Contains(printed[0], "survives") {
		t.Errorf("printed = %v, want the post-panic event to print", printed)
	}
}

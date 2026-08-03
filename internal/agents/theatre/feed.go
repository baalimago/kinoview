package theatre

import (
	"fmt"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

// feedLineChan buffers events between the stage manager and the feed
// goroutine. Emitters block when the feed falls behind — events are never
// dropped, only delayed.
const feedLineChan = 64

// feed is the theatre's stdout writer (decision D10): one goroutine consumes
// transcript events and prints one compact ancli line per event, in transcript
// order. The stage manager is the only sender; the feed is the only printer —
// agents never write stdout. ancli's own mutexes make every line atomic; the
// single consumer makes the order authoritative.
type feed struct {
	gen    string
	ch     chan TranscriptEvent
	doneCh chan struct{}
	// print is the ancli mapper, split out so tests can record lines or
	// inject a panic without touching stdout.
	print func(level, msg string)
}

// newFeed starts the feed goroutine for one generation.
func newFeed(gen string) *feed {
	f := &feed{
		gen:    gen,
		ch:     make(chan TranscriptEvent, feedLineChan),
		doneCh: make(chan struct{}),
		print: func(level, msg string) {
			switch level {
			case "ok":
				ancli.Okf("%s", msg)
			case "warning":
				ancli.Warnf("%s", msg)
			case "error":
				ancli.Errf("%s", msg)
			default:
				ancli.Noticef("%s", msg)
			}
		},
	}
	go f.run()
	return f
}

// send queues one event for printing. Sends are serialised by the stage
// manager, so the print order is the transcript order.
func (f *feed) send(ev TranscriptEvent) { f.ch <- ev }

// close stops the feed after the queued events have drained.
func (f *feed) close() { close(f.ch) }

// done is closed once every queued event has been printed.
func (f *feed) done() <-chan struct{} { return f.doneCh }

func (f *feed) run() {
	defer close(f.doneCh)
	for ev := range f.ch {
		f.emit(ev)
	}
}

// emit prints one event. A panic here (a hostile body, a broken ancli) must
// not take the generation down: it is recovered, logged, and the feed keeps
// consuming.
func (f *feed) emit(ev TranscriptEvent) {
	defer func() {
		if r := recover(); r != nil {
			ancli.Errf("theatre: feed recovered from panic: %v", r)
		}
	}()
	f.print(levelOf(ev), fmt.Sprintf("[theatre %s] %s", f.gen, FormatEventLine(ev)))
}

// levelOf maps an event to its feed severity. An explicit level wins; without
// one the kind decides: submits succeed, failures fail, everything else is a
// notice.
func levelOf(ev TranscriptEvent) string {
	if ev.Level != "" {
		return ev.Level
	}
	switch ev.Kind {
	case "submit":
		return "ok"
	case "fail":
		return "error"
	default:
		return "notice"
	}
}

// FormatEventLine renders one transcript event as the compact feed line body:
// → for inter-agent messages, ⇉ for artifact deliveries, ─ for phase lines,
// ✓/✗ for submit/fail. The feed and the debug renderer share this rendering,
// so stdout and the dialog can never disagree.
func FormatEventLine(ev TranscriptEvent) string {
	switch ev.Kind {
	case "deliver":
		return fmt.Sprintf("%s⇉%s: %s", ev.From, ev.To, ev.Body)
	case "phase":
		return "─ " + ev.Body
	case "submit":
		return "✓ " + ev.Body
	case "fail":
		return "✗ " + ev.Body
	default: // post, consult, answer, note
		if ev.To != "" {
			return fmt.Sprintf("%s→%s: %s", ev.From, ev.To, ev.Body)
		}
		return fmt.Sprintf("%s: %s", ev.From, ev.Body)
	}
}

package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/table"
	"github.com/baalimago/kinoview/internal/model"
)

// classifyState is the derived per-item classification state rendered by the
// progress watcher. It is computed from the persisted store item, so the
// running server's writes are reflected without any server API.
type classifyState int

const (
	stateQueued classifyState = iota
	stateInProgress
	stateDone
	stateFailed
	stateStopped // attempts >= maxAttempts and no metadata
	stateNotClassifiable
)

func (s classifyState) String() string {
	switch s {
	case stateQueued:
		return "queued"
	case stateInProgress:
		return "in progress"
	case stateDone:
		return "done"
	case stateFailed:
		return "failed"
	case stateStopped:
		return "stopped"
	case stateNotClassifiable:
		return "not classifiable"
	}
	return "unknown"
}

// terminal reports whether the watch can stop once every item is in this
// state: the item has been attempted (success or failure), hit the attempt
// ceiling, or can never classify. Queued and in-progress items keep the watch
// alive — the server's backoff retries failed items on its own.
func (s classifyState) terminal() bool {
	switch s {
	case stateDone, stateFailed, stateStopped, stateNotClassifiable:
		return true
	}
	return false
}

// stateSymbol maps a state to its single-character live indicator.
func (s classifyState) stateSymbol() string {
	switch s {
	case stateQueued:
		return "…"
	case stateInProgress:
		return "→"
	case stateDone:
		return "✓"
	case stateFailed:
		return "✗"
	case stateStopped:
		return "⊘"
	case stateNotClassifiable:
		return "–"
	}
	return "?"
}

// itemState is one watched item's live classification state.
type itemState struct {
	name    string
	state   classifyState
	attempt int
	err     string
}

// deriveClassificationState maps a persisted item to a watch state.
// Only videos classify; images and other types never do.
func deriveClassificationState(it model.Item, maxAttempts int) classifyState {
	if !strings.Contains(it.MIMEType, "video") {
		return stateNotClassifiable
	}
	if it.Metadata != nil {
		return stateDone
	}
	if maxAttempts > 0 && it.ClassificationAttempts >= maxAttempts {
		return stateStopped
	}
	if it.ClassificationAttempts > 0 {
		if it.ClassificationError != "" {
			return stateFailed
		}
		return stateInProgress
	}
	return stateQueued
}

// readItemFromDisk loads one item's persisted JSON from the store directory.
// The server and the CLI share this directory, so this observes the server's
// classification writes without any server API.
func readItemFromDisk(storePath, id string) (model.Item, bool) {
	data, err := os.ReadFile(path.Join(storePath, id))
	if err != nil {
		return model.Item{}, false
	}
	var it model.Item
	if err := json.Unmarshal(data, &it); err != nil {
		return model.Item{}, false
	}
	return it, true
}

// readItemStates polls the store directory once for each watched item. A
// failed read (mid-write, missing file) keeps the previous state so the live
// line does not flicker; prev may be nil for the initial poll.
func readItemStates(storePath string, items []model.Item, maxAttempts int, prev []itemState) []itemState {
	states := make([]itemState, 0, len(items))
	for i, it := range items {
		st := itemState{name: it.Name}
		if i < len(prev) {
			st = prev[i]
			st.name = it.Name
		}
		persisted, ok := readItemFromDisk(storePath, it.ID)
		if !ok {
			states = append(states, st)
			continue
		}
		st.attempt = persisted.ClassificationAttempts
		st.err = persisted.ClassificationError
		st.state = deriveClassificationState(persisted, maxAttempts)
		states = append(states, st)
	}
	return states
}

// statesChanged reports whether any watched item changed state since prev.
func statesChanged(prev, cur []itemState) bool {
	if len(prev) != len(cur) {
		return true
	}
	for i := range cur {
		if prev[i].state != cur[i].state || prev[i].attempt != cur[i].attempt {
			return true
		}
	}
	return false
}

// renderProgressLine builds the single-line live status. For a single item it
// shows that item's detail; for a group it shows the aggregate counts plus the
// item currently being classified. lineWidth <= 0 falls back to the terminal
// width via table.TermWidth.
func renderProgressLine(label string, states []itemState, elapsed time.Duration, hint string, lineWidth int) string {
	line := ""
	if len(states) == 1 {
		st := states[0]
		line = fmt.Sprintf("%s: %s %s", label, st.state.stateSymbol(), st.state)
		if st.state == stateFailed && st.err != "" {
			line += " — " + st.err
		}
		if st.state == stateInProgress {
			line += fmt.Sprintf(" (attempt %d)", st.attempt)
		}
	} else {
		var done, inProgress, queued, failed, stopped int
		current := ""
		currentAttempt := 0
		for _, st := range states {
			switch st.state {
			case stateDone:
				done++
			case stateInProgress:
				inProgress++
				if current == "" {
					current = st.name
					currentAttempt = st.attempt
				}
			case stateQueued:
				queued++
			case stateFailed:
				failed++
			case stateStopped:
				stopped++
			}
		}
		parts := []string{fmt.Sprintf("%d/%d done", done, len(states))}
		if current != "" {
			parts = append(parts, fmt.Sprintf("%s (attempt %d)", current, currentAttempt))
		}
		if queued > 0 {
			parts = append(parts, fmt.Sprintf("%d queued", queued))
		}
		if failed > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", failed))
		}
		if stopped > 0 {
			parts = append(parts, fmt.Sprintf("%d stopped", stopped))
		}
		line = label + ": " + strings.Join(parts, " · ")
	}
	line += " · " + elapsed.Round(time.Second).String()
	if hint != "" {
		line += " · " + hint
	}
	if lineWidth <= 0 {
		if tw, err := table.TermWidth(); err == nil {
			lineWidth = tw
		} else {
			lineWidth = 80
		}
	}
	return truncateMiddle(line, lineWidth)
}

// truncateMiddle shortens s to width runes, keeping the head and tail and
// inserting " … " when truncation is needed.
func truncateMiddle(s string, width int) string {
	r := []rune(s)
	if width <= 0 || len(r) <= width {
		return s
	}
	const infix = " … "
	infixLen := len([]rune(infix))
	if width <= infixLen {
		return string(r[:width])
	}
	avail := width - infixLen
	head := (avail + 1) / 2
	tail := avail - head
	return string(r[:head]) + infix + string(r[len(r)-tail:])
}

// progressWatchOptions tune the watch loop. Zero values pick sane defaults.
type progressWatchOptions struct {
	pollInterval        time.Duration
	noProgressHintAfter time.Duration
	// quit, when non-nil, is polled once per tick; returning true stops the
	// watch. When nil, the watch polls /dev/tty non-blocking for q/quit so it
	// never blocks and never leaves a reader behind that could steal input
	// from the table afterwards. Ctrl+C (ctx cancel) always works.
	quit func() bool
	// termWidth overrides the live-line width; 0 uses the terminal width.
	termWidth int
}

// ttyQuit polls /dev/tty in non-blocking mode for a q/quit line. It is the
// watch's default quit detector: because reads never block and the fd is
// closed when the watch ends, no lingering reader can steal input from the
// table afterwards (the failure mode of a background ReadUserInput goroutine).
//
// The fd is a raw O_NONBLOCK descriptor opened with syscall.Open and read
// with syscall.Read — deliberately not an os.File. os.File.Read funnels
// through the runtime netpoller, which parks the goroutine on EAGAIN for
// character devices, and os.File.Fd resets the descriptor to blocking mode;
// either would make the "non-blocking" poll block forever on an idle tty,
// so the watch could never observe ctx cancellation (or quit).
type ttyQuit struct {
	fd      int
	pending strings.Builder
	dead    bool
}

// openTTYQuit opens /dev/tty non-blocking. Returns nil when the terminal is
// unavailable (e.g. no TTY), in which case Ctrl+C remains the escape hatch.
func openTTYQuit() *ttyQuit {
	ttyPath := os.Getenv("TTY")
	if ttyPath == "" {
		ttyPath = "/dev/tty"
	}
	fd, err := syscall.Open(ttyPath, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	// A read from the controlling terminal by a background process group
	// would otherwise SIGTTIN-stop the whole CLI (scripts, CI, sandboxes).
	// With SIGTTIN ignored the read fails with EIO instead and polling
	// simply stops, leaving Ctrl+C as the escape hatch.
	signal.Ignore(syscall.SIGTTIN)
	return &ttyQuit{fd: fd}
}

// poll drains any complete lines available on the tty and reports whether the
// user asked to quit (q or quit). Non-quit input is consumed but ignored.
func (q *ttyQuit) poll() bool {
	if q.dead {
		return false
	}
	buf := make([]byte, 64)
	for {
		// Raw syscall.Read on the raw O_NONBLOCK fd: it returns EAGAIN
		// immediately when the tty is idle, so the loop always gets back
		// to its select (see the ttyQuit doc comment).
		n, err := syscall.Read(q.fd, buf)
		if n > 0 {
			q.pending.Write(buf[:n])
		}
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				break
			}
			// EIO (background process group), EOF, or another fatal error:
			// stop polling, Ctrl+C still works.
			q.dead = true
			syscall.Close(q.fd)
			q.fd = -1
			break
		}
		if n == 0 {
			break
		}
	}
	line := strings.TrimSpace(q.pending.String())
	if line == "" {
		return false
	}
	q.pending.Reset()
	return line == "q" || line == "quit"
}

func (q *ttyQuit) close() {
	if q != nil && q.fd >= 0 {
		syscall.Close(q.fd)
		q.fd = -1
	}
	signal.Reset(syscall.SIGTTIN)
}

// allTerminal reports whether every watched item reached a terminal state.
func allTerminal(states []itemState) bool {
	for _, st := range states {
		if !st.state.terminal() {
			return false
		}
	}
	return true
}

// writeLiveLine redraws a single status line in place with a carriage return
// so the watch never consumes terminal height.
func writeLiveLine(out io.Writer, line string) {
	table.ClearLine(out)
	fmt.Fprint(out, line)
}

// printFinalReport prints the watch's outcome: one summary line plus a line
// per item that did not finish classified (failed, stopped, queued).
// statusWord is "finished", "stopped" or "interrupted".
func printFinalReport(out io.Writer, label string, states []itemState, elapsed time.Duration, statusWord string) {
	done, notClassifiable := 0, 0
	for _, st := range states {
		if st.state == stateDone {
			done++
		}
		if st.state == stateNotClassifiable {
			notClassifiable++
		}
	}
	fmt.Fprintf(out, "%s: reclassification %s — %d/%d done", label, statusWord, done, len(states))
	if notClassifiable > 0 {
		fmt.Fprintf(out, " · %d not classifiable", notClassifiable)
	}
	fmt.Fprintf(out, " · %s\n", elapsed.Round(time.Second).String())
	for _, st := range states {
		if st.state == stateDone || st.state == stateNotClassifiable {
			continue
		}
		line := fmt.Sprintf("  %s %s", st.state.stateSymbol(), st.name)
		if st.state == stateFailed && st.err != "" {
			line += " — " + st.err
		}
		if st.state == stateStopped {
			line += " — hit the max attempt ceiling"
		}
		if st.state == stateQueued {
			line += " — not picked up yet (restart kinoview to re-queue)"
		}
		fmt.Fprintln(out, line)
	}
}

// watchClassificationProgress polls the store directory and redraws a single
// status line until every item has been classified or attempted, the user
// quits (q via the tty poller), or ctx is cancelled (Ctrl+C). The final
// report is printed before returning.
func watchClassificationProgress(
	ctx context.Context,
	storePath, label string,
	items []model.Item,
	maxAttempts int,
	out io.Writer,
	opts progressWatchOptions,
) error {
	if opts.pollInterval <= 0 {
		opts.pollInterval = time.Second
	}
	if opts.noProgressHintAfter <= 0 {
		opts.noProgressHintAfter = 45 * time.Second
	}
	if out == nil {
		out = os.Stdout
	}

	start := time.Now()
	states := readItemStates(storePath, items, maxAttempts, nil)
	writeLiveLine(out, renderProgressLine(label, states, 0, "", opts.termWidth))

	quit := opts.quit
	var tq *ttyQuit
	if quit == nil {
		tq = openTTYQuit()
		if tq != nil {
			defer tq.close()
			quit = tq.poll
		}
	}

	ticker := time.NewTicker(opts.pollInterval)
	defer ticker.Stop()

	lastMove := start
	hintShown := false
	for {
		if allTerminal(states) {
			writeLiveLine(out, renderProgressLine(label, states, time.Since(start), "", opts.termWidth))
			fmt.Fprintln(out)
			printFinalReport(out, label, states, time.Since(start), "finished")
			return nil
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			writeLiveLine(out, renderProgressLine(label, states, time.Since(start), "", opts.termWidth))
			fmt.Fprintln(out)
			printFinalReport(out, label, states, time.Since(start), "interrupted")
			return nil
		}

		if quit != nil && quit() {
			writeLiveLine(out, renderProgressLine(label, states, time.Since(start), "", opts.termWidth))
			fmt.Fprintln(out)
			printFinalReport(out, label, states, time.Since(start), "stopped")
			return nil
		}

		cur := readItemStates(storePath, items, maxAttempts, states)
		moved := statesChanged(states, cur)
		if moved {
			lastMove = time.Now()
			hintShown = false
		}
		states = cur

		hint := ""
		if !moved && time.Since(lastMove) >= opts.noProgressHintAfter && !hintShown && hasQueued(states) {
			hint = "no progress — is the server running?"
			hintShown = true
		}
		writeLiveLine(out, renderProgressLine(label, states, time.Since(start), hint, opts.termWidth))
	}
}

// hasQueued reports whether any watched item is still waiting to be picked up.
func hasQueued(states []itemState) bool {
	for _, st := range states {
		if st.state == stateQueued {
			return true
		}
	}
	return false
}

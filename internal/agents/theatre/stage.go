package theatre

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// Default call budgets, tuned later from telemetry (decision D8): the director
// spends at most DefaultDirectorBudget calls; every LLM call anywhere spends
// DefaultGlobalBudget; one generation runs at most DefaultWallClock on the
// wall.
const (
	DefaultDirectorBudget = 50
	DefaultGlobalBudget   = 200
	DefaultWallClock      = 10 * time.Minute
)

// phaseOrder is the production flow the director moves through. The working
// file's statuses are the -ed forms of these phases. Validate precedes submit
// in the flow (phase 4's production prompt: brief → draft → dress → validate →
// submit), so the phase line reads the flow as it is suggested.
var phaseOrder = []string{"brief", "draft", "dress", "validate", "submit"}

// Stage is the stage-manager wrapper: the single writer of a generation's
// transcript, the owner of the stdout feed and the keeper of the progress
// ledger. Every inter-agent event flows through it, so the transcript, the
// feed and the ledger can never disagree about what happened and when.
//
// Observability failures never stop the show: a transcript or ledger write
// error is logged and the event still prints — the composer floor stands
// below everything (decision D11).
type Stage struct {
	company *Company
	feed    *feed
	gen     string

	mu       sync.Mutex // serialises emits: transcript order == feed order
	ledger   Ledger
	lastRole string // most recently active actor, for phase lines
	logSink  func(model.LogMessage)
	closed   bool
}

// StageOption configures a Stage at open.
type StageOption func(*Stage)

// WithBudgets sets the generation's call budgets.
func WithBudgets(directorMax, globalMax int) StageOption {
	return func(s *Stage) {
		s.ledger.Budget.DirectorMax = directorMax
		s.ledger.Budget.GlobalMax = globalMax
	}
}

// WithWallDeadline caps the generation's wall clock, counted from open.
func WithWallDeadline(d time.Duration) StageOption {
	return func(s *Stage) {
		s.ledger.WallDeadline = time.Now().Add(d)
	}
}

// WithLogSink streams mini-agent session lines to the house loghandler (or
// anywhere else that accepts model.LogMessage).
func WithLogSink(sink func(model.LogMessage)) StageOption {
	return func(s *Stage) { s.logSink = sink }
}

// OpenStage starts a generation's paperwork: the feed goroutine runs and the
// ledger is initialised with the generation id, the opening phase and the
// budgets. Nothing is written to disk until the first transition or call.
func OpenStage(c *Company, gen string, opts ...StageOption) *Stage {
	s := &Stage{
		company: c,
		feed:    newFeed(gen),
		gen:     gen,
		ledger: Ledger{
			Generation:  gen,
			Phase:       phaseOrder[0],
			PhaseIndex:  1,
			PhasesTotal: len(phaseOrder),
			Budget:      Budget{DirectorMax: DefaultDirectorBudget, GlobalMax: DefaultGlobalBudget},
			StartedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Emit records one inter-agent event: it is appended to the transcript and
// handed to the feed, in that order. An event the transcript would reject
// prints nothing — the feed and the file never disagree. A failed transcript
// write is logged and the line still prints.
func (s *Stage) Emit(ev TranscriptEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.emitLocked(ev)
}

// SetPhase moves the production to the next phase: the ledger is updated and
// a "─ phase N/M … ─" line prints.
func (s *Stage) SetPhase(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.ledger.Phase = status
	s.ledger.PhaseIndex = phaseIndexOf(status)
	s.ledger.PhasesTotal = len(phaseOrder)
	s.emitLocked(TranscriptEvent{Kind: "phase", From: "stage", Body: s.phaseBody()})
	s.saveLedger()
}

// RecordCall accounts one budgeted tool execution for a role. Director calls
// also spend the director budget; every tool call anywhere spends the global
// budget. The budgets cap WithMaxToolCalls executions, so the final answer of
// an invocation is not a RecordCall (see RecordAnswer).
func (s *Stage) RecordCall(role, action string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	a := s.actor(role)
	a.Calls++
	a.Status = "active"
	a.LastAction = action
	s.ledger.Budget.GlobalUsed++
	if role == "director" {
		s.ledger.Budget.DirectorUsed++
	}
	s.lastRole = role
	s.saveLedger()
}

// RecordAnswer accounts an invocation's final answer: the loop's terminal
// roundtrip that produced the deliverable text. It is not a budgeted tool
// call — the per-invocation budgets cap WithMaxToolCalls executions, and the
// answer must always be allowed or the loop would yield nothing — so it never
// pushes an actor, the director budget or the global budget over their caps
// in the phase lines and the dialog summary (review 3, R3-03). It is visible
// as the actor's last action and in the token telemetry.
func (s *Stage) RecordAnswer(role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	a := s.actor(role)
	a.Status = "active"
	a.LastAction = "answer"
	s.lastRole = role
	s.saveLedger()
}

// RecordTokens accounts token usage for a role.
func (s *Stage) RecordTokens(role string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.actor(role).Tokens += n
	s.saveLedger()
}

// RecordConsult accounts one consultation for a role and records the deepest
// hop the consultation reached.
func (s *Stage) RecordConsult(role string, depth int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	a := s.actor(role)
	a.Consults++
	if depth > a.HopDepth {
		a.HopDepth = depth
	}
	s.saveLedger()
}

// RecordFailure marks a role's invocation as failed in the ledger. The
// generation itself continues — only this invocation failed.
func (s *Stage) RecordFailure(role, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	a := s.actor(role)
	a.Status = "failed"
	a.LastAction = msg
	s.saveLedger()
}

// BudgetSnapshot reports the generation's budget position: global calls used
// vs the global cap, and the wall-clock deadline (zero when unset). The
// runner and the broker consult it before every spawn, so a generation past
// its budget or deadline refuses new work instead of burning calls.
func (s *Stage) BudgetSnapshot() (globalUsed, globalMax int, deadline time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ledger.Budget.GlobalUsed, s.ledger.Budget.GlobalMax, s.ledger.WallDeadline
}

// SetActorBudget pins a role's per-invocation call budget (the "8" in
// "scenographer 2/8 calls").
func (s *Stage) SetActorBudget(role string, budget int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.actor(role).Budget = budget
	s.saveLedger()
}

// Submit ends the generation successfully: the submit line prints (with wall
// time, director calls and consult count), the ledger lands in its final
// state and the feed drains before the stage returns — no feed goroutine
// outlives the generation, and the transcript is flushed before the caller
// moves on (the phase-6 contract).
func (s *Stage) Submit(title string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.ledger.Phase = "submitted"
	s.ledger.PhaseIndex = len(phaseOrder)
	s.ledger.PhasesTotal = len(phaseOrder)
	s.emitLocked(TranscriptEvent{Kind: "submit", From: "stage", Body: s.submitBody(title), Level: "ok"})
	s.saveLedger()
	done := s.feed.done()
	s.feed.close()
	s.closed = true
	s.mu.Unlock()
	<-done
}

// Fail ends the generation in failure: the ✗ line prints and the feed
// drains. The composer floor answers for the story itself.
func (s *Stage) Fail(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	body := "unknown failure"
	if err != nil {
		body = err.Error()
	}
	s.emitLocked(TranscriptEvent{Kind: "fail", From: "stage", Body: body, Level: "error"})
	s.saveLedger()
	done := s.feed.done()
	s.feed.close()
	s.closed = true
	s.mu.Unlock()
	<-done
}

// Close aborts the generation's paperwork without a submit or fail line,
// and drains the feed before returning.
func (s *Stage) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	done := s.feed.done()
	s.feed.close()
	s.closed = true
	s.mu.Unlock()
	<-done
}

// Log streams one mini-agent session line through the log sink, tagged with
// the role and the generation (logger "theatre.<role>", corrID <gen>), the
// shape the house loghandler prints.
func (s *Stage) Log(level model.LogLevel, role, msg string) {
	if s.logSink == nil {
		return
	}
	s.logSink(model.LogMessage{
		Level:   level,
		Logger:  "theatre." + role,
		Message: fmt.Sprintf("corrID: %s — %s", s.gen, msg),
	})
}

// emitLocked stamps, validates and records one event. The caller holds s.mu,
// so transcript order and feed order are the same order.
func (s *Stage) emitLocked(ev TranscriptEvent) {
	ev.Gen = s.gen
	ev.normalize()
	if !ev.valid() {
		return
	}
	if err := s.company.AppendTranscript(ev); err != nil {
		ancli.Errf("theatre: transcript write failed: %v", err)
	}
	s.feed.send(ev)
}

// saveLedger writes the ledger atomically. A failure is logged and the
// generation continues — the ledger is telemetry, not the show.
func (s *Stage) saveLedger() {
	s.ledger.UpdatedAt = time.Now()
	if err := s.company.SaveLedger(s.ledger); err != nil {
		ancli.Errf("theatre: ledger write failed: %v", err)
	}
}

// actor finds a role's record, creating it on first sight.
func (s *Stage) actor(role string) *Actor {
	for i := range s.ledger.Actors {
		if s.ledger.Actors[i].Role == role {
			return &s.ledger.Actors[i]
		}
	}
	s.ledger.Actors = append(s.ledger.Actors, Actor{Role: role, Status: "idle"})
	return &s.ledger.Actors[len(s.ledger.Actors)-1]
}

// phaseBody renders the "─ phase N/M … ─" line body from the ledger: the
// phase, the most recently acting role's progress, and the director budget.
func (s *Stage) phaseBody() string {
	l := s.ledger
	var b strings.Builder
	if l.PhaseIndex > 0 {
		fmt.Fprintf(&b, "phase %d/%d %s", l.PhaseIndex, l.PhasesTotal, l.Phase)
	} else {
		fmt.Fprintf(&b, "phase %s", l.Phase)
	}
	if a, ok := s.activeActor(); ok {
		if a.Budget > 0 {
			fmt.Fprintf(&b, " ─ %s %d/%d calls", a.Role, a.Calls, a.Budget)
		} else {
			fmt.Fprintf(&b, " ─ %s %d calls", a.Role, a.Calls)
		}
	}
	fmt.Fprintf(&b, " ─ budget %d/%d", l.Budget.DirectorUsed, l.Budget.DirectorMax)
	return b.String()
}

// activeActor is the most recently acting role, if any.
func (s *Stage) activeActor() (Actor, bool) {
	if s.lastRole == "" {
		return Actor{}, false
	}
	for _, a := range s.ledger.Actors {
		if a.Role == s.lastRole {
			return a, true
		}
	}
	return Actor{}, false
}

// submitBody renders the "✓ submitted …" line body: wall time, director calls
// and consult count.
func (s *Stage) submitBody(title string) string {
	titlePart := title
	if title != "" {
		titlePart = fmt.Sprintf("%q", title)
	}
	consults := 0
	for _, a := range s.ledger.Actors {
		consults += a.Consults
	}
	return fmt.Sprintf("submitted %s — %.1fs, %d calls, %d consults",
		titlePart, time.Since(s.ledger.StartedAt).Seconds(),
		s.ledger.Budget.DirectorUsed, consults)
}

// phaseIndexOf returns the 1-based position of a phase in the production
// flow, or 0 for a phase the flow does not know.
func phaseIndexOf(status string) int {
	for i, p := range phaseOrder {
		if p == status {
			return i + 1
		}
	}
	return 0
}

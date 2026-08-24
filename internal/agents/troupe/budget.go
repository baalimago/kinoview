package troupe

import (
	"context"
	"errors"
	"sync"

	"github.com/baalimago/clai/pkg/text/models"
)

// The termination authority bounds one generation regardless of swarm size or
// spawn depth. Three guards, of which only one is operator-tunable — the
// token stoploss (the -troupeTokenStoploss flag); the global call max and the
// depth cap are hardcoded constants, defense-in-depth rather than operator
// surface. The cooldown between generations is a separate guard: it gates how
// often a generation may start, not how large it grows (decision 13).

const (
	// maxGenerationCalls is the hardcoded global maximum: the most budgeted
	// model calls one generation may make. A budgeted call is a model step
	// that ended with a tool call; the final answer is never budgeted, so an
	// actor's counted calls never show it over its cap.
	maxGenerationCalls = 256

	// maxSpawnDepth is the hardcoded recursion cap: a spawn_role call at
	// depth maxSpawnDepth is refused, never spawned. The director's own run
	// is depth 0; every spawn_role recursion adds one.
	maxSpawnDepth = 8

	// spawnReserveTokens is the nominal token allowance one admitted spawn
	// reserves for the duration of its run: large enough that concurrent
	// spawns cannot jointly pass a near-exhausted stoploss, small enough
	// that a healthy budget keeps the swarm concurrent. A spawn that would
	// cross the threshold reserves the remaining allowance instead, or is
	// refused atomically.
	spawnReserveTokens = 10_000
)

// Budget is the termination authority for one generation: the token
// stoploss with atomic reservation, the hardcoded global call max and the
// depth cap. Every spawn admits against the same budget and every spawned
// agent accounts its model calls into it, so the generation is bounded
// regardless of swarm size or spawn depth. One budget per generation; it is
// never shared across generations.
type Budget struct {
	mu       sync.Mutex
	stoploss int // token cap; <= 0 disables the stoploss

	used     int // counted token usage from completed budgeted calls
	reserved int // token allowance held by in-flight spawns
	calls    int // budgeted model calls, capped at maxGenerationCalls
	depth    int // in-flight spawns, capped at maxSpawnDepth
}

// BudgetOption configures one Budget.
type BudgetOption func(*Budget)

// WithStoploss sets the generation's token stoploss — the value of the
// -troupeTokenStoploss flag. A value <= 0 disables the stoploss, leaving the
// two hardcoded guards in force.
func WithStoploss(tokens int) BudgetOption {
	return func(b *Budget) { b.stoploss = tokens }
}

// NewBudget builds the generation budget. The global call max and the depth
// cap are always on; the token stoploss is off until WithStoploss sets it.
func NewBudget(opts ...BudgetOption) *Budget {
	b := &Budget{}
	for _, o := range opts {
		o(b)
	}
	return b
}

// The three refusal errors. Admit returns them unwrapped so callers can
// classify the guard that refused; the spawner wraps them with the role id
// before they reach the spawning agent.
var (
	// ErrStoploss refuses a spawn when the claimed token usage has reached
	// the generation's stoploss.
	ErrStoploss = errors.New("troupe: spawn refused: token stoploss reached")
	// ErrCallMax refuses a spawn when the generation's budgeted model calls
	// have reached the hardcoded global maximum.
	ErrCallMax = errors.New("troupe: spawn refused: generation call max reached")
	// ErrDepthCap refuses a spawn past the hardcoded recursion cap.
	ErrDepthCap = errors.New("troupe: spawn refused: depth cap reached")
)

// Admit gates one spawn under the termination authority: the depth cap, the
// global call max and the token stoploss are checked and the spawn's token
// allowance is reserved under one lock, so concurrent spawns cannot both
// pass a threshold. A spawn that would cross the stoploss reserves the
// remaining allowance — the last crumbs, never more than the budget holds —
// or is refused atomically. On success Admit returns a release that must be
// called exactly once when the spawn finishes, even on error.
func (b *Budget) Admit() (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.depth >= maxSpawnDepth {
		return nil, ErrDepthCap
	}
	if b.calls >= maxGenerationCalls {
		return nil, ErrCallMax
	}
	var reserved int
	if b.stoploss > 0 {
		remaining := b.stoploss - b.used - b.reserved
		if remaining <= 0 {
			return nil, ErrStoploss
		}
		reserved = min(remaining, spawnReserveTokens)
		b.reserved += reserved
	}
	b.depth++
	return func() { b.release(reserved) }, nil
}

// release ends one spawn: the depth drops and this spawn's reservation is
// returned to the allowance. Real usage already accrued through Record; the
// reservation was only ever a concurrency claim, never a cap.
func (b *Budget) release(reserved int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.depth--
	b.reserved -= reserved
	if b.reserved < 0 {
		b.reserved = 0
	}
}

// Record accrues one completed model call into the generation budget. Only
// budgeted calls count: a model step that ended with a tool call accrues its
// tokens and one call; the final answer (EndedWithReply) and stop-ended
// steps are never budgeted, so telemetry never shows an actor over its cap.
// The call counter is capped at maxGenerationCalls — the hard cap — while
// token usage always accrues: the stoploss gates new spawns, it never
// rewrites history. Record implements models.CallUsageRecorder.
func (b *Budget) Record(_ context.Context, call models.CompletedModelCall) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !call.EndedWithTool {
		return nil
	}
	if b.calls < maxGenerationCalls {
		b.calls++
	}
	if b.stoploss > 0 && call.Usage != nil {
		b.used += call.Usage.TotalTokens
	}
	return nil
}

// Stats is a point-in-time read of the generation budget: the counted token
// usage, the allowance reserved by in-flight spawns, the budgeted model
// calls and the in-flight spawn depth.
type Stats struct {
	Tokens   int
	Reserved int
	Calls    int
	Depth    int
}

// Stats returns a point-in-time read of the generation budget.
func (b *Budget) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{Tokens: b.used, Reserved: b.reserved, Calls: b.calls, Depth: b.depth}
}

// Reset returns the budget to a fresh state for a new generation: the token
// usage, the in-flight reservation, the budgeted call count and the spawn
// depth all return to zero. The facade calls it at the generation boundary
// (before the director runs), so the termination authority bounds one
// generation — never the accumulated history of many. Without it the
// hardcoded call max and the token stoploss would be spent permanently
// after the first few generations.
func (b *Budget) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used = 0
	b.reserved = 0
	b.calls = 0
	b.depth = 0
}

// compile-time proof: the budget is the usage recorder every spawned agent
// accounts into.
var _ models.CallUsageRecorder = (*Budget)(nil)

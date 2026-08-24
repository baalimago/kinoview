package troupe

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
)

// budgetedCall is one model step that ended with a tool call — a budgeted
// call. It accrues tokens and one call against the generation budget.
func budgetedCall(tokens int) models.CompletedModelCall {
	return models.CompletedModelCall{
		EndedWithTool: true,
		Usage:         &models.Usage{TotalTokens: tokens},
	}
}

// finalAnswerCall is one model step that ended with a reply — the final
// answer. It is never budgeted, no matter how large its usage.
func finalAnswerCall(tokens int) models.CompletedModelCall {
	return models.CompletedModelCall{
		EndedWithReply: true,
		Usage:          &models.Usage{TotalTokens: tokens},
	}
}

// stopCall is one model step that ended by a stop condition — neither a tool
// call nor a final answer. It is never budgeted.
func stopCall(tokens int) models.CompletedModelCall {
	return models.CompletedModelCall{
		EndedWithStop: true,
		Usage:         &models.Usage{TotalTokens: tokens},
	}
}

// TestBudget_Stoploss_RefusesCrossingSpawns pins the token stoploss: once
// the claimed usage reaches the stoploss, a new spawn is refused; the final
// answer is never counted against the budget.
func TestBudget_Stoploss_RefusesCrossingSpawns(t *testing.T) {
	const stoploss = 1000
	b := NewBudget(WithStoploss(stoploss))
	ctx := context.Background()

	// One completed spawn that burned 600 budgeted tokens.
	release, err := b.Admit()
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	for _, call := range []models.CompletedModelCall{budgetedCall(400), budgetedCall(200)} {
		if err := b.Record(ctx, call); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	release()

	// The final answer and stop-ended steps are never budgeted: a huge reply
	// moves neither the tokens nor the calls.
	if err := b.Record(ctx, finalAnswerCall(99_999)); err != nil {
		t.Fatalf("Record final answer: %v", err)
	}
	if err := b.Record(ctx, stopCall(99_999)); err != nil {
		t.Fatalf("Record stop-ended step: %v", err)
	}
	if got := b.Stats().Tokens; got != 600 {
		t.Fatalf("Tokens = %d, want 600 (the final answer is never budgeted)", got)
	}
	if got := b.Stats().Calls; got != 2 {
		t.Fatalf("Calls = %d, want 2", got)
	}

	// A spawn that would cross the threshold reserves the remaining allowance
	// — the last 400 tokens — or is refused atomically.
	release2, err := b.Admit()
	if err != nil {
		t.Fatalf("admit with 400 remaining: %v", err)
	}
	if got := b.Stats(); got.Tokens != 600 || got.Reserved != 400 {
		t.Fatalf("after crumbs admission: Tokens=%d Reserved=%d, want 600/400", got.Tokens, got.Reserved)
	}
	// While the crumbs are held, the allowance is fully claimed: no other
	// spawn can pass.
	if _, err := b.Admit(); !errors.Is(err, ErrStoploss) {
		t.Fatalf("concurrent admit while crumbs held: err = %v, want ErrStoploss", err)
	}
	// The crumbs spawn consumes its remaining allowance and finishes: the
	// budget lands exactly on the stoploss.
	if err := b.Record(ctx, budgetedCall(400)); err != nil {
		t.Fatalf("crumbs Record: %v", err)
	}
	release2()

	// Claimed usage at the stoploss refuses new spawns.
	if _, err := b.Admit(); !errors.Is(err, ErrStoploss) {
		t.Fatalf("admit at the spent stoploss: err = %v, want ErrStoploss", err)
	}
	if got := b.Stats(); got.Tokens != stoploss || got.Reserved != 0 {
		t.Fatalf("final: Tokens=%d Reserved=%d, want %d/0", got.Tokens, got.Reserved, stoploss)
	}
}

// TestBudget_Stoploss_Disabled pins that a zero stoploss disables the token
// guard: usage accrues nothing and spawns are never refused for tokens.
func TestBudget_Stoploss_Disabled(t *testing.T) {
	b := NewBudget()
	ctx := context.Background()
	for range 10 {
		if err := b.Record(ctx, budgetedCall(500)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if got := b.Stats().Tokens; got != 0 {
		t.Fatalf("Tokens = %d, want 0 with the stoploss disabled", got)
	}
	if _, err := b.Admit(); err != nil {
		t.Fatalf("admit without a stoploss: %v", err)
	}
}

// TestBudget_Stoploss_ConcurrentSpawnsNeverExceed pins the reservation
// atomicity: N concurrent spawns against a near-exhausted budget never
// jointly claim more than the allowance — exactly one passes, the rest are
// refused with the stoploss error.
func TestBudget_Stoploss_ConcurrentSpawnsNeverExceed(t *testing.T) {
	const (
		stoploss = 1000
		seeded   = 990 // near-exhausted: 10 tokens of allowance left
		spawns   = 32
	)
	b := NewBudget(WithStoploss(stoploss))
	ctx := context.Background()

	release, err := b.Admit()
	if err != nil {
		t.Fatalf("seed admit: %v", err)
	}
	for range seeded / 10 {
		if err := b.Record(ctx, budgetedCall(10)); err != nil {
			t.Fatalf("seed Record: %v", err)
		}
	}
	release()
	if got := b.Stats().Tokens; got != seeded {
		t.Fatalf("seeded Tokens = %d, want %d", got, seeded)
	}

	// N concurrent spawns race for the last 10 tokens.
	var wg sync.WaitGroup
	admitted := make([]func(), 0, spawns)
	refused := 0
	var mu sync.Mutex
	for range spawns {
		wg.Go(func() {
			r, err := b.Admit()
			if err != nil {
				if !errors.Is(err, ErrStoploss) {
					t.Errorf("concurrent admit: err = %v, want ErrStoploss", err)
				}
				mu.Lock()
				refused++
				mu.Unlock()
				return
			}
			mu.Lock()
			admitted = append(admitted, r)
			mu.Unlock()
		})
	}
	wg.Wait()

	// At most one spawn passed, and the claimed allowance never exceeded the
	// stoploss at any point: the holder reserved exactly the last crumbs.
	if len(admitted) != 1 {
		t.Fatalf("admitted = %d concurrent spawns, want exactly 1", len(admitted))
	}
	if refused != spawns-1 {
		t.Fatalf("refused = %d, want %d", refused, spawns-1)
	}
	if got := b.Stats(); got.Tokens+got.Reserved != stoploss {
		t.Fatalf("claimed = %d, want exactly the stoploss %d", got.Tokens+got.Reserved, stoploss)
	}

	// The admitted spawn consumes its crumbs and finishes: the budget lands
	// exactly on the allowance, never above it.
	if err := b.Record(ctx, budgetedCall(10)); err != nil {
		t.Fatalf("crumbs Record: %v", err)
	}
	admitted[0]()
	if got := b.Stats().Tokens; got != stoploss {
		t.Fatalf("final Tokens = %d, want the stoploss %d", got, stoploss)
	}
	if got := b.Stats(); got.Tokens+got.Reserved != stoploss {
		t.Fatalf("final claimed = %d, want %d", got.Tokens+got.Reserved, stoploss)
	}
}

// TestBudget_Stoploss_ConcurrentFreshBudget pins that the reservation does
// not serialize a healthy budget: concurrent spawns are admitted together
// (up to the depth cap) until the nominal reservations approach the
// stoploss.
func TestBudget_Stoploss_ConcurrentFreshBudget(t *testing.T) {
	const stoploss = 1_000_000
	b := NewBudget(WithStoploss(stoploss))

	// The depth cap bounds in-flight spawns; every slot admits on a fresh
	// budget — the stoploss reservation never serializes them.
	releases := make([]func(), 0, maxSpawnDepth)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range maxSpawnDepth {
		wg.Go(func() {
			r, err := b.Admit()
			if err != nil {
				t.Errorf("fresh concurrent admit: %v", err)
				return
			}
			mu.Lock()
			releases = append(releases, r)
			mu.Unlock()
		})
	}
	wg.Wait()
	if len(releases) != maxSpawnDepth {
		t.Fatalf("admitted = %d, want all %d on a fresh budget", len(releases), maxSpawnDepth)
	}
	if got := b.Stats().Reserved; got != maxSpawnDepth*spawnReserveTokens {
		t.Fatalf("Reserved = %d, want %d", got, maxSpawnDepth*spawnReserveTokens)
	}
	if _, err := b.Admit(); !errors.Is(err, ErrDepthCap) {
		t.Fatalf("a fifth concurrent spawn: err = %v, want ErrDepthCap", err)
	}
	for _, r := range releases {
		r()
	}
	if got := b.Stats(); got.Tokens != 0 || got.Reserved != 0 || got.Depth != 0 {
		t.Fatalf("after release: %+v, want all zeros", got)
	}
}

// TestBudget_CallMax_HardCap pins the global maximum: total budgeted calls
// in a generation can never exceed the hardcoded cap, and a spawn is
// refused once it is reached.
func TestBudget_CallMax_HardCap(t *testing.T) {
	b := NewBudget()
	ctx := context.Background()

	// Final answers and stop-ended steps never count as calls.
	if err := b.Record(ctx, finalAnswerCall(1)); err != nil {
		t.Fatalf("Record final answer: %v", err)
	}
	if err := b.Record(ctx, stopCall(1)); err != nil {
		t.Fatalf("Record stop-ended step: %v", err)
	}
	if got := b.Stats().Calls; got != 0 {
		t.Fatalf("Calls = %d, want 0 before any budgeted call", got)
	}

	// Accrue past the cap: the counter is capped, never over.
	for range maxGenerationCalls + 50 {
		if err := b.Record(ctx, budgetedCall(1)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if got := b.Stats().Calls; got != maxGenerationCalls {
		t.Fatalf("Calls = %d, want the hardcoded cap %d", got, maxGenerationCalls)
	}
	if _, err := b.Admit(); !errors.Is(err, ErrCallMax) {
		t.Fatalf("admit at the call cap: err = %v, want ErrCallMax", err)
	}
}

// TestBudget_DepthCap_RefusesPastCap pins the recursion guard: a spawn past
// the hardcoded depth cap is refused, never spawned; releasing a slot admits
// again.
func TestBudget_DepthCap_RefusesPastCap(t *testing.T) {
	b := NewBudget()
	releases := make([]func(), 0, maxSpawnDepth)
	for i := range maxSpawnDepth {
		r, err := b.Admit()
		if err != nil {
			t.Fatalf("admit %d: %v", i, err)
		}
		releases = append(releases, r)
	}
	if got := b.Stats().Depth; got != maxSpawnDepth {
		t.Fatalf("Depth = %d, want %d", got, maxSpawnDepth)
	}
	if _, err := b.Admit(); !errors.Is(err, ErrDepthCap) {
		t.Fatalf("admit past the cap: err = %v, want ErrDepthCap", err)
	}
	// The cap is about in-flight depth, not history: releasing one slot
	// admits again.
	releases[0]()
	r, err := b.Admit()
	if err != nil {
		t.Fatalf("admit after release: %v", err)
	}
	r()
	for _, rel := range releases[1:] {
		rel()
	}
	if got := b.Stats().Depth; got != 0 {
		t.Fatalf("Depth = %d after all releases, want 0", got)
	}
}

// TestBudget_RecorderContract pins the compile-time contract: the budget is
// the usage recorder every spawned agent accounts into.
func TestBudget_RecorderContract(t *testing.T) {
	var _ models.CallUsageRecorder = NewBudget()
}

// TestBudget_Reset pins that Reset returns one generation's budget to a
// fresh state: token usage, reservation, call count and depth all return to
// zero, and a fresh admit gets the full allowance back. The facade calls it
// at the generation boundary so the termination authority never carries over
// between generations.
func TestBudget_Reset(t *testing.T) {
	b := NewBudget(WithStoploss(500))
	ctx := context.Background()

	release, err := b.Admit()
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := b.Record(ctx, budgetedCall(300)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := b.Record(ctx, budgetedCall(100)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	release()

	if got := b.Stats(); got.Tokens != 400 || got.Calls != 2 {
		t.Fatalf("pre-reset = %+v, want 400 tokens and 2 calls accrued", got)
	}

	b.Reset()
	if got := b.Stats(); got.Tokens != 0 || got.Reserved != 0 || got.Calls != 0 || got.Depth != 0 {
		t.Fatalf("post-reset = %+v, want all zeros", got)
	}

	// A fresh admit gets the full allowance back: the stoploss is intact, so
	// a spawn that would have been refused by the spent history is admitted.
	r2, err := b.Admit()
	if err != nil {
		t.Fatalf("admit after reset: %v", err)
	}
	if got := b.Stats().Reserved; got != 500 {
		t.Fatalf("Reserved after fresh admit = %d, want the full %d allowance", got, 500)
	}
	r2()
}

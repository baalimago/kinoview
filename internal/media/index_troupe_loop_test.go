package media

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeTrigger stands in for the troupe facade: it records the reasons the
// loop asked for a generation and the moment each one started, so a test can
// pin the trigger and the cadence without running a swarm.
type fakeTrigger struct {
	mu       sync.Mutex
	warmed   int
	starts   []time.Time
	reasons  []string
	cooldown time.Duration
}

func (f *fakeTrigger) Warm(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warmed++
}

func (f *fakeTrigger) Prepare(_ context.Context, reason string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reasons = append(f.reasons, reason)
	f.starts = append(f.starts, time.Now())
	return true
}

func (f *fakeTrigger) Cooldown() time.Duration { return f.cooldown }

func (f *fakeTrigger) snapshot() (int, []string, []time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.warmed, append([]string(nil), f.reasons...), append([]time.Time(nil), f.starts...)
}

// troupeLoopIndexer builds the smallest indexer the loop touches.
func troupeLoopIndexer(trigger *fakeTrigger, startupDelay time.Duration) *Indexer {
	return &Indexer{
		troupe:                trigger,
		audience:              make(chan struct{}, 1),
		conciergeStartupDelay: startupDelay,
	}
}

// TestRunTroupeLoop_IdleGeneratesNothing pins the new trigger: no visitor, no
// generation — an idle server spends no tokens regardless of how long it runs.
func TestRunTroupeLoop_IdleGeneratesNothing(t *testing.T) {
	trigger := &fakeTrigger{cooldown: 20 * time.Millisecond}
	idx := troupeLoopIndexer(trigger, time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go idx.runTroupeLoop(ctx)

	time.Sleep(100 * time.Millisecond)
	warmed, reasons, _ := trigger.snapshot()
	if warmed != 1 {
		t.Errorf("Warm calls = %d, want 1", warmed)
	}
	if len(reasons) != 0 {
		t.Errorf("generations = %v, want none without an audience", reasons)
	}
}

// TestRunTroupeLoop_AudienceTriggersGeneration pins the wiring: one audience
// signal runs one generation, named for what triggered it.
func TestRunTroupeLoop_AudienceTriggersGeneration(t *testing.T) {
	trigger := &fakeTrigger{cooldown: time.Minute}
	idx := troupeLoopIndexer(trigger, time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go idx.runTroupeLoop(ctx)

	idx.noteAudience()
	waitFor(t, func() bool {
		_, reasons, _ := trigger.snapshot()
		return len(reasons) == 1
	})
	if _, reasons, _ := trigger.snapshot(); reasons[0] != "audience" {
		t.Errorf("reason = %q, want audience", reasons[0])
	}
}

// TestRunTroupeLoop_OneGenerationPerCooldown pins the cadence: a visitor who
// arrives during the cooldown is remembered by the buffered signal and served
// when the cooldown ends; a third arrival in the same window is still the same
// proof, so no extra generation runs.
func TestRunTroupeLoop_OneGenerationPerCooldown(t *testing.T) {
	const cooldown = 80 * time.Millisecond
	trigger := &fakeTrigger{cooldown: cooldown}
	idx := troupeLoopIndexer(trigger, time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go idx.runTroupeLoop(ctx)

	idx.noteAudience()
	waitFor(t, func() bool {
		_, reasons, _ := trigger.snapshot()
		return len(reasons) == 1
	})

	idx.noteAudience()
	idx.noteAudience()
	waitFor(t, func() bool {
		_, reasons, _ := trigger.snapshot()
		return len(reasons) == 2
	})

	time.Sleep(cooldown)
	_, reasons, starts := trigger.snapshot()
	if len(reasons) != 2 {
		t.Fatalf("generations = %d, want 2 (one per cooldown)", len(reasons))
	}
	if gap := starts[1].Sub(starts[0]); gap < cooldown {
		t.Errorf("gap between generations = %v, want at least %v", gap, cooldown)
	}
}

// TestRunTroupeLoop_StopsWithContext pins shutdown: a cancelled server leaves
// no generation loop behind.
func TestRunTroupeLoop_StopsWithContext(t *testing.T) {
	trigger := &fakeTrigger{cooldown: time.Minute}
	idx := troupeLoopIndexer(trigger, time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		idx.runTroupeLoop(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runTroupeLoop did not return after cancellation")
	}
}

// TestNoteAudience_Coalesces pins the buffer: a refresh, a reconnect storm and
// a second tab all collapse into one buffered proof of an audience.
func TestNoteAudience_Coalesces(t *testing.T) {
	idx := &Indexer{audience: make(chan struct{}, 1)}
	for range 5 {
		idx.noteAudience()
	}
	if got := len(idx.audience); got != 1 {
		t.Errorf("audience buffer = %d, want 1", got)
	}
}

// TestWithTroupe_NilStaysNil pins the disabled state: a nil facade must leave
// the interface nil, so the loop is never started for a troupe that has no
// director to drive.
func TestWithTroupe_NilStaysNil(t *testing.T) {
	idx := &Indexer{}
	WithTroupe(nil)(idx)
	if idx.troupe != nil {
		t.Fatalf("troupe = %#v, want a nil interface for a disabled troupe", idx.troupe)
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before the deadline")
}

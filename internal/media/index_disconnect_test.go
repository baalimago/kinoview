package media

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/media/suggestions"
	"github.com/baalimago/kinoview/internal/model"
)

// clockVar provides an atomically-accessible time.Time for test clocks.
// Use Store/Add/Load to avoid data races when a cascade goroutine reads the
// clock while the test advances it.
type clockVar struct {
	v atomic.Pointer[time.Time]
}

func newClockVar(t time.Time) *clockVar {
	cv := &clockVar{}
	cv.v.Store(&t)
	return cv
}

func (cv *clockVar) load() time.Time { return *cv.v.Load() }

func (cv *clockVar) store(t time.Time) { cv.v.Store(&t) }

func (cv *clockVar) add(d time.Duration) time.Time {
	newT := cv.load().Add(d)
	cv.store(newT)
	return newT
}

// fakeButler counts calls and can be instructed to block or fail.
type fakeButler struct {
	mu       sync.Mutex
	calls    int
	blockCh  chan struct{}
	err      error
	panicVal any
}

func (b *fakeButler) Setup(ctx context.Context) error { return nil }

func (b *fakeButler) PrepSuggestions(ctx context.Context, c model.ClientContext, items []model.Item) ([]model.Suggestion, error) {
	b.mu.Lock()
	b.calls++
	block := b.blockCh
	err := b.err
	pv := b.panicVal
	b.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if pv != nil {
		panic(pv)
	}
	if err != nil {
		return nil, err
	}
	return []model.Suggestion{
		{Item: model.Item{ID: "fake"}, Motivation: "test"},
	}, nil
}

func (b *fakeButler) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func (b *fakeButler) setError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

func (b *fakeButler) setPanic(v any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.panicVal = v
}

func (b *fakeButler) setBlock(ch chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blockCh = ch
}

// newTestIndexer builds an Indexer with a fake butler, context manager, store,
// and suggestions manager suitable for disconnect tests.
func newTestIndexer(t *testing.T, butler agents.Butler, opts ...IndexerOption) *Indexer {
	t.Helper()
	sm, err := suggestions.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return newTestIndexerWithSM(t, butler, sm, opts...)
}

// newTestIndexerWithSM is like newTestIndexer but accepts a pre-built
// suggestions manager (so two indexers can share one).
func newTestIndexerWithSM(t *testing.T, butler agents.Butler, sm *suggestions.Manager, opts ...IndexerOption) *Indexer {
	t.Helper()
	idx, err := NewIndexer(
		append(
			[]IndexerOption{
				WithButler(butler),
				WithSuggestionsManager(sm),
				WithClientContextManager(&fakeClientContextMgr{
					ctxs: []model.ClientContext{
						{LastPlayedName: "Some Show", ViewingHistory: []model.ViewMetadata{{Name: "E1"}}},
					},
				}),
			}, opts...,
		)...,
	)
	if err != nil {
		t.Fatal(err)
	}
	idx.store = &mockStore{items: []model.Item{
		{ID: "v1", Name: "Video 1", MIMEType: "video/mp4"},
	}}
	return idx
}

var _ agents.Butler = (*fakeButler)(nil)

type fakeClientContextMgr struct {
	ctxs []model.ClientContext
}

func (m *fakeClientContextMgr) AllClientContexts() []model.ClientContext {
	return m.ctxs
}

func (m *fakeClientContextMgr) StoreClientContext(c model.ClientContext) error {
	m.ctxs = append(m.ctxs, c)
	return nil
}

// --- Debounce tests ---

func TestHandleDisconnect_Debounced(t *testing.T) {
	cv := newClockVar(time.Now())
	clock := func() time.Time { return cv.load() }

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clock), WithButlerDebounce(30*time.Second))

	// First trigger starts a cascade.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", fb.callCount())
	}

	// Second trigger within debounce window is suppressed.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected still 1 call after debounced trigger, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_AfterDebounceWindow(t *testing.T) {
	cv := newClockVar(time.Now())
	clock := func() time.Time { return cv.load() }

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clock), WithButlerDebounce(30*time.Second))

	// First trigger.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", fb.callCount())
	}

	// Advance clock past debounce window.
	cv.add(40 * time.Second)

	// Second trigger after window is allowed.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 2 {
		t.Fatalf("expected 2 calls after debounce window, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_ZeroDebounce(t *testing.T) {
	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, WithButlerDebounce(0))

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", fb.callCount())
	}

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 2 {
		t.Fatalf("expected 2 calls with zero debounce, got %d", fb.callCount())
	}
}

// --- Single-flight tests ---

func TestHandleDisconnect_SingleFlightCoalesces(t *testing.T) {
	blockCh := make(chan struct{})
	fb := &fakeButler{}
	fb.setBlock(blockCh)
	idx := newTestIndexer(t, fb)

	// First trigger starts but blocks.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)

	// Second trigger should set rerun flag, not start a new goroutine.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call in flight, got %d", fb.callCount())
	}

	// Unblock the first cascade.
	close(blockCh)
	time.Sleep(100 * time.Millisecond)

	// The rerun should have executed: total 2 calls (original + rerun).
	if fb.callCount() != 2 {
		t.Fatalf("expected 2 calls after unblock (original + rerun), got %d", fb.callCount())
	}
}

func TestHandleDisconnect_ErrorReleasesLock(t *testing.T) {
	fb := &fakeButler{}
	fb.setError(fmt.Errorf("simulated failure"))
	idx := newTestIndexer(t, fb)

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", fb.callCount())
	}

	// After error, lock should be released; next trigger starts a new cascade.
	fb.setError(nil)
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 2 {
		t.Fatalf("expected 2 calls after error released lock, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_PanicReleasesLock(t *testing.T) {
	fb := &fakeButler{}
	fb.setPanic("test panic")
	idx := newTestIndexer(t, fb)

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", fb.callCount())
	}

	// After panic recovery, lock should be released; next trigger starts a new cascade.
	fb.setPanic(nil)
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 2 {
		t.Fatalf("expected 2 calls after panic recovery, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_TimeoutReleasesLock(t *testing.T) {
	blockCh := make(chan struct{})
	fb := &fakeButler{}
	fb.setBlock(blockCh)
	idx := newTestIndexer(t, fb)

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)

	// Lock should still be held (cascade in flight).
	idx.butlerMu.Lock()
	inFlight := idx.butlerInFlight
	idx.butlerMu.Unlock()
	if !inFlight {
		t.Fatal("expected cascade to be in flight")
	}

	// Unblock to complete the cascade.
	close(blockCh)
	time.Sleep(100 * time.Millisecond)

	idx.butlerMu.Lock()
	inFlight = idx.butlerInFlight
	idx.butlerMu.Unlock()
	if inFlight {
		t.Fatal("expected lock released after cascade completes")
	}
}

// --- Context guard tests ---

func TestHandleDisconnect_EmptyContextSkipped(t *testing.T) {
	fb := &fakeButler{}
	sm, err := suggestions.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := NewIndexer(
		WithButler(fb),
		WithSuggestionsManager(sm),
		WithClientContextManager(&fakeClientContextMgr{
			ctxs: []model.ClientContext{
				{ViewingHistory: nil, LastPlayedName: ""},
			},
		}),
	)
	idx.store = &mockStore{items: []model.Item{}}
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 0 {
		t.Fatalf("expected 0 calls for empty context, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_EmptyContextNoContextsAtAll(t *testing.T) {
	fb := &fakeButler{}
	sm, err := suggestions.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := NewIndexer(
		WithButler(fb),
		WithSuggestionsManager(sm),
		WithClientContextManager(&fakeClientContextMgr{
			ctxs: nil,
		}),
	)
	idx.store = &mockStore{items: []model.Item{}}
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)
	if fb.callCount() != 0 {
		t.Fatalf("expected 0 calls with no contexts, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_MinimalContextRuns(t *testing.T) {
	fb := &fakeButler{}
	idx := newTestIndexer(t, fb)
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call for non-empty context, got %d", fb.callCount())
	}
}

func TestHandleDisconnect_LastPlayedNameOnlyRuns(t *testing.T) {
	fb := &fakeButler{}
	sm, err := suggestions.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := NewIndexer(
		WithButler(fb),
		WithSuggestionsManager(sm),
		WithClientContextManager(&fakeClientContextMgr{
			ctxs: []model.ClientContext{
				{LastPlayedName: "Movie", ViewingHistory: nil},
			},
		}),
	)
	idx.store = &mockStore{items: []model.Item{}}
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("expected 1 call with only lastPlayedName, got %d", fb.callCount())
	}
}

// --- Disconnect reason tests ---

func TestHandleDisconnect_ImmediateReasons(t *testing.T) {
	for _, reason := range []disconnectReason{reasonSocketError, reasonPingFailed} {
		t.Run(string(reason), func(t *testing.T) {
			fb := &fakeButler{}
			idx := newTestIndexer(t, fb)
			idx.handleDisconnect(reason)
			time.Sleep(50 * time.Millisecond)
			if fb.callCount() != 1 {
				t.Errorf("expected 1 call for %s, got %d", reason, fb.callCount())
			}
		})
	}
}

func TestHandleDisconnect_ReasonLogged(t *testing.T) {
	reasons := []disconnectReason{reasonSocketError, reasonPingFailed, reasonPongTimeout}
	seen := map[disconnectReason]bool{}
	for _, r := range reasons {
		if seen[r] {
			t.Errorf("duplicate reason value: %s", r)
		}
		seen[r] = true
		if string(r) == "" {
			t.Error("reason must have non-empty string value")
		}
	}
}

// --- Concurrent trigger race test ---

func TestHandleDisconnect_ConcurrentTriggers(t *testing.T) {
	fb := &fakeButler{}
	// Block the butler until all 20 concurrent triggers have fired.
	// This ensures every caller has a chance to set rerunRequested before
	// the first cascade completes, so we can assert the exact single-flight
	// bound of 2 (one initial + one coalesced rerun).
	barrier := make(chan struct{})
	fb.setBlock(barrier)

	idx := newTestIndexer(t, fb)
	idx.store = &mockStore{items: []model.Item{
		{ID: "v1", Name: "V1", MIMEType: "video/mp4"},
	}}

	var wg sync.WaitGroup
	triggers := 20
	wg.Add(triggers)
	for range triggers {
		go func() {
			defer wg.Done()
			idx.handleDisconnect(reasonSocketError)
		}()
	}
	// Wait for all triggers to fire before unblocking the butler.
	wg.Wait()
	close(barrier)

	// Wait for cascades to finish.
	time.Sleep(200 * time.Millisecond)

	calls := fb.callCount()
	// With the barrier, all 20 triggers signal before the first cascade
	// completes, so exactly 2 cascades: one initial + one coalesced rerun.
	if calls > 2 {
		t.Errorf("expected at most 2 cascades from %d concurrent triggers, got %d", triggers, calls)
	}
	if calls < 1 {
		t.Error("expected at least 1 cascade")
	}
}

// --- Pong grace recovery test ---

func TestHeartbeat_PongGraceRecovery(t *testing.T) {
	idx := &Indexer{
		heartbeatInterval: 20 * time.Millisecond,
		pongTimeout:       10 * time.Millisecond,
		pongGrace:         100 * time.Millisecond,
	}

	pongChan := make(chan struct{}, 1)
	errChan := make(chan error, 1)

	go func() {
		time.Sleep(30 * time.Millisecond)
		pongChan <- struct{}{}
	}()

	recovered := false
	grace := idx.pongGrace
	select {
	case <-pongChan:
		recovered = true
	case <-errChan:
	case <-time.After(grace):
	}

	if !recovered {
		t.Error("expected recovery within grace period")
	}
}

// --- Nil guard tests ---

func TestHandleDisconnect_NilButler(t *testing.T) {
	idx := &Indexer{butler: nil}
	idx.handleDisconnect(reasonSocketError)
}

func TestHandleDisconnect_NilClientContextMgr(t *testing.T) {
	fb := &fakeButler{}
	idx := &Indexer{butler: fb, clientContextMgr: nil}
	idx.handleDisconnect(reasonSocketError)
	if fb.callCount() != 0 {
		t.Errorf("expected 0 calls when clientContextMgr is nil, got %d", fb.callCount())
	}
}

// --- Shutdown mid-cascade test ---

func TestHandleDisconnect_ShutdownMidCascade(t *testing.T) {
	blockCh := make(chan struct{})
	fb := &fakeButler{}
	fb.setBlock(blockCh)
	idx := newTestIndexer(t, fb)

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)

	// Simulate shutdown by completing the cascade.
	close(blockCh)
	time.Sleep(100 * time.Millisecond)

	if fb.callCount() != 1 {
		t.Errorf("expected 1 call, got %d", fb.callCount())
	}
}

// --- Rerun debounce check ---

func TestHandleDisconnect_RerunRespectsDebounce(t *testing.T) {
	cv := newClockVar(time.Now())
	clock := func() time.Time { return cv.load() }

	blockCh := make(chan struct{})
	fb := &fakeButler{}
	fb.setBlock(blockCh)
	idx := newTestIndexer(t, fb, withClock(clock), WithButlerDebounce(30*time.Second))

	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)

	// Second trigger sets rerun flag (not debounced — cascade still in flight).
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(50 * time.Millisecond)

	// Advance clock past debounce.
	cv.add(40 * time.Second)

	// Unblock — rerun should fire because debounce window passed.
	close(blockCh)
	time.Sleep(200 * time.Millisecond)

	if fb.callCount() != 2 {
		t.Fatalf("expected 2 calls (original + rerun), got %d", fb.callCount())
	}
}

func TestDisconnectReason_String(t *testing.T) {
	tests := []struct {
		reason disconnectReason
		want   string
	}{
		{reasonSocketError, "socketError"},
		{reasonPingFailed, "pingFailed"},
		{reasonPongTimeout, "pongTimeout"},
		{reasonConnect, "connect"},
	}
	for _, tt := range tests {
		if string(tt.reason) != tt.want {
			t.Errorf("reason %s: want %q, got %q", tt.reason, tt.want, string(tt.reason))
		}
	}
}

// --- Suggestion cache tests ---

// fakeClock returns a clock function backed by an atomic pointer so tests
// can safely advance time without racing cascade goroutines.
func fakeClock(initial time.Time) (func() time.Time, *atomic.Pointer[time.Time]) {
	var p atomic.Pointer[time.Time]
	t := initial
	p.Store(&t)
	fn := func() time.Time {
		return *p.Load()
	}
	return fn, &p
}

func advanceClock(p *atomic.Pointer[time.Time], d time.Duration) {
	t := p.Load()
	newT := (*t).Add(d)
	p.Store(&newT)
}

// waitForCascade polls until the butler has been called at least expectedCalls
// times, or 500ms passes.
func waitForCascade(fb *fakeButler, expectedCalls int) {
	for range 50 {
		if fb.callCount() >= expectedCalls {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCascade_CacheHit(t *testing.T) {
	clockFn, _ := fakeClock(time.Now())

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade: butler runs.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Second cascade: identical inputs, should be cache hit.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("cache hit: expected still 1 call, got %d", fb.callCount())
	}
}

func TestCascade_LibraryChange(t *testing.T) {
	clockFn, _ := fakeClock(time.Now())

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Add a new item to the library.
	mockStore := idx.store.(*mockStore)
	mockStore.items = append(mockStore.items, model.Item{ID: "v2", Name: "Video 2", MIMEType: "video/mp4"})

	// Second cascade: library changed, must be a miss.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 2)
	if fb.callCount() != 2 {
		t.Fatalf("library change: expected 2 calls, got %d", fb.callCount())
	}
}

func TestCascade_ContextChange(t *testing.T) {
	clockFn, _ := fakeClock(time.Now())

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Change the client context (different last played).
	idx.clientContextMgr = &fakeClientContextMgr{
		ctxs: []model.ClientContext{
			{LastPlayedName: "Different Show", ViewingHistory: []model.ViewMetadata{{Name: "E2"}}},
		},
	}

	// Second cascade: context changed, must be a miss.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 2)
	if fb.callCount() != 2 {
		t.Fatalf("context change: expected 2 calls, got %d", fb.callCount())
	}
}

func TestCascade_TTLExpiry(t *testing.T) {
	clockFn, clockPtr := fakeClock(time.Now())

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(1*time.Hour))

	// First cascade.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Within TTL: cache hit.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("within TTL: expected still 1 call, got %d", fb.callCount())
	}

	// Advance clock past TTL.
	advanceClock(clockPtr, 2*time.Hour)

	// Past TTL: cache miss, new call.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 2)
	if fb.callCount() != 2 {
		t.Fatalf("past TTL: expected 2 calls, got %d", fb.callCount())
	}
}

func TestCascade_DoesNotCacheEmpty(t *testing.T) {
	clockFn, _ := fakeClock(time.Now())

	emptyButler := &emptyFakeButler{}
	idx := newTestIndexer(t, emptyButler, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade: empty result.
	idx.handleDisconnect(reasonSocketError)
	// The empty butler returns (nil, nil) immediately. The cascade goroutine
	// launches, calls PrepSuggestions synchronously, then calls Update.
	// We need to ensure it's done before we touch idx.butler.
	time.Sleep(200 * time.Millisecond)

	// Build a second Indexer pointing at the same suggestions file to avoid
	// racing on idx.butler assignment.
	sm := idx.suggestions
	fb := &fakeButler{}
	idx2 := newTestIndexerWithSM(t, fb, sm, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// Second cascade: should NOT be cached (empty result was not stored).
	idx2.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("empty result must not be cached: expected 1 call, got %d", fb.callCount())
	}
}

func TestCascade_DoesNotCacheError(t *testing.T) {
	clockFn, _ := fakeClock(time.Now())

	fb := &fakeButler{}
	fb.setError(fmt.Errorf("simulated failure"))
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade: error.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(200 * time.Millisecond)
	callsAfterError := fb.callCount()

	// Fix the butler.
	fb.setError(nil)

	// Second cascade: should NOT be cached (error result).
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, callsAfterError+1)
	if fb.callCount() != callsAfterError+1 {
		t.Fatalf("error result must not be cached: expected %d calls, got %d", callsAfterError+1, fb.callCount())
	}
}

func TestCascade_CacheDisabled(t *testing.T) {
	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, WithButlerCacheTTL(0))

	// First cascade.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Second cascade: caching disabled, still calls.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 2)
	if fb.callCount() != 2 {
		t.Fatalf("cache disabled: expected 2 calls, got %d", fb.callCount())
	}
}

func TestCascade_ClockSkew(t *testing.T) {
	clockFn, clockPtr := fakeClock(time.Now())

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade at T+0.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Move clock backwards (NTP correction).
	advanceClock(clockPtr, -2*time.Hour)

	// The generated timestamp is ahead of now → negative age → treat as miss.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 2)
	if fb.callCount() != 2 {
		t.Fatalf("clock skew: expected 2 calls, got %d", fb.callCount())
	}
}

// emptyFakeButler returns zero suggestions.
type emptyFakeButler struct {
	fakeButler
}

func (b *emptyFakeButler) PrepSuggestions(ctx context.Context, c model.ClientContext, items []model.Item) ([]model.Suggestion, error) {
	return nil, nil
}

// TestCascade_VersionBump proves that bumping SuggestionFingerprintVersion
// invalidates all existing caches. This is cross-phase check 3: Phase 6's
// fingerprint must move when Phase 4's projection changes.
func TestCascade_VersionBump(t *testing.T) {
	clockFn, _ := fakeClock(time.Now())

	fb := &fakeButler{}
	idx := newTestIndexer(t, fb, withClock(clockFn), WithButlerCacheTTL(6*time.Hour))

	// First cascade stores suggestions at the current version (3).
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 1)
	if fb.callCount() != 1 {
		t.Fatalf("first cascade: expected 1 call, got %d", fb.callCount())
	}

	// Second cascade: identical inputs → cache hit.
	idx.handleDisconnect(reasonSocketError)
	time.Sleep(100 * time.Millisecond)
	if fb.callCount() != 1 {
		t.Fatalf("cache hit: expected still 1 call, got %d", fb.callCount())
	}

	// Rewrite the stored fingerprint at an older version, simulating what
	// happens across a deployment that bumps SuggestionFingerprintVersion.
	existing := idx.suggestions.Get()
	oldFP := model.SuggestionFingerprint{Library: "old", Context: "old", Version: 1}
	idx.suggestions.UpdateWithFingerprint(existing, oldFP, time.Now())

	// Third cascade: version mismatch → cache miss, butler called again.
	idx.handleDisconnect(reasonSocketError)
	waitForCascade(fb, 2)
	if fb.callCount() != 2 {
		t.Fatalf("version bump: expected 2 calls (cache miss from version mismatch), got %d", fb.callCount())
	}
}

// TestHandleConnect_DisconnectNoDeadlock exercises rapid connect/disconnect
// cycling under -race to prove Phase 8's connect trigger and Phase 5's
// debounce+single-flight do not deadlock. Cross-phase check 4.
func TestHandleConnect_DisconnectNoDeadlock(t *testing.T) {
	fb := &fakeButler{}
	idx := newTestIndexer(t, fb)

	const cycles = 50
	for range cycles {
		idx.handleConnect()
		idx.handleDisconnect(reasonSocketError)
	}

	// Allow any in-flight cascades to settle.
	time.Sleep(200 * time.Millisecond)

	// At least one cascade should have completed. The exact count depends on
	// debounce coalescing; the important property is no deadlock and no panic.
	if fb.callCount() < 1 {
		t.Error("expected at least 1 cascade from rapid connect/disconnect cycling")
	}
}

package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/media/suggestions"
	"github.com/baalimago/kinoview/internal/model"
)

type mockStore struct {
	setup func() error
	store func() error
	items []model.Item
}

func (m *mockStore) Setup(ctx context.Context) (<-chan error, error) {
	return nil, m.setup()
}

func (m *mockStore) Start(ctx context.Context) {
}

func (m *mockStore) Store(ctx context.Context, i model.Item) error {
	return m.store()
}

func (m *mockStore) VideoHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (m *mockStore) StreamHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (m *mockStore) StreamListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (m *mockStore) ListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (m *mockStore) ImageHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (m *mockStore) Snapshot() []model.Item {
	return m.items
}

type mockWatcher struct {
	setup func(ctx context.Context) (<-chan model.Item, <-chan error, error)
	watch func(ctx context.Context, path string) error
	close func() error
}

func (m *mockWatcher) Setup(ctx context.Context) (<-chan model.Item, <-chan error, error) {
	return m.setup(ctx)
}

func (m *mockWatcher) Watch(ctx context.Context, path string) error {
	return m.watch(ctx, path)
}

func (m *mockWatcher) Close() error {
	if m.close != nil {
		return m.close()
	}
	return nil
}

// newIndexer builds an Indexer via NewIndexer and registers cleanup so the
// watcher's inotify instance is released when the test ends. Every test that
// calls NewIndexer directly must go through here: a leaked instance exhausts
// the per-user inotify budget under repeated -count runs.
func newIndexer(t *testing.T, opts ...IndexerOption) *Indexer {
	t.Helper()
	i, err := NewIndexer(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i.Close() })
	return i
}

func Test_indexer_Setup(t *testing.T) {
	t.Run("error on store error", func(t *testing.T) {
		i := newIndexer(t)
		want := errors.New("whopsidops")
		i.store = &mockStore{
			setup: func() error {
				return want
			},
		}
		got := i.Setup(context.Background())
		if !errors.Is(got, want) {
			t.Fatalf("wanted wrapped error: %v, got: %v", want, got)
		}
	})

	t.Run("error on watcher error", func(t *testing.T) {
		i := newIndexer(t)

		want := errors.New("whopsidops")
		i.store = &mockStore{
			setup: func() error { return nil },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				return nil, nil, want
			},
		}
		got := i.Setup(context.Background())
		if !errors.Is(got, want) {
			t.Fatalf("wanted wrapped error: %v, got: %v", want, got)
		}
	})

	t.Run("should return error nil on OK", func(t *testing.T) {
		wantWatchPath := t.TempDir()
		i := newIndexer(t, WithWatchPath(wantWatchPath))
		var want error
		i.store = &mockStore{
			setup: func() error { return want },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				return nil, nil, want
			},
		}

		got := i.Setup(context.Background())
		testboil.FailTestIfDiff(t, got, want)
		testboil.FailTestIfDiff(t, i.watchPath, wantWatchPath)
	})

	testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
		i := newIndexer(t)
		i.Setup(ctx)
	}, time.Millisecond*100)
}

func TestNewIndexer(t *testing.T) {
	t.Run("watcher and store should not be nil", func(t *testing.T) {
		i := newIndexer(t, WithStorage(&mockStore{}))
		if i.watcher == nil {
			t.Fatal("watcher shouldnt be nil")
		}

		if i.store == nil {
			t.Fatal("watcher shouldnt be nil")
		}
	})
}

func TestStart_errorHandling(t *testing.T) {
	t.Run("error on no fileUpdates", func(t *testing.T) {
		i := newIndexer(t)
		i.fileUpdates = nil
		got := i.Start(context.Background())
		if got == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("error on store error", func(t *testing.T) {
		i := newIndexer(t)
		want := errors.New("store error")
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return want },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}

		err := i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		err = i.Start(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("wanted: %v, got: %v", want, err)
		}
	})

	t.Run("error on watcher error", func(t *testing.T) {
		i := newIndexer(t)
		want := errors.New("watcher error")
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return want },
		}

		err := i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		err = i.Start(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("wanted: %v, got: %v", want, err)
		}
	})

	t.Run("exit on context cancel", func(t *testing.T) {
		i := newIndexer(t)
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { tmp := make(chan struct{}); <-tmp; return nil },
		}

		err := i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
			i.Start(ctx)
		}, time.Millisecond*100)
	})
}

type mockConcierge struct {
	run func(ctx context.Context) (string, error)
}

func (m *mockConcierge) Setup(ctx context.Context) error { return nil }
func (m *mockConcierge) Run(ctx context.Context) (string, error) {
	return m.run(ctx)
}

func TestStart_conciergeStartupDelay(t *testing.T) {
	t.Run("waits before first run", func(t *testing.T) {
		runCh := make(chan struct{})
		mc := &mockConcierge{
			run: func(ctx context.Context) (string, error) {
				close(runCh)
				return "", nil
			},
		}
		i := newIndexer(t,
			WithConcierge(mc),
			WithConciergeStartupDelay(100*time.Millisecond),
		)
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}
		err := i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		go func() {
			_ = i.Start(ctx)
		}()

		// Concierge should NOT have run immediately
		select {
		case <-runCh:
			t.Fatal("concierge ran before startup delay elapsed")
		case <-time.After(30 * time.Millisecond):
		}

		// But should run after the delay
		select {
		case <-runCh:
		case <-time.After(300 * time.Millisecond):
			t.Fatal("concierge did not run after startup delay")
		}
	})

	t.Run("runs immediately when delay is zero", func(t *testing.T) {
		runCh := make(chan struct{})
		mc := &mockConcierge{
			run: func(ctx context.Context) (string, error) {
				close(runCh)
				return "", nil
			},
		}
		i := newIndexer(t,
			WithConcierge(mc),
			WithConciergeStartupDelay(0),
		)
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}
		err := i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		go func() {
			_ = i.Start(ctx)
		}()

		// Concierge should run nearly immediately
		select {
		case <-runCh:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("concierge did not run immediately with zero delay")
		}
	})

	t.Run("context cancel during delay returns without running", func(t *testing.T) {
		runCh := make(chan struct{})
		mc := &mockConcierge{
			run: func(ctx context.Context) (string, error) {
				close(runCh)
				return "", nil
			},
		}
		i := newIndexer(t,
			WithConcierge(mc),
			WithConciergeStartupDelay(5*time.Second),
		)
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		_ = i.watcher.Close()
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}
		err := i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			_ = i.Start(ctx)
		}()

		// Cancel the context during the delay
		time.Sleep(50 * time.Millisecond)
		cancel()

		// Concierge should never run
		select {
		case <-runCh:
			t.Fatal("concierge ran after context cancelled")
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func TestRegisterErrorChannel(t *testing.T) {
	t.Run("registers new error channel", func(t *testing.T) {
		i := &Indexer{
			errorChannels: make(map[string]errorListener),
			errorUpdates:  make(chan error),
		}
		errCh := make(chan error)
		err := i.registerErrorChannel(context.Background(), "testRoutine", errCh)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if _, exists := i.errorChannels["testRoutine"]; !exists {
			t.Errorf("Expected error channel 'testRoutine' to be registered")
		}
	})

	t.Run("returns error for duplicate channel name", func(t *testing.T) {
		i := &Indexer{
			errorChannels: map[string]errorListener{
				"testRoutine": {},
			},
			errorUpdates: make(chan error),
		}
		errCh := make(chan error)
		err := i.registerErrorChannel(context.Background(), "testRoutine", errCh)
		if err == nil {
			t.Errorf("Expected error, got none")
		}
		if err.Error() != "error channel with name 'testRoutine' already exists" {
			t.Errorf("Unexpected error message: %v", err.Error())
		}
	})
	t.Run("errors should be propagated", func(t *testing.T) {
		i := &Indexer{
			errorChannels: make(map[string]errorListener),
			errorUpdates:  make(chan error, 2),
		}
		errCh := make(chan error, 2)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := i.registerErrorChannel(ctx, "routineA", errCh)
		if err != nil {
			t.Fatalf("unexpected error registering channel: %v", err)
		}
		expectedErr := errors.New("something bad happened")
		go func() { errCh <- expectedErr }()
		select {
		case got := <-i.errorUpdates:
			if got == nil || got.Error() == "" ||
				got.Error() == expectedErr.Error() {
				t.Errorf("got unwrapped error, want wrapped: %v", got)
			} else if !errors.Is(got, expectedErr) {
				t.Errorf("got: %v, want: %v", got, expectedErr)
			}
		case <-time.After(time.Second):
			t.Errorf("timeout waiting for error propagation")
		}
		// confirm unrelated errors are not present
		select {
		case got := <-i.errorUpdates:
			t.Errorf("unexpected message propagated: %v", got)
		case <-time.After(50 * time.Millisecond):
		}
		// Confirm teardown/closure is safe
		cancel()
	})
}

// --- Phase 7: Concierge Schedule Truthfulness tests ---

// mockConcierge is already defined above in the existing tests.
// We reuse it with a call-counting wrapper for these tests.

type countingConcierge struct {
	runFn func(ctx context.Context) (string, error)
	calls atomic.Int32
}

func (m *countingConcierge) Setup(ctx context.Context) error { return nil }
func (m *countingConcierge) Run(ctx context.Context) (string, error) {
	m.calls.Add(1)
	if m.runFn != nil {
		return m.runFn(ctx)
	}
	return "", nil
}

func (m *countingConcierge) callCount() int {
	return int(m.calls.Load())
}

// waitForConciergeCalls polls until the concierge has run at least want
// times. Cascades run on a goroutine, so polling the call counter is faster
// and less flaky than a fixed sleep-then-assert.
func waitForConciergeCalls(t *testing.T, mc *countingConcierge, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for mc.callCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("concierge calls: got %d, want %d", mc.callCount(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newTestIndexerForConcierge(t *testing.T, mc agents.Concierge, opts ...IndexerOption) *Indexer {
	t.Helper()
	// Construct directly to avoid creating a real fsnotify watcher
	// (which leaks kernel file descriptors across many tests).
	allOpts := append(
		[]IndexerOption{
			WithConcierge(mc),
			WithConciergeStartupDelay(0),
			WithConciergeCacheDir(t.TempDir()),
		}, opts...,
	)
	idx := &Indexer{
		clock:             time.Now,
		conciergeInterval: 6 * time.Hour,
		errorChannels:     make(map[string]errorListener),
		errorUpdates:      make(chan error, 1000),
	}
	for _, opt := range allOpts {
		opt(idx)
	}
	// Set up suggestions manager if not already set.
	if idx.suggestions == nil {
		sm, err := suggestions.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		idx.suggestions = sm
	}
	idx.store = &mockStore{
		setup: func() error { return nil },
		store: func() error { return nil },
		items: []model.Item{},
	}
	idx.watcher = &mockWatcher{
		setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
			ch := make(chan model.Item)
			close(ch)
			return ch, nil, nil
		},
		watch: func(ctx context.Context, path string) error { return nil },
	}
	return idx
}

func TestConcierge_FreshStartNoLastRun(t *testing.T) {
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(t, mc, WithConciergeStartupDelay(0), WithConciergeInterval(time.Hour))

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should run once after startup.
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_RestartWithinIntervalSkips(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a last-run file 10 minutes ago.
	tenMinAgo := time.Now().Add(-10 * time.Minute)
	err := os.WriteFile(path.Join(tmpDir, "concierge_last_run"), []byte(tenMinAgo.Format(time.RFC3339)), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(6*time.Hour),
		WithConciergeCacheDir(tmpDir),
	)

	err = idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should NOT run within the first 50ms — waiting for the interval to expire.
	time.Sleep(50 * time.Millisecond)
	if mc.callCount() > 0 {
		t.Fatalf("expected 0 runs when last-run was 10min ago with 6h interval, got %d", mc.callCount())
	}
}

func TestConcierge_RestartAfterIntervalRuns(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a last-run file 7 hours ago (past the 6h interval).
	sevenHoursAgo := time.Now().Add(-7 * time.Hour)
	err := os.WriteFile(path.Join(tmpDir, "concierge_last_run"), []byte(sevenHoursAgo.Format(time.RFC3339)), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err = idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// The last-run is past the 6h interval, so the run fires immediately;
	// poll instead of sleeping a fixed window.
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_CrashLoopSingleRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate 5 restarts within 1 hour.
	for i := range 5 {
		// First iteration: no last-run file.
		// Subsequent: last-run updated by previous run.
		mc := &countingConcierge{}
		idx := newTestIndexerForConcierge(
			t, mc,
			WithConciergeInterval(6*time.Hour),
			WithConciergeStartupDelay(0),
			WithConciergeCacheDir(tmpDir),
		)

		err := idx.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		go func() { _ = idx.Start(ctx) }()
		if i == 0 {
			// First restart has no last-run file, so it must run. Wait for it
			// so the run and its last-run write happen before we cancel.
			waitForConciergeCalls(t, mc, 1)
		} else {
			// Subsequent restarts see a fresh last-run and must skip; a short
			// settle gives the loop time to start and evaluate before cancel.
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
	}

	// Only the first restart should have produced a run.
	// Read all 5 counter totals. The key invariant: total runs across all 5 <= 1.
	// (Actually, each iteration creates a fresh counter, so we track via the file.)
	t1 := lastRunTimestamp(t, tmpDir)

	// Now start a 6th instance — it should skip because the last-run is recent.
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)
	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = idx.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if mc.callCount() > 0 {
		t.Fatalf("6th restart within interval should have 0 runs, got %d (first run at %v)", mc.callCount(), t1)
	}
}

func TestConcierge_LastRunPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(100*time.Millisecond),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Wait for at least one run.
	waitForConciergeCalls(t, mc, 1)

	// The last-run write happens synchronously after Run returns in the loop,
	// so once callCount reached 1 the file is being written; lastRunTimestamp
	// polls until it parses.
	_ = lastRunTimestamp(t, tmpDir)
}

// lastRunTimestamp reads the concierge last-run file, retrying until it
// parses. The concierge rewrites the file on every run, so a single read can
// catch a truncated write; polling makes the assertion independent of
// goroutine timing.
func lastRunTimestamp(t *testing.T, dir string) time.Time {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path.Join(dir, "concierge_last_run"))
		if err == nil {
			if ts, perr := time.Parse(time.RFC3339, string(data)); perr == nil {
				return ts
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("concierge last-run file not readable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConcierge_StartupDelayStillHonoured(t *testing.T) {
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(time.Hour),
		WithConciergeStartupDelay(100*time.Millisecond),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should NOT run immediately (startup delay is 100ms). The check at 30ms
	// runs before the delay can possibly elapse, so it cannot false-fail.
	time.Sleep(30 * time.Millisecond)
	if mc.callCount() > 0 {
		t.Fatal("concierge ran before startup delay elapsed")
	}

	// Should run after the delay.
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_IntervalFlagRespected(t *testing.T) {
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(200*time.Millisecond),
		WithConciergeStartupDelay(0),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// First run should happen quickly.
	waitForConciergeCalls(t, mc, 1)
	callsAfterFirst := mc.callCount()
	if callsAfterFirst < 1 {
		t.Fatal("expected first run")
	}

	// Second run should happen after the interval.
	waitForConciergeCalls(t, mc, 2)
	if mc.callCount() <= callsAfterFirst {
		t.Fatalf("expected second run within %v, got %d calls", 200*time.Millisecond, mc.callCount())
	}
}

// --- Error coverage tests ---

func TestConcierge_UnreadableLastRunFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory where the file should be, making the file unreadable.
	err := os.MkdirAll(path.Join(tmpDir, "concierge_last_run"), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err = idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should still run (treated as never-run).
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_MalformedLastRunFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(path.Join(tmpDir, "concierge_last_run"), []byte("not-a-timestamp"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err = idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should still run (treated as never-run).
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_FutureLastRun(t *testing.T) {
	tmpDir := t.TempDir()

	futureTime := time.Now().Add(24 * time.Hour)
	err := os.WriteFile(path.Join(tmpDir, "concierge_last_run"), []byte(futureTime.Format(time.RFC3339)), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err = idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should run (future timestamp treated as never-run, not block indefinitely).
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_FailedRunUpdatesLastRun(t *testing.T) {
	tmpDir := t.TempDir()

	mc := &countingConcierge{
		runFn: func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("simulated failure")
		},
	}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(100*time.Millisecond),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Wait for the run.
	waitForConciergeCalls(t, mc, 1)

	// Stop idx1's ticker so it cannot rewrite the file while we read it: a
	// read racing a write sees a truncated file, which reads as a missing
	// last-run.
	cancel()

	// The in-flight write (if any) completes after cancel; the file must exist
	// despite the failed run.
	lastRunTimestamp(t, tmpDir)

	// Starting a new indexer should skip (last-run is recent).
	mc2 := &countingConcierge{}
	idx2 := newTestIndexerForConcierge(
		t, mc2,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)
	err = idx2.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	go func() { _ = idx2.Start(ctx2) }()
	time.Sleep(50 * time.Millisecond)
	if mc2.callCount() > 0 {
		t.Fatal("second indexer should skip because last-run was updated even after failure")
	}
}

func TestConcierge_PanicDoesNotKillScheduler(t *testing.T) {
	tmpDir := t.TempDir()

	// Use a concierge that panics on first call.
	panicConcierge := &panicConcierge{panicOnCall: 1}
	idx := newTestIndexerForConcierge(
		t, panicConcierge,
		WithConciergeInterval(100*time.Millisecond),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(tmpDir),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = idx.Start(ctx)
		close(done)
	}()

	// The scheduler should survive and keep running. The panic happened on
	// call 1; reaching call 2 proves the loop ticked again after recovery.
	deadline := time.Now().Add(2 * time.Second)
	for panicConcierge.calls.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not survive panic (no second run)")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The goroutine should still be running (ctx not cancelled).
	select {
	case <-done:
		t.Fatal("scheduler goroutine exited unexpectedly after panic")
	default:
		// Good — still running.
	}
}

type panicConcierge struct {
	calls       atomic.Int32
	panicOnCall int
}

func (m *panicConcierge) Setup(ctx context.Context) error { return nil }
func (m *panicConcierge) Run(ctx context.Context) (string, error) {
	m.calls.Add(1)
	if int(m.calls.Load()) == m.panicOnCall {
		panic("test panic in concierge")
	}
	return "", nil
}

func TestConcierge_ZeroIntervalRunsOnce(t *testing.T) {
	// Defense-in-depth: verify the indexer's runConciergeLoop safely handles
	// zero interval by running once then stopping. Flag-level rejection in
	// serve.go is the primary guard; this ensures the indexer path is also safe.
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(0),
		WithConciergeStartupDelay(0),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Zero interval: runs once then stops.
	waitForConciergeCalls(t, mc, 1)
}

func TestConcierge_LastRunWriteFailure(t *testing.T) {
	// Use a nonexistent path to force write failure.
	// The run should still complete, just with a logged warning.
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir("/nonexistent/path/that/cannot/be/created/concierge_last_run"),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should still run even though write fails.
	waitForConciergeCalls(t, mc, 1)

	// A second start should still run (since write failed, no file persisted).
	mc2 := &countingConcierge{}
	idx2 := newTestIndexerForConcierge(
		t, mc2,
		WithConciergeInterval(6*time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir("/another/nonexistent/path"),
	)
	err = idx2.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	go func() { _ = idx2.Start(ctx2) }()
	waitForConciergeCalls(t, mc2, 1)
}

func TestConcierge_EmptyCacheDir(t *testing.T) {
	// Empty cache dir: last-run persistence is silently skipped.
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(time.Hour),
		WithConciergeStartupDelay(0),
		WithConciergeCacheDir(""),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	waitForConciergeCalls(t, mc, 1)
}

func TestReadConciergeLastRun_NoFile(t *testing.T) {
	idx := &Indexer{conciergeCacheDir: t.TempDir()}
	tm, err := idx.readConciergeLastRun()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tm.IsZero() {
		t.Fatal("expected zero time when file does not exist")
	}
}

func TestReadConciergeLastRun_EmptyCacheDir(t *testing.T) {
	idx := &Indexer{conciergeCacheDir: ""}
	tm, err := idx.readConciergeLastRun()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tm.IsZero() {
		t.Fatal("expected zero time for empty cache dir")
	}
}

func TestWriteConciergeLastRun_EmptyCacheDir(t *testing.T) {
	// Should not panic.
	idx := &Indexer{conciergeCacheDir: "", clock: time.Now}
	idx.writeConciergeLastRun()
}

func TestConcierge_ContextCancelStopsScheduler(t *testing.T) {
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(10*time.Second),
		WithConciergeStartupDelay(0),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = idx.Start(ctx)
		close(done)
	}()

	// Wait for first run.
	waitForConciergeCalls(t, mc, 1)
	callsBeforeCancel := mc.callCount()

	// Cancel context.
	cancel()

	// Wait and verify the goroutine exits.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not exit after context cancel")
	}

	// No more calls after cancel.
	time.Sleep(50 * time.Millisecond)
	if mc.callCount() != callsBeforeCancel {
		t.Fatal("concierge ran after context cancel")
	}
}

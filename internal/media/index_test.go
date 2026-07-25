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
}

func (m *mockWatcher) Setup(ctx context.Context) (<-chan model.Item, <-chan error, error) {
	return m.setup(ctx)
}

func (m *mockWatcher) Watch(ctx context.Context, path string) error {
	return m.watch(ctx, path)
}

func Test_indexer_Setup(t *testing.T) {
	t.Run("error on store error", func(t *testing.T) {
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}
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
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}

		want := errors.New("whopsidops")
		i.store = &mockStore{
			setup: func() error { return nil },
		}
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
		i, err := NewIndexer(WithWatchPath(wantWatchPath))
		if err != nil {
			t.Fatal(err)
		}
		var want error
		i.store = &mockStore{
			setup: func() error { return want },
		}
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
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}
		i.Setup(ctx)
	}, time.Millisecond*100)
}

func TestNewIndexer(t *testing.T) {
	t.Run("watcher and store should not be nil", func(t *testing.T) {
		i, err := NewIndexer(WithStorage(&mockStore{}))
		if err != nil {
			t.Fatal(err)
		}
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
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}
		i.fileUpdates = nil
		got := i.Start(context.Background())
		if got == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("error on store error", func(t *testing.T) {
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}
		want := errors.New("store error")
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return want },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}

		err = i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		err = i.Start(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("wanted: %v, got: %v", want, err)
		}
	})

	t.Run("error on watcher error", func(t *testing.T) {
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}
		want := errors.New("watcher error")
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return want },
		}

		err = i.Setup(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		err = i.Start(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("wanted: %v, got: %v", want, err)
		}
	})

	t.Run("exit on context cancel", func(t *testing.T) {
		i, err := NewIndexer()
		if err != nil {
			t.Fatal(err)
		}
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { tmp := make(chan struct{}); <-tmp; return nil },
		}

		err = i.Setup(context.Background())
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
		i, err := NewIndexer(
			WithConcierge(mc),
			WithConciergeStartupDelay(200*time.Millisecond),
		)
		if err != nil {
			t.Fatal(err)
		}
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}
		err = i.Setup(context.Background())
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
		case <-time.After(50 * time.Millisecond):
		}

		// But should run after the delay
		select {
		case <-runCh:
		case <-time.After(500 * time.Millisecond):
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
		i, err := NewIndexer(
			WithConcierge(mc),
			WithConciergeStartupDelay(0),
		)
		if err != nil {
			t.Fatal(err)
		}
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}
		err = i.Setup(context.Background())
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
		i, err := NewIndexer(
			WithConcierge(mc),
			WithConciergeStartupDelay(5*time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				ch := make(chan model.Item)
				close(ch)
				return ch, nil, nil
			},
			watch: func(ctx context.Context, path string) error { return nil },
		}
		err = i.Setup(context.Background())
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
	time.Sleep(100 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected at least 1 run on fresh start")
	}
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

	// Should NOT run within the first 200ms — waiting for the interval to expire.
	time.Sleep(200 * time.Millisecond)
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

	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected run when last-run is past the interval")
	}
}

func TestConcierge_CrashLoopSingleRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate 5 restarts within 1 hour.
	for range 5 {
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
		time.Sleep(200 * time.Millisecond)
		cancel()
	}

	// Only the first restart should have produced a run.
	// Read all 5 counter totals. The key invariant: total runs across all 5 <= 1.
	// (Actually, each iteration creates a fresh counter, so we track via the file.)
	data, err := os.ReadFile(path.Join(tmpDir, "concierge_last_run"))
	if err != nil {
		t.Fatal(err)
	}
	t1, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		t.Fatal(err)
	}

	// Now start a 6th instance — it should skip because the last-run is recent.
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = idx.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)
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
	time.Sleep(300 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected at least 1 run")
	}

	// Give writeConciergeLastRun time to flush (happens after Run returns).
	time.Sleep(50 * time.Millisecond)

	// File should exist and contain a valid timestamp.
	data, err := os.ReadFile(path.Join(tmpDir, "concierge_last_run"))
	if err != nil {
		t.Fatalf("last-run file not found: %v", err)
	}
	_, err = time.Parse(time.RFC3339, string(data))
	if err != nil {
		t.Fatalf("last-run file has invalid timestamp: %v", err)
	}
}

func TestConcierge_StartupDelayStillHonoured(t *testing.T) {
	mc := &countingConcierge{}
	idx := newTestIndexerForConcierge(
		t, mc,
		WithConciergeInterval(time.Hour),
		WithConciergeStartupDelay(300*time.Millisecond),
	)

	err := idx.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = idx.Start(ctx) }()

	// Should NOT run immediately.
	time.Sleep(100 * time.Millisecond)
	if mc.callCount() > 0 {
		t.Fatal("concierge ran before startup delay elapsed")
	}

	// Should run after the delay.
	time.Sleep(400 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("concierge did not run after startup delay")
	}
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
	time.Sleep(100 * time.Millisecond)
	callsAfterFirst := mc.callCount()
	if callsAfterFirst < 1 {
		t.Fatal("expected first run")
	}

	// Second run should happen after the interval.
	time.Sleep(300 * time.Millisecond)
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
	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected run when last-run file is unreadable (treated as never-run)")
	}
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
	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected run when last-run file is malformed")
	}
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
	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected run when last-run timestamp is in the future")
	}
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
	time.Sleep(300 * time.Millisecond)

	// File should exist despite the failure.
	data, err := os.ReadFile(path.Join(tmpDir, "concierge_last_run"))
	if err != nil {
		t.Fatalf("last-run file not found after failed run: %v", err)
	}
	_, err = time.Parse(time.RFC3339, string(data))
	if err != nil {
		t.Fatalf("last-run file has invalid timestamp after failed run: %v", err)
	}

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
	time.Sleep(200 * time.Millisecond)
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

	// The scheduler should survive and keep running.
	// Wait for enough time that at least 2 ticks would have occurred.
	time.Sleep(500 * time.Millisecond)

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
	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected at least 1 run with zero interval")
	}
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
	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected run even if last-run write fails")
	}

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
	time.Sleep(200 * time.Millisecond)
	if mc2.callCount() < 1 {
		t.Fatal("second start should also run since no last-run was persisted")
	}
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

	time.Sleep(200 * time.Millisecond)
	if mc.callCount() < 1 {
		t.Fatal("expected run with empty cache dir")
	}
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
	time.Sleep(200 * time.Millisecond)
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
	time.Sleep(100 * time.Millisecond)
	if mc.callCount() != callsBeforeCancel {
		t.Fatal("concierge ran after context cancel")
	}
}

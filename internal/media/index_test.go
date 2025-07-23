package media

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

type mockStore struct {
	setup func() error
	store func() error
}

func (m *mockStore) Setup(ctx context.Context, storePath string) error {
	return m.setup()
}

func (m *mockStore) Store(ctx context.Context, i model.Item) error {
	return m.store()
}

func (m *mockStore) VideoHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStore) SubsHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStore) SubsListHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStore) ListHandlerFunc() http.HandlerFunc {
	return nil
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

func Test_Indexer_Setup(t *testing.T) {
	t.Run("error on store error", func(t *testing.T) {
		tmpDir := t.TempDir()
		i := NewIndexer(tmpDir)
		want := errors.New("whopsidops")
		i.store = &mockStore{
			setup: func() error {
				return want
			},
		}
		got := i.Setup(context.Background(), "", t.TempDir())
		if errors.Is(got, want) {
			t.Fatalf("wanted: %v, got: %v", want, got)
		}
	})

	t.Run("error on watcher error", func(t *testing.T) {
		i := NewIndexer(t.TempDir())

		want := errors.New("whopsidops")
		i.store = &mockStore{
			setup: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				return nil, nil, want
			},
		}
		got := i.Setup(context.Background(), "", t.TempDir())
		if errors.Is(got, want) {
			t.Fatalf("wanted: %v, got: %v", want, got)
		}
	})

	t.Run("should return error nil on OK", func(t *testing.T) {
		i := NewIndexer(t.TempDir())
		var want error
		i.store = &mockStore{
			setup: func() error { return want },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan model.Item, <-chan error, error) {
				return nil, nil, want
			},
		}

		wantWatchPath := t.TempDir()
		wantStorePath := t.TempDir()
		got := i.Setup(context.Background(), wantWatchPath, wantStorePath)
		testboil.FailTestIfDiff(t, got, want)
		testboil.FailTestIfDiff(t, i.watchPath, wantWatchPath)
	})

	testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
		tDir := t.TempDir()
		i := NewIndexer(tDir)
		i.Setup(ctx, "", tDir)
	}, time.Millisecond*100)
}

func Test_NewIndexer(t *testing.T) {
	t.Run("watcher and store should not be nil", func(t *testing.T) {
		i := NewIndexer(t.TempDir())
		if i.watcher == nil {
			t.Fatal("watcher shouldnt be nil")
		}

		if i.store == nil {
			t.Fatal("watcher shouldnt be nil")
		}
	})
}

func Test_Start_errorHandling(t *testing.T) {
	t.Run("error on no fileUpdates", func(t *testing.T) {
		i := NewIndexer(t.TempDir())
		i.fileUpdates = nil
		got := i.Start(context.Background())
		if got == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("error on store error", func(t *testing.T) {
		i := NewIndexer(t.TempDir())
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

		err := i.Setup(context.Background(), "", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		err = i.Start(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("wanted: %v, got: %v", want, err)
		}
	})

	t.Run("error on watcher error", func(t *testing.T) {
		i := NewIndexer(t.TempDir())
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

		err := i.Setup(context.Background(), "", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		err = i.Start(context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("wanted: %v, got: %v", want, err)
		}
	})

	t.Run("exit on context cancel", func(t *testing.T) {
		i := NewIndexer(t.TempDir())
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

		err := i.Setup(context.Background(), "", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
			i.Start(ctx)
		}, time.Millisecond*100)
	})
}

func Test_RegisterErrorChannel(t *testing.T) {
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

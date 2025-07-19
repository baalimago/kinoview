package media

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
)

type mockStore struct {
	setup func() error
	store func() error
}

func (m *mockStore) Setup(ctx context.Context, storePath string) error {
	return m.setup()
}

func (m *mockStore) Store(i Item) error {
	return m.store()
}

func (m *mockStore) ItemHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStore) ListHandlerFunc() http.HandlerFunc {
	return nil
}

type mockWatcher struct {
	setup func(ctx context.Context) (<-chan Item, error)
	watch func(ctx context.Context, path string) error
}

func (m *mockWatcher) Setup(ctx context.Context) (<-chan Item, error) {
	return m.setup(ctx)
}

func (m *mockWatcher) Watch(ctx context.Context, path string) error {
	return m.watch(ctx, path)
}

func Test_Indexer_Setup(t *testing.T) {
	t.Run("error on store error", func(t *testing.T) {
		i := NewIndexer()
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
		i := NewIndexer()
		want := errors.New("whopsidops")
		i.store = &mockStore{
			setup: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan Item, error) {
				return nil, want
			},
		}
		got := i.Setup(context.Background(), "", t.TempDir())
		if errors.Is(got, want) {
			t.Fatalf("wanted: %v, got: %v", want, got)
		}
	})

	t.Run("should return error nil on OK", func(t *testing.T) {
		i := NewIndexer()
		var want error
		i.store = &mockStore{
			setup: func() error { return want },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan Item, error) {
				return nil, want
			},
		}

		wantWatchPath := t.TempDir()
		wantStorePath := t.TempDir()
		got := i.Setup(context.Background(), wantWatchPath, wantStorePath)
		testboil.FailTestIfDiff(t, got, want)
		testboil.FailTestIfDiff(t, i.watchPath, wantWatchPath)
	})

	testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
		i := NewIndexer()
		i.Setup(ctx, "", t.TempDir())
	}, time.Millisecond*100)
}

func Test_NewIndexer(t *testing.T) {
	t.Run("watcher and store should not be nil", func(t *testing.T) {
		i := NewIndexer()
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
		i := NewIndexer()
		i.fileUpdates = nil
		got := i.Start(context.Background())
		if got == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("error on store error", func(t *testing.T) {
		i := NewIndexer()
		want := errors.New("store error")
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return want },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan Item, error) {
				ch := make(chan Item)
				close(ch)
				return ch, nil
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
		i := NewIndexer()
		want := errors.New("watcher error")
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan Item, error) {
				ch := make(chan Item)
				close(ch)
				return ch, nil
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
		i := NewIndexer()
		i.store = &mockStore{
			setup: func() error { return nil },
			store: func() error { return nil },
		}
		i.watcher = &mockWatcher{
			setup: func(ctx context.Context) (<-chan Item, error) {
				ch := make(chan Item)
				close(ch)
				return ch, nil
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

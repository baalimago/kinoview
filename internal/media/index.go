package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

type storage interface {
	Setup(ctx context.Context, storePath string) error
	Store(i model.Item) error
	ListHandlerFunc() http.HandlerFunc
	ItemHandlerFunc() http.HandlerFunc
}

type watcher interface {
	Setup(ctx context.Context) (<-chan model.Item, error)
	Watch(ctx context.Context, path string) error
}

type Indexer struct {
	watchPath string
	storePath string
	watcher   watcher
	store     storage

	fileUpdates <-chan model.Item
}

func NewIndexer() *Indexer {
	i := &Indexer{}
	// Ignore error as this only affects buffered fsnotify.Watchers
	w, _ := newRecursiveWatcher()
	i.watcher = w
	i.store = newJSONStore()
	return i
}

func (i *Indexer) Setup(ctx context.Context, watchPath, storePath string) error {
	err := i.store.Setup(ctx, storePath)
	if err != nil {
		return fmt.Errorf("Setup store: %v", err)
	}

	fileUpdates, err := i.watcher.Setup(ctx)
	if err != nil {
		return fmt.Errorf("Setup watcher: %v", err)
	}

	i.fileUpdates = fileUpdates
	i.watchPath = watchPath
	i.storePath = storePath

	ancli.Okf("indexer.Setup OK")
	return nil
}

func (i *Indexer) Start(ctx context.Context) error {
	if i.fileUpdates == nil {
		return errors.New("fileUpdates must not be nil. Please run Setup")
	}
	watcherErrChan := make(chan error)
	go func() {
		watcherErrChan <- i.watcher.Watch(ctx, i.watchPath)
	}()

	storeErrChan := make(chan error)
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(storeErrChan)
				return
			case bareItem := <-i.fileUpdates:
				err := i.store.Store(bareItem)
				if err != nil {
					storeErrChan <- err
				}
			}
		}
	}()

	for {
		select {
		case err := <-watcherErrChan:
			if err != nil {
				return fmt.Errorf("Start got watcher err: %w", err)
			}
		case err := <-storeErrChan:
			if err != nil {
				return fmt.Errorf("Start got store err: %w", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (i *Indexer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", i.store.ListHandlerFunc())
	mux.HandleFunc("/{id}", i.store.ItemHandlerFunc())
	return mux
}

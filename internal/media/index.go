package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	int_watcher "github.com/baalimago/kinoview/internal/media/watcher"
	"github.com/baalimago/kinoview/internal/model"
)

type storage interface {
	// Setup a storage and return a channel for errors if successful or
	// or an error explaining why it failed
	Setup(ctx context.Context) (<-chan error, error)
	// Start any internal routines
	Start(ctx context.Context)
	// Store some item, return error on failure
	Store(ctx context.Context, i model.Item) error
	ListHandlerFunc() http.HandlerFunc
	VideoHandlerFunc() http.HandlerFunc
	SubsListHandlerFunc() http.HandlerFunc
	SubsHandlerFunc() http.HandlerFunc
}

type watcher interface {
	// Setup a watcher, returning its update channel and error channel. If error is not nil
	// the setup has failed. The error channel will propagate errors back to parent routine
	// where severity of issue may be handled
	Setup(ctx context.Context) (<-chan model.Item, <-chan error, error)

	// Watch the path, error on catastrophic failure to start
	// Will propagate errors via error cannel from Setup
	Watch(ctx context.Context, path string) error
}

// errorListener is slightly overengineered. But we don't care about that
// this is fine.
type errorListener struct {
	// stopContext function to cancel the errorListener whenever
	// we wish to deregister it
	stopContext func()
	name        string
	in          <-chan error
	out         chan<- error
}

func (el *errorListener) start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-el.in:
			el.out <- fmt.Errorf("%v: %w", el.name, e)
		}
	}
}

type Indexer struct {
	watchPath string
	watcher   watcher
	store     storage

	fileUpdates   <-chan model.Item
	errorChannels map[string]errorListener
	errorUpdates  chan error
}

type IndexerOption func(*Indexer)

func WithStorage(s storage) IndexerOption {
	return func(i *Indexer) {
		i.store = s
	}
}

func WithWatchPath(watchPath string) IndexerOption {
	return func(i *Indexer) {
		i.watchPath = watchPath
	}
}

func NewIndexer(opts ...IndexerOption) (*Indexer, error) {
	w, err := int_watcher.NewRecursiveWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create recursive watcher: %w", err)
	}

	i := &Indexer{
		watcher:       w,
		errorChannels: make(map[string]errorListener),
		errorUpdates:  make(chan error, 1000),
	}

	for _, opt := range opts {
		opt(i)
	}

	return i, nil
}

func (i *Indexer) registerErrorChannel(ctx context.Context, subRoutineName string, errChan <-chan error) error {
	_, exists := i.errorChannels[subRoutineName]
	if exists {
		return fmt.Errorf("error channel with name '%v' already exists", subRoutineName)
	}

	errChanCtx, errChanCtxCancel := context.WithCancel(ctx)
	errL := errorListener{
		name:        subRoutineName,
		stopContext: errChanCtxCancel,
		in:          errChan,
		out:         i.errorUpdates,
	}
	go errL.start(errChanCtx)

	i.errorChannels[subRoutineName] = errL

	return nil
}

func (i *Indexer) Setup(ctx context.Context) error {
	if i.store == nil {
		return errors.New("store must be set, please create Indexer with some store")
	}
	storeErrors, err := i.store.Setup(ctx)
	if err != nil {
		return fmt.Errorf("setup store: %w", err)
	}

	fileUpdates, watcherErrors, err := i.watcher.Setup(ctx)
	if err != nil {
		return fmt.Errorf("setup watcher: %w", err)
	}

	i.fileUpdates = fileUpdates
	i.registerErrorChannel(ctx, "watcher", watcherErrors)
	i.registerErrorChannel(ctx, "store", storeErrors)

	ancli.Okf("indexer.Setup OK")
	return nil
}

func (i *Indexer) handleNewItem(ctx context.Context, item model.Item) error {
	err := i.store.Store(ctx, item)
	if err != nil {
		return fmt.Errorf("Indexer failed to handle new item: %w", err)
	}
	return nil
}

func (i *Indexer) Start(ctx context.Context) error {
	if i.store != nil {
		i.store.Start(ctx)
	}
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
				err := i.handleNewItem(ctx, bareItem)
				if err != nil {
					storeErrChan <- err
				}
			}
		}
	}()

	for {
		select {
		case err := <-i.errorUpdates:
			ancli.Errf("indexer subroutine err: %v", err)
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
	mux.HandleFunc("/video/{id}", i.store.VideoHandlerFunc())
	mux.HandleFunc("/subs/{vid}", i.store.SubsListHandlerFunc())
	mux.HandleFunc("/subs/{vid}/{sub_idx}", i.store.SubsHandlerFunc())
	return mux
}

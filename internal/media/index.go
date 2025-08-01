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
	storePath string
	watcher   watcher
	store     storage

	fileUpdates   <-chan model.Item
	errorChannels map[string]errorListener
	errorUpdates  chan error
}

func NewIndexer() *Indexer {
	i := &Indexer{}
	// Ignore error as this only affects buffered fsnotify.Watchers
	w, _ := newRecursiveWatcher()
	i.watcher = w
	i.store = newJSONStore()
	i.errorChannels = make(map[string]errorListener)
	i.errorUpdates = make(chan error, 1000)
	return i
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

func (i *Indexer) Setup(ctx context.Context, watchPath, storePath string) error {
	err := i.store.Setup(ctx, storePath)
	if err != nil {
		return fmt.Errorf("Setup store: %v", err)
	}

	fileUpdates, watcherErrors, err := i.watcher.Setup(ctx)
	if err != nil {
		return fmt.Errorf("Setup watcher: %v", err)
	}

	i.fileUpdates = fileUpdates
	i.registerErrorChannel(ctx, "watcher", watcherErrors)
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

package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/recommender"
	"github.com/baalimago/kinoview/internal/loghandler"
	"github.com/baalimago/kinoview/internal/media/suggestions"
	int_watcher "github.com/baalimago/kinoview/internal/media/watcher"
	"github.com/baalimago/kinoview/internal/model"
)

type Storage interface {
	// Setup a storage and return a channel for errors if successful or
	// or an error explaining why it failed
	Setup(ctx context.Context) (<-chan error, error)
	// Start any internal routines
	Start(ctx context.Context)
	// Store some item, return error on failure
	Store(ctx context.Context, i model.Item) error
	// Snapshot of the current item state. Thread safe, returns a copy of cache.
	Snapshot() []model.Item
	ListHandlerFunc() http.HandlerFunc
	VideoHandlerFunc() http.HandlerFunc
	ImageHandlerFunc() http.HandlerFunc
	StreamListHandlerFunc() http.HandlerFunc
	StreamHandlerFunc() http.HandlerFunc
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
	watchPath   string
	watcher     watcher
	store       Storage
	recommender agents.Recommender
	butler      agents.Butler
	concierge   agents.Concierge

	userContextMgr agents.UserContextManager

	suggestions *suggestions.Manager

	fileUpdates   <-chan model.Item
	errorChannels map[string]errorListener
	errorUpdates  chan error
}

type IndexerOption func(*Indexer)

func WithStorage(s Storage) IndexerOption {
	return func(i *Indexer) {
		i.store = s
	}
}

func WithWatchPath(watchPath string) IndexerOption {
	return func(i *Indexer) {
		i.watchPath = watchPath
	}
}

func WithRecommender(r agents.Recommender) IndexerOption {
	return func(i *Indexer) {
		i.recommender = r
	}
}

func WithButler(b agents.Butler) IndexerOption {
	return func(i *Indexer) {
		i.butler = b
	}
}

func WithConcierge(c agents.Concierge) IndexerOption {
	return func(i *Indexer) {
		i.concierge = c
	}
}

func WithSuggestionsManager(s *suggestions.Manager) IndexerOption {
	return func(i *Indexer) {
		i.suggestions = s
	}
}

func NewIndexer(opts ...IndexerOption) (*Indexer, error) {
	w, err := int_watcher.NewRecursiveWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create recursive watcher: %w", err)
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		ancli.Warnf("failed to find user config dir: %v", err)
	}
	claiPath := path.Join(cfgDir, "kinoview", "clai")

	i := &Indexer{
		watcher: w,
		recommender: recommender.New(models.Configurations{
			Model:         "gpt-5",
			ConfigDir:     claiPath,
			InternalTools: []models.ToolName{},
		}),
		errorChannels: make(map[string]errorListener),
		errorUpdates:  make(chan error, 1000),
	}

	for _, opt := range opts {
		opt(i)
	}

	if i.suggestions == nil {
		sm, err := suggestions.NewManager("")
		if err != nil {
			return nil, fmt.Errorf("failed to create suggestions manager: %w", err)
		}
		i.suggestions = sm
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

	recSetupErr := i.recommender.Setup(ctx)
	if recSetupErr != nil {
		ancli.Errf("failed to setup recommender, recommendations wont work. Err: %v", err)
	}

	if i.butler != nil {
		butlerSetupErr := i.butler.Setup(ctx)
		if butlerSetupErr != nil {
			ancli.Errf("failed to setup butler: %v", butlerSetupErr)
		}
	}

	if i.concierge != nil {
		conciergeSetupErr := i.concierge.Setup(ctx)
		if conciergeSetupErr != nil {
			ancli.Errf("failed to setup concierge: %v", conciergeSetupErr)
			// Reset concirege as its broken, this is a flag to not attempt to use it downstream
			i.concierge = nil
		}
	}

	i.fileUpdates = fileUpdates
	err = i.registerErrorChannel(ctx, "watcher", watcherErrors)
	if err != nil {
		return fmt.Errorf("failed to add watcher error chan: %w", err)
	}

	err = i.registerErrorChannel(ctx, "store", storeErrors)
	if err != nil {
		return fmt.Errorf("failed to add store error chan: %w", err)
	}

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

	if i.concierge != nil {
		conciergeErrChan := make(chan error, 1)
		tick := time.NewTicker(time.Minute * 15)
		go func() {
			do := func() {
				ancli.Okf("Running concierge")
				_, err := i.concierge.Run(ctx)
				if err != nil && !errors.Is(err, context.Canceled) {
					conciergeErrChan <- err
				}
			}
			// Run on startup for fun
			do()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					do()
				}
			}
		}()
		i.registerErrorChannel(ctx, "concierge", conciergeErrChan)
	}

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
	mux.HandleFunc("/streams/{vid}", i.store.StreamListHandlerFunc())
	mux.HandleFunc("/streams/{vid}/subs/{sub_idx}", i.store.StreamHandlerFunc())
	mux.HandleFunc("/image/{id}", i.store.ImageHandlerFunc())
	mux.HandleFunc("/recommend", i.recomendHandler())
	mux.HandleFunc("/suggestions", i.suggestionsHandler())
	mux.HandleFunc("/log", loghandler.Func())
	mux.HandleFunc("/ws", i.eventStream())
	return mux
}

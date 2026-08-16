package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
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

	// Close releases any OS resources (e.g. inotify instances) held by the
	// watcher. Idempotent.
	Close() error
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

type disconnectReason string

const (
	reasonSocketError disconnectReason = "socketError"
	reasonPingFailed  disconnectReason = "pingFailed"
	reasonPongTimeout disconnectReason = "pongTimeout"
	reasonConnect     disconnectReason = "connect"
)

type Indexer struct {
	watchPath string
	watcher   watcher
	store     Storage

	// Agents
	recommender           agents.Recommender
	butler                agents.Butler
	concierge             agents.Concierge
	conciergeStartupDelay time.Duration
	conciergeInterval     time.Duration
	conciergeCacheDir     string
	conciergeTimeout      time.Duration
	// theatre prepares the intro splash story (the agents.Teller contract).
	theatre agents.Teller
	// feedback records audience notes into the shared notebook (the
	// agents.Feedbacker contract). Nil when the notebook is disabled; the
	// feedback handler then answers 501.
	feedback agents.Feedbacker

	// Agent support managers
	clientContextMgr agents.ClientContextManager
	suggestions      *suggestions.Manager

	// websocket health/heartbeat tuning (mainly for tests)
	heartbeatInterval time.Duration
	pongTimeout       time.Duration
	pingWriteTimeout  time.Duration

	// Butler cascade rate-limiting
	butlerDebounce       time.Duration
	butlerCacheTTL       time.Duration
	pongGrace            time.Duration
	butlerLastCascadeAt  time.Time
	butlerInFlight       bool
	butlerRerunRequested bool
	butlerRerunConsumed  bool
	butlerMu             sync.Mutex
	clock                func() time.Time

	// cascadeGen is bumped every time triggerCascade starts a fresh cascade
	// (i.e. when butlerInFlight was false). The cascade goroutine captures
	// the generation and only manages its own flight slot — preventing a
	// fast cascade from completing before queued callers have signalled,
	// which would otherwise spawn extra cascades.
	cascadeGen atomic.Int64

	// Suggestions event broadcast to connected websocket clients
	suggestionSubscribersMu sync.Mutex
	suggestionSubscribers   []chan model.SuggestionsPayload
	wsWriteMu               sync.Mutex // serializes writes across all websocket connections

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

// WithTheatre sets the agent which prepares the intro splash story.
func WithTheatre(t agents.Teller) IndexerOption {
	return func(i *Indexer) {
		i.theatre = t
	}
}

// WithFeedbacker sets the recorder audience notes land in. Nil disables the
// feedback endpoint (the handler answers 501).
func WithFeedbacker(fb agents.Feedbacker) IndexerOption {
	return func(i *Indexer) {
		i.feedback = fb
	}
}

func WithConciergeStartupDelay(d time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.conciergeStartupDelay = d
	}
}

// WithConciergeInterval sets the interval between concierge runs.
func WithConciergeInterval(d time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.conciergeInterval = d
	}
}

// WithConciergeCacheDir sets the directory where the concierge last-run
// timestamp is persisted.
func WithConciergeCacheDir(dir string) IndexerOption {
	return func(i *Indexer) {
		i.conciergeCacheDir = dir
	}
}

// WithConciergeTimeout sets the wall-clock cap for a single concierge run. A
// run stuck on a looping model (endless reasoning stream) is aborted after
// this duration; the next run happens at the next interval. Zero disables the
// cap.
func WithConciergeTimeout(d time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.conciergeTimeout = d
	}
}

func WithSuggestionsManager(s *suggestions.Manager) IndexerOption {
	return func(i *Indexer) {
		i.suggestions = s
	}
}

func WithClientContextManager(m agents.ClientContextManager) IndexerOption {
	return func(i *Indexer) {
		i.clientContextMgr = m
	}
}

// WithHeartbeatConfig allows overriding ping interval and timeouts.
// Zero/negative values keep defaults.
func WithHeartbeatConfig(interval, pongTimeout, pingWriteTimeout time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.heartbeatInterval = interval
		i.pongTimeout = pongTimeout
		i.pingWriteTimeout = pingWriteTimeout
	}
}

// WithButlerDebounce sets the minimum interval between butler suggestion
// cascades. Triggers inside the window are dropped. A zero value disables
// debounce (single-flight still applies).
func WithButlerDebounce(d time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.butlerDebounce = d
	}
}

// WithButlerCacheTTL sets how long a cached suggestion set is served before
// the butler is re-queried. A zero value disables caching entirely.
func WithButlerCacheTTL(d time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.butlerCacheTTL = d
	}
}

// WithPongGrace sets the grace period after a pong timeout before a
// disconnect cascade fires. If the client sends a pong within this window
// the cascade is cancelled.
func WithPongGrace(d time.Duration) IndexerOption {
	return func(i *Indexer) {
		i.pongGrace = d
	}
}

// withClock sets the time source for debounce checks. Only exported for
// tests; production code uses time.Now.
func withClock(fn func() time.Time) IndexerOption {
	return func(i *Indexer) {
		i.clock = fn
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
		watcher:           w,
		clock:             time.Now,
		pongGrace:         defaultPongGrace,
		conciergeInterval: 6 * time.Hour,
		conciergeTimeout:  10 * time.Minute,
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

	if i.recommender != nil {
		recSetupErr := i.recommender.Setup(ctx)
		if recSetupErr != nil {
			ancli.Errf("failed to setup recommender, recommendations wont work. Err: %v", err)
		}
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
	if misc.Truthy(os.Getenv("DEBUG_NEW_ITEMS")) {
		ancli.Noticef("found: %v", item.Path)
	}
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
		i.registerErrorChannel(ctx, "concierge", conciergeErrChan)
		go i.runConciergeLoop(ctx, conciergeErrChan)
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

// Close releases the filesystem watcher created by NewIndexer. Safe to call
// after Start has returned: Watch closes the underlying watcher on return, and
// recursiveWatcher.Close is idempotent.
func (i *Indexer) Close() error {
	if i.watcher == nil {
		return nil
	}
	return i.watcher.Close()
}

// runConciergeLoop is the background goroutine that schedules and executes
// concierge runs at the configured interval. On startup it reads the persisted
// last-run timestamp to avoid re-running after a restart within the interval.
func (i *Indexer) runConciergeLoop(ctx context.Context, errChan chan<- error) {
	do := func() {
		defer func() {
			if r := recover(); r != nil {
				ancli.Errf("concierge: panic recovered: %v", r)
			}
		}()
		ancli.Okf("Running concierge")
		// Bound each run so a looping model (endless reasoning stream, the
		// 2026-08-11 OOM root cause) cannot hold the loop open forever. The
		// timeout ctx is derived from the loop ctx, so server shutdown still
		// cancels promptly.
		runCtx := ctx
		cancel := func() {}
		if i.conciergeTimeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, i.conciergeTimeout)
		}
		_, err := i.concierge.Run(runCtx)
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		// Always persist last-run to prevent crash-loop hot re-runs.
		i.writeConciergeLastRun()
	}

	now := i.clock()
	lastRun, err := i.readConciergeLastRun()

	var firstRunDelay time.Duration

	switch {
	case err != nil || lastRun.IsZero():
		// Never run before, or unreadable file. Apply startup delay.
		firstRunDelay = i.conciergeStartupDelay
		ancli.Noticef("concierge: no previous run recorded, scheduling first run after %v", firstRunDelay)
	case lastRun.After(now):
		// Clock skew: future timestamp. Treat as never-run.
		ancli.Warnf("concierge: last-run timestamp %v is in the future (now=%v), treating as never-run", lastRun, now)
		firstRunDelay = i.conciergeStartupDelay
	case now.Sub(lastRun) < i.conciergeInterval:
		// Within interval — skip initial run, schedule next at lastRun + interval.
		wait := i.conciergeInterval - now.Sub(lastRun)
		ancli.Noticef("concierge: last run at %v, next run in %v", lastRun, wait.Round(time.Second))
		// Wait, then run, then switch to ticker.
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		do()
		if i.conciergeInterval <= 0 {
			ancli.Noticef("concierge: interval is %v, running once then stopping", i.conciergeInterval)
			return
		}
		tick := time.NewTicker(i.conciergeInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				do()
			}
		}
	default:
		// Past interval — run after startup delay.
		firstRunDelay = i.conciergeStartupDelay
		ancli.Noticef("concierge: last run at %v (elapsed %v > interval %v), scheduling after startup delay %v",
			lastRun, now.Sub(lastRun).Round(time.Second), i.conciergeInterval, firstRunDelay)
	}

	// First-run path: wait the startup delay, then run, then ticker.
	if firstRunDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(firstRunDelay):
		}
	}
	do()
	if i.conciergeInterval <= 0 {
		// Zero or negative interval: run once then stop (no periodic runs).
		ancli.Noticef("concierge: interval is %v, running once then stopping", i.conciergeInterval)
		return
	}
	tick := time.NewTicker(i.conciergeInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			do()
		}
	}
}

// conciergeLastRunPath returns the path to the persisted last-run timestamp file.
func (i *Indexer) conciergeLastRunPath() string {
	return path.Join(i.conciergeCacheDir, "concierge_last_run")
}

// readConciergeLastRun reads the last-run timestamp. Returns zero time if the
// file does not exist or is unreadable.
func (i *Indexer) readConciergeLastRun() (time.Time, error) {
	if i.conciergeCacheDir == "" {
		return time.Time{}, nil
	}
	data, err := os.ReadFile(i.conciergeLastRunPath())
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		return time.Time{}, fmt.Errorf("malformed last-run file: %w", err)
	}
	return t, nil
}

// writeConciergeLastRun persists the current time as the last-run timestamp.
func (i *Indexer) writeConciergeLastRun() {
	if i.conciergeCacheDir == "" {
		return
	}
	now := i.clock()
	err := os.WriteFile(i.conciergeLastRunPath(), []byte(now.Format(time.RFC3339)), 0o644)
	if err != nil {
		ancli.Errf("concierge: failed to write last-run file: %v", err)
	}
}

// subscribeSuggestions returns a channel that receives suggestions payloads
// when a cascade completes. Close the channel when done; the Indexer drops
// closed channels automatically on the next broadcast.
func (i *Indexer) subscribeSuggestions() chan model.SuggestionsPayload {
	ch := make(chan model.SuggestionsPayload, 1)
	i.suggestionSubscribersMu.Lock()
	i.suggestionSubscribers = append(i.suggestionSubscribers, ch)
	i.suggestionSubscribersMu.Unlock()
	return ch
}

// unsubscribeSuggestions removes a subscriber channel. Safe to call with a nil
// or already-closed channel.
func (i *Indexer) unsubscribeSuggestions(ch chan model.SuggestionsPayload) {
	if ch == nil {
		return
	}
	i.suggestionSubscribersMu.Lock()
	defer i.suggestionSubscribersMu.Unlock()
	for idx, sub := range i.suggestionSubscribers {
		if sub == ch {
			i.suggestionSubscribers = append(i.suggestionSubscribers[:idx], i.suggestionSubscribers[idx+1:]...)
			return
		}
	}
}

// broadcastSuggestions sends the payload to all active subscribers.
// Uses non-blocking sends; a slow consumer with a full buffer is skipped.
func (i *Indexer) broadcastSuggestions(payload model.SuggestionsPayload) {
	i.suggestionSubscribersMu.Lock()
	// Prune closed channels while we hold the lock.
	active := i.suggestionSubscribers[:0]
	for _, ch := range i.suggestionSubscribers {
		select {
		case ch <- payload:
			active = append(active, ch)
		default:
			// Consumer is slow or channel is closed; drop it.
			ancli.Warnf("broadcastSuggestions: dropping subscriber (buffer full or closed)")
		}
	}
	i.suggestionSubscribers = active
	i.suggestionSubscribersMu.Unlock()
}

func (i *Indexer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", i.store.ListHandlerFunc())
	mux.HandleFunc("/video/{id}", i.store.VideoHandlerFunc())
	mux.HandleFunc("/streams/{vid}", i.store.StreamListHandlerFunc())
	mux.HandleFunc("/streams/{vid}/stream/{stream_idx}", i.store.StreamHandlerFunc())
	mux.HandleFunc("/image/{id}", i.store.ImageHandlerFunc())
	mux.HandleFunc("/recommend", i.recomendHandler())
	mux.HandleFunc("/suggestions", i.suggestionsHandler())
	mux.HandleFunc("/shows", i.showsHandler())
	mux.HandleFunc("/intro/story", i.introStoryHandler())
	mux.HandleFunc("/intro/session-end", i.introSessionEndHandler())
	mux.HandleFunc("/intro/feedback", i.introFeedbackHandler())
	mux.HandleFunc("/log", loghandler.Func())
	mux.HandleFunc("/ws", i.eventStream())
	return mux
}

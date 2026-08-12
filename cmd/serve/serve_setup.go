package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"os"
	"path"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/butler"
	"github.com/baalimago/kinoview/internal/agents/classifier"
	"github.com/baalimago/kinoview/internal/agents/concierge"
	"github.com/baalimago/kinoview/internal/agents/recommender"
	"github.com/baalimago/kinoview/internal/agents/theatre"
	"github.com/baalimago/kinoview/internal/agents/tools"
	"github.com/baalimago/kinoview/internal/loghandler"
	"github.com/baalimago/kinoview/internal/media"
	"github.com/baalimago/kinoview/internal/media/clientcontext"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/media/stream"
	"github.com/baalimago/kinoview/internal/media/suggestions"
	wd41serve "github.com/baalimago/wd-41/cmd/serve"
)

func (c *command) Setup(ctx context.Context) error {
	relPath := ""

	if c.flagset == nil {
		return errors.New("flagset not set; use the Command function")
	}

	if c.conciergeInterval != nil && *c.conciergeInterval <= 0 {
		return fmt.Errorf("-conciergeInterval must be positive (got %v); set a positive duration or omit the flag to use the default (6h)", *c.conciergeInterval)
	}

	if len(c.flagset.Args()) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		relPath = wd
	} else {
		relPath = c.flagset.Arg(0)
	}
	c.watchPath = path.Clean(relPath)

	storePath := path.Join(*c.configDir, "store")
	subsPath := path.Join(*c.configDir, "subtitles")

	////////////
	// Subtitle stream manager setup
	////////////
	subsManager, err := stream.NewManager(
		stream.WithStoragePath(subsPath),
		stream.WithSubtitleCachePath(*c.cacheDir),
	)
	if err != nil {
		ancli.Warnf("failed to create subtitle stream manager, some features may not work: %v", err)
		subsManager = nil
	}

	suggestionsManager, err := suggestions.NewManager(*c.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to create suggestions manager: %w", err)
	}

	////////////
	// Storage setup (early, without classifier for circular dep resolution)
	////////////
	store := storage.NewStore(
		storage.WithStorePath(storePath),
		storage.WithSubtitlesManager(subsManager),
		storage.WithClassificationWorkers(*c.classificationWorkers),
		storage.WithClassificationRate(*c.classificationRate),
		storage.WithClassificationBurst(*c.classificationBurst),
		storage.WithClassificationStartupCooldown(*c.classificationStartupCooldown),
		storage.WithClassificationTimeout(*c.classificationTimeout),
		storage.WithStartupWriteDelay(*c.startupWriteDelay),
	)
	// Give shutdown a wait point: cancelling the context stops the store's
	// goroutines, and Wait guarantees deferred writes flush before exit.
	c.storeWait = store.Wait

	////////////
	// Classifier setup
	////////////
	var clifier agents.Classifier
	if *c.classificationModel != "" {
		ancli.Noticef("creating new classifier")
		classifierConf := models.Configurations{
			Model:     *c.classificationModel,
			ConfigDir: *c.configDir,
			InternalTools: []models.ToolName{
				models.CatTool,
				models.FindTool,
				models.FFProbeTool,
				models.WebsiteTextTool,
				models.RipGrepTool,
			},
		}
		// Fetch subtitles tool (if OpenSubtitles API key is configured)
		fetchTool := tools.NewFetchSubtitlesTool(store, subsManager, *c.cacheDir)
		if fetchTool != nil {
			clifier = classifier.NewWithTools(classifierConf, []models.LLMTool{fetchTool})
		} else {
			ancli.Warnf("OPENSUBTITLES_API_KEY not set — fetch_subtitles tool will not be available")
			clifier = classifier.New(classifierConf)
		}
		store.SetClassifier(clifier)
	}

	////////////
	// Recommender setup
	////////////
	var r agents.Recommender
	if *c.recommenderModel != "" {
		r = recommender.New(models.Configurations{
			Model:         *c.recommenderModel,
			ConfigDir:     *c.configDir,
			InternalTools: []models.ToolName{},
		})
	}

	////////////
	// Butler setup
	////////////
	var alfred agents.Butler
	if *c.butlerModel != "" {
		if subsManager == nil {
			ancli.Warnf("subsManager not available, skipping butler setup")
		} else {
			alfred = butler.New(
				models.Configurations{
					Model:         *c.butlerModel,
					ConfigDir:     *c.configDir,
					InternalTools: []models.ToolName{},
				}, subsManager,
			)
		}
	}

	////////////
	// User context setup
	////////////
	userContextMgr, err := clientcontext.New(*c.cacheDir)
	if err != nil {
		ancli.Warnf("failed to create user context manager: %v", err)
	}

	////////////
	// Concierge setup
	////////////
	var conkidonk agents.Concierge
	if *c.conciergeModel != "" {
		conkidonk, err = concierge.New(
			concierge.WithItemGetter(store),
			concierge.WithMetadataManager(store),
			concierge.WithSubtitleManager(subsManager),
			concierge.WithSuggestionManager(suggestionsManager),
			concierge.WithConfigDir(*c.configDir),
			concierge.WithStoreDir(storePath),
			concierge.WithCacheDir(*c.cacheDir),
			concierge.WithUserContextManager(userContextMgr),
			concierge.WithModel(*c.conciergeModel),
		)
		if err == nil {
			ancli.Noticef("concierge setup OK")
		} else {
			ancli.Errf("failed to create concierge. His services will not be available: %v", err)
		}
	}

	////////////
	// Theatre setup — the theatre runs the company (decision D13)
	////////////
	// Always constructed: with no model configured it runs composer-only, which
	// is what keeps the intro splash working offline and without an API key.
	// Phase 9 renamed the intro-story flags to -theatre*; the cache path and
	// cooldown semantics are unchanged, so a pre-migration cache still loads.
	bard := theatre.New(
		models.Configurations{
			Model:         *c.theatreModel,
			ConfigDir:     *c.configDir,
			InternalTools: []models.ToolName{},
		}, *c.cacheDir, *c.theatreCooldown,
		// The play takes its theme from whatever was watched most recently.
		// Read lazily: preparation happens long after the request that triggered
		// it, and by then the household may have moved on to something else.
		theatre.WithMuse(theatre.MuseFunc(func() string {
			if userContextMgr == nil {
				return ""
			}
			return theatre.LatestTheme(userContextMgr.AllClientContexts())
		})),
		// Budgets are flags, tuned later from telemetry (decision D8).
		theatre.WithCallBudgets(*c.theatreMaxCalls, *c.theatreGlobalCalls),
		theatre.WithWallClock(*c.theatreWallClock),
		// Mini-agent sessions stream through the house loghandler format
		// (phase 2's serve-side hookup).
		theatre.WithSessionSink(loghandler.Print),
	)
	if *c.theatreModel == "" {
		ancli.Noticef("theatre running composer-only (no -theatre model set)")
	}
	// Get a story ready before anyone shows up, so the first visit is not
	// stuck with a composed one while the LLM sits idle.
	bard.Warm(ctx)

	////////////
	// Indexer setup
	////////////
	indexer, err := media.NewIndexer(
		media.WithStorage(store),
		media.WithRecommender(r),
		media.WithWatchPath(c.watchPath),
		media.WithSuggestionsManager(suggestionsManager),
		// butler may be nil here, intentionally, if subsManager isnt properly setup
		media.WithButler(alfred),
		media.WithConcierge(conkidonk),
		media.WithConciergeStartupDelay(*c.conciergeStartupDelay),
		media.WithClientContextManager(userContextMgr),
		media.WithTheatre(bard),
		media.WithButlerDebounce(*c.butlerDebounce),
		media.WithButlerCacheTTL(*c.butlerCacheTTL),
		media.WithPongGrace(*c.pongGrace),
		media.WithConciergeInterval(*c.conciergeInterval),
		media.WithConciergeTimeout(*c.conciergeTimeout),
		media.WithConciergeCacheDir(*c.cacheDir),
	)
	if err != nil {
		return fmt.Errorf("c.indexer.Setup failed to create Indexer, err: %v", err)
	}
	c.indexer = indexer

	err = c.indexer.Setup(ctx)
	if err != nil {
		return fmt.Errorf("c.indexer.Setup failed to setup Indexer, err: %w", err)
	}

	return nil
}

func (c *command) setupMux() (*http.ServeMux, error) {
	mux := http.NewServeMux()

	if c.pprof != nil && *c.pprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		ancli.Noticef("pprof endpoints enabled at /debug/pprof/")
	}

	subFs, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		return nil, fmt.Errorf("c.Run failed to get frontendFiles sub: %w", err)
	}
	fs := http.FS(subFs)
	fsh := http.FileServer(fs)
	fsh = wd41serve.SlogHandler(fsh)
	fsh = wd41serve.CacheHandler(fsh, *c.cacheControl)
	fsh = wd41serve.CrossOriginIsolationHandler(fsh)
	mux.Handle("/gallery/", http.StripPrefix("/gallery", c.indexer.Handler()))
	mux.Handle("/", fsh)
	return mux, nil
}

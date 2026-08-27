package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"path"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/butler"
	"github.com/baalimago/kinoview/internal/agents/classifier"
	"github.com/baalimago/kinoview/internal/agents/concierge"
	"github.com/baalimago/kinoview/internal/agents/recommender"
	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
	"github.com/baalimago/kinoview/internal/agents/tools"
	"github.com/baalimago/kinoview/internal/agents/troupe"
	"github.com/baalimago/kinoview/internal/media"
	"github.com/baalimago/kinoview/internal/media/clientcontext"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/media/stream"
	"github.com/baalimago/kinoview/internal/media/suggestions"
	wd41serve "github.com/baalimago/wd-41/cmd/serve"
)

// seedNotebook materialises the shared notebook into the worktree. It is a
// var so serve tests can inject a failing seed and pin the degrade path.
var seedNotebook = slivingdoc.Seed

func (c *command) Setup(ctx context.Context) error {
	relPath := ""

	// The troupe facade + play API. All nil when the troupe is disabled (no
	// notebook or no -troupe); the play API then stays unmounted (404).
	var (
		troupeFacade   *troupe.Troupe
		troupeLib      *troupe.PlayLibrary
		troupeFeedback *troupe.FeedbackWriter
	)

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
	// slivingdocWorkspaceRoot is the single shared worktree every agent
	// materialises the notebook into (decision D-2); -slivingdocWorkspace
	// overrides the <cache>/slivingdoc default.
	slivingdocWorkspaceRoot := *c.slivingdocWorkspace

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
	// slivingdoc setup — the shared agent notebook (callsign + worktree) over
	// an external S3 backend. kinoview no longer spawns or supervises
	// SeaweedFS: the backend is a standalone Docker SeaweedFS (see
	// docker-compose.yml) and the operator supplies the matching credentials in
	// the standard AWS env vars. The feature is on when those resolve and
	// -slivingdocDisable is false; any other state logs one warning and the
	// server runs without the notebook.
	////////////
	if !*c.slivingdocDisable {
		accessKey, secretKey := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
		if accessKey == "" || secretKey == "" {
			ancli.Warnf("running without the S3-backed agent notebook: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are not set")
		} else if runner, err := resolveSlivingdoc(*c.slivingdocCommand); err != nil {
			ancli.Warnf("running without the slivingdoc agent notebook: %v", err)
		} else {
			privateRoot := path.Join(*c.cacheDir, "slivingdoc-private")
			envFile := path.Join(*c.configDir, "slivingdoc", "credentials.env")
			if err := os.MkdirAll(path.Dir(envFile), 0o755); err != nil {
				ancli.Warnf("running without the slivingdoc agent notebook: %v", err)
			} else if err := slivingdoc.WriteEnvFile(envFile, *c.slivingdocEndpoint, *c.slivingdocBucket, *c.slivingdocRegion, accessKey, secretKey); err != nil {
				ancli.Warnf("running without the slivingdoc agent notebook: %v", err)
			} else {
				server := slivingdoc.Server(
					runner,
					*c.slivingdocBucket,
					*c.slivingdocRegion,
					*c.slivingdocEndpoint,
					slivingdocWorkspaceRoot,
					privateRoot,
				)
				// The S3 credentials reach the MCP child through the env file
				// clai injects; the seed/pull/commit CLI runs read the same file.
				server.EnvFile = envFile
				if err := seedNotebook(runner, slivingdocWorkspaceRoot, privateRoot, envFile); err != nil {
					ancli.Warnf("slivingdoc worktree seed failed, agents run without the shared notebook: %v", err)
				} else {
					c.slivingdocServer = server
					ancli.Noticef("slivingdoc notebook ready at %s", slivingdocWorkspaceRoot)
				}

				////////////
				// Troupe setup — the director + swarm + critic over the shared
				// notebook (phase 9 cutover: the troupe is the only splash path).
				// Gated on the notebook and -troupe: with either missing the
				// troupe does not start and the play API returns 404. Two flags
				// only (decision 19); the generation cadence is the hardcoded
				// cooldown, driven by the indexer's troupe loop.
				////////////
				if c.slivingdocServer.Name != "" && *c.troupeModel != "" {
					notebook := slivingdoc.NewNotebook(runner, slivingdocWorkspaceRoot, privateRoot, envFile)
					commit := func(filename string) error {
						return notebook.Commit("append " + filename)
					}
					budget := troupe.NewBudget(troupe.WithStoploss(*c.troupeTokenStoploss))
					spawner, err := troupe.NewSpawner(
						troupe.WithModel(*c.troupeModel),
						troupe.WithConfigDir(*c.configDir),
						troupe.WithNotebook(c.slivingdocServer, slivingdocWorkspaceRoot),
						troupe.WithBudget(budget),
						// The live role source: roles are notes the director authors
						// during the generation, so the swarm must read them from the
						// worktree at spawn time, never a setup-time snapshot.
						troupe.WithRoleSource(troupe.NewWorktreeRoleSource(slivingdocWorkspaceRoot)),
					)
					if err != nil {
						return fmt.Errorf("troupe spawner: %w", err)
					}
					submitter, err := troupe.NewSubmitter(slivingdocWorkspaceRoot)
					if err != nil {
						return fmt.Errorf("troupe submitter: %w", err)
					}
					director, err := troupe.NewDirector(
						troupe.WithSwarm(spawner),
						troupe.WithSubmitter(submitter),
						troupe.WithGenerationBudget(budget),
						troupe.WithDirectorModel(*c.troupeModel),
						troupe.WithDirectorConfigDir(*c.configDir),
						troupe.WithDirectorNotebook(c.slivingdocServer, slivingdocWorkspaceRoot),
					)
					if err != nil {
						return fmt.Errorf("troupe director: %w", err)
					}
					criticismWriter, err := troupe.NewCriticismWriter(slivingdocWorkspaceRoot,
						troupe.WithCriticismCommit(commit))
					if err != nil {
						return fmt.Errorf("troupe criticism writer: %w", err)
					}
					critic, err := troupe.NewCritic(
						troupe.WithCriticismWriter(criticismWriter),
						troupe.WithCriticModel(*c.troupeModel),
						troupe.WithCriticConfigDir(*c.configDir),
					)
					if err != nil {
						return fmt.Errorf("troupe critic: %w", err)
					}
					troupeFacade, err = troupe.NewTroupe(
						troupe.WithGenerationDirector(director),
						troupe.WithGenerationCritic(critic),
						troupe.WithMaterialise(func(ctx context.Context) error {
							return slivingdoc.Pull(runner, slivingdocWorkspaceRoot, privateRoot, envFile)
						}),
					)
					if err != nil {
						return fmt.Errorf("troupe facade: %w", err)
					}
					troupeFeedback, err = troupe.NewFeedbackWriter(slivingdocWorkspaceRoot,
						troupe.WithFeedbackCommit(commit))
					if err != nil {
						return fmt.Errorf("troupe feedback writer: %w", err)
					}
					troupeLib, err = troupe.NewPlayLibrary(slivingdocWorkspaceRoot)
					if err != nil {
						return fmt.Errorf("troupe play library: %w", err)
					}
					c.troupeEnabled = true
					ancli.Noticef("troupe ready over the shared notebook at %s", slivingdocWorkspaceRoot)
				}
			}
		}
	}

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
			// The shared agent notebook: a zero callsign (no S3 backend or no
			// slivingdoc command) keeps the concierge running exactly as before.
			concierge.WithSlivingdocServer(c.slivingdocServer),
			concierge.WithSlivingdocWorkspace(slivingdocWorkspaceRoot),
		)
		if err == nil {
			ancli.Noticef("concierge setup OK")
		} else {
			ancli.Errf("failed to create concierge. His services will not be available: %v", err)
		}
	}

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
		// The troupe (phase 9): the generation facade, the play API's read
		// view and the audience feedback gate — all nil when disabled, which
		// leaves the /api/v1/troupe surface unmounted (404).
		media.WithTroupe(troupeFacade),
		media.WithTroupeLibrary(troupeLib),
		media.WithTroupeFeedback(troupeFeedback),
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

// resolveSlivingdoc returns how to invoke the slivingdoc CLI: the explicit
// -slivingdocCommand path is a prebuilt binary (the production arm path) run
// directly; empty resolves npx on PATH and runs the npm package through
// npx -y slivingdoc. npx ships with Node.js, so a missing npx means no
// Node.js — the notebook then degrades with one warning.
func resolveSlivingdoc(explicit string) (slivingdoc.Runner, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return slivingdoc.Runner{}, fmt.Errorf("slivingdoc binary at %q: %w", explicit, err)
		}
		return slivingdoc.BinaryRunner(explicit), nil
	}
	if p, err := exec.LookPath("npx"); err == nil {
		return slivingdoc.NpxRunner(p), nil
	}
	return slivingdoc.Runner{}, errors.New("npx command not found on PATH (install Node.js to enable the slivingdoc notebook)")
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
	if c.troupeEnabled {
		if h := c.indexer.TroupeHandler(); h != nil {
			mux.Handle("/api/v1/troupe/", h)
		}
	}
	mux.Handle("/", fsh)
	return mux, nil
}

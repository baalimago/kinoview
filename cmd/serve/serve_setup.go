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

	var parts troupeParts

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
	// slivingdoc setup — the shared agent notebook (callsign + worktree) over
	// an external S3 backend. kinoview no longer spawns or supervises
	// SeaweedFS: the backend is a standalone Docker SeaweedFS (see
	// docker-compose.yml) and the operator supplies the matching credentials in
	// the standard AWS env vars. The feature is on when those resolve and
	// -slivingdocDisable is false; any other state logs one warning and the
	// server runs without the notebook.
	////////////
	if !*c.slivingdocDisable {
		cfg, err := c.setupNotebook()
		if err != nil {
			ancli.Warnf("running without the slivingdoc agent notebook: %v", err)
		} else if *c.troupeModel != "" {
			parts, err = c.setupTroupe(cfg)
			if err != nil {
				return err
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
			concierge.WithSlivingdocWorkspace(*c.slivingdocWorkspace),
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
		media.WithTroupe(parts.facade),
		media.WithTroupeLibrary(parts.lib),
		media.WithTroupeFeedback(parts.feedback),
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

// troupeParts is what the troupe wiring hands the indexer: the generation
// facade, the play API's read view and the audience feedback gate. All nil
// when the troupe is off, which leaves /api/v1/troupe unmounted.
type troupeParts struct {
	facade   *troupe.Troupe
	lib      *troupe.PlayLibrary
	feedback *troupe.FeedbackWriter
}

// setupNotebook resolves the slivingdoc binary and the S3 credentials, writes
// the credentials env file the CLI children source, seeds the shared worktree
// and registers the MCP callsign. The returned config drives every other
// notebook writer, so the binary, the roots and the env file cannot drift.
func (c *command) setupNotebook() (slivingdoc.Config, error) {
	accessKey, secretKey := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return slivingdoc.Config{}, errors.New("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are not set")
	}
	command, err := resolveSlivingdoc(*c.slivingdocCommand)
	if err != nil {
		return slivingdoc.Config{}, err
	}
	envFile := path.Join(*c.configDir, "slivingdoc", "credentials.env")
	if err := os.MkdirAll(path.Dir(envFile), 0o755); err != nil {
		return slivingdoc.Config{}, fmt.Errorf("notebook credentials dir: %w", err)
	}
	if err := slivingdoc.WriteEnvFile(envFile, *c.slivingdocEndpoint, *c.slivingdocBucket, *c.slivingdocRegion, accessKey, secretKey); err != nil {
		return slivingdoc.Config{}, err
	}
	cfg := slivingdoc.Config{
		Command:       command,
		Bucket:        *c.slivingdocBucket,
		Region:        *c.slivingdocRegion,
		Endpoint:      *c.slivingdocEndpoint,
		WorkspaceRoot: *c.slivingdocWorkspace,
		PrivateRoot:   path.Join(*c.cacheDir, "slivingdoc-private"),
		EnvFile:       envFile,
	}
	if err := seedNotebook(cfg); err != nil {
		return slivingdoc.Config{}, fmt.Errorf("worktree seed: %w", err)
	}
	c.slivingdocServer = slivingdoc.Server(cfg)
	ancli.Noticef("slivingdoc notebook ready at %s", cfg.WorkspaceRoot)
	return cfg, nil
}

// setupTroupe wires the director, swarm and critic over the seeded notebook
// plus the two server-side writers (feedback, criticism) and the play library
// the play API serves. Gated on -troupe only: with no notebook it never runs,
// so the play API stays a 404. The generation cadence is the hardcoded
// cooldown driven by the indexer's troupe loop (decision 19: two flags only).
func (c *command) setupTroupe(cfg slivingdoc.Config) (troupeParts, error) {
	notebook := slivingdoc.NewNotebook(cfg)
	commit := func(filename string) error {
		return notebook.Commit("append " + filename)
	}
	budget := troupe.NewBudget(troupe.WithStoploss(*c.troupeTokenStoploss))
	spawner, err := troupe.NewSpawner(
		troupe.WithModel(*c.troupeModel),
		troupe.WithConfigDir(*c.configDir),
		troupe.WithNotebook(c.slivingdocServer, cfg.WorkspaceRoot),
		troupe.WithBudget(budget),
		// The live role source: roles are notes the director authors during
		// the generation, so the swarm must read them from the worktree at
		// spawn time, never a setup-time snapshot.
		troupe.WithRoleSource(troupe.NewWorktreeRoleSource(cfg.WorkspaceRoot)),
	)
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe spawner: %w", err)
	}
	submitter, err := troupe.NewSubmitter(cfg.WorkspaceRoot)
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe submitter: %w", err)
	}
	director, err := troupe.NewDirector(
		troupe.WithSwarm(spawner),
		troupe.WithSubmitter(submitter),
		troupe.WithGenerationBudget(budget),
		troupe.WithDirectorModel(*c.troupeModel),
		troupe.WithDirectorConfigDir(*c.configDir),
		troupe.WithDirectorNotebook(c.slivingdocServer, cfg.WorkspaceRoot),
	)
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe director: %w", err)
	}
	criticismWriter, err := troupe.NewCriticismWriter(cfg.WorkspaceRoot,
		troupe.WithCriticismCommit(commit))
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe criticism writer: %w", err)
	}
	critic, err := troupe.NewCritic(
		troupe.WithCriticismWriter(criticismWriter),
		troupe.WithCriticModel(*c.troupeModel),
		troupe.WithCriticConfigDir(*c.configDir),
	)
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe critic: %w", err)
	}
	facade, err := troupe.NewTroupe(
		troupe.WithGenerationDirector(director),
		troupe.WithGenerationCritic(critic),
		troupe.WithMaterialise(func(context.Context) error {
			return slivingdoc.Pull(cfg)
		}),
	)
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe facade: %w", err)
	}
	feedback, err := troupe.NewFeedbackWriter(cfg.WorkspaceRoot,
		troupe.WithFeedbackCommit(commit))
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe feedback writer: %w", err)
	}
	lib, err := troupe.NewPlayLibrary(cfg.WorkspaceRoot)
	if err != nil {
		return troupeParts{}, fmt.Errorf("troupe play library: %w", err)
	}
	c.troupeEnabled = true
	ancli.Noticef("troupe ready over the shared notebook at %s", cfg.WorkspaceRoot)
	return troupeParts{facade: facade, lib: lib, feedback: feedback}, nil
}

// resolveSlivingdoc returns the slivingdoc CLI command: the -slivingdocCommand
// path when set, else the release binary on PATH. Both are the same native
// binary (the slivingdoc release asset for the host arch), so the notebook
// needs no runtime package manager.
func resolveSlivingdoc(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("slivingdoc binary at %q: %w", explicit, err)
		}
		return explicit, nil
	}
	if p, err := exec.LookPath("slivingdoc"); err == nil {
		return p, nil
	}
	return "", errors.New("slivingdoc not found on PATH (install the slivingdoc release binary or pass -slivingdocCommand)")
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

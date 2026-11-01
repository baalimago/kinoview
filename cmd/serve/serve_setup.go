package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

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
	if err := slivingdoc.CheckVersion(command); err != nil {
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
	showURL, perceptionErr := c.troupeShowURL()
	budget := troupe.NewBudget(troupe.WithStoploss(*c.troupeTokenStoploss))
	spawner, err := troupe.NewSpawner(
		troupe.WithModel(*c.troupeModel),
		troupe.WithConfigDir(*c.configDir),
		troupe.WithNotebook(c.slivingdocServer, cfg.WorkspaceRoot),
		troupe.WithShowURL(showURL),
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
		troupe.WithDirectorShowURL(showURL),
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
		troupe.WithCriticShowURL(showURL),
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
	if perceptionErr != nil {
		ancli.Noticef("troupe perception off: %v; roles cannot select the browser tools", perceptionErr)
	} else {
		ancli.Noticef("troupe perception: the browser tools inspect the show at %s", showURL)
	}
	return troupeParts{facade: facade, lib: lib, feedback: feedback}, nil
}

// troupeShowURL resolves the perception switch: the local URL the Playwright
// MCP server inspects, or the one reason the troupe has no browser tools. The
// switch follows the operator's Playwright MCP server config in the clai
// config dir the spawned agents load; no startable server, no perception.
// port 0 means the OS chooses the port after setup, so no stable URL exists.
func (c *command) troupeShowURL() (string, error) {
	server, err := readPlaywrightServer(*c.configDir)
	if err != nil {
		return "", err
	}
	if err := server.available(); err != nil {
		return "", err
	}
	tls := *c.tlsCertPath != "" && *c.tlsKeyPath != ""
	showURL := playURL(*c.host, *c.port, tls)
	if showURL == "" {
		return "", errors.New("the show URL is unknown (the port is chosen by the OS)")
	}
	return showURL, nil
}

// playwrightServer is the part of a clai MCP server config the availability
// check needs: a local command, or the endpoint of a remote server.
type playwrightServer struct {
	Command string `json:"command"`
	URL     string `json:"url"`
}

// readPlaywrightServer loads the Playwright MCP server config the spawned
// troupe agents load. A missing file is the normal off state.
func readPlaywrightServer(configDir string) (playwrightServer, error) {
	configPath := playwrightServerPath(configDir)
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return playwrightServer{}, fmt.Errorf("no Playwright MCP server at %s", configPath)
	}
	if err != nil {
		return playwrightServer{}, fmt.Errorf("read Playwright MCP server %s: %w", configPath, err)
	}
	var server playwrightServer
	if err := json.Unmarshal(data, &server); err != nil {
		return playwrightServer{}, fmt.Errorf("parse Playwright MCP server %s: %w", configPath, err)
	}
	return server, nil
}

// available reports whether the server can start on this host and reach the
// show. It mirrors the parts of clai's own config validation that decide
// whether the file yields a server at all: exactly one of command and url,
// and an absolute http or https url. A local command must also resolve on
// PATH, so a config naming a missing binary (for example npx on a host
// without Node) keeps perception off instead of promising the troupe tools
// that never start. A url endpoint must be loopback: the show URL addresses
// this host, so a browser on another host would inspect its own loopback.
func (p playwrightServer) available() error {
	switch {
	case p.Command != "" && p.URL != "":
		return errors.New(`the Playwright MCP server config sets both "command" and "url"; exactly one is required`)
	case p.Command == "" && p.URL == "":
		return errors.New(`the Playwright MCP server config sets neither "command" nor "url"; exactly one is required`)
	}
	if p.URL != "" {
		parsed, err := url.Parse(p.URL)
		if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("the Playwright MCP server url %q is not an absolute http or https URL", p.URL)
		}
		if !isLoopbackHost(parsed.Hostname()) {
			return fmt.Errorf("the Playwright MCP server url %q is not on this host, so it cannot reach the show", p.URL)
		}
		return nil
	}
	if _, err := exec.LookPath(p.Command); err != nil {
		return fmt.Errorf("the Playwright MCP server command %q does not resolve: %w", p.Command, err)
	}
	return nil
}

// isLoopbackHost reports whether host names this machine: a literal loopback
// address or localhost.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// playwrightServerPath returns the clai MCP server config the spawned troupe
// agents load as the browser callsign: the file name is the server name, so
// the tools arrive as mcp_playwright_*.
func playwrightServerPath(configDir string) string {
	return path.Join(claiConfigDir(configDir), "mcpServers", "playwright.json")
}

// claiConfigDir mirrors clai's agent.WithConfigDir suffix rule: a dir already
// ending in clai is used as-is, any other dir gets a clai child. The troupe
// agents and this availability check must resolve the same directory.
func claiConfigDir(configDir string) string {
	if strings.HasSuffix(configDir, "clai") {
		return configDir
	}
	return path.Join(configDir, "clai")
}

// playURL builds the URL a browser uses to render the served play. The browser
// sidecar runs beside kinoview, so a wildcard bind is addressed through
// loopback. port 0 returns an empty URL: the OS chooses the port later.
func playURL(host string, port int, tls bool) string {
	if port == 0 {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	scheme := "http"
	if tls {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
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

package serve

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/s3embed"
)

//go:embed frontend/*
var frontendFiles embed.FS

type Indexer interface {
	Setup(ctx context.Context) error
	Start(ctx context.Context) error
	Handler() http.Handler
	// TroupeHandler returns the /api/v1/troupe mux, or nil when the troupe
	// is disabled (phase 9) — the play API is mounted only then.
	TroupeHandler() http.Handler
}

type command struct {
	indexer Indexer
	// storeWait blocks until the store's background goroutines (deferred-write
	// flush included) have exited. Set during Setup; nil if Setup did not run.
	storeWait func()
	// s3Supervisor owns the SeaweedFS child providing the S3-backed agent
	// notebook. Set during Setup when the weed binary resolved and the child
	// became ready; nil otherwise (the server runs without the notebook).
	s3Supervisor *s3embed.Supervisor
	// slivingdocServer is the MCP callsign for the shared agent notebook,
	// built during Setup when both the weed binary and the slivingdoc command
	// resolved. A zero value means the notebook is disabled and the agents run
	// without it.
	slivingdocServer models.McpServer

	binPath   string
	watchPath string

	configDir *string
	cacheDir  *string

	host *string
	port *int

	flagset      *flag.FlagSet
	cacheControl *string
	tlsCertPath  *string
	tlsKeyPath   *string

	classificationModel           *string
	classificationWorkers         *int
	classificationRate            *float64
	classificationBurst           *int
	classificationStartupCooldown *time.Duration
	classificationTimeout         *time.Duration
	pprof                         *bool
	butlerModel                   *string
	recommenderModel              *string
	conciergeModel                *string
	conciergeStartupDelay         *time.Duration
	startupWriteDelay             *time.Duration
	butlerDebounce                *time.Duration
	butlerCacheTTL                *time.Duration
	pongGrace                     *time.Duration
	conciergeInterval             *time.Duration
	conciergeTimeout              *time.Duration
	// The troupe: two flags only (decision 19).
	troupeModel         *string
	troupeTokenStoploss *int
	// troupeEnabled reports whether Setup wired the troupe facade: the play
	// API is mounted only then, and a missing notebook or model leaves it
	// unmounted (404).
	troupeEnabled bool
	// S3 backend for the shared agent notebook: the supervised SeaweedFS child.
	s3ServerPath *string
	s3ServerPort *int
	s3ServerDir  *string
	s3MasterPort *int
	s3VolumePort *int
	s3FilerPort  *int
	// Shared agent notebook: the slivingdoc CLI over the S3 backend.
	slivingdocCommand   *string
	slivingdocBucket    *string
	slivingdocRegion    *string
	slivingdocEndpoint  *string
	slivingdocWorkspace *string
	slivingdocDisable   *bool
}

func Command() *command {
	defaultModel := ""
	defaultCooldown := 10 * time.Second
	ret := command{
		classificationModel:           &defaultModel,
		recommenderModel:              &defaultModel,
		butlerModel:                   &defaultModel,
		conciergeModel:                &defaultModel,
		conciergeStartupDelay:         new(time.Duration),
		classificationWorkers:         new(int),
		classificationRate:            new(float64),
		classificationBurst:           new(int),
		classificationStartupCooldown: &defaultCooldown,
		classificationTimeout:         new(time.Duration),
		troupeModel:                   &defaultModel,
		troupeTokenStoploss:           new(int),
	}
	*ret.classificationWorkers = 10
	*ret.classificationRate = 0.2
	*ret.classificationBurst = 3
	*ret.classificationTimeout = 5 * time.Minute
	*ret.conciergeStartupDelay = 60 * time.Second
	ret.startupWriteDelay = new(time.Duration)
	*ret.startupWriteDelay = 30 * time.Second
	ret.butlerDebounce = new(time.Duration)
	*ret.butlerDebounce = 30 * time.Second
	ret.pongGrace = new(time.Duration)
	*ret.pongGrace = 10 * time.Second
	ret.butlerCacheTTL = new(time.Duration)
	*ret.butlerCacheTTL = 6 * time.Hour
	ret.conciergeInterval = new(time.Duration)
	*ret.conciergeInterval = 6 * time.Hour
	ret.conciergeTimeout = new(time.Duration)
	*ret.conciergeTimeout = 10 * time.Minute
	ret.s3ServerPath = new(string)
	ret.s3ServerPort = new(int)
	*ret.s3ServerPort = s3embed.DefaultS3Port
	ret.s3MasterPort = new(int)
	*ret.s3MasterPort = s3embed.DefaultMasterPort
	ret.s3VolumePort = new(int)
	*ret.s3VolumePort = s3embed.DefaultVolumePort
	ret.s3FilerPort = new(int)
	*ret.s3FilerPort = s3embed.DefaultFilerPort
	ret.slivingdocCommand = new(string)
	ret.slivingdocBucket = new(string)
	*ret.slivingdocBucket = s3embed.DefaultBucket
	ret.slivingdocRegion = new(string)
	*ret.slivingdocRegion = s3embed.DefaultRegion
	ret.slivingdocEndpoint = new(string)
	ret.slivingdocDisable = new(bool)
	configDir, err := os.UserConfigDir()
	if err != nil {
		ancli.Errf("failed to find user config dir: %v", err)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		ancli.Errf("failed to find user cache dir: %v", err)
	}

	kinoviewConfigDir := path.Join(configDir, "kinoview")
	kinoviewCacheDir := path.Join(cacheDir, "kinoview")
	err = os.MkdirAll(kinoviewConfigDir, 0o755)
	if err != nil {
		ancli.Errf("failed to create: '%v'", kinoviewConfigDir)
	}
	err = os.MkdirAll(kinoviewCacheDir, 0o755)
	if err != nil {
		ancli.Errf("failed to create: '%v'", kinoviewCacheDir)
	}
	r, err := os.Executable()
	if err != nil {
		ancli.Errf("failed to find bin path: %v", err)
	}
	ret.binPath = r
	ret.configDir = &kinoviewConfigDir
	ret.cacheDir = &kinoviewCacheDir
	// The S3 data dir and the notebook worktree default under the config and
	// cache dirs; the Flagset registrations read these computed defaults so
	// -help shows the concrete paths.
	ret.s3ServerDir = new(string)
	*ret.s3ServerDir = path.Join(kinoviewConfigDir, "s3")
	ret.slivingdocWorkspace = new(string)
	*ret.slivingdocWorkspace = path.Join(kinoviewCacheDir, "slivingdoc")
	return &ret
}

func (c *command) startServeRoutine(mux *http.ServeMux, serverErrChan chan error) (func(context.Context) error, error) {
	// Bind explicitly so a port conflict surfaces here, synchronously, and so
	// port 0 can ask the OS for a free ephemeral port.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%v", *c.port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %v: %w", *c.port, err)
	}
	port := *c.port
	if port == 0 {
		port = ln.Addr().(*net.TCPAddr).Port
	}
	s := http.Server{
		Handler:     mux,
		ReadTimeout: 0,
	}
	serveTLS := *c.tlsCertPath != "" && *c.tlsKeyPath != ""

	hostname := *c.host
	protocol := "http"
	if serveTLS {
		protocol = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", protocol, hostname, port)

	ancli.Okf("Server started successfully:")
	ancli.Noticef("- URL: %s", baseURL)
	ancli.Noticef("- Browsing for media in: '%v'", c.watchPath)
	if serveTLS {
		ancli.Noticef("- TLS enabled (cert: '%v', key: '%v')", *c.tlsCertPath, *c.tlsKeyPath)
	} else {
		ancli.Noticef("- TLS disabled")
	}

	go func() {
		if serveTLS {
			err = s.ServeTLS(ln, *c.tlsCertPath, *c.tlsKeyPath)
		} else {
			err = s.Serve(ln)
		}
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	return s.Shutdown, nil
}

func (c *command) Run(ctx context.Context) error {
	mux, err := c.setupMux()
	if err != nil {
		return fmt.Errorf("c.Run failed, err: %v", err)
	}

	serverErrChan := make(chan error, 1)
	fsErrChan := make(chan error, 1)
	serverShutdown, err := c.startServeRoutine(mux, serverErrChan)
	if err != nil {
		return fmt.Errorf("c.Run failed to start server: %w", err)
	}
	go func() {
		ancli.Noticef("starting fsnotify file detector")
		indexErr := c.indexer.Start(ctx)
		if indexErr != nil {
			fsErrChan <- indexErr
		}
	}()
	var retErr error
	normalShutdown := false
	select {
	case <-ctx.Done():
		normalShutdown = true
	case serveErr := <-serverErrChan:
		retErr = serveErr
		break
	case fsErr := <-fsErrChan:
		retErr = fsErr
		break
	}
	ancli.PrintNotice("initiating webserver graceful shutdown")
	err = serverShutdown(ctx)
	if err != nil {
		ancli.Errf("failed to shutdown error: %v", err)
	}
	// Stop the S3 child on every exit path — a supervised child must never be
	// orphaned. Its Stop uses its own bounded window, so a cancelled context
	// does not skip the graceful SIGTERM.
	if c.s3Supervisor != nil {
		if err := c.s3Supervisor.Stop(context.Background()); err != nil {
			ancli.Errf("failed to stop SeaweedFS: %v", err)
		}
	}
	// On a normal shutdown the context is cancelled, which stops the store's
	// goroutines. Wait for them so deferred writes flush before we exit. The
	// error paths leave the context alive, so waiting there would hang.
	if normalShutdown && c.storeWait != nil {
		c.storeWait()
	}
	ancli.Okf("shutdown complete")
	return retErr
}

func (c *command) Help() string {
	return "Serve some filesystem. Set the directory as the second argument: kinoview serve <dir>. If omitted, current wd will be used."
}

func (c *command) Describe() string {
	return fmt.Sprintf("a webserver. Usage: '%v serve <path>'. If <path> is left unfilled, current pwd will be used.", c.binPath)
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	c.host = fs.String("host", "localhost", "hostname to serve on")
	c.port = fs.Int("port", 8080, "port to serve on")

	fs.StringVar(c.cacheDir, "cacheDir", *c.cacheDir, "Set to custom cache dir")
	fs.StringVar(c.configDir, "configDir", *c.configDir, "Set to custom config dir")

	c.cacheControl = fs.String("cacheControl", "no-cache", "set to configure the cache-control header")

	c.tlsCertPath = fs.String("tlsCertPath", "", "set to a path to a cert, requires tlsKeyPath to be set")
	c.tlsKeyPath = fs.String("tlsKeyPath", "", "set to a path to a key, requires tlsCertPath to be set")

	c.classificationModel = fs.String("classifier", "", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.classificationWorkers = fs.Int("classifierWorkers", 2, "set amount of workers used for classification")
	c.pprof = fs.Bool("pprof", false, "enable /debug/pprof/ endpoints for memory profiling")
	c.classificationRate = fs.Float64("classificationRate", 0.2, "classifications per second (1 every 5s default)")
	c.classificationBurst = fs.Int("classificationBurst", 3, "max burst before rate limit kicks in")
	c.classificationStartupCooldown = fs.Duration("classificationStartupCooldown", 10*time.Second, "delay before first classification is admitted")
	c.classificationTimeout = fs.Duration("classifierTimeout", 5*time.Minute, "wall-clock cap for one classification call; a classifier stuck on a looping model is aborted after this and the attempt counts against the item's max-attempts budget")
	c.recommenderModel = fs.String("recommender", "", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.butlerModel = fs.String("butler", "", "set to LLM text model you'd like to use for the butler. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.conciergeModel = fs.String("concierge", "", "set to LLM text model you'd like to use for the concierge. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.conciergeStartupDelay = fs.Duration("conciergeStartupDelay", 60*time.Second, "delay before first concierge run, 0 runs immediately")
	c.startupWriteDelay = fs.Duration("startupWriteDelay", 30*time.Second, "delay before store writes are flushed to disk, 0 writes immediately")
	c.butlerDebounce = fs.Duration("butlerDebounce", 30*time.Second, "minimum interval between butler suggestion cascades; triggers within the window are dropped")
	c.butlerCacheTTL = fs.Duration("butlerCacheTTL", 6*time.Hour, "how long a cached suggestion set is served before re-querying the butler; 0 disables caching")
	c.pongGrace = fs.Duration("pongGrace", 10*time.Second, "grace period after a pong timeout before a disconnect cascade fires; 0 disables")
	c.conciergeInterval = fs.Duration("conciergeInterval", 6*time.Hour, "interval between concierge runs")
	c.conciergeTimeout = fs.Duration("conciergeTimeout", 10*time.Minute, "wall-clock cap for a single concierge run; a run stuck on a looping model is aborted after this and the next run happens at the next interval")

	// The troupe: the director + critic run one generation per cooldown,
	// gated on the shared notebook. Two flags only (decision 19).
	c.troupeModel = fs.String("troupeModel", "", "set to LLM text model you'd like to use for the troupe (director + critic). Supports multiple vendors automatically via clai. If unset, or the shared notebook is disabled, the troupe does not start and the play API returns 404.")
	c.troupeTokenStoploss = fs.Int("troupeTokenStoploss", 0, "token stoploss for one troupe generation: once the generation's cumulative token usage crosses it, spawn_role refuses new spawns (0 disables)")

	// The shared agent notebook: a supervised SeaweedFS child (the S3 backend)
	// and the slivingdoc MCP callsign over it. The feature is on when both
	// binaries resolve and -slivingdocDisable is false; any other state logs
	// one warning and the server runs without the notebook.
	c.s3ServerPath = fs.String("s3ServerPath", "", "path to the weed binary; empty auto-discovers next to the kinoview binary, then weed on PATH")
	c.s3ServerPort = fs.Int("s3ServerPort", s3embed.DefaultS3Port, "S3 gateway listen port for the SeaweedFS child")
	fs.StringVar(c.s3ServerDir, "s3ServerDir", *c.s3ServerDir, "SeaweedFS data dir (default <configDir>/s3)")
	c.s3MasterPort = fs.Int("s3MasterPort", s3embed.DefaultMasterPort, "SeaweedFS master HTTP listen port")
	c.s3VolumePort = fs.Int("s3VolumePort", s3embed.DefaultVolumePort, "SeaweedFS volume server HTTP listen port")
	c.s3FilerPort = fs.Int("s3FilerPort", s3embed.DefaultFilerPort, "SeaweedFS filer HTTP listen port")
	c.slivingdocCommand = fs.String("slivingdocCommand", "", "path to a prebuilt slivingdoc binary; empty runs npx -y slivingdoc")
	c.slivingdocBucket = fs.String("slivingdocBucket", s3embed.DefaultBucket, "S3 bucket backing the shared agent notebook")
	c.slivingdocRegion = fs.String("slivingdocRegion", s3embed.DefaultRegion, "AWS region label for the notebook bucket")
	c.slivingdocEndpoint = fs.String("slivingdocEndpoint", "", "S3 endpoint for the notebook; empty derives http://127.0.0.1:<s3ServerPort> from the supervised SeaweedFS child")
	fs.StringVar(c.slivingdocWorkspace, "slivingdocWorkspace", *c.slivingdocWorkspace, "shared worktree every agent materialises the notebook into (default <cache>/slivingdoc)")
	c.slivingdocDisable = fs.Bool("slivingdocDisable", false, "force-disable the shared agent notebook even when the weed and slivingdoc binaries exist")

	c.flagset = fs
	return fs
}

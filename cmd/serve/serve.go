package serve

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/storyteller"
)

//go:embed frontend/*
var frontendFiles embed.FS

type Indexer interface {
	Setup(ctx context.Context) error
	Start(ctx context.Context) error
	Handler() http.Handler
}

type command struct {
	indexer Indexer
	// storeWait blocks until the store's background goroutines (deferred-write
	// flush included) have exited. Set during Setup; nil if Setup did not run.
	storeWait func()

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
	pprof                         *bool
	butlerModel                   *string
	recommenderModel              *string
	conciergeModel                *string
	conciergeStartupDelay         *time.Duration
	startupWriteDelay             *time.Duration
	storytellerModel              *string
	storytellerCooldown           *time.Duration
	butlerDebounce                *time.Duration
	butlerCacheTTL                *time.Duration
	pongGrace                     *time.Duration
	conciergeInterval             *time.Duration
}

func Command() *command {
	defaultModel := ""
	defaultCooldown := 10 * time.Second
	ret := command{
		classificationModel:           &defaultModel,
		recommenderModel:              &defaultModel,
		butlerModel:                   &defaultModel,
		conciergeModel:                &defaultModel,
		storytellerModel:              &defaultModel,
		conciergeStartupDelay:         new(time.Duration),
		classificationWorkers:         new(int),
		classificationRate:            new(float64),
		classificationBurst:           new(int),
		classificationStartupCooldown: &defaultCooldown,
	}
	*ret.classificationWorkers = 2
	*ret.classificationRate = 0.2
	*ret.classificationBurst = 3
	*ret.conciergeStartupDelay = 60 * time.Second
	ret.storytellerCooldown = new(time.Duration)
	*ret.storytellerCooldown = storyteller.DefaultCooldown
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
	return &ret
}

func (c *command) startServeRoutine(mux *http.ServeMux, serverErrChan chan error) func(context.Context) error {
	s := http.Server{
		Addr:        fmt.Sprintf(":%v", *c.port),
		Handler:     mux,
		ReadTimeout: 0,
	}
	serveTLS := *c.tlsCertPath != "" && *c.tlsKeyPath != ""

	hostname := *c.host
	protocol := "http"
	if serveTLS {
		protocol = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", protocol, hostname, *c.port)

	ancli.Okf("Server started successfully:")
	ancli.Noticef("- URL: %s", baseURL)
	ancli.Noticef("- Browsing for media in: '%v'", c.watchPath)
	if serveTLS {
		ancli.Noticef("- TLS enabled (cert: '%v', key: '%v')", *c.tlsCertPath, *c.tlsKeyPath)
	} else {
		ancli.Noticef("- TLS disabled")
	}

	var err error
	go func() {
		if serveTLS {
			err = s.ListenAndServeTLS(*c.tlsCertPath, *c.tlsKeyPath)
		} else {
			err = s.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	return s.Shutdown
}

func (c *command) Run(ctx context.Context) error {
	mux, err := c.setupMux()
	if err != nil {
		return fmt.Errorf("c.Run failed, err: %v", err)
	}

	serverErrChan := make(chan error, 1)
	fsErrChan := make(chan error, 1)
	serverShutdown := c.startServeRoutine(mux, serverErrChan)
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
	c.recommenderModel = fs.String("recommender", "", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.butlerModel = fs.String("butler", "", "set to LLM text model you'd like to use for the butler. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.conciergeModel = fs.String("concierge", "", "set to LLM text model you'd like to use for the concierge. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.conciergeStartupDelay = fs.Duration("conciergeStartupDelay", 60*time.Second, "delay before first concierge run, 0 runs immediately")
	c.storytellerModel = fs.String("storyteller", "", "set to LLM text model used to write the intro splash story. If unset, a deterministic composer is used instead.")
	c.storytellerCooldown = fs.Duration("storytellerCooldown", storyteller.DefaultCooldown, "minimum time between intro story generations, so refreshes don't each cost an LLM call")
	c.startupWriteDelay = fs.Duration("startupWriteDelay", 30*time.Second, "delay before store writes are flushed to disk, 0 writes immediately")
	c.butlerDebounce = fs.Duration("butlerDebounce", 30*time.Second, "minimum interval between butler suggestion cascades; triggers within the window are dropped")
	c.butlerCacheTTL = fs.Duration("butlerCacheTTL", 6*time.Hour, "how long a cached suggestion set is served before re-querying the butler; 0 disables caching")
	c.pongGrace = fs.Duration("pongGrace", 10*time.Second, "grace period after a pong timeout before a disconnect cascade fires; 0 disables")
	c.conciergeInterval = fs.Duration("conciergeInterval", 6*time.Hour, "interval between concierge runs")

	c.flagset = fs
	return fs
}

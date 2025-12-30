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

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
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

	binPath   string
	configDir string
	watchPath string

	host *string
	port *int

	flagset      *flag.FlagSet
	cacheControl *string
	tlsCertPath  *string
	tlsKeyPath   *string

	classificationModel *string
	butlerModel         *string
	recommenderModel    *string
	conciergeModel      *string
}

func Command() *command {
	configDir, err := os.UserConfigDir()
	if err != nil {
		ancli.Errf("failed to find user config dir: %v", err)
	}
	kinoviewConfigDir := path.Join(configDir, "kinoview")
	r, _ := os.Executable()

	defaultModel := ""
	return &command{
		binPath:             r,
		configDir:           kinoviewConfigDir,
		classificationModel: &defaultModel,
		recommenderModel:    &defaultModel,
		butlerModel:         &defaultModel,
		conciergeModel:      &defaultModel,
	}
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
	select {
	case <-ctx.Done():
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
	ancli.Okf("shutdown complete")
	return retErr
}

func (c *command) Help() string {
	return "Serve some filesystem. Set the directory as the second argument: wd-41 serve <dir>. If omitted, current wd will be used."
}

func (c *command) Describe() string {
	return fmt.Sprintf("a webserver. Usage: '%v serve <path>'. If <path> is left unfilled, current pwd will be used.", c.binPath)
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	c.host = fs.String("host", "localhost", "hostname to serve on")
	c.port = fs.Int("port", 8080, "port to serve on")
	c.cacheControl = fs.String("cacheControl", "no-cache", "set to configure the cache-control header")

	c.tlsCertPath = fs.String("tlsCertPath", "", "set to a path to a cert, requires tlsKeyPath to be set")
	c.tlsKeyPath = fs.String("tlsKeyPath", "", "set to a path to a key, requires tlsCertPath to be set")

	c.classificationModel = fs.String("classifier", "", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.recommenderModel = fs.String("recommender", "", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.butlerModel = fs.String("butler", "", "set to LLM text model you'd like to use for the butler. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")
	c.conciergeModel = fs.String("concierge", "", "set to LLM text model you'd like to use for the concierge. Supports multiple vendors automatically via clai. If unset, feature will be disabled.")

	c.flagset = fs
	return fs
}

package serve

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/classifier"
	"github.com/baalimago/kinoview/internal/agents/recommender"
	"github.com/baalimago/kinoview/internal/media"
	"github.com/baalimago/kinoview/internal/media/storage"
	wd41serve "github.com/baalimago/wd-41/cmd/serve"
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

	generateMetadata    *bool
	classificationModel *string
	recommenderModel    *string
}

func Command() *command {
	configDir, err := os.UserConfigDir()
	if err != nil {
		ancli.Errf("failed to find user config dir: %v", err)
	}
	kinoviewConfigDir := path.Join(configDir, "kinoview")
	r, _ := os.Executable()
	if err != nil {
		ancli.Errf("failed to create indexer: %v", err)
		return nil
	}

	return &command{
		binPath:   r,
		configDir: kinoviewConfigDir,
	}
}

func (c *command) Setup(ctx context.Context) error {
	relPath := ""

	if c.flagset == nil {
		return errors.New("flagset not set; use the Command function")
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

	if c.configDir == "" {
		userCfgDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config dir: %v", err)
		}
		c.configDir = userCfgDir
	}

	if _, err := os.Stat(c.configDir); os.IsNotExist(err) {
		ancli.Noticef("config dir non-existent, creating: '%v'", c.configDir)
		if err := os.MkdirAll(c.configDir, 0o755); err != nil {
			return fmt.Errorf("could not create config dir: %w", err)
		}
	}
	claiPath := path.Join(c.configDir)
	storePath := path.Join(c.configDir, "store")
	classificationModel := "gpt-5"
	if c.classificationModel != nil {
		classificationModel = *c.classificationModel
	}
	recommenderModel := "gpt-5"
	if c.recommenderModel != nil {
		recommenderModel = *c.recommenderModel
	}
	indexer, err := media.NewIndexer(
		media.WithStorage(
			storage.NewStore(
				storage.WithStorePath(storePath),
				storage.WithClassificationWorkers(5),
				storage.WithClassifier(classifier.NewClassifier(models.Configurations{
					Model:     classificationModel,
					ConfigDir: claiPath,
					InternalTools: []models.ToolName{
						models.CatTool,
						models.FindTool,
						models.FFProbeTool,
						models.WebsiteTextTool,
						models.RipGrepTool,
					},
				})),
			),
		),
		media.WithRecommender(recommender.NewRecommender(models.Configurations{
			Model:         recommenderModel,
			ConfigDir:     claiPath,
			InternalTools: []models.ToolName{},
		})),
		media.WithWatchPath(c.watchPath),
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

	c.generateMetadata = fs.Bool("generateMetadata", true, "set to true if you want LLM generated metadata using clai")
	c.classificationModel = fs.String("classificationModel", "gpt-5", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai.")
	c.recommenderModel = fs.String("recommenderModel", "gpt-5", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai.")

	c.flagset = fs
	return fs
}

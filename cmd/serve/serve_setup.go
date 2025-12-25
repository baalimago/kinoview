package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/butler"
	"github.com/baalimago/kinoview/internal/agents/classifier"
	"github.com/baalimago/kinoview/internal/agents/concierge"
	"github.com/baalimago/kinoview/internal/agents/recommender"
	"github.com/baalimago/kinoview/internal/media"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/media/subtitles"
	"github.com/baalimago/kinoview/internal/media/suggestions"
	"github.com/baalimago/kinoview/internal/media/usercontext"
	wd41serve "github.com/baalimago/wd-41/cmd/serve"
)

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
	storePath := path.Join(c.configDir, "store")
	subsPath := path.Join(c.configDir, "subtitles")
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		ancli.Warnf("failed to get user cache dir: %v", err)
	}
	suggestionsManager, err := suggestions.NewManager(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to create suggestions manager: %w", err)
	}

	////////////
	// Recommender setup
	////////////
	recommender := recommender.New(models.Configurations{
		Model:         *c.recommenderModel,
		ConfigDir:     c.configDir,
		InternalTools: []models.ToolName{},
	})

	////////////
	// Butler setup
	////////////
	subsManager, err := subtitles.NewManager(subtitles.WithStoragePath(
		subsPath,
	))
	var b agents.Butler
	if err != nil {
		ancli.Warnf("failed to setup subsManager, skipping butler setup. subsManager error: %v", err)
	} else {
		b = butler.New(models.Configurations{
			Model:         *c.classificationModel,
			ConfigDir:     c.configDir,
			InternalTools: []models.ToolName{},
		}, subsManager,
		)
	}

	////////////
	// Storage setup
	////////////
	store := storage.NewStore(
		storage.WithStorePath(storePath),
		storage.WithSubtitlesManager(subsManager),
		storage.WithClassificationWorkers(5),
		storage.WithClassifier(classifier.New(models.Configurations{
			Model:     *c.classificationModel,
			ConfigDir: c.configDir,
			InternalTools: []models.ToolName{
				models.CatTool,
				models.FindTool,
				models.FFProbeTool,
				models.WebsiteTextTool,
				models.RipGrepTool,
			},
		})),
	)

	////////////
	// User context setup
	////////////
	userContextMgr, err := usercontext.New(cacheDir)
	if err != nil {
		ancli.Warnf("failed to create user context manager: %v", err)
	}

	////////////
	// Concierge setup
	////////////
	concidonk, err := concierge.New(
		concierge.WithItemGetter(store),
		concierge.WithMetadataManager(store),
		concierge.WithSubtitleManager(subsManager),
		concierge.WithSuggestionManager(suggestionsManager),
		concierge.WithSubtitleSelector(butler.NewSelector(models.Configurations{
			Model:     *c.classificationModel,
			ConfigDir: c.configDir,
		})),
		concierge.WithConfigDir(c.configDir),
		concierge.WithStoreDir(storePath),
		concierge.WithCacheDir(cacheDir),
		concierge.WithUserContextManager(userContextMgr),
		concierge.WithModel("gpt-5.2"),
	)
	if err != nil {
		ancli.Errf("failed to create concierge. His services will not be available: %v", err)
	} else {
		ancli.Noticef("concierge setup OK")
	}

	////////////
	// Indexer setup
	////////////
	indexer, err := media.NewIndexer(
		media.WithStorage(store),
		media.WithRecommender(recommender),
		media.WithWatchPath(c.watchPath),
		media.WithSuggestionsManager(suggestionsManager),
		// butler may be nil here, intentionally, if subsManager isnt properly setup
		media.WithButler(b),
		media.WithConcierge(concidonk),
		media.WithUserContextManager(userContextMgr),
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

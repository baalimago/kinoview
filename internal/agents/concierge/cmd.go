package concierge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/butler"
	"github.com/baalimago/kinoview/internal/media/clientcontext"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/media/stream"
	"github.com/baalimago/kinoview/internal/media/suggestions"
)

type command struct {
	con       agents.Concierge
	configDir *string
	cacheDir  *string
	model     *string
}

func Command() *command {
	ret := command{
		model: misc.Pointer("gpt-5.2"),
	}
	configDir, err := os.UserConfigDir()
	if err == nil {
		ret.configDir = misc.Pointer(path.Join(configDir, "kinoview"))
	} else {
		ancli.Errf("failed to find user config dir: %v", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err == nil {
		ret.cacheDir = misc.Pointer(path.Join(cacheDir, "kinoview"))
	} else {
		ancli.Errf("failed to find user cache dir: %v", err)
	}
	return &ret
}

func (c *command) Describe() string {
	return "Command concierge will trigger the concierge on the current live media state"
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("concierge", flag.ContinueOnError)
	fs.StringVar(c.configDir, "confDir", *c.configDir, "Overwrite config dir")
	fs.StringVar(c.cacheDir, "cacheDir", *c.cacheDir, "Overwrite cache dir")
	fs.StringVar(c.model, "model", *c.model, "Model to use for all internal agentic systems")
	return fs
}

func (c *command) Help() string {
	return "Don't we all need some help sometimes?"
}

func (c *command) Run(ctx context.Context) error {
	ancli.Okf("concierge running with model: %v", *c.model)
	resp, err := c.con.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to run concierge: %w", err)
	} else {
		ancli.Okf("concierge returned: %v", resp)
	}
	return nil
}

func (c *command) Setup(ctx context.Context) error {
	if c.configDir == nil {
		// The user as most likely not been able to find config dir with os.UserConfigDir
		return errors.New("config dir is nil, please set it using -configDir flag")
	}

	if c.cacheDir == nil {
		return errors.New("cache dir is nil, please set it using -cacheDir flag")
	}

	ancli.Noticef("configDir: '%v', cacheDir: '%v'", *c.configDir, *c.cacheDir)

	subsPath := path.Join(*c.configDir, "subtitles")
	storePath := path.Join(*c.configDir, "store")
	suggestionsManager, err := suggestions.NewManager(*c.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to create suggestions manager: %w", err)
	}
	subsManager, err := stream.NewManager(stream.WithStoragePath(
		subsPath,
	))
	if err != nil {
		return fmt.Errorf("failed to setup stream manager: %w", err)
	}
	store := storage.NewStore(
		storage.WithStorePath(storePath),
		storage.WithSubtitlesManager(subsManager),
	)

	// Ignore error channel as we only want to load persisted items
	_, err = store.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup store: %v", err)
	}
	userContextMgr, err := clientcontext.New(*c.cacheDir)
	if err != nil {
		ancli.Warnf("failed to create user context manager: %v", err)
	}

	conkidonk, err := New(
		WithItemGetter(store),
		WithMetadataManager(store),
		WithSubtitleManager(subsManager),
		WithSuggestionManager(suggestionsManager),
		WithSubtitleSelector(butler.NewSelector(models.Configurations{
			Model:     *c.model,
			ConfigDir: *c.configDir,
		})),
		WithConfigDir(*c.configDir),
		WithStoreDir(storePath),
		WithCacheDir(*c.cacheDir),
		WithUserContextManager(userContextMgr),
		WithModel(*c.model),
	)
	if err != nil {
		return fmt.Errorf("failed to setup concierge: %w", err)
	}
	c.con = conkidonk
	return nil
}

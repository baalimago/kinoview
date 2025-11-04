package classify

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/classifier"
	"github.com/baalimago/kinoview/internal/media"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/model"
)

type stationStorage interface {
	storage.ClassificationStation
	media.Storage
}

type command struct {
	binPath   string
	configDir string
	storePath string

	model   *string
	workers *int

	store stationStorage

	flagset *flag.FlagSet
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
		storePath: path.Join(kinoviewConfigDir, "store"),
	}
}

func (c *command) Describe() string {
	return "Run classification on existing items."
}

func (c *command) Help() string {
	return "I dont quite know what this command should do yet, I cant help you. Maybe sometime soon."
}

func (c *command) Setup(ctx context.Context) error {
	c.store = storage.NewStore(
		storage.WithStorePath(c.storePath),
		storage.WithClassificationWorkers(5),
		storage.WithClassifier(classifier.NewClassifier(models.Configurations{
			Model:     *c.model,
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
	_, err := c.store.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup store: %w", err)
	}
	return nil
}

func (c *command) Run(ctx context.Context) error {
	errChan := make(chan error)
	go func() {
		err := c.store.StartClassificationStation(ctx)
		if err != nil {
			errChan <- err
		}
	}()
	items := c.store.Snapshot()

	time.Sleep(time.Second)
	reader := bufio.NewReader(os.Stdin)
	ancli.Okf("Found: '%v' items. Filter by name (empty for all): ", len(items))
	filter, _ := reader.ReadString('\n')
	filter = strings.TrimSpace(filter)
	filteredItems := make([]model.Item, 0)
	for _, i := range items {
		if filter == "" ||
			strings.Contains(
				strings.ToLower(i.Name),
				strings.ToLower(filter),
			) {
			// Slight hack for now, metadata is mostly for videos
			if strings.Contains(i.MIMEType, "video") {
				filteredItems = append(filteredItems, i)
			}
		}
	}
	ancli.Okf("Found: '%v' items. Proceed to classify? (y/N): ", len(filteredItems))
	resp, _ := reader.ReadString('\n')
	resp = strings.TrimSpace(strings.ToLower(resp))
	ancli.Newline = true
	if resp != "y" && resp != "yes" {
		return errors.New("user abort")
	}
	for _, i := range filteredItems {
		c.store.AddToClassificationQueue(i)
	}

	select {
	case <-ctx.Done():
		return nil
	case classifyErr := <-errChan:
		return classifyErr
	}
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)

	c.model = fs.String("model", "gpt-5", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai.")
	c.workers = fs.Int("workers", 5, "set amount of workers to classify the media with")

	c.flagset = fs
	return fs
}

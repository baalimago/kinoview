package classify

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

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
	filter    string

	flagset *flag.FlagSet

	model   *string
	workers *int

	store stationStorage

	userInput io.Reader
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
		userInput: os.Stdin,
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

	if c.flagset != nil {
		c.filter = strings.Join(c.flagset.Args(), " ")
	}
	return nil
}

func (c *command) findFilteredItems() []model.Item {
	filteredItems := make([]model.Item, 0)
	for _, i := range c.store.Snapshot() {
		if c.filter == "" ||
			strings.Contains(
				strings.ToLower(i.Name),
				strings.ToLower(c.filter),
			) {
			// Slight hack for now, metadata is mostly for videos
			if strings.Contains(i.MIMEType, "video") {
				filteredItems = append(filteredItems, i)
			}
		}
	}
	return filteredItems
}

func determineIfProceed(w io.Reader) bool {
	reader := bufio.NewReader(w)
	resp, _ := reader.ReadString('\n')
	resp = strings.TrimSpace(strings.ToLower(resp))
	if resp == "y" || resp == "yes" {
		return true
	}
	return false
}

func (c *command) startClassificationStation(ctx context.Context) (chan error, error) {
	errChan := make(chan error)
	go func() {
		err := c.store.StartClassificationStation(ctx)
		if err != nil {
			errChan <- err
		}
	}()

	select {
	case <-c.store.Ready():
		return errChan, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func errorMonitor(ctx context.Context, errChan chan error) error {
	select {
	case <-ctx.Done():
		return nil
	case classifyErr := <-errChan:
		return classifyErr
	}
}

func (c *command) Run(ctx context.Context) error {
	errChan, err := c.startClassificationStation(ctx)
	if err != nil {
		return fmt.Errorf("classify.Run failed to startClassificationStation: %w", err)
	}
	filteredItems := c.findFilteredItems()

	ancli.Okf("Found: '%v' items. Proceed to classify? (y/N): ", len(filteredItems))
	shouldProceed := determineIfProceed(c.userInput)

	if !shouldProceed {
		return errors.New("user abort")
	}

	for _, i := range filteredItems {
		c.store.AddToClassificationQueue(i)
	}

	return errorMonitor(ctx, errChan)
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)

	c.model = fs.String("model", "gpt-5", "set to LLM text model you'd like to use for the classifier. Supports multiple vendors automatically via clai.")
	c.workers = fs.Int("workers", 5, "set amount of workers to classify the media with")

	c.flagset = fs
	return fs
}

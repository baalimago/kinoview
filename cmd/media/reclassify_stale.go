package media

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/model"
)

// staleStore is the slice of the store this command needs.
type staleStore interface {
	Snapshot() []model.Item
	ClearClassificationStopLoss(id string) (bool, error)
	ClassificationMaxAttempts() int
}

type reclassifyStaleCmd struct {
	storePath string
	dryRun    bool
	force     bool
	flagset   *flag.FlagSet

	store staleStore
}

func reclassifyStaleCommand() *reclassifyStaleCmd {
	cfgDir, err := os.UserConfigDir()
	storePath := ""
	if err == nil {
		storePath = path.Join(cfgDir, "kinoview", "store")
	}
	return &reclassifyStaleCmd{storePath: storePath}
}

func (c *reclassifyStaleCmd) Describe() string {
	return "Reset the classification stop-loss on items that hit the max-attempts ceiling."
}

func (c *reclassifyStaleCmd) Help() string {
	return `= media reclassify-stale =

Items that fail classification too many times are permanently skipped, so a
transient problem — a missing API key, a rate limit, a bad network — can park
media in a state the server will never retry.

This resets the attempt counter on exactly those items, so the running server
picks them up again on its next pass. Metadata is left alone: this re-opens
classification, it does not discard what an item already has.

It only touches items at or above the ceiling. Anything still inside its normal
retry budget is left for the server's own backoff to handle.

Flags:
  -store-path   Path to the kinoview store directory
  -dry-run      List what would be reset and change nothing
  -force        Skip the confirmation prompt

The server re-reads items as it goes, so this can be run while kinoview is up.`
}

func (c *reclassifyStaleCmd) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("reclassify-stale", flag.ExitOnError)
	fs.StringVar(&c.storePath, "store-path", c.storePath, "Path to kinoview store directory")
	fs.BoolVar(&c.dryRun, "dry-run", false, "List affected items without changing anything")
	fs.BoolVar(&c.force, "force", false, "Skip the confirmation prompt")
	c.flagset = fs
	return fs
}

func (c *reclassifyStaleCmd) Setup(ctx context.Context) error {
	if c.flagset == nil {
		return errors.New("flagset can't be nil")
	}
	return nil
}

func (c *reclassifyStaleCmd) Run(ctx context.Context) error {
	if _, err := os.Stat(c.storePath); os.IsNotExist(err) {
		return fmt.Errorf("store path does not exist: %v", c.storePath)
	}

	if c.store == nil {
		// Classifier is nil on purpose: this command only rewrites state on disk,
		// it never classifies anything itself.
		s := storage.NewStore(
			storage.WithStorePath(c.storePath),
			storage.WithClassifier(nil),
		)
		if _, err := s.Setup(ctx); err != nil {
			return fmt.Errorf("failed to setup store: %w", err)
		}
		c.store = s
	}

	maxAttempts := c.store.ClassificationMaxAttempts()
	stale := staleItems(c.store.Snapshot(), maxAttempts)

	if len(stale) == 0 {
		ancli.Okf("No items are blocked by the classification stop-loss (ceiling is %v attempts).", maxAttempts)
		return nil
	}

	ancli.Noticef("%v item(s) blocked at >= %v attempts:", len(stale), maxAttempts)
	for _, i := range stale {
		reason := i.ClassificationError
		if reason == "" {
			reason = "no error recorded"
		}
		fmt.Printf("  %-40.40s attempts=%-3v %v\n", i.Name, i.ClassificationAttempts, reason)
	}

	if c.dryRun {
		ancli.Noticef("Dry run: nothing changed.")
		return nil
	}

	if !c.force && !readYesNo(fmt.Sprintf("Reset the stop-loss on %v item(s)? (y/N): ", len(stale))) {
		ancli.Noticef("Cancelled.")
		return nil
	}

	var reset int
	var failed []error
	for _, i := range stale {
		changed, err := c.store.ClearClassificationStopLoss(i.ID)
		if err != nil {
			// Keep going: one unwritable item should not strand the rest.
			failed = append(failed, err)
			continue
		}
		if changed {
			reset++
		}
	}

	ancli.Okf("Reset the stop-loss on %v item(s). The server will retry them on its next pass.", reset)
	for _, err := range failed {
		ancli.Errf("%v", err)
	}
	if len(failed) > 0 {
		return fmt.Errorf("%v item(s) could not be reset", len(failed))
	}
	return nil
}

// staleItems returns the items that classification has permanently given up on.
func staleItems(items []model.Item, maxAttempts int) []model.Item {
	var out []model.Item
	for _, i := range items {
		if i.ClassificationAttempts >= maxAttempts {
			out = append(out, i)
		}
	}
	return out
}

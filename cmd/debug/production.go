package debug

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/theatre"
)

// productionCmd renders one theatre generation's production dialog from the
// company files: the transcript as a readable script, the ledger as the final
// summary. It never modifies company files.
type productionCmd struct {
	flagset  *flag.FlagSet
	cacheDir *string
}

func productionCommand() *productionCmd {
	c := &productionCmd{cacheDir: new("")}
	if dir, err := os.UserCacheDir(); err == nil {
		c.cacheDir = new(path.Join(dir, "kinoview"))
	} else {
		ancli.Errf("failed to find user cache dir: %v", err)
	}
	return c
}

func (c *productionCmd) Describe() string {
	return "Render a theatre generation's production dialog from transcript and ledger"
}

func (c *productionCmd) Help() string {
	return "Usage: kinoview debug production <generationID> [-cacheDir <dir>]\n\n" +
		"Renders one generation's transcript and ledger as a readable dialog.\n" +
		"The generation id is the short id printed on theatre feed lines, e.g.\n" +
		"stry_ab12. Company files live under <cacheDir>/intro/company/."
}

func (c *productionCmd) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("production", flag.ContinueOnError)
	fs.StringVar(c.cacheDir, "cacheDir", *c.cacheDir, "Overwrite cache dir")
	c.flagset = fs
	return fs
}

func (c *productionCmd) Setup(ctx context.Context) error {
	if c.cacheDir == nil || *c.cacheDir == "" {
		return errors.New("cache dir is empty, please set it using -cacheDir flag")
	}
	return nil
}

func (c *productionCmd) Run(ctx context.Context) error {
	args := c.flagset.Args()
	if len(args) != 1 {
		return errors.New("usage: kinoview debug production <generationID>")
	}
	dialog, err := theatre.RenderDialog(theatre.Open(*c.cacheDir), args[0])
	if err != nil {
		return err
	}
	fmt.Print(dialog)
	return nil
}

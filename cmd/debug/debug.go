package debug

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/baalimago/go_away_boilerplate/pkg/cmd"
	"github.com/baalimago/kinoview/internal/agents/concierge"
)

const usage = `= Debug =

Use the debug subcommands to tweak prompts and investigate potential issues without
starting adjacent large clunky systems.

Commands:
%v`

var commands = map[string]cmd.Command{
	"con|concierge": concierge.Command(),
}

type command struct {
	flagset *flag.FlagSet
}

func Command() *command {
	return &command{}
}

func (c *command) Describe() string {
	return "Debug subsystems by targetting them with some subcommand."
}

func (c *command) Help() string {
	return "Use the debug command to debug different sub-systems and agents without initiating all. See each subcommand for additional information."
}

func (c *command) Setup(ctx context.Context) error {
	if c.flagset == nil {
		return errors.New("flagset cant be nil")
	}
	return nil
}

func (c *command) Run(ctx context.Context) error {
	exitCode := cmd.Run(ctx, os.Args[:2], commands, usage)
	if exitCode > 0 {
		return fmt.Errorf("non nil exit code from: '%v', code: %v", c.flagset.Args(), exitCode)
	}
	return nil
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("debug", flag.ContinueOnError)

	c.flagset = fs
	return fs
}

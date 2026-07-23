// Package media provides kinoview subcommands for interacting with the media
// store directly from the CLI without starting the full server stack.
package media

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/baalimago/go_away_boilerplate/pkg/cmd"
)

const usage = `= Media =

Interact with the kinoview media store directly from the CLI.
Commands allow browsing, inspecting, deleting, and reclassifying items
without starting the full server.

Commands:
%v`

var subcommands = map[string]cmd.Command{
	"l|list": listCommand(),
}

func run(ctx context.Context, args []string) int {
	return cmd.Run(ctx, args, subcommands, usage)
}

type command struct {
	flagset *flag.FlagSet
}

// Command returns the top-level "media" command ready for registration in main.
func Command() *command {
	return &command{}
}

func (c *command) Describe() string {
	return "Interact with the media store from the CLI — list, inspect, delete, reclassify."
}

func (c *command) Help() string {
	return "Use 'media list' to browse and manage media items. See subcommand help for details."
}

func (c *command) Setup(ctx context.Context) error {
	if c.flagset == nil {
		return errors.New("flagset can't be nil")
	}
	return nil
}

func (c *command) Run(ctx context.Context) error {
	args := append([]string{os.Args[0]}, c.flagset.Args()...)
	exitCode := run(ctx, args)
	if exitCode > 0 {
		return fmt.Errorf("media subcommand exited with code %v", exitCode)
	}
	return nil
}

func (c *command) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("media", flag.ContinueOnError)
	c.flagset = fs
	return fs
}

package concierge

import (
	"context"
	"flag"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

type command struct{}

func Command() *command {
	return &command{}
}

func (c *command) Describe() string {
	return "Command concierge will trigger the concierge on the current live media state"
}

func (c *command) Flagset() *flag.FlagSet {
	return flag.NewFlagSet("concierge", flag.ContinueOnError)
}

func (c *command) Help() string {
	return "Don't we all need some help sometimes?"
}

func (c *command) Run(ctx context.Context) error {
	ancli.Okf("concierge command running")
	return nil
}

func (c *command) Setup(ctx context.Context) error {
	ancli.Okf("concierge command setting up")
	return nil
}

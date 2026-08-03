package serve

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/agents/theatre"
)

func TestSetup_ModelsEmptyString_DisablesAgents(t *testing.T) {
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	c.configDir = new(t.TempDir())

	// Explicitly ensure all models are empty strings, should keep agents nil.
	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = new(1)

	err := c.Setup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.indexer == nil {
		t.Fatalf("expected indexer to be set")
	}
}

// The theatre's generation budgets are flags with the theatre's defaults:
// the director's call cap, the global call cap and the wall clock, all
// documented in the flag help.
func TestCommand_TheatreBudgetFlagsDefault(t *testing.T) {
	c := Command()
	c.Flagset()

	if c.theatreMaxCalls == nil || *c.theatreMaxCalls != theatre.DefaultDirectorBudget {
		t.Errorf("theatreMaxCalls = %v, want the director budget default %d", *c.theatreMaxCalls, theatre.DefaultDirectorBudget)
	}
	if c.theatreGlobalCalls == nil || *c.theatreGlobalCalls != theatre.DefaultGlobalBudget {
		t.Errorf("theatreGlobalCalls = %v, want the global budget default %d", *c.theatreGlobalCalls, theatre.DefaultGlobalBudget)
	}
	if c.theatreWallClock == nil || *c.theatreWallClock != theatre.DefaultWallClock {
		t.Errorf("theatreWallClock = %v, want the wall-clock default %v", *c.theatreWallClock, theatre.DefaultWallClock)
	}
	if c.theatreCooldown == nil || *c.theatreCooldown != theatre.DefaultCooldown {
		t.Errorf("theatreCooldown = %v, want %v", *c.theatreCooldown, theatre.DefaultCooldown)
	}
}

// The new flags parse from the command line and are honoured by Setup: a
// small budget still yields a working composer-only indexer.
func TestSetup_TheatreBudgetFlagsParseAndApply(t *testing.T) {
	c := Command()
	fs := c.Flagset()
	if err := fs.Parse([]string{
		"-theatreMaxCalls", "30",
		"-theatreGlobalCalls", "120",
		"-theatreWallClock", "5m",
	}); err != nil {
		t.Fatal(err)
	}
	if *c.theatreMaxCalls != 30 || *c.theatreGlobalCalls != 120 || *c.theatreWallClock != 5*time.Minute {
		t.Errorf("flags not applied: maxCalls=%d globalCalls=%d wallClock=%v",
			*c.theatreMaxCalls, *c.theatreGlobalCalls, *c.theatreWallClock)
	}

	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = new(1)
	c.configDir = new(t.TempDir())
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup with budget flags: %v", err)
	}
}

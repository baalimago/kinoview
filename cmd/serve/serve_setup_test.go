package serve

import (
	"context"
	"flag"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/misc"
)

func TestSetup_ModelsEmptyString_DisablesAgents(t *testing.T) {
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	c.configDir = misc.Pointer(t.TempDir())

	// Explicitly ensure all models are empty strings, should keep agents nil.
	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = misc.Pointer(1)

	err := c.Setup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.indexer == nil {
		t.Fatalf("expected indexer to be set")
	}
}

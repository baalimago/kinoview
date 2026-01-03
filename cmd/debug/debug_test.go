package debug

import (
	"context"
	"strings"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/cmd"
)

func TestCommand_Flagset_SetsInternalFlagset(t *testing.T) {
	c := Command()
	if c.flagset != nil {
		t.Fatalf("expected flagset to start nil")
	}

	fs := c.Flagset()
	if fs == nil {
		t.Fatalf("expected returned flagset to be non-nil")
	}
	if c.flagset == nil {
		t.Fatalf("expected internal flagset to be set")
	}
	if fs != c.flagset {
		t.Fatalf("expected returned flagset to match internal flagset")
	}
	if fs.Name() != "debug" {
		t.Fatalf("expected flagset name 'debug', got %q", fs.Name())
	}
}

func TestCommand_Setup_ErrWhenFlagsetNil(t *testing.T) {
	c := Command()
	if err := c.Setup(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestCommand_Setup_OkAfterFlagset(t *testing.T) {
	c := Command()
	c.Flagset()
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestCommand_Run_ExitCodeZeroOk(t *testing.T) {
	saved := runFn
	t.Cleanup(func() { runFn = saved })

	runFn = func(context.Context, []string, map[string]cmd.Command, string) int {
		return 0
	}

	c := Command()
	c.Flagset()
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestCommand_Run_NonZeroExitCodeErrors(t *testing.T) {
	saved := runFn
	t.Cleanup(func() { runFn = saved })

	runFn = func(context.Context, []string, map[string]cmd.Command, string) int {
		return 7
	}

	c := Command()
	c.Flagset()

	err := c.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "code: 7") {
		t.Fatalf("expected error to contain exit code, got: %v", err)
	}
}

package serve

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetup_ConfigDirNotSet_UsesUserConfigDir(t *testing.T) {
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	// ensure branch where configDir is empty
	c.configDir = ""

	err := c.Setup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.configDir == "" {
		t.Fatalf("expected configDir to be set")
	}
	ucd, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir unexpectedly failed: %v", err)
	}
	if !filepath.IsAbs(c.configDir) {
		t.Fatalf("expected absolute configDir, got %q", c.configDir)
	}
	if !strings.HasPrefix(filepath.Clean(c.configDir), filepath.Clean(ucd)) {
		t.Fatalf("configDir=%q does not appear to be under UserConfigDir=%q", c.configDir, ucd)
	}
}

func TestSetup_SuggestionsManagerCreationFails(t *testing.T) {
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	c.configDir = t.TempDir()

	// suggestions.NewManager(cacheDir) uses os.MkdirAll(<cacheDir>/kinoview).
	// So make cacheDir an existing *file* to force MkdirAll to fail with ENOTDIR.
	cacheBase := filepath.Join(t.TempDir(), "cachebase")
	if err := os.WriteFile(cacheBase, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to create file for cacheBase: %v", err)
	}

	oldHome := os.Getenv("HOME")
	oldXDGCache := os.Getenv("XDG_CACHE_HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
		_ = os.Setenv("XDG_CACHE_HOME", oldXDGCache)
	})

	// Ensure os.UserCacheDir returns our chosen path.
	_ = os.Setenv("HOME", t.TempDir())
	_ = os.Setenv("XDG_CACHE_HOME", cacheBase)

	err := c.Setup(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create suggestions manager") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetup_ModelsEmptyString_DisablesAgents(t *testing.T) {
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	c.configDir = t.TempDir()

	// Explicitly ensure all models are empty strings, should keep agents nil.
	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty

	err := c.Setup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.indexer == nil {
		t.Fatalf("expected indexer to be set")
	}
}

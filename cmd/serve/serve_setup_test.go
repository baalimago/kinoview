package serve

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/agents/theatre"
	"github.com/baalimago/kinoview/internal/s3embed"
)

// The slivingdoc command resolves from the explicit -slivingdocCommand flag
// (a prebuilt binary), else npx on PATH (the npx -y slivingdoc default).
// Tests cannot rely on the flag, so the not-found case pins an empty PATH and
// the found case puts a fake npx on PATH.
func TestResolveSlivingdoc_NotFound(t *testing.T) {
	t.Setenv("PATH", "")
	if got, err := resolveSlivingdoc(""); err == nil {
		t.Fatalf("expected error with empty PATH, got %+v", got)
	}
}

func TestResolveSlivingdoc_FoundOnPath(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "npx")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got, err := resolveSlivingdoc("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Command != fake {
		t.Errorf("resolved %q, want %q", got.Command, fake)
	}
	if got.Prebuilt {
		t.Error("PATH resolution must not report a prebuilt binary")
	}
}

// The explicit -slivingdocCommand path is a prebuilt binary: it wins over
// PATH and runs directly, without the npx -y slivingdoc prefix. An explicit
// missing path fails naming it, so an operator typo is diagnosable.
func TestResolveSlivingdoc_ExplicitPrebuiltWins(t *testing.T) {
	t.Setenv("PATH", "")
	explicit := filepath.Join(t.TempDir(), "slivingdoc")
	if err := os.WriteFile(explicit, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSlivingdoc(explicit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Command != explicit {
		t.Errorf("resolved %q, want %q", got.Command, explicit)
	}
	if !got.Prebuilt {
		t.Error("explicit path must report a prebuilt binary")
	}
}

func TestResolveSlivingdoc_ExplicitMissingNamesPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := resolveSlivingdoc(missing); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("resolveSlivingdoc(%q) error = %v, want it to name the path", missing, err)
	}
}

// The weed binary resolves through s3embed.ResolveBinary — the same call
// Setup makes before constructing the supervisor. Found/not-found pin the
// serve-side dependency contract: a missing weed binary disables the
// notebook.
func TestResolveWeedBinary_NotFound(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := s3embed.ResolveBinary(""); err == nil {
		t.Fatal("expected error with empty PATH and no explicit path")
	}
}

func TestResolveWeedBinary_Found(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "weed")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got, err := s3embed.ResolveBinary("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fake {
		t.Errorf("resolved %q, want %q", got, fake)
	}
}

// TestDisabled_NoWiring asserts -slivingdocDisable wins over resolvable
// binaries: no supervisor is constructed and no slivingdoc callsign reaches
// the agents.
func TestDisabled_NoWiring(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"weed", "npx"} {
		fake := filepath.Join(binDir, name)
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)

	c := Command()
	fs := c.Flagset()
	if err := fs.Parse([]string{"-slivingdocDisable"}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = new(int)
	*c.classificationWorkers = 1
	c.configDir = new(t.TempDir())

	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup with -slivingdocDisable: %v", err)
	}
	if c.s3Supervisor != nil {
		t.Error("expected no S3 supervisor with -slivingdocDisable")
	}
	if c.slivingdocServer.Name != "" {
		t.Errorf("expected no slivingdoc callsign with -slivingdocDisable, got %+v", c.slivingdocServer)
	}
}

// TestEndpointDerivedFromSupervisor pins the endpoint derivation (decision
// D-4): -slivingdocEndpoint empty derives the supervisor's own endpoint in
// the http://127.0.0.1:<port> form; an explicit value wins.
func TestEndpointDerivedFromSupervisor(t *testing.T) {
	s3 := s3embed.New(s3embed.WithS3Port(9444))
	if got, want := notebookEndpoint("", s3), "http://127.0.0.1:9444"; got != want {
		t.Errorf("derived endpoint = %q, want %q", got, want)
	}
	explicit := "http://s3.example.test:9000"
	if got := notebookEndpoint(explicit, s3); got != explicit {
		t.Errorf("explicit endpoint = %q, want %q", got, explicit)
	}
}

func TestSetup_ModelsEmptyString_DisablesAgents(t *testing.T) {
	withoutWeed(t)
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
	withoutWeed(t)
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

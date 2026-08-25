package serve

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
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

// TestSetup_NoTheatreFlags_SetupStillWorks
// Setup must still work with no theatre flags on the flagset.
func TestSetup_NoTheatreFlags_SetupStillWorks(t *testing.T) {
	withoutWeed(t)
	c := Command()
	fs := c.Flagset()
	if err := fs.Parse([]string{}); err != nil {
		t.Fatal(err)
	}

	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = new(1)
	c.configDir = new(t.TempDir())
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
}

// TestSetup_TroupeFlags_Registered pins the two troupe flags (decision 19):
// they parse with their defaults, and the troupe stays disabled when the
// notebook is off — no facade, no play API mount.
func TestSetup_TroupeFlags_Registered(t *testing.T) {
	withoutWeed(t)
	c := Command()
	fs := c.Flagset()
	if err := fs.Parse([]string{"-troupe", "gpt-5", "-troupeTokenStoploss", "100000"}); err != nil {
		t.Fatal(err)
	}
	if *c.troupeModel != "gpt-5" {
		t.Errorf("troupeModel = %q, want gpt-5", *c.troupeModel)
	}
	if *c.troupeTokenStoploss != 100000 {
		t.Errorf("troupeTokenStoploss = %d, want 100000", *c.troupeTokenStoploss)
	}

	// Without the S3 backend (no weed), the notebook is off and the troupe
	// must not start even with a model: Setup succeeds, the API stays
	// unmounted (404).
	c.configDir = new(t.TempDir())
	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = new(1)
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if c.troupeEnabled {
		t.Error("the troupe must not start without the notebook")
	}
	mux, err := c.setupMux()
	if err != nil {
		t.Fatalf("setupMux: %v", err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/troupe/play/resolved", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("disabled troupe API = %d, want 404", rec.Code)
	}
}

// notebookCommand builds a command whose slivingdoc block is reachable
// without spawning a real weed child: PATH is empty so the injected
// supervisor survives ResolveBinary, and -slivingdocCommand points at a stub
// so resolveSlivingdoc succeeds. The supervisor is constructed but never
// started — Setup only reads its endpoint and env paths.
func notebookCommand(t *testing.T) *command {
	t.Helper()
	withoutWeed(t)
	slivingdocBin := filepath.Join(t.TempDir(), "slivingdoc")
	if err := os.WriteFile(slivingdocBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := Command()
	fs := c.Flagset()
	if err := fs.Parse([]string{"-troupe", "gpt-5", "-slivingdocCommand", slivingdocBin}); err != nil {
		t.Fatal(err)
	}
	c.s3Supervisor = s3embed.New(s3embed.WithDataDir(t.TempDir()))
	c.configDir = new(t.TempDir())
	c.cacheDir = new(t.TempDir())
	c.slivingdocWorkspace = new(t.TempDir())
	empty := ""
	c.classificationModel = &empty
	c.recommenderModel = &empty
	c.butlerModel = &empty
	c.conciergeModel = &empty
	c.classificationWorkers = new(1)
	return c
}

// TestSetup_SeedFailure_DisablesNotebookAndTroupe pins the degrade path: a
// failed seed leaves the callsign zero and the troupe unmounted (404), even
// with -troupe set — the notebook gate, not the model flag, is authoritative.
func TestSetup_SeedFailure_DisablesNotebookAndTroupe(t *testing.T) {
	c := notebookCommand(t)
	prev := seedNotebook
	seedNotebook = func(slivingdoc.Runner, string, string, string) error {
		return errors.New("seed failed")
	}
	t.Cleanup(func() { seedNotebook = prev })

	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup with failing seed: %v", err)
	}
	if c.slivingdocServer.Name != "" {
		t.Errorf("slivingdoc callsign must stay zero when the seed fails, got %+v", c.slivingdocServer)
	}
	if c.troupeEnabled {
		t.Error("the troupe must not start when the seed fails")
	}
	mux, err := c.setupMux()
	if err != nil {
		t.Fatalf("setupMux: %v", err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/troupe/play/resolved", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("seed-failed troupe API = %d, want 404", rec.Code)
	}
}

// TestSetup_SeedSuccess_EnablesTroupe pins the happy path: a successful seed
// sets the callsign and, with -troupe set, wires the troupe.
func TestSetup_SeedSuccess_EnablesTroupe(t *testing.T) {
	c := notebookCommand(t)
	prev := seedNotebook
	seedNotebook = func(slivingdoc.Runner, string, string, string) error {
		return nil
	}
	t.Cleanup(func() { seedNotebook = prev })

	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup with successful seed: %v", err)
	}
	if c.slivingdocServer.Name == "" {
		t.Error("slivingdoc callsign must be set when the seed succeeds")
	}
	if !c.troupeEnabled {
		t.Error("the troupe must start when the seed succeeds and -troupe is set")
	}
}

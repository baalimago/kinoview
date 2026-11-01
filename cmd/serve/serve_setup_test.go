package serve

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
)

// The slivingdoc command resolves from the explicit -slivingdocCommand flag,
// else the release binary on PATH. Tests cannot rely on the flag, so the
// not-found case pins an empty PATH and the found case puts a fake slivingdoc
// on PATH.
func TestResolveSlivingdoc_NotFound(t *testing.T) {
	t.Setenv("PATH", "")
	if got, err := resolveSlivingdoc(""); err == nil {
		t.Fatalf("expected error with empty PATH, got %q", got)
	}
}

func TestResolveSlivingdoc_FoundOnPath(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "slivingdoc")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got, err := resolveSlivingdoc("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fake {
		t.Errorf("resolved %q, want %q", got, fake)
	}
}

// The explicit -slivingdocCommand path wins over PATH. An explicit missing
// path fails naming it, so an operator typo is diagnosable.
func TestResolveSlivingdoc_ExplicitWins(t *testing.T) {
	t.Setenv("PATH", "")
	explicit := filepath.Join(t.TempDir(), "slivingdoc")
	if err := os.WriteFile(explicit, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSlivingdoc(explicit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != explicit {
		t.Errorf("resolved %q, want %q", got, explicit)
	}
}

func TestResolveSlivingdoc_ExplicitMissingNamesPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := resolveSlivingdoc(missing); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("resolveSlivingdoc(%q) error = %v, want it to name the path", missing, err)
	}
}

// TestDisabled_NoWiring asserts -slivingdocDisable wins over resolvable
// credentials and binaries: no slivingdoc callsign reaches the agents.
func TestDisabled_NoWiring(t *testing.T) {
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
	if c.slivingdocServer.Name != "" {
		t.Errorf("expected no slivingdoc callsign with -slivingdocDisable, got %+v", c.slivingdocServer)
	}
}

func TestSetup_ModelsEmptyString_DisablesAgents(t *testing.T) {
	withoutCredentials(t)
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
	withoutCredentials(t)
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
	withoutCredentials(t)
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

	// Without the S3 backend (no AWS credentials), the notebook is off and the
	// troupe must not start even with a model: Setup succeeds, the API stays
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
// without talking to a real S3 backend: the AWS credentials are set so Setup
// writes the env file, and -slivingdocCommand points at a stub so
// resolveSlivingdoc succeeds. The seed is the injected seam (see the Seed
// success/failure tests below).
func notebookCommand(t *testing.T) *command {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "kinoview")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "kinoview-notebook-secret")
	slivingdocBin := filepath.Join(t.TempDir(), "slivingdoc")
	if err := os.WriteFile(slivingdocBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := Command()
	fs := c.Flagset()
	if err := fs.Parse([]string{"-troupe", "gpt-5", "-slivingdocCommand", slivingdocBin}); err != nil {
		t.Fatal(err)
	}
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
	seedNotebook = func(slivingdoc.Config) error {
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
	seedNotebook = func(slivingdoc.Config) error {
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

// TestClaiConfigDir pins the suffix rule the perception availability check
// mirrors from clai: a dir already ending in clai is used as-is, any other dir
// gets a clai child.
func TestClaiConfigDir(t *testing.T) {
	cases := map[string]string{
		"/home/u/.config/kinoview": "/home/u/.config/kinoview/clai",
		"/custom/clai":             "/custom/clai",
	}
	for in, want := range cases {
		if got := claiConfigDir(in); got != want {
			t.Errorf("claiConfigDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsLoopbackHost pins the locality rule: only this machine's addresses
// can reach the show the troupe inspects.
func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"0.0.0.0":     false,
		"example.com": false,
		"":            false,
	}
	for host, want := range cases {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// writePerceptionServer writes the Playwright MCP server config the troupe
// agents load into the clai config dir under the test's config dir.
func writePerceptionServer(t *testing.T, configDir, body string) {
	t.Helper()
	cfg := filepath.Join(configDir, "clai", "mcpServers", "playwright.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTroupeShowURL pins the perception switch: a perception server config in
// the clai config dir the spawned agents load turns perception on, but only
// when the server can start here; a missing config, a malformed config and a
// command that does not resolve all leave it off.
func TestTroupeShowURL(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string // empty: no config file
		want string
	}{
		{"no perception config", "", ""},
		{"command on PATH", `{"command":` + strconv.Quote(executable) + `}`, "http://localhost:8080/"},
		{"loopback endpoint", `{"url":"http://127.0.0.1:8931/mcp"}`, "http://localhost:8080/"},
		{"remote endpoint", `{"url":"http://browserbox:8931/mcp"}`, ""},
		{"url without a scheme", `{"url":"127.0.0.1:8931"}`, ""},
		{"both transports", `{"command":` + strconv.Quote(executable) + `,"url":"http://127.0.0.1:8931/mcp"}`, ""},
		{"command off PATH", `{"command":"kinoview-no-such-perception-server"}`, ""},
		{"command and url absent", `{}`, ""},
		{"malformed config", `{`, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := Command()
			c.Flagset()
			dir := t.TempDir()
			c.configDir = &dir
			if tt.body != "" {
				writePerceptionServer(t, dir, tt.body)
			}
			if got := c.troupeShowURL(); got != tt.want {
				t.Errorf("show URL = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPlayURL pins the browser address: wildcard binds are addressed through
// loopback, TLS changes the scheme, and port 0 yields no stable URL.
func TestPlayURL(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		tls  bool
		want string
	}{
		{"localhost", "localhost", 8080, false, "http://localhost:8080/"},
		{"wildcard ipv4", "0.0.0.0", 80, false, "http://127.0.0.1:80/"},
		{"empty host", "", 80, false, "http://127.0.0.1:80/"},
		{"tls", "localhost", 443, true, "https://localhost:443/"},
		{"port zero", "localhost", 0, false, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := playURL(tt.host, tt.port, tt.tls); got != tt.want {
				t.Errorf("playURL(%q, %d, %v) = %q, want %q", tt.host, tt.port, tt.tls, got, tt.want)
			}
		})
	}
}

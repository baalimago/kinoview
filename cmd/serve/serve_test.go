package serve

import (
	"context"
	"flag"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

func TestSetup(t *testing.T) {
	withoutCredentials(t)
	t.Run("error if flagset is not set", func(t *testing.T) {
		c := &command{}
		err := c.Setup(context.Background())
		if err == nil {
			t.Error("expected error for nil flagset")
		}
	})

	t.Run("watchPath set from Getwd when no args", func(t *testing.T) {
		c := Command()
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		want, _ := os.Getwd()
		c.classificationWorkers = new(1)
		err := c.Setup(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.watchPath != path.Clean(want) {
			t.Errorf("watchPath = %v, want %v", c.watchPath, want)
		}
	})

	t.Run("watchPath set from first arg", func(t *testing.T) {
		c := Command()
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		_ = c.flagset.Parse([]string{"/tmp"})
		c.classificationWorkers = new(1)
		err := c.Setup(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.watchPath != path.Clean("/tmp") {
			t.Errorf("watchPath = %v, want /tmp", c.watchPath)
		}
	})

	t.Run("configDir is created if missing", func(t *testing.T) {
		dir := t.TempDir()
		c := Command()
		c.configDir = new(path.Join(dir, "doesnotexist"))
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		c.classificationWorkers = new(1)
		if err := c.Setup(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(*c.configDir); err != nil {
			t.Errorf("configDir not created: %v", err)
		}
	})

	t.Run("table-driven: argument handling", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
		}{
			{"no args", nil},
			{"with arg", []string{"/tmp"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c := Command()
				c.classificationWorkers = new(1)
				c.flagset = flag.NewFlagSet("x", flag.ContinueOnError)
				if tt.args != nil {
					_ = c.flagset.Parse(tt.args)
				}
				if err := c.Setup(context.Background()); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("validate side effects", func(t *testing.T) {
		dir := t.TempDir()
		c := Command()
		c.configDir = new(path.Join(dir, "abc"))
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		c.classificationWorkers = new(1)
		_ = c.Setup(context.Background())
		if _, err := os.Stat(*c.configDir); err != nil {
			t.Error("side effect: configDir not created")
		}
	})

	t.Run("clean up after test run", func(t *testing.T) {
		dir := t.TempDir()
		c := Command()
		c.configDir = new(path.Join(dir, "gone"))
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		_ = c.Setup(context.Background())
		if err := os.RemoveAll(*c.configDir); err != nil {
			t.Errorf("cleanup failed: %v", err)
		}
	})
}

// withoutCredentials clears the AWS env vars so Setup degrades to no
// notebook: a missing credential pair is the gate that disables the shared
// notebook (the external Docker SeaweedFS is the S3 backend).
func withoutCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
}

// TestSetup_S3DegradesGracefully: with no AWS credentials, Setup still
// succeeds and the server runs without the S3-backed notebook — no slivingdoc
// callsign.
func TestSetup_S3DegradesGracefully(t *testing.T) {
	withoutCredentials(t)
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	c.configDir = new(t.TempDir())
	c.classificationWorkers = new(int)
	*c.classificationWorkers = 1

	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.slivingdocServer.Name != "" {
		t.Fatalf("expected no slivingdoc callsign without S3 credentials, got %+v", c.slivingdocServer)
	}
}

func TestSetup_ZeroIntervalRejected(t *testing.T) {
	withoutCredentials(t)
	c := Command()
	c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
	c.configDir = new(t.TempDir())
	c.classificationWorkers = new(int)
	*c.classificationWorkers = 1

	zero := time.Duration(0)
	c.conciergeInterval = &zero

	err := c.Setup(context.Background())
	if err == nil {
		t.Fatal("expected error for zero conciergeInterval")
	}
	if !strings.Contains(err.Error(), "-conciergeInterval must be positive") {
		t.Errorf("error message does not explain the rejection: %v", err)
	}
}

func TestRun(t *testing.T) {
	t.Run("successful run", func(t *testing.T) {
		withoutCredentials(t)
		c := Command()
		c.Flagset()
		c.configDir = new(t.TempDir())
		ctx, cancel := context.WithTimeout(context.Background(), time.Second/2)
		t.Cleanup(func() {
			cancel()
		})
		// Port 0 asks the OS for a free ephemeral port, so repeated runs
		// (-count=3) and parallel package runs can never collide.
		c.port = new(int)
		err := c.Setup(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c.configDir = new(t.TempDir())
		c.watchPath = t.TempDir()
		err = c.Run(ctx)
		if err != nil {
			t.Errorf("unexpected error during Run: %v", err)
		}
	})
}

func TestTroupeMountPointIsFullscreen(t *testing.T) {
	// The troupe replaces the old fullscreen intro overlay: #troupe must be a
	// fixed, viewport-filling layer, or the engine renders into a 0-height box
	// and the splash is invisible / a thin strip.
	css, err := frontendFiles.ReadFile("frontend/style.css")
	if err != nil {
		t.Fatalf("read embedded style.css: %v", err)
	}
	body := string(css)
	if !strings.Contains(body, "#troupe") {
		t.Fatal("style.css has no #troupe rule")
	}
	for _, want := range []string{
		"position: fixed",
		"width: 100%",
		"height: 100%",
		"overflow: hidden",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("#troupe rule is missing %q", want)
		}
	}
	// The splash is gated on `.live`: an empty stage (no submitted play) must
	// not paint a black wall over the gallery. The blocking audience control
	// must be styled too.
	for _, want := range []string{
		"#troupe.live",
		".troupe-feedback",
		".troupe-feedback-thumb",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("troupe frontend is missing %q", want)
		}
	}
}

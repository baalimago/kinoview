package serve

import (
	"context"
	"flag"
	"os"
	"path"
	"testing"
	"time"
)

func Test_Setup(t *testing.T) {
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
		c.configDir = path.Join(dir, "doesnotexist")
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		if err := c.Setup(context.Background()); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if _, err := os.Stat(c.configDir); err != nil {
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
				c.flagset = flag.NewFlagSet("x", flag.ContinueOnError)
				if tt.args != nil {
					_ = c.flagset.Parse(tt.args)
				}
				if err := c.Setup(context.Background()); err != nil {
					t.Errorf("fail: %v", err)
				}
			})
		}
	})

	t.Run("validate side effects", func(t *testing.T) {
		dir := t.TempDir()
		c := Command()
		c.configDir = path.Join(dir, "abc")
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		_ = c.Setup(context.Background())
		if _, err := os.Stat(c.configDir); err != nil {
			t.Error("side effect: configDir not created")
		}
	})

	t.Run("clean up after test run", func(t *testing.T) {
		dir := t.TempDir()
		c := Command()
		c.configDir = path.Join(dir, "gone")
		c.flagset = flag.NewFlagSet("test", flag.ContinueOnError)
		_ = c.Setup(context.Background())
		if err := os.RemoveAll(c.configDir); err != nil {
			t.Errorf("cleanup failed: %v", err)
		}
	})
}

func Test_Run(t *testing.T) {
	t.Run("successful run", func(t *testing.T) {
		c := Command()
		c.Flagset()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second/2)
		t.Cleanup(func() {
			cancel()
		})
		err := c.Setup(ctx)
		if err != nil {
			t.Fatal(err)
		}
		c.configDir = t.TempDir()
		err = c.Run(ctx)
		if err != nil {
			t.Errorf("unexpected error during Run: %v", err)
		}
	})
}

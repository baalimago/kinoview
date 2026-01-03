package concierge

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

type stubConcierge struct {
	runFn func(ctx context.Context) (string, error)
}

func (s stubConcierge) Setup(context.Context) error { return nil }

func (s stubConcierge) Run(ctx context.Context) (string, error) {
	if s.runFn != nil {
		return s.runFn(ctx)
	}
	return "ok", nil
}

func TestCommand_DescribeAndHelp_AreStable(t *testing.T) {
	c := &command{}
	if got := c.Describe(); got == "" {
		t.Fatalf("Describe() returned empty string")
	}
	if got := c.Help(); got == "" {
		t.Fatalf("Help() returned empty string")
	}
}

func TestCommand_Flagset_BindsPointersAndParses(t *testing.T) {
	conf := "/default/conf"
	cache := "/default/cache"
	model := "gpt-test"
	c := &command{configDir: &conf, cacheDir: &cache, model: &model}

	fs := c.Flagset()
	if fs == nil {
		t.Fatal("Flagset() returned nil")
	}
	if fs.Name() != "concierge" {
		t.Fatalf("unexpected flagset name: %q", fs.Name())
	}

	if err := fs.Parse([]string{"-confDir=/x/conf", "-cacheDir=/y/cache", "-model=llm"}); err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if *c.configDir != "/x/conf" {
		t.Fatalf("configDir not updated, got %q", *c.configDir)
	}
	if *c.cacheDir != "/y/cache" {
		t.Fatalf("cacheDir not updated, got %q", *c.cacheDir)
	}
	if *c.model != "llm" {
		t.Fatalf("model not updated, got %q", *c.model)
	}
}

func TestCommand_Flagset_NilPointersPanics_RegressionGuard(t *testing.T) {
	// Flagset() dereferences *c.configDir, *c.cacheDir, *c.model.
	// This test documents that behavior so accidental changes are caught.
	c := &command{}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic due to nil pointers, got none")
		}
	}()
	_ = c.Flagset()
}

func TestCommand_Run_WrapsConciergeError(t *testing.T) {
	c := &command{con: stubConcierge{runFn: func(ctx context.Context) (string, error) {
		return "", errors.New("boom")
	}}}

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to run concierge") {
		t.Fatalf("expected wrapped message; got: %v", err)
	}
}

func TestCommand_Run_SuccessReturnsNil(t *testing.T) {
	c := &command{con: stubConcierge{runFn: func(ctx context.Context) (string, error) {
		return "resp", nil
	}}}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestCommand_Setup_NilConfigDirFails(t *testing.T) {
	cache := "/tmp/cache"
	model := "gpt-test"
	c := &command{configDir: nil, cacheDir: &cache, model: &model}

	err := c.Setup(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "config dir is nil") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Regression guard: error should mention a flag name at all.
	if !strings.Contains(err.Error(), "flag") {
		t.Fatalf("error should guide user to a flag; got: %v", err)
	}
}

func TestCommand_Setup_NilCacheDirFails(t *testing.T) {
	conf := "/tmp/conf"
	model := "gpt-test"
	c := &command{configDir: &conf, cacheDir: nil, model: &model}

	err := c.Setup(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cache dir is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Regression guard: keep message aligned with actual flag.
	if !strings.Contains(err.Error(), "-cacheDir") {
		t.Fatalf("error should reference -cacheDir; got: %v", err)
	}
}

func TestCommand_Flagset_ContainsExpectedFlags(t *testing.T) {
	conf := "/default/conf"
	cache := "/default/cache"
	model := "gpt-test"
	c := &command{configDir: &conf, cacheDir: &cache, model: &model}
	fs := c.Flagset()

	want := []string{"confDir", "cacheDir", "model"}
	for _, name := range want {
		if fs.Lookup(name) == nil {
			t.Fatalf("expected flag %q to exist", name)
		}
	}

	// Ensure ContinueOnError is used (regression guard: CLI shouldn't os.Exit on parse errors)
	if fs.ErrorHandling() != flag.ContinueOnError {
		t.Fatalf("expected ContinueOnError")
	}
}

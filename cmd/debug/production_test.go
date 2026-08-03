package debug

import (
	"context"
	"strings"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/agents/theatre"
)

// fixtureGeneration writes a small production (transcript + ledger) into a
// cache dir, as a real run would have left it. It writes the company files
// directly — no Stage, so no feed — the CLI test is about the renderer, not
// the stage.
func fixtureGeneration(t *testing.T, cacheDir string) {
	t.Helper()
	co := theatre.Open(cacheDir)
	for _, ev := range []theatre.TranscriptEvent{
		{Gen: "stry_ab12", Kind: "phase", From: "stage", Body: "phase 1/6 brief ─ budget 0/50"},
		{Gen: "stry_ab12", Kind: "post", From: "director", To: "dramaturg", Body: "brief (mood=standoff, lineup=3)"},
		{Gen: "stry_ab12", Kind: "submit", From: "stage", Body: `submitted "The Long Night" — 0.0s, 1 calls, 0 consults`, Level: "ok"},
	} {
		if err := co.AppendTranscript(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := co.SaveLedger(theatre.Ledger{
		Generation:  "stry_ab12",
		Phase:       "submitted",
		PhaseIndex:  6,
		PhasesTotal: 6,
		Budget:      theatre.Budget{DirectorUsed: 1, DirectorMax: 50, GlobalUsed: 1, GlobalMax: 200},
		Actors:      []theatre.Actor{{Role: "director", Status: "active", Calls: 1, LastAction: "read_story"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProductionCommand_RendersFixtureGeneration(t *testing.T) {
	cacheDir := t.TempDir()
	fixtureGeneration(t, cacheDir)

	c := productionCommand()
	c.cacheDir = &cacheDir
	c.Flagset()
	if err := c.flagset.Parse([]string{"stry_ab12"}); err != nil {
		t.Fatal(err)
	}

	out := testboil.CaptureStdout(t, func(t *testing.T) {
		if err := c.Run(context.Background()); err != nil {
			t.Errorf("Run failed: %v", err)
		}
	})
	for _, want := range []string{
		"── production stry_ab12 ──",
		"[1] ─ phase 1/6 brief ─ budget 0/50",
		"director→dramaturg: brief (mood=standoff, lineup=3)",
		`✓ submitted "The Long Night"`,
		"ledger: phase submitted (6/6) · director 1/50 calls",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog missing %q\nfull dialog:\n%s", want, out)
		}
	}
}

func TestProductionCommand_UnknownGenerationErrors(t *testing.T) {
	cacheDir := t.TempDir()
	fixtureGeneration(t, cacheDir)

	c := productionCommand()
	c.cacheDir = &cacheDir
	c.Flagset()
	if err := c.flagset.Parse([]string{"stry_zzz"}); err != nil {
		t.Fatal(err)
	}

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unknown generation")
	}
	if !strings.Contains(err.Error(), "no such generation") {
		t.Errorf("error = %v, want a 'no such generation' error", err)
	}
}

func TestProductionCommand_RequiresGenerationID(t *testing.T) {
	cacheDir := t.TempDir()
	c := productionCommand()
	c.cacheDir = &cacheDir
	c.Flagset()

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected a usage error without a generation id")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("error = %v, want a usage error", err)
	}
}

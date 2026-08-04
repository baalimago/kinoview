package theatre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLedger_MissingIsZero(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	l, err := co.LoadLedger()
	if err != nil {
		t.Fatalf("missing ledger must not error: %v", err)
	}
	if l.Generation != "" || l.Budget != (Budget{}) {
		t.Errorf("expected zero ledger, got %+v", l)
	}
}

// A corrupt ledger degrades to the zero ledger, never a crash.
func TestLoadLedger_CorruptFileFallsBackToZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, ledgerFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir).LoadLedger()
	if err == nil {
		t.Fatal("expected an error for a corrupt ledger")
	}
	if l.Generation != "" {
		t.Errorf("corrupt ledger leaked state: %+v", l)
	}
}

// Negative counters clamp and actors naming unknown roles drop on load.
func TestLoadLedger_Normalizes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, CompanyDir, ledgerFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
		"generation": "stry_test1",
		"phaseIndex": -3,
		"budget": {"directorUsed": -1, "directorMax": 50},
		"actors": [
			{"role": "playwright", "calls": 8},
			{"role": "alien", "calls": 99}
		]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(dir).LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if l.PhaseIndex != 0 {
		t.Errorf("phaseIndex = %d, want 0", l.PhaseIndex)
	}
	if l.Budget.DirectorUsed != 0 {
		t.Errorf("directorUsed = %d, want 0", l.Budget.DirectorUsed)
	}
	if len(l.Actors) != 1 || l.Actors[0].Role != "playwright" || l.Actors[0].Calls != 8 {
		t.Errorf("actors = %+v, want just playwright with 8 calls", l.Actors)
	}
}

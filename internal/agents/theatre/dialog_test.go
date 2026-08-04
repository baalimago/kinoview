package theatre

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RenderDialog turns a fixture generation's transcript and ledger into a
// readable script: phase markers, role lines with arrows, final summary.
func TestRenderDialog_FixtureGeneration(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	scriptedProduction(t, co, "stry_ab12", false)

	dialog, err := RenderDialog(co, "stry_ab12")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"── production stry_ab12 ──",
		"[1] ─ phase 1/6 brief ─ budget 0/50",
		"[2] director→dramaturg: brief (mood=standoff, lineup=3)",
		`[9] playwright→wardrobe: "does silver read on night?"`,
		`[11] playwright⇉draft: 16 beats / 3 acts / "The Long Night"`,
		"[13] ─ phase 3/6 dress ─ scenographer 2/8 calls ─ budget 3/50",
		`✓ submitted "The Long Night"`,
		"ledger: phase submitted (6/6) · director 3/50 calls · global 5/200 calls",
		"dramaturg: 0 calls · 0 tokens · 0 consults · hop 0",
		"playwright: 0 calls · 1234 tokens · 1 consults · hop 2",
		"scenographer: 2 calls · 0 tokens · 0 consults · hop 0",
		"director: 3 calls · 0 tokens · 0 consults · hop 0",
	} {
		if !strings.Contains(dialog, want) {
			t.Errorf("dialog missing %q\nfull dialog:\n%s", want, dialog)
		}
	}
}

// An unknown generation — nothing in the transcript, no matching ledger — is
// a clear error, never an empty dialog.
func TestRenderDialog_UnknownGeneration(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	scriptedProduction(t, co, "stry_ab12", false)

	_, err := RenderDialog(co, "stry_zzz")
	if err == nil {
		t.Fatal("expected an error for an unknown generation")
	}
	if !strings.Contains(err.Error(), "no such generation") {
		t.Errorf("error = %v, want a 'no such generation' error", err)
	}
}

// A corrupt transcript renders its readable events and warns, instead of
// failing wholesale: a partial transcript is more useful than none.
func TestRenderDialog_CorruptTranscriptWarns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	path := filepath.Join(dir, CompanyDir, transcriptFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"gen":"stry_c1","seq":1,"kind":"post","from":"director","to":"stage","body":"first"}` + "\n" +
		"garbage line\n" +
		`{"gen":"stry_c1","seq":2,"kind":"post","from":"stage","to":"director","body":"second"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	dialog, err := RenderDialog(co, "stry_c1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[1] director→stage: first",
		"[2] stage→director: second",
		"(warning: 1 unreadable transcript line(s) dropped)",
	} {
		if !strings.Contains(dialog, want) {
			t.Errorf("dialog missing %q\nfull dialog:\n%s", want, dialog)
		}
	}
}

// A ledger that belongs to another generation is shown as such, so the dialog
// never silently attributes stale numbers.
func TestRenderDialog_LedgerForOtherGenerationNoted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	path := filepath.Join(dir, CompanyDir, transcriptFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"gen":"stry_old","seq":1,"kind":"post","from":"director","to":"stage","body":"ancient"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// The ledger is the *latest* generation's; an older gen must not claim it.
	scriptedProduction(t, co, "stry_new", false)
	if err := co.SaveLedger(Ledger{Generation: "stry_new"}); err != nil {
		t.Fatal(err)
	}

	dialog, err := RenderDialog(co, "stry_old")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dialog, "ancient") || !strings.Contains(dialog, "(ledger belongs to generation stry_new)") {
		t.Errorf("dialog = %q, want the events plus a ledger mismatch note", dialog)
	}
}

// RenderDialog never writes: a read-only cache dir must not break rendering.
func TestRenderDialog_NeverWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	co := Open(dir)
	scriptedProduction(t, co, "stry_ab12", false)

	if _, err := RenderDialog(co, "stry_ab12"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, CompanyDir))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{boardFileName: false, workingFileName: false, ledgerFileName: true, transcriptFileName: true}
	for _, e := range entries {
		if _, ok := want[e.Name()]; !ok {
			t.Errorf("RenderDialog created unexpected file %q", e.Name())
		}
	}
}

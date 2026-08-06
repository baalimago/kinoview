package theatre

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

// seededCompose composes a story through the deterministic floor for a fixed
// seed — the composer-only mode's regression surface lives in floor_test.go.
func seededCompose(seed int64) model.Story {
	return ComposeThemed(rand.New(rand.NewSource(seed)), "")
}

// newTestTheatre builds a composer-only theatre over a temp cache dir — the
// regression surface for the composer-only mode (no director is ever built).
func newTestTheatre(t *testing.T, opts ...Option) *Theatre {
	t.Helper()
	th := New(models.Configurations{}, t.TempDir(), time.Hour, opts...)
	// Next persists what it composes in a background goroutine; the TempDir
	// cleanup must not race that write (writeWG tracks it for exactly this).
	t.Cleanup(th.writeWG.Wait)
	return th
}

// fixtureScript is a scripted fake LLM that runs one full production: the
// director calls brief → draft → dress → validate → pin → submit through its
// tools, and every role answers with its deliverable writer. The playwright
// writes the fixture story; the scenographer dresses it; the rest is
// bookkeeping. The machinery — board, working file, broker, ledger, transcript
// — all runs for real; only the LLM is stubbed.
func fixtureScript(t *testing.T) func(context.Context, llmParams) (llmOutcome, error) {
	t.Helper()
	return func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "dramaturg_brief", models.Input{"notes": "standoff, three actors"})
			callTool(t, p, "draft_story", models.Input{"notes": "follow the brief"})
			callTool(t, p, "dress_set", models.Input{"notes": "a night garden"})
			callTool(t, p, "validate_story", models.Input{})
			callTool(t, p, "pin_identity", models.Input{})
			out := callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: out}, nil
		case strings.Contains(p.prompt, "You are the dramaturg."):
			callTool(t, p, "write_brief", models.Input{"brief": "mood=standoff, lineup=3"})
			return llmOutcome{text: deliverable("brief posted")}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		case strings.Contains(p.prompt, "You are the scenographer."):
			callTool(t, p, "write_scene", models.Input{"backdrop": "garden", "report": "a night garden"})
			return llmOutcome{text: deliverable("scene saved")}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}
}

// The fixture production runs the documented flow — brief → draft → dress →
// validate → pin → submit — with the transcript and the ledger recording
// every step, and the submitted story persisted to intro_story.json.
func TestTheatre_FixtureProductionRunsFlow(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, cacheDir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	th.runLLM = fixtureScript(t)

	if !th.Prepare(context.Background(), "test") {
		t.Fatal("Prepare should run a production")
	}
	story := th.Next()
	if story.Title != "The Test Night" {
		t.Errorf("story title = %q, want the playwright's draft", story.Title)
	}
	if story.Origin != "llm" {
		t.Errorf("origin = %q, want llm", story.Origin)
	}

	// The submitted story is persisted, and its id is the generation id.
	b, err := os.ReadFile(filepath.Join(cacheDir, "intro_story.json"))
	if err != nil {
		t.Fatalf("intro story not persisted: %v", err)
	}
	var onDisk model.Story
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.ID != story.ID || story.ID == "" {
		t.Errorf("story id = %q (on disk %q), want the generation id", story.ID, onDisk.ID)
	}

	co := Open(cacheDir)
	board, err := co.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if board.Generation != story.ID {
		t.Errorf("board generation = %q, want %q", board.Generation, story.ID)
	}
	if len(board.Entries) == 0 || board.Entries[0].Kind != "brief" || board.Entries[0].Author != "dramaturg" {
		t.Errorf("board = %+v, want the dramaturg's brief first", board.Entries)
	}

	// The transcript records every step: the six phase transitions, the
	// deliverables and the submit.
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	phases := map[string]bool{}
	delivers := map[string]bool{}
	submitted := false
	for _, ev := range tr.Events {
		if ev.Kind == "phase" {
			phases[ev.Body] = true
		}
		if ev.Kind == "deliver" {
			delivers[ev.From] = true
		}
		if ev.Kind == "submit" {
			submitted = true
		}
	}
	for _, want := range []string{"brief", "draft", "dress", "validate", "pin", "submit"} {
		found := false
		for body := range phases {
			if strings.Contains(body, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("transcript lacks a %q phase line; phases = %v", want, phases)
		}
	}
	for _, role := range []string{"director", "dramaturg", "playwright", "scenographer"} {
		if !delivers[role] {
			t.Errorf("transcript lacks a deliver from %s", role)
		}
	}
	if !submitted {
		t.Error("transcript lacks a submit event")
	}

	// The ledger records the final state and every actor's telemetry.
	ledger, err := co.LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Phase != "submitted" {
		t.Errorf("ledger phase = %q, want submitted", ledger.Phase)
	}
	byRole := map[string]bool{}
	for _, a := range ledger.Actors {
		// The playwright's structured story is its final answer, not a tool
		// call (machine fix, 2026-08-03): its activity shows as the answer
		// record, so presence — not a call count — is the telemetry proof.
		byRole[a.Role] = a.Status == "active" || a.Calls > 0
	}
	for _, role := range []string{"director", "dramaturg", "playwright", "scenographer"} {
		if !byRole[role] {
			t.Errorf("ledger lacks calls for %s; actors = %v", role, ledger.Actors)
		}
	}

	// The working file carries the brief the draft was written from, captured
	// at draft-write time — the distill's out-of-band copy (review 3, R3-02).
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if w.Brief != "mood=standoff, lineup=3" {
		t.Errorf("working brief = %q, want the posted brief", w.Brief)
	}
}

// The director's tool set is the spec's nine: the three role spawns, the two
// working-file gates, the deterministic pin, the shared post and consult, and
// the submit gate. Every spec validates (the tools package's spec-shape test
// covers the shape; here the set is asserted).
func TestTheatre_DirectorToolSetRegistered(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	p := th.openProduction("")
	defer p.stage.Close()

	tools := p.directorTools(context.Background())
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Specification().Name] = true
	}
	for _, want := range []string{
		"dramaturg_brief", "draft_story", "dress_set", "read_story",
		"validate_story", "pin_identity", "post_to_board", "consult",
		"submit_story",
	} {
		if !names[want] {
			t.Errorf("director tool set lacks %q; got %v", want, names)
		}
	}
	if len(tools) != 9 {
		t.Errorf("director tool set has %d tools, want 9", len(tools))
	}
}

// A director that never calls submit leaves a validated draft; the exhaustion
// path ships that last validated draft — never an invalid one — and the
// transcript records the exhaustion.
func TestTheatre_BudgetExhaustionShipsLastValidatedDraft(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "validate_story", models.Input{})
			return llmOutcome{text: "out of budget"}, nil // never submits
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	story, err := th.runProduction(context.Background(), "")
	if err != nil {
		t.Fatalf("runProduction: %v", err)
	}
	if story.Title != "The Test Night" {
		t.Errorf("story = %q, want the validated draft", story.Title)
	}
	if story.Origin != "llm" {
		t.Errorf("origin = %q, want the draft, not the composer", story.Origin)
	}

	// The exhausted draft was persisted and the transcript records the
	// exhaustion note and the submit.
	if _, err := os.Stat(filepath.Join(th.cacheDir, "intro_story.json")); err != nil {
		t.Errorf("draft not persisted: %v", err)
	}
	co := Open(th.cacheDir)
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var noted, submitted bool
	for _, ev := range tr.Events {
		if ev.Kind == "note" && strings.Contains(ev.Body, "without submitting") {
			noted = true
		}
		if ev.Kind == "submit" {
			submitted = true
		}
	}
	if !noted {
		t.Error("transcript lacks the exhaustion note")
	}
	if !submitted {
		t.Error("transcript lacks the submit event")
	}
}

// A playable draft that never passed validate_story must not ship on
// exhaustion: the R7-01 gate — the exhaustion path ships only the last
// validated draft, and a draft-only generation falls through to the composer
// floor.
func TestTheatre_DraftOnlyExhaustionFallsToComposer(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			// The playwright writes a playable draft, then the director
			// exhausts before ever calling validate_story.
			callTool(t, p, "draft_story", models.Input{})
			return llmOutcome{text: "out of budget"}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	_, err := th.runProduction(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "never validated") {
		t.Fatalf("err = %v, want the never-validated refusal", err)
	}
	// The unvalidated draft was never persisted as the intro story.
	if _, err := os.Stat(filepath.Join(th.cacheDir, "intro_story.json")); err == nil {
		t.Error("an unvalidated draft reached the intro story file")
	}
	// The composer floor answers through Prepare.
	if !th.Prepare(context.Background(), "after exhaustion") {
		t.Fatal("Prepare should compose")
	}
	if s := th.Next(); s.Origin != "composer" {
		t.Errorf("origin = %q, want composer, not the unvalidated draft", s.Origin)
	}
}

// The validate_story blessing belongs to the exact content that passed the
// gate: a draft rewritten after validation loses it, so an exhaustion after
// the rewrite still falls through to the composer floor (review 7, R7-01).
func TestTheatre_ValidatedThenRewrittenDraftDoesNotShip(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "validate_story", models.Input{})
			// The playwright rewrites the draft; the blessing is cleared.
			callTool(t, p, "draft_story", models.Input{})
			return llmOutcome{text: "out of budget"}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	_, err := th.runProduction(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "never validated") {
		t.Fatalf("err = %v, want the never-validated refusal for the rewritten draft", err)
	}
}

// A director that never writes a draft and never submits leaves nothing; the
// generation fails and the caller answers with the composer floor.
func TestTheatre_NoDraftFailsToComposer(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		if strings.Contains(p.prompt, "You are the director of") {
			return llmOutcome{text: "nothing produced"}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	_, err := th.runProduction(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "no playable draft") {
		t.Fatalf("err = %v, want the no-draft error", err)
	}
	// The composer floor answers through Prepare.
	if !th.Prepare(context.Background(), "after failure") {
		t.Fatal("Prepare should compose")
	}
	if s := th.Next(); s.Origin != "composer" {
		t.Errorf("origin = %q, want composer", s.Origin)
	}
}

// submit_story is the final gate: a second call for the same generation is
// refused, and the story is persisted exactly once.
func TestTheatre_SubmitTwiceRefused(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	var second string
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "submit_story", models.Input{})
			second = callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: second}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	if _, err := th.runProduction(context.Background(), ""); err != nil {
		t.Fatalf("runProduction: %v", err)
	}
	if !strings.Contains(second, "already submitted") {
		t.Errorf("second submit = %q, want the refusal", second)
	}
}

// submit_story refuses a story that fails model.Story.Validate and returns
// the exact errors to the director.
func TestTheatre_SubmitRefusesInvalidDraft(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	// A working file holding an unplayable story, written around SaveWorking's
	// gate the way a hostile or hand-edited file would appear.
	workingPath := filepath.Join(th.cacheDir, CompanyDir, workingFileName)
	if err := os.MkdirAll(filepath.Dir(workingPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workingPath, []byte(`{"story":{"id":"bad","title":"no cast"},"revision":1,"status":"draft"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var submitOut, validateOut string
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		if strings.Contains(p.prompt, "You are the director of") {
			validateOut = callTool(t, p, "validate_story", models.Input{})
			submitOut = callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: submitOut}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	_, err := th.runProduction(context.Background(), "")
	if err == nil {
		t.Fatal("expected the production to fail — no playable draft")
	}
	for _, want := range []string{"no valid cast"} {
		if !strings.Contains(submitOut, want) {
			t.Errorf("submit refused with %q, want the exact error mentioning %q", submitOut, want)
		}
		if !strings.Contains(validateOut, want) {
			t.Errorf("validate returned %q, want the exact error mentioning %q", validateOut, want)
		}
	}
}

// submit_story aborts when the story cannot be persisted: the working file
// is not marked submitted and the library is not distilled — paperwork must
// never claim a success the disk did not record (review 7, R7-02).
func TestTheatre_SubmitAbortsWhenStoryNotPersisted(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	// intro_story.json is a directory: the atomic rename onto it fails while
	// the company paperwork stays writable — the exact R7-02 failure shape.
	if err := os.Mkdir(filepath.Join(cacheDir, "intro_story.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, cacheDir, time.Hour)

	var submitOut string
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			submitOut = callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: submitOut}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	// The submit refused; with no validated draft left behind, the
	// production fails to the composer floor.
	_, err := th.runProduction(context.Background(), "")
	if err == nil {
		t.Fatal("production should fail when the story cannot be persisted")
	}
	if !strings.Contains(submitOut, "submit_story: submit refused: story not persisted") {
		t.Errorf("submit = %q, want the persistence refusal", submitOut)
	}

	// The working file is not marked submitted and the library is not
	// distilled.
	co := Open(cacheDir)
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if w.Status == "submitted" {
		t.Error("working file marked submitted although the story was not persisted")
	}
	if lib := co.LoadLibrary(); len(lib.Premises) != 0 {
		t.Errorf("library distilled without a persisted story: %+v", lib.Premises)
	}
}

// Past the wall-clock deadline the broker refuses spawns with a clear
// message, and the last validated draft ships.
func TestTheatre_WallClockRefusesSpawnsPastDeadline(t *testing.T) {
	t.Parallel()
	th := New(models.Configurations{}, t.TempDir(), time.Hour, WithWallClock(80*time.Millisecond))
	var dressOut string
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "validate_story", models.Input{})
			time.Sleep(160 * time.Millisecond) // past the deadline
			dressOut = callTool(t, p, "dress_set", models.Input{})
			return llmOutcome{text: dressOut}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	story, err := th.runProduction(context.Background(), "")
	if err != nil {
		t.Fatalf("runProduction: %v", err)
	}
	if !strings.Contains(dressOut, "deadline exceeded") {
		t.Errorf("dress_set output = %q, want the deadline refusal", dressOut)
	}
	if story.Title != "The Test Night" {
		t.Errorf("story = %q, want the validated draft (pre-deadline work)", story.Title)
	}
}

// A working file that becomes unreadable at submit time cannot ship; the
// production fails and the composer floor answers, with the failure logged.
func TestTheatre_WorkingFileUnreadableShipsComposer(t *testing.T) {
	th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, t.TempDir(), time.Hour)
	workingPath := filepath.Join(th.cacheDir, CompanyDir, workingFileName)
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			// The working file dies between the draft and the submit.
			if err := os.WriteFile(workingPath, []byte("{garbage"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: out}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	errOut := testboil.CaptureStderr(t, func(t *testing.T) {
		if !th.Prepare(context.Background(), "test") {
			t.Fatal("Prepare should run")
		}
	})
	if s := th.Next(); s.Origin != "composer" {
		t.Errorf("origin = %q, want the composer floor", s.Origin)
	}
	if !strings.Contains(errOut, "theatre: production failed") {
		t.Errorf("stderr = %q, want the production failure logged", errOut)
	}
}

// A director that fails at construction (the stub's LLM errors on the
// director prompt) falls back to composer-only generation: Prepare composes,
// logs the failure and returns true.
func TestTheatre_DirectorConstructionFailsFallsBackToComposer(t *testing.T) {
	th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, t.TempDir(), time.Hour)
	th.runLLM = func(context.Context, llmParams) (llmOutcome, error) {
		return llmOutcome{}, errors.New("setup agent: no credentials")
	}

	errOut := testboil.CaptureStderr(t, func(t *testing.T) {
		if !th.Prepare(context.Background(), "test") {
			t.Fatal("Prepare should run")
		}
	})
	if s := th.Next(); s.Origin != "composer" {
		t.Errorf("origin = %q, want the composer floor", s.Origin)
	}
	if !strings.Contains(errOut, "production failed") {
		t.Errorf("stderr = %q, want the failure logged", errOut)
	}
}

// A subagent that fails mid-production does not stop the show: its fallback
// answers (the phase-5 seam), the production continues, and the transcript
// records the fallback.
func TestTheatre_SubagentFailureFallsBackAndContinues(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "dramaturg_brief", models.Input{})
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "dress_set", models.Input{})
			callTool(t, p, "validate_story", models.Input{})
			out := callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: out}, nil
		case strings.Contains(p.prompt, "You are the dramaturg."):
			// The dramaturg's LLM fails; the fallback posts a brief anyway.
			return llmOutcome{}, errors.New("llm query: boom")
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		case strings.Contains(p.prompt, "You are the scenographer."):
			callTool(t, p, "write_scene", models.Input{"backdrop": "garden"})
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	story, err := th.runProduction(context.Background(), "")
	if err != nil {
		t.Fatalf("runProduction: %v", err)
	}
	if story.Title != "The Test Night" {
		t.Errorf("story = %q, want the production to continue past the failure", story.Title)
	}

	co := Open(th.cacheDir)
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var fallback bool
	for _, ev := range tr.Events {
		if ev.Kind == "note" && strings.Contains(ev.Body, "fallback: dramaturg") {
			fallback = true
		}
	}
	if !fallback {
		t.Error("transcript lacks the dramaturg fallback note")
	}
}

// ── composer-only mode: the pre-migration behaviour, reproduced exactly ──
// (the frozen snapshot in floor_test.go proves the floor)

// Repeated prepares within the cooldown are blocked; a generation after the
// cooldown runs.
func TestTheatre_PrepareCooldownBlocksRepeats(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	ctx := context.Background()
	if !th.Prepare(ctx, "first") {
		t.Fatal("first Prepare should run")
	}
	for range 5 {
		if th.Prepare(ctx, "refresh") {
			t.Fatalf("Prepare ran again within the cooldown")
		}
	}
}

func TestTheatre_PrepareRunsAgainAfterCooldown(t *testing.T) {
	t.Parallel()
	th := New(models.Configurations{}, t.TempDir(), 10*time.Millisecond)
	if !th.Prepare(context.Background(), "first") {
		t.Fatal("first Prepare should run")
	}
	time.Sleep(20 * time.Millisecond)
	if !th.Prepare(context.Background(), "second") {
		t.Fatal("Prepare should run once the cooldown has elapsed")
	}
}

// A prepared story is persisted and picked back up by a fresh theatre over
// the same cache dir.
func TestTheatre_PreparePersistsAndReloads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	if !th.Prepare(context.Background(), "test") {
		t.Fatal("Prepare should run")
	}
	want := th.Next()

	if _, err := os.Stat(filepath.Join(dir, "intro_story.json")); err != nil {
		t.Fatalf("story not written to disk: %v", err)
	}
	reloaded := New(models.Configurations{}, dir, time.Hour)
	if got := reloaded.Next(); got.ID != want.ID {
		t.Errorf("reloaded story ID = %q, want %q", got.ID, want.ID)
	}
}

// A corrupt or hostile cache file must not reach the player.
func TestTheatre_LoadFromDiskRejectsInvalidCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := model.Story{ID: "../../escape", Title: "nope", Cast: []model.Cast{{ID: "x", Character: "dragon"}}}
	b, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "intro_story.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	if s := th.Next(); len(s.Cast) == 0 || s.Origin != "composer" {
		t.Errorf("invalid cache served: %+v", s)
	}
}

// writeCachedStory puts a story of a given origin on disk, as a previous run
// would have left it.
func writeCachedStory(t *testing.T, dir, origin string, age time.Duration) string {
	t.Helper()
	s := seededCompose(9)
	s.Origin = origin
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "intro_story.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// The cooldown must survive a restart for LLM stories: lastGen is in-memory,
// so without the mtime fallback a crash-loop would cost one production per
// restart.
func TestTheatre_CooldownSurvivesRestartForLLMStories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCachedStory(t, dir, "llm", 0)

	restarted := New(models.Configurations{}, dir, time.Hour)
	if restarted.Prepare(context.Background(), "after restart") {
		t.Error("a restart reset the cooldown; generation ran again immediately")
	}
}

// An old cache is fair game again.
func TestTheatre_CooldownExpiredCacheAllowsRegeneration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCachedStory(t, dir, "llm", 3*time.Hour)

	restarted := New(models.Configurations{}, dir, time.Hour)
	if !restarted.Prepare(context.Background(), "stale cache") {
		t.Error("expected regeneration once the cached story aged past the cooldown")
	}
}

// A composed story on disk must NOT hold the cooldown shut: the cooldown
// limits API spend, and a composed story cost nothing.
func TestTheatre_CooldownComposerStoryDoesNotGateLLM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seeder := New(models.Configurations{}, dir, time.Hour)
	seeder.Warm(context.Background())

	reloaded := New(models.Configurations{}, dir, time.Hour)
	if !reloaded.lastGen.IsZero() {
		t.Errorf("a composer story started the cooldown (lastGen=%v)", reloaded.lastGen)
	}
	if !reloaded.Prepare(context.Background(), "upgrade") {
		t.Error("Prepare was blocked by a cooldown that a composed story should not have started")
	}
}

// An LLM story, on the other hand, must hold it.
func TestTheatre_CooldownLLMStoryGatesRegeneration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCachedStory(t, dir, "llm", 0)

	th := New(models.Configurations{}, dir, time.Hour)
	if th.lastGen.IsZero() {
		t.Error("an llm story did not start the cooldown")
	}
	if th.Prepare(context.Background(), "too soon") {
		t.Error("Prepare ran despite a recent llm generation")
	}
}

// Warm stores a story synchronously — no visitor can arrive before one is
// prepared — themed on the last watched item.
func TestTheatre_WarmStoresSynchronouslyFromLastViewed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour,
		WithMuse(MuseFunc(func() string { return "Solaris 1972" })))

	th.Warm(context.Background())

	if _, err := os.Stat(filepath.Join(dir, "intro_story.json")); err != nil {
		t.Fatalf("no story on disk when Warm returned: %v", err)
	}
	s := th.Next()
	if s.Theme != "Solaris 1972" {
		t.Errorf("theme = %q, want the last watched title", s.Theme)
	}
	if !strings.Contains(s.Title, "Solaris 1972") {
		t.Errorf("title %q does not mention the last watched item", s.Title)
	}
}

// ...but it must not burn a generation when a good story is already cached.
func TestTheatre_WarmNoopWhenCached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seed := New(models.Configurations{}, dir, time.Hour)
	if !seed.Prepare(context.Background(), "initial") {
		t.Fatal("seed Prepare should run")
	}
	want := seed.Next().ID

	restarted := New(models.Configurations{}, dir, time.Hour)
	restarted.Warm(context.Background())
	time.Sleep(150 * time.Millisecond)

	if got := restarted.Next().ID; got != want {
		t.Errorf("Warm regenerated over a cached story: got %q, want %q", got, want)
	}
}

// Even the last-resort synchronous compose inside Next must end up on disk.
func TestTheatre_NextPersistsWhatItInvents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)

	_ = th.Next() // nothing cached, nothing warmed

	path := filepath.Join(dir, "intro_story.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Next composed a story but never persisted it")
}

// Concurrent writers must never leave a partially written file behind.
func TestTheatre_SaveStoryIsAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	path := filepath.Join(dir, "intro_story.json")

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			th.saveStory(seededCompose(int64(n)))
		}(i)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			b, err := os.ReadFile(path)
			if err != nil {
				continue // not created yet; a missing file is fine, a torn one is not
			}
			var s model.Story
			if err := json.Unmarshal(b, &s); err != nil {
				t.Errorf("observed a torn story file: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	<-done
}

// Single-flight: a generation in progress refuses a second Prepare.
func TestTheatre_SingleFlight(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	th.inFlight = true
	if th.Prepare(context.Background(), "second") {
		t.Error("Prepare ran while another generation was in flight")
	}
}

// A panicking muse degrades to the empty theme (error table: the dramaturg
// fallback then riffs on nothing, and the production never goes down for a
// splash story).
func TestTheatre_MusePanicGuarded(t *testing.T) {
	t.Parallel()
	th := New(models.Configurations{}, t.TempDir(), time.Hour,
		WithMuse(MuseFunc(func() string { panic("boom") })))
	if got := th.theme(); got != "" {
		t.Errorf("theme = %q, want empty after a panicking muse", got)
	}
}

// The theatre's options land on the facade: muse, budgets, wall clock and the
// session sink.
func TestTheatre_OptionsApplied(t *testing.T) {
	t.Parallel()
	sink := func(model.LogMessage) {}
	muse := MuseFunc(func() string { return "Solaris" })
	th := New(models.Configurations{}, t.TempDir(), time.Hour,
		WithMuse(muse),
		WithCallBudgets(30, 120),
		WithWallClock(5*time.Minute),
		WithSessionSink(sink),
	)
	if th.directorMax != 30 || th.globalMax != 120 {
		t.Errorf("budgets = {%d, %d}, want {30, 120}", th.directorMax, th.globalMax)
	}
	if th.wallClock != 5*time.Minute {
		t.Errorf("wallClock = %v", th.wallClock)
	}
	if th.logSink == nil || th.muse == nil {
		t.Error("sink or muse not applied")
	}
	if got := th.theme(); got != "Solaris" {
		t.Errorf("theme = %q, want the muse's answer", got)
	}
}

// The director's read_story tool returns the working draft or one requested
// part, and refuses unknown parts and a missing draft.
func TestTheatre_ReadStoryParts(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	p := th.openProduction("")
	defer p.stage.Close()

	// No draft yet: a clear message, not a crash.
	if _, err := p.readStory(""); err == nil || !strings.Contains(err.Error(), "playwright") {
		t.Fatalf("readStory without a draft: %v", err)
	}

	if err := p.company.SaveWorking(Working{Story: validStory(), Revision: 1, Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		part string
		want string
	}{
		{"", "The Test Night"},
		{"story", "The Test Night"},
		{"cast", "ina"},
		{"beats", "enter"},
		{"scene", "night"},
		{"title", "The Test Night"},
	}
	for _, tt := range cases {
		out, err := p.readStory(tt.part)
		if err != nil {
			t.Errorf("readStory(%q): %v", tt.part, err)
			continue
		}
		if !strings.Contains(out, tt.want) {
			t.Errorf("readStory(%q) = %q, want it to contain %q", tt.part, out, tt.want)
		}
	}
	if _, err := p.readStory("bogus"); err == nil || !strings.Contains(err.Error(), "unknown part") {
		t.Errorf("readStory(bogus) = %v, want the unknown-part refusal", err)
	}
}

// A story that cannot be written (the cache path is a file) returns an
// error — the splash must not depend on disk health, so the caller decides
// what to do with it: Next/Prepare/Warm log it and keep serving from memory,
// while submit_story aborts the submit (review 7, R7-02).
func TestTheatre_SaveStoryWriteFailureReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The cache dir itself is a file, so every mkdir/write fails.
	cacheDir := filepath.Join(dir, "cache")
	if err := os.WriteFile(cacheDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	th := New(models.Configurations{}, cacheDir, time.Hour)

	if err := th.saveStory(validStory()); err == nil || !strings.Contains(err.Error(), "mkdir cache") {
		t.Errorf("saveStory err = %v, want the write failure", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "intro_story.json")); err == nil {
		t.Errorf("a story file appeared under a file")
	}
}

// Concurrent Next + Prepare compose through the same random source. Next
// composes under t.mu while Prepare composes outside it (it must not hold
// t.mu across an LLM production), so without a dedicated rnd lock the two
// goroutines draw from math/rand at the same time (review 1, R1-01). The
// -race detector fails the pre-fix code here.
func TestTheatre_ConcurrentNextAndPrepareComposeSafely(t *testing.T) {
	t.Parallel()
	for range 400 {
		th := newTestTheatre(t)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = th.Next()
		}()
		go func() {
			defer wg.Done()
			th.Prepare(context.Background(), "concurrent")
		}()
		wg.Wait()
		// Next persists what it composes in a background goroutine; the
		// TempDir cleanup must not race that write.
		th.writeWG.Wait()
	}
}

// Concurrent Next + Prepare on a model-configured theatre: Next composes
// under rndMu while Prepare runs a production whose openProduction draws the
// generation id from the same source (review 2, R2-01). The R1-01 test above
// cannot reach this path — newTestTheatre is composer-only, so the
// production's generation-id draw is never exercised. The -race detector
// fails the pre-fix code here.
func TestTheatre_ConcurrentNextAndPrepareProductionSafely(t *testing.T) {
	t.Parallel()
	for range 400 {
		th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, t.TempDir(), time.Hour)
		// A terminal text is all the production needs: openProduction draws
		// the generation id before the runner's first LLM call, and with no
		// draft the composer floor answers (Prepare composes after the
		// production fails).
		th.runLLM = func(context.Context, llmParams) (llmOutcome, error) {
			return llmOutcome{text: "ok"}, nil
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = th.Next()
		}()
		go func() {
			defer wg.Done()
			th.Prepare(context.Background(), "concurrent production")
		}()
		wg.Wait()
		th.writeWG.Wait()
	}
}

// Next never returns an unplayable story.
func TestTheatre_NextNeverEmpty(t *testing.T) {
	t.Parallel()
	th := newTestTheatre(t)
	s := th.Next()
	if len(s.Cast) == 0 || len(s.Beats) == 0 {
		t.Fatalf("Next returned an unplayable story: %+v", s)
	}
}

// Two sequential generations (acceptance criterion): generation 2's
// playwright context carries generation 1's canon facts, the dramaturg
// context carries generation 1's premise in the no-repeat list, and the
// director context carries generation 1's critique lesson. The library is
// the only channel between the two — the board is per-generation.
func TestTheatre_SecondGenerationReadsFirstLibrary(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ctx := context.Background()

	// Generation 1: brief → draft (with a canon fact) → dress → validate →
	// pin → submit (with a lesson).
	gen1 := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, cacheDir, time.Hour)
	gen1.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "dramaturg_brief", models.Input{"notes": "keep it dry"})
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "dress_set", models.Input{})
			callTool(t, p, "validate_story", models.Input{})
			callTool(t, p, "pin_identity", models.Input{})
			out := callTool(t, p, "submit_story", models.Input{"notes": "two stares in a row is dead air"})
			return llmOutcome{text: out}, nil
		case strings.Contains(p.prompt, "You are the dramaturg."):
			callTool(t, p, "write_brief", models.Input{"brief": `{"mood":"standoff","shape":"mousehunt","theme":"Solaris 1972"}`})
			return llmOutcome{text: "brief posted"}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyWithCanon(t, "the mouse got away")}, nil
		case strings.Contains(p.prompt, "You are the scenographer."):
			callTool(t, p, "write_scene", models.Input{"backdrop": "garden"})
			return llmOutcome{text: "scene saved"}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}
	if !gen1.Prepare(ctx, "gen1") {
		t.Fatal("generation 1 should run")
	}

	// Generation 2 over the same cache dir: capture each role's prompt.
	// runProduction bypasses the cooldown, which generation 1's submit
	// started.
	var director, dramaturg, playwright string
	gen2 := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, cacheDir, time.Hour)
	gen2.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			director = p.prompt
			callTool(t, p, "dramaturg_brief", models.Input{})
			callTool(t, p, "draft_story", models.Input{})
			out := callTool(t, p, "submit_story", models.Input{})
			return llmOutcome{text: out}, nil
		case strings.Contains(p.prompt, "You are the dramaturg."):
			dramaturg = p.prompt
			callTool(t, p, "write_brief", models.Input{"brief": `{"mood":"cozy","shape":"greeting","theme":"The Long Night"}`})
			return llmOutcome{text: "brief posted"}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			playwright = p.prompt
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}
	if _, err := gen2.runProduction(ctx, ""); err != nil {
		t.Fatalf("generation 2 should run: %v", err)
	}

	if !strings.Contains(playwright, "the mouse got away") {
		t.Errorf("generation 2 playwright context lacks generation 1's canon fact")
	}
	if !strings.Contains(playwright, "The Test Night") {
		t.Errorf("generation 2 playwright context lacks generation 1's play summary")
	}
	if !strings.Contains(dramaturg, "Solaris 1972") {
		t.Errorf("generation 2 dramaturg context lacks generation 1's premise (no-repeat list)")
	}
	if !strings.Contains(director, "two stares in a row is dead air") {
		t.Errorf("generation 2 director context lacks generation 1's lesson")
	}

	// The bulletins and canon facts are durable on disk too.
	co := Open(cacheDir)
	lib := co.LoadLibrary()
	if len(lib.Repertoire.Facts) != 1 || lib.Repertoire.Facts[0] != "the mouse got away" {
		t.Errorf("facts = %v, want generation 1's canon fact persisted", lib.Repertoire.Facts)
	}
	if len(lib.Premises) != 2 { // generation 1's brief premise + generation 2's free-text brief
		t.Errorf("premises = %+v, want both generations' premises", lib.Premises)
	}
	if len(lib.Director) != 1 || lib.Director[0].Text != "two stares in a row is dead air" {
		t.Errorf("director = %+v, want generation 1's lesson persisted", lib.Director)
	}
}

// The director can canonize a new named character at submit: a valid entry
// in the draft enters the registry and survives the restart (the integration
// contract: registry is the only place identities are born).
func TestTheatre_SubmitCanonizesApprovedCharacter(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, cacheDir, time.Hour)

	// A draft with a second mouse on stage.
	draft := func() string {
		s := validStory()
		s.Cast = append(s.Cast, model.Cast{ID: "mouse2", Character: "mouse", Coat: "white", Lane: 1, X: 0.6})
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	var submitOut string
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			callTool(t, p, "pin_identity", models.Input{})
			submitOut = callTool(t, p, "submit_story", models.Input{
				"characters": `[{"id":"mouse2","species":"mouse","coat":"white"}]`,
			})
			return llmOutcome{text: submitOut}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: draft()}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}

	if _, err := th.runProduction(context.Background(), ""); err != nil {
		t.Fatalf("production: %v", err)
	}
	if !strings.Contains(submitOut, "canonized 1 characters") {
		t.Errorf("submit = %q, want the canonization reported", submitOut)
	}

	// The registry doc on disk carries the new character, and a restart
	// picks it up.
	co := Open(cacheDir)
	lib := co.LoadLibrary()
	if len(lib.Registry) != 5 {
		t.Fatalf("registry = %+v, want the permanent cast + mouse2", lib.Registry)
	}
	restarted := New(models.Configurations{}, cacheDir, time.Hour)
	if !restarted.registry.Known("mouse2") {
		t.Error("restart lost the canonized character")
	}

	// A character not in the draft is refused.
	submitOut = ""
	th.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		switch {
		case strings.Contains(p.prompt, "You are the director of"):
			callTool(t, p, "draft_story", models.Input{})
			submitOut = callTool(t, p, "submit_story", models.Input{
				"characters": `[{"id":"ghost","species":"cat","coat":"ginger"}]`,
			})
			return llmOutcome{text: submitOut}, nil
		case strings.Contains(p.prompt, "You are the playwright."):
			return llmOutcome{text: storyJSON(t)}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}
	if _, err := th.runProduction(context.Background(), ""); err != nil {
		t.Fatalf("production: %v", err)
	}
	if !strings.Contains(submitOut, "ghost\": not in the draft cast") {
		t.Errorf("submit = %q, want the refusal", submitOut)
	}
}

// Feedback (the agents.Feedbacker contract) appends through the facade's
// persistent company: notes land in audience.json newest first, and a fresh
// theatre over the same cache dir reads them back.
func TestTheatre_FeedbackAppendsToAudienceDoc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	ctx := context.Background()

	if err := th.Feedback(ctx, "stry_first", 1, "more dog"); err != nil {
		t.Fatal(err)
	}
	if err := th.Feedback(ctx, "stry_second", -1, "too slow"); err != nil {
		t.Fatal(err)
	}

	doc := Open(dir).LoadAudience()
	if len(doc) != 2 {
		t.Fatalf("audience = %d notes, want 2", len(doc))
	}
	if doc[0].StoryID != "stry_second" || doc[0].Rating != -1 || doc[0].Comment != "too slow" {
		t.Errorf("newest note = %+v, want the second note first", doc[0])
	}
	first := doc[1]
	if first.StoryID != "stry_first" || first.Rating != 1 || first.Comment != "more dog" {
		t.Errorf("older note = %+v, want the first note", first)
	}
	if got, want := first.Date, dateStamp(time.Now()); got != want {
		t.Errorf("note date = %q, want today's %q", got, want)
	}
}

// The facade is the trust boundary, like submit_story: a rating outside
// {+1, -1} is rejected with an error and nothing is written.
func TestTheatre_FeedbackRejectsBadRating(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	ctx := context.Background()

	for _, rating := range []int{0, 5, -2} {
		if err := th.Feedback(ctx, "stry_ok01", rating, "nope"); err == nil {
			t.Errorf("rating %d: Feedback accepted it", rating)
		}
	}
	if doc := Open(dir).LoadAudience(); len(doc) != 0 {
		t.Errorf("rejected ratings still wrote %d notes", len(doc))
	}
}

// A story id that could never have come from a validated story is rejected
// before the note is written.
func TestTheatre_FeedbackRejectsBadStoryID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	ctx := context.Background()

	for _, id := range []string{"", "../etc", "stry_ABC", "stry_" + strings.Repeat("x", 25)} {
		if err := th.Feedback(ctx, id, 1, ""); err == nil {
			t.Errorf("story id %q: Feedback accepted it", id)
		}
	}
	if doc := Open(dir).LoadAudience(); len(doc) != 0 {
		t.Errorf("bad story ids still wrote %d notes", len(doc))
	}
}

// A long comment is clipped to the cap, never rejected (decision D-3).
func TestTheatre_FeedbackTruncatesLongComment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	th := New(models.Configurations{}, dir, time.Hour)
	t.Cleanup(th.writeWG.Wait)
	ctx := context.Background()

	long := strings.Repeat("è", 1000)
	if err := th.Feedback(ctx, "stry_ok01", 1, long); err != nil {
		t.Fatal(err)
	}
	doc := Open(dir).LoadAudience()
	if len(doc) != 1 {
		t.Fatalf("audience = %d notes, want 1", len(doc))
	}
	if got, want := doc[0].Comment, string([]rune(long)[:audienceCommentMax]); got != want {
		t.Errorf("comment = %d runes, want the first %d", len([]rune(got)), audienceCommentMax)
	}
}

// A note the disk cannot hold returns the error instead of being swallowed.
func TestTheatre_FeedbackWriteFailureReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The cache dir itself is a file, so every mkdir/write fails — the same
	// shape as the SaveStory write-failure test.
	cacheDir := filepath.Join(dir, "cache")
	if err := os.WriteFile(cacheDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	th := New(models.Configurations{}, cacheDir, time.Hour)

	if err := th.Feedback(context.Background(), "stry_ok01", 1, ""); err == nil {
		t.Fatal("Feedback accepted a note it could not persist")
	}
}

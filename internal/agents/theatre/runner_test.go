package theatre

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents/theatre/tools"
	"github.com/baalimago/kinoview/internal/model"
)

// callTool finds a tool by spec name and calls it with the given input — the
// stub LLM's way of "using a tool".
func callTool(t *testing.T, p llmParams, name string, input models.Input) string {
	t.Helper()
	for _, tool := range p.tools {
		if tool.Specification().Name != name {
			continue
		}
		out, err := tool.Call(input)
		if err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		return out
	}
	t.Fatalf("tool %q not in the tool set", name)
	return ""
}

// The runner assembles the working-context standard: generation, theme, board
// excerpt, working summary, role prompt and task — in that order — and every
// piece is present in the prompt the LLM sees.
func TestRunner_AssemblesWorkingContext(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)

	if err := co.SaveBoard(Board{
		Generation: "stry_ab12",
		Theme:      "The Long Night",
		Entries: []Entry{
			{Author: "director", Kind: "note", To: "dramaturg", Body: "make it standoff-ish"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := co.SaveWorking(Working{
		Story:    validStory(),
		Revision: 3,
		Status:   "draft",
	}); err != nil {
		t.Fatal(err)
	}

	runner, prompts := stubRunner(t, stage, nil)
	res, err := runner.Run(context.Background(), Invocation{
		Role: "dramaturg", Task: "write the brief", Budget: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "ok" {
		t.Errorf("text = %q, want the stub's answer", res.Text)
	}
	if len(*prompts) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(*prompts))
	}
	prompt := (*prompts)[0]

	order := []struct{ name, needle string }{
		{"generation", "Generation: stry_ab12"},
		{"theme", "Theme: The Long Night"},
		{"board excerpt", "make it standoff-ish"},
		{"working summary", "Title: The Test Night"},
		{"working revision status", "Status: draft"},
		{"role prompt", "You are the dramaturg."},
		{"task", "write the brief"},
	}
	prev := -1
	for _, o := range order {
		idx := strings.Index(prompt, o.needle)
		if idx < 0 {
			t.Errorf("prompt missing %s (%q)", o.name, o.needle)
			continue
		}
		if idx < prev {
			t.Errorf("%s (%q) appears before an earlier section — order broken", o.name, o.needle)
		}
		prev = idx
	}
}

// The runner produces a valid artifact for each of the four roles against a
// fixture board: the stub uses each role's deliverable writer and the side
// effect lands where it belongs. The playwright is the exception since the
// 2026-08-03 machine fix: its story arrives as the structured final answer
// (tool == "") and the runner persists it into the working file.
func TestRunner_ProducesArtifactForEachRole(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		role      string
		tool      string
		input     models.Input
		wantCalls int
		verify    func(t *testing.T, co *Company, res Result, toolOut string)
	}{
		{
			name: "dramaturg writes the brief to the board",
			role: "dramaturg", tool: "write_brief", wantCalls: 1,
			input: models.Input{"brief": "mood=standoff, lineup=3"},
			verify: func(t *testing.T, co *Company, _ Result, _ string) {
				board, err := co.LoadBoard()
				if err != nil {
					t.Fatal(err)
				}
				if len(board.Entries) != 1 || board.Entries[0].Kind != "brief" || board.Entries[0].Body != "mood=standoff, lineup=3" {
					t.Errorf("board = %+v, want one brief entry", board.Entries)
				}
			},
		},
		{
			name: "playwright's structured final answer lands in the working file",
			role: "playwright", tool: "",
			verify: func(t *testing.T, co *Company, res Result, _ string) {
				w, err := co.LoadWorking()
				if err != nil {
					t.Fatal(err)
				}
				if w.Status != "draft" || w.Revision != 1 || w.Story.Title != "The Test Night" {
					t.Errorf("working = {status %q, rev %d, title %q}", w.Status, w.Revision, w.Story.Title)
				}
				if w.Story.ID != "stry_ab12" {
					t.Errorf("story id = %q, want the generation id", w.Story.ID)
				}
				if w.Story.Origin != "llm" {
					t.Errorf("story origin = %q, want llm", w.Story.Origin)
				}
				// The director sees the compact report, not the full story JSON.
				if !strings.Contains(res.Text, "draft written") || strings.Contains(res.Text, `"cast"`) {
					t.Errorf("deliverable = %q, want the compact report", res.Text)
				}
			},
		},
		{
			name: "scenographer dresses the working draft",
			role: "scenographer", tool: "write_scene", wantCalls: 1,
			input: models.Input{"backdrop": "garden", "report": "a night garden"},
			verify: func(t *testing.T, co *Company, _ Result, _ string) {
				w, err := co.LoadWorking()
				if err != nil {
					t.Fatal(err)
				}
				if w.Status != "dressed" || w.Story.Scene.Backdrop != "garden" {
					t.Errorf("working = {status %q, backdrop %q}", w.Status, w.Story.Scene.Backdrop)
				}
			},
		},
		{
			name: "wardrobe answers in text",
			role: "wardrobe", tool: "advise", wantCalls: 1,
			input: models.Input{"answer": "silver reads; keep ina lane 1"},
			verify: func(t *testing.T, _ *Company, res Result, toolOut string) {
				if toolOut != "silver reads; keep ina lane 1" {
					t.Errorf("advise output = %q, want the answer passed through", toolOut)
				}
				if res.Text != "report: done" {
					t.Errorf("deliverable = %q, want the stub's final text", res.Text)
				}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := Open(t.TempDir())
			stage := OpenStage(co, "stry_ab12")
			silenceFeed(stage)
			// The scenographer dresses a draft, so the working file exists
			// before its invocation.
			if tt.role == "scenographer" {
				if err := co.SaveWorking(Working{Story: validStory(), Revision: 1, Status: "draft"}); err != nil {
					t.Fatal(err)
				}
			}

			var gotParams llmParams
			var toolOut string
			runner := NewRunner(co, stage, WithCacheDir(t.TempDir()))
			runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
				gotParams = p
				// The playwright's story is the structured final answer; the
				// other roles "use" their writer tool, then report.
				if tt.tool == "" {
					return llmOutcome{text: storyJSON(t)}, nil
				}
				toolOut = callTool(t, p, tt.tool, tt.input)
				return llmOutcome{text: "report: done"}, nil
			}
			res, err := runner.Run(context.Background(), Invocation{Role: tt.role, Task: "do your thing", Budget: 8})
			if err != nil {
				t.Fatalf("run %s: %v", tt.role, err)
			}
			tt.verify(t, co, res, toolOut)
			if gotParams.tools == nil {
				t.Error("LLM received no tools")
			}

			// Every invocation is accounted: the writer tool call. The final
			// answer is the loop's terminal roundtrip, not a budgeted call
			// (review 3, R3-03). The playwright's structured story costs no
			// tool calls.
			if calls := ledgerCalls(t, stage, tt.role); calls != tt.wantCalls {
				t.Errorf("ledger calls for %s = %d, want %d", tt.role, calls, tt.wantCalls)
			}
			// The deliver event names the role's artifact.
			tr, err := co.LoadTranscript()
			if err != nil {
				t.Fatal(err)
			}
			var delivered bool
			for _, ev := range tr.Events {
				if ev.Kind == "deliver" && ev.From == tt.role {
					delivered = true
					if ev.To != artifactName(tt.role) {
						t.Errorf("deliver to = %q, want %q", ev.To, artifactName(tt.role))
					}
				}
			}
			if !delivered {
				t.Error("no deliver event for the invocation")
			}
		})
	}
}

func storyJSON(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(validStory())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A failing LLM call falls back to the role's deterministic answer (the
// phase-5 seam), which the runner reports on the result and the transcript.
func TestRunner_LLMFailureFallsBack(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)

	runner, _ := stubRunner(t, stage, nil)
	runner.fallback = func(role, task string) (string, error) {
		return "composer floor for " + role, nil
	}
	runner.runLLM = func(context.Context, llmParams) (llmOutcome, error) {
		return llmOutcome{}, errors.New("llm query: boom")
	}

	res, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Fallback || res.Text != "composer floor for dramaturg" {
		t.Errorf("result = %+v, want the fallback's text with Fallback set", res)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "boom") {
		t.Errorf("result.Err = %v, want the llm failure", res.Err)
	}

	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var noted bool
	for _, ev := range tr.Events {
		if ev.Kind == "note" && ev.Level == "warning" && strings.Contains(ev.Body, "fallback") {
			noted = true
		}
	}
	if !noted {
		t.Error("transcript lacks the fallback warning note")
	}
}

// When both the LLM and the fallback fail, the runner returns a real error —
// the caller answers with the composer floor.
func TestRunner_LLMAndFallbackBothFail(t *testing.T) {
	t.Parallel()
	_, stage, _, _, _ := wiredProduction(t, nil)
	runner := NewRunner(stage.company, stage)
	runner.runLLM = func(context.Context, llmParams) (llmOutcome, error) {
		return llmOutcome{}, errors.New("llm query: boom")
	}
	runner.fallback = func(string, string) (string, error) {
		return "", errors.New("fallback broke too")
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err == nil {
		t.Fatal("want an error when LLM and fallback both fail")
	}
}

// A run past the global call cap stops at the cap with a clear refusal and
// never reaches the LLM.
func TestRunner_GlobalBudgetCapRefused(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, prompts := wiredProduction(t, nil, WithBudgets(50, 0))
	_, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8})
	if err == nil || !strings.Contains(err.Error(), "call budget exhausted") {
		t.Fatalf("err = %v, want the budget refusal", err)
	}
	if len(*prompts) != 0 {
		t.Errorf("LLM ran %d loops, want 0", len(*prompts))
	}
	if got := ledgerCalls(t, stage, "dramaturg"); got != 0 {
		t.Errorf("dramaturg calls = %d, want 0", got)
	}
}

// A run past the wall-clock deadline is refused without an LLM call.
func TestRunner_WallDeadlineRefused(t *testing.T) {
	t.Parallel()
	_, _, runner, _, prompts := wiredProduction(t, nil, WithWallDeadline(-time.Minute))
	_, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8})
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("err = %v, want the deadline refusal", err)
	}
	if len(*prompts) != 0 {
		t.Errorf("LLM ran %d loops, want 0", len(*prompts))
	}
}

// A panicking agent loop is recovered: the runner returns an error and the
// ledger records the failure — the generation continues.
func TestRunner_PanicRecovered(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, _ := wiredProduction(t, nil)
	runner.runLLM = func(context.Context, llmParams) (llmOutcome, error) {
		panic("boom")
	}
	_, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8})
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("err = %v, want the recovered panic", err)
	}
	ledger, lerr := stage.company.LoadLedger()
	if lerr != nil {
		t.Fatal(lerr)
	}
	for _, a := range ledger.Actors {
		if a.Role == "dramaturg" && a.Status == "failed" {
			return
		}
	}
	t.Errorf("ledger actors = %+v, want dramaturg marked failed", ledger.Actors)
}

// A board read failure degrades to the empty board: the subagent gets the
// empty excerpt and the generation continues.
func TestRunner_BoardReadFailureGetsEmptyBoard(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	if err := os.MkdirAll(filepath.Dir(co.boardPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(co.boardPath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, prompts := stubRunner(t, stage, nil)
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "(empty — nothing posted yet)") {
		t.Errorf("prompt = %q, want the empty board excerpt", (*prompts)[0])
	}
}

// A collaborations field in the deliverable yields exactly one re-invocation
// of the original role, with the consulted role's answer present in its task.
func TestRunner_CollaborationsResolvedOnce(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, prompts := wiredProduction(t, func(prompt string) llmOutcome {
		switch {
		case strings.Contains(prompt, "does silver read?"):
			return llmOutcome{text: "silver reads; keep ina lane 1"}
		case strings.Contains(prompt, "The consulted wardrobe answered"):
			return llmOutcome{text: deliverable("revised brief")}
		}
		return llmOutcome{text: deliverable("first brief", "wardrobe|does silver read?")}
	})

	res, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "write the brief", Budget: 8})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "revised brief" {
		t.Errorf("text = %q, want the revised brief", res.Text)
	}

	// The original role ran twice (initial + one revision), the consulted
	// role once — exactly one re-invocation.
	dramaturgRuns := 0
	wardrobeRuns := 0
	for _, p := range *prompts {
		if strings.Contains(p, "You are the dramaturg.") {
			dramaturgRuns++
		}
		if strings.Contains(p, "You are the wardrobe consultant.") {
			wardrobeRuns++
		}
	}
	if dramaturgRuns != 2 {
		t.Errorf("dramaturg ran %d times, want 2 (initial + one revision)", dramaturgRuns)
	}
	if wardrobeRuns != 1 {
		t.Errorf("wardrobe ran %d times, want 1", wardrobeRuns)
	}

	// The answer is present in the revision's task.
	var revision string
	for _, p := range *prompts {
		if strings.Contains(p, "The consulted wardrobe answered") {
			revision = p
		}
	}
	if !strings.Contains(revision, "silver reads; keep ina lane 1") {
		t.Errorf("revision prompt lacks the consulted answer:\n%s", revision)
	}

	// The broker posted the question and the answer to the board.
	board, err := stage.company.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Entries) != 2 {
		t.Errorf("board entries = %d, want the question + answer", len(board.Entries))
	}
}

// The wrapper resolves at most two collaboration rounds per invocation, no
// matter how many the deliverable requests.
func TestRunner_CollaborationsCappedAtTwoRounds(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, prompts := wiredProduction(t, func(prompt string) llmOutcome {
		switch {
		case strings.Contains(prompt, "You are the wardrobe consultant."):
			return llmOutcome{text: "wardrobe says: fine"}
		case strings.Contains(prompt, "You are the scenographer."):
			return llmOutcome{text: "scenographer says: fine"}
		case strings.Contains(prompt, "You are the playwright."):
			return llmOutcome{text: "playwright says: fine"}
		case strings.Contains(prompt, "The consulted"):
			return llmOutcome{text: deliverable("revised")}
		}
		return llmOutcome{text: deliverable("first",
			"wardrobe|q1", "scenographer|q2", "playwright|q3")}
	})

	res, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "revised" {
		t.Errorf("text = %q", res.Text)
	}

	// Two rounds: two consult spawns (wardrobe, scenographer) and two
	// revisions. The third collaboration (playwright) is never spawned.
	if promptsContain(prompts, "You are the playwright.") {
		t.Error("the playwright was spawned — the round cap failed")
	}
	marker := map[string]string{
		"wardrobe":     "You are the wardrobe consultant.",
		"scenographer": "You are the scenographer.",
		"playwright":   "You are the playwright.",
	}
	spawns := map[string]int{}
	for _, p := range *prompts {
		for role, m := range marker {
			if strings.Contains(p, m) {
				spawns[role]++
			}
		}
	}
	if spawns["wardrobe"] != 1 || spawns["scenographer"] != 1 || spawns["playwright"] != 0 {
		t.Errorf("consult spawns = %v, want wardrobe 1, scenographer 1, playwright 0", spawns)
	}
	_ = stage
}

// The runner writes the session to the per-role log file: the assembled
// context is in the file, and nothing reaches stdout.
func TestRunner_SessionLogWritten(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	cacheDir := t.TempDir()

	runner, _ := stubRunner(t, stage, nil)
	runner.cacheDir = cacheDir
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "write the brief", Budget: 8}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(cacheDir, CompanyDir, "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("session log dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("session logs = %d, want 1", len(entries))
	}
	b, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	for _, needle := range []string{"Generation: stry_ab12", "You are the dramaturg.", "write the brief"} {
		if !strings.Contains(content, needle) {
			t.Errorf("session log missing %q", needle)
		}
	}
}

// The runner streams session entries tagged theatre.<role> with the
// generation id as corrID — the shape the house loghandler prints.
func TestRunner_StreamsTaggedLogEntries(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	var msgs []model.LogMessage
	stage := OpenStage(co, "stry_ab12", WithLogSink(func(m model.LogMessage) {
		msgs = append(msgs, m)
	}))
	silenceFeed(stage)
	runner, _ := stubRunner(t, stage, nil)
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 {
		t.Fatalf("log entries = %d, want at least start + deliver", len(msgs))
	}
	for _, m := range msgs {
		if m.Logger != "theatre.dramaturg" {
			t.Errorf("logger = %q, want theatre.dramaturg", m.Logger)
		}
		if !strings.Contains(m.Message, "corrID: stry_ab12") {
			t.Errorf("message = %q, want the generation corrID", m.Message)
		}
	}
}

// The playwright's structured story carries its canon facts: the wrapper
// captures the story's "canon" array into the working file, capped in count
// and length (machine fix, 2026-08-03).
func TestRunner_StoryCanonAccumulates(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	if err := co.SaveWorking(Working{Story: validStory(), Revision: 1, Status: "draft"}); err != nil {
		t.Fatal(err)
	}

	var gotParams llmParams
	runner := NewRunner(co, stage, WithCacheDir(t.TempDir()))
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		gotParams = p
		return llmOutcome{text: storyWithCanon(t, "the mouse got away")}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "playwright", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if gotParams.tools == nil {
		t.Fatal("no tools")
	}
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Canon) != 1 || w.Canon[0] != "the mouse got away" {
		t.Errorf("canon = %v, want the story's canon fact", w.Canon)
	}
}

// The shared tools work in-loop: read_board returns the current excerpt and
// consult routes through the broker, so a subagent can read the room and ask
// a production role mid-invocation.
func TestRunner_SharedToolsWorkInLoop(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, _ := wiredProduction(t, nil)
	if err := stage.company.SaveBoard(Board{
		Generation: "stry_ab12",
		Entries:    []Entry{{Author: "director", Kind: "note", To: "dramaturg", Body: "keep it dry"}},
	}); err != nil {
		t.Fatal(err)
	}

	var consultAnswer string
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		// Only the top-level invocation uses the tools; the consulted role
		// just answers.
		if strings.Contains(p.prompt, "You are the dramaturg.") {
			excerpt := callTool(t, p, "read_board", models.Input{})
			if !strings.Contains(excerpt, "keep it dry") {
				t.Errorf("read_board excerpt = %q, want the posted note", excerpt)
			}
			consultAnswer = callTool(t, p, "consult", models.Input{"role": "wardrobe", "question": "does silver read?"})
			return llmOutcome{text: "report: done"}, nil
		}
		return llmOutcome{text: "ok"}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if consultAnswer != "ok" {
		t.Errorf("consult answer = %q, want the stub's answer", consultAnswer)
	}
	// The in-loop consult recorded the spawn: the consulted wardrobe ran its
	// loop (an answer roundtrip) without any budgeted tool call (review 3,
	// R3-03).
	if calls := ledgerCalls(t, stage, "wardrobe"); calls != 0 {
		t.Errorf("wardrobe calls = %d, want 0 (the consulted role only answered)", calls)
	}
}

// post_to_board rejects unknown kinds with a message the model can read; the
// board gate would otherwise drop the entry silently.
func TestRunner_PostToBoardRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, _ := wiredProduction(t, nil)
	var out string
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		out = callTool(t, p, "post_to_board", models.Input{"kind": "rant", "to": "director", "body": "this is bad"})
		return llmOutcome{text: "report: done"}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unknown kind") {
		t.Errorf("tool output = %q, want the unknown-kind message", out)
	}
	board, err := stage.company.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Entries) != 0 {
		t.Errorf("board entries = %d, want 0 — the rejected entry must not land", len(board.Entries))
	}
}

// post_to_board rejects an unknown addressee with a message the model can
// read, so the board and the transcript cannot diverge over it: the board
// gate would clear an invalid to and keep the entry while the transcript
// dropped the same event (review 1, R1-04). A valid addressee records on
// both.
func TestRunner_PostToBoardRejectsUnknownAddressee(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, _ := wiredProduction(t, nil)
	var bad, good string
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		bad = callTool(t, p, "post_to_board", models.Input{"kind": "note", "to": "costume", "body": "retired role"})
		good = callTool(t, p, "post_to_board", models.Input{"kind": "note", "to": "director", "body": "a real note"})
		return llmOutcome{text: "report: done"}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bad, "unknown addressee") {
		t.Errorf("tool output = %q, want the unknown-addressee message", bad)
	}
	if !strings.Contains(good, "posted to board") {
		t.Errorf("tool output = %q, want the success message", good)
	}

	board, err := stage.company.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Entries) != 1 || board.Entries[0].Body != "a real note" {
		t.Errorf("board entries = %+v, want only the accepted post", board.Entries)
	}
	tr, err := stage.company.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	posts := 0
	for _, ev := range tr.Events {
		if ev.Kind == "post" {
			posts++
		}
	}
	if posts != 1 {
		t.Errorf("transcript post events = %d, want 1 — board and transcript must agree", posts)
	}
}

// The writer tools answer with clear messages when their preconditions are
// missing: a scene without a draft.
func TestRunner_WriterErrorPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		role  string
		tool  string
		input models.Input
		want  string
		setup func(t *testing.T, co *Company)
	}{
		{"scene without draft", "scenographer", "write_scene", models.Input{"backdrop": "garden"}, "no draft", nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, stage, runner, _, _ := wiredProduction(t, nil)
			if tt.setup != nil {
				tt.setup(t, stage.company)
			}
			var out string
			runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
				out = callTool(t, p, tt.tool, tt.input)
				return llmOutcome{text: "report: done"}, nil
			}
			if _, err := runner.Run(context.Background(), Invocation{Role: tt.role, Task: "t", Budget: 8}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("tool output = %q, want it to mention %q", out, tt.want)
			}
		})
	}
}

// The runner options apply: model, config dir and the deterministic fallback
// seam all land on the runner.
func TestRunner_OptionsApplied(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	fallback := func(role, task string) (string, error) { return "floor", nil }
	runner := NewRunner(co, stage,
		WithModel("gpt-5.2"),
		WithConfigDir("/tmp/clai"),
		WithCacheDir(t.TempDir()),
		WithFallback(fallback),
	)
	if runner.model != "gpt-5.2" || runner.configDir != "/tmp/clai" || runner.cacheDir == "" {
		t.Errorf("runner config = {%q, %q, %q}", runner.model, runner.configDir, runner.cacheDir)
	}
	got, err := runner.fallback("dramaturg", "t")
	if err != nil || got != "floor" {
		t.Errorf("fallback = (%q, %v), want the injected one", got, err)
	}
}

// A director invocation runs with the facade's injected tool set: the runner
// is the director's loop too (phase 4), and the tool wiring is the seam
// between the two.
func TestRunner_DirectorUsesInjectedTools(t *testing.T) {
	t.Parallel()
	_, stage, runner, _, _ := wiredProduction(t, nil)
	called := false
	runner.directorTools = func(context.Context) []models.LLMTool {
		called = true
		return []models.LLMTool{
			tools.NewSubmitStory(func(string, string) (string, error) { return "submitted", nil }),
		}
	}

	var out string
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		if strings.Contains(p.prompt, "You are the director of") {
			out = callTool(t, p, "submit_story", models.Input{})
		}
		return llmOutcome{text: "ok"}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "director", Task: "t", Budget: 50, Depth: 0}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !called {
		t.Error("director tools were never built")
	}
	if out != "submitted" {
		t.Errorf("tool output = %q, want the injected submit result", out)
	}
	if calls := ledgerCalls(t, stage, "director"); calls != 1 {
		t.Errorf("director calls = %d, want 1 (the submit tool call; the answer is not budgeted)", calls)
	}
}

// R3-03 acceptance: an actor that exhausts its full tool budget never shows
// more calls than its budget — the trailing answer is the loop's terminal
// roundtrip, not a budgeted call, so the phase line reads exactly budget/budget.
func TestRunner_FullBudgetNeverShowsOverCap(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12", WithBudgets(50, 200))
	silenceFeed(stage)
	runner := NewRunner(co, stage, WithCacheDir(t.TempDir()))
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		// The dramaturg spends its full 8-call tool budget, then answers.
		for range 8 {
			callTool(t, p, "post_to_board", models.Input{"kind": "note", "body": "spam"})
		}
		return llmOutcome{text: "brief posted"}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if calls := ledgerCalls(t, stage, "dramaturg"); calls != 8 {
		t.Errorf("dramaturg calls = %d, want 8 (the full tool budget; the answer is not budgeted)", calls)
	}

	// The phase line renders the actor at exactly its budget — never over it.
	stage.SetPhase("brief")
	if body := stage.phaseBody(); !strings.Contains(body, "dramaturg 8/8 calls") {
		t.Errorf("phase line = %q, want the actor at exactly its budget", body)
	}
	stage.Close()
	<-stage.feed.done()
}

// Canon facts round-trip (acceptance): facts injected into the playwright's
// context are visible in the working file's canon when the playwright
// appends them through append_canon — the soft-continuity seam (D6). The
// playwright's story itself arrives as the structured final answer (machine
// fix, 2026-08-03).
func TestRunner_CanonFactsRoundTrip(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	// The working file's canon is the injection seam: phase 6 seeds it from
	// the repertoire doc at generation start; here the playwright reads what
	// a previous generation left.
	if err := co.SaveWorking(Working{
		Story: validStory(), Revision: 1, Status: "draft",
		Canon: []string{"the mouse got away"},
	}); err != nil {
		t.Fatal(err)
	}

	runner, _ := stubRunner(t, stage, nil)
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		if !strings.Contains(p.prompt, "the mouse got away") {
			t.Errorf("playwright context lacks the injected canon fact")
		}
		return llmOutcome{text: storyWithCanon(t, "the box was claimed")}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "playwright", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}

	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	// The injected fact is kept, the appended one added — both capped and
	// deduped by the working file's own gate. The story is the playwright's
	// structured deliverable, not the fixture's.
	if len(w.Canon) != 2 || w.Canon[0] != "the mouse got away" || w.Canon[1] != "the box was claimed" {
		t.Errorf("canon = %v, want the injected and the appended facts", w.Canon)
	}
	if w.Story.Title != "The Test Night" || w.Story.Origin != "llm" {
		t.Errorf("story = {title %q, origin %q}, want the structured deliverable", w.Story.Title, w.Story.Origin)
	}
}

// A canon fact past the length cap is truncated to the cap on write — the
// same bound the working file's own gate applies on load.
func TestRunner_CanonFactTruncatedToCap(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)

	longFact := strings.Repeat("the mouse got away ", 30)
	runner := NewRunner(co, stage, WithCacheDir(t.TempDir()))
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		return llmOutcome{text: storyWithCanon(t, longFact)}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "playwright", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Canon) != 1 || len([]rune(w.Canon[0])) > CanonMaxFact {
		t.Errorf("canon = %v, want one fact truncated to %d runes", w.Canon, CanonMaxFact)
	}
}

// The scenographer's scene is validated against the draft (cross-schema
// contract): a prop placement naming a prop the draft does not have is
// dropped, and the placements for the draft's own props are applied.
func TestRunner_SceneValidatedAgainstDraft(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	story := validStory()
	story.Props = []model.Prop{{ID: "yarn1", Prop: "yarn", Lane: 0, X: 0.3}}
	if err := co.SaveWorking(Working{Story: story, Revision: 1, Status: "draft"}); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(co, stage, WithCacheDir(t.TempDir()))
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		callTool(t, p, "write_scene", models.Input{
			"backdrop": "livingroom",
			"report": `{"backdrop":"livingroom",` +
				`"cells":[{"row":"far","col":5,"piece":"window"}],` +
				`"props":[{"id":"yarn1","x":0.7,"lane":1},{"id":"bogus1","x":0.2,"lane":2}],` +
				`"reason":"test"}`,
		})
		return llmOutcome{text: "report: done"}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "scenographer", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}

	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Story.Props) != 1 || w.Story.Props[0].X != 0.7 || w.Story.Props[0].Lane != 1 {
		t.Errorf("props = %+v, want yarn1 moved to x=0.7 lane=1 (bogus1 dropped)", w.Story.Props)
	}
	if len(w.Story.Scene.Cells) != 1 || w.Story.Scene.Cells[0].Piece != "window" {
		t.Errorf("cells = %+v, want the window dressed", w.Story.Scene.Cells)
	}
}

// A playwright loop that ends without a playable draft is answered by the
// composer draft (error table): the director always has a working file to
// build on, and the transcript notes the offer.
func TestRunner_PlaywrightNoDraftFallsBack(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage, WithCacheDir(t.TempDir()))
	runner.runLLM = func(context.Context, llmParams) (llmOutcome, error) {
		// The playwright "succeeds" but never calls write_draft.
		return llmOutcome{text: "i decided against a draft"}, nil
	}

	res, err := runner.Run(context.Background(), Invocation{Role: "playwright", Task: "t", Budget: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fallback {
		t.Error("want the composer draft to answer")
	}
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatalf("no draft in the working file: %v", err)
	}
	if w.Story.Origin != "composer" || w.Story.ID != stage.gen {
		t.Errorf("offered draft = {origin %q, id %q}, want the composer draft under the generation id", w.Story.Origin, w.Story.ID)
	}
	tr, err := co.LoadTranscript()
	if err != nil {
		t.Fatal(err)
	}
	var noted bool
	for _, ev := range tr.Events {
		if ev.Kind == "note" && strings.Contains(ev.Body, "composer draft offered") {
			noted = true
		}
	}
	if !noted {
		t.Error("transcript lacks the composer-draft-offered note")
	}
}

// The production clai path needs credentials and a configured model; without
// them Setup fails cleanly and the dramaturg's deterministic fallback answers
// with a brief posted to the board — nothing crashes and the production
// still has an artifact.
func TestRunner_RunClaiFailsWithoutModel(t *testing.T) {
	t.Parallel()
	co, stage, _, _, _ := wiredProduction(t, nil)
	// Rebuild without the stub: the production runLLM (runClai) and the
	// internal fallback dispatcher. The config dir is a temp dir so clai's
	// setup never touches the repo.
	runner := NewRunner(stage.company, stage, WithConfigDir(t.TempDir()))
	runner.broker = nil
	res, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8})
	if err != nil {
		t.Fatalf("run: %v — the fallback should answer", err)
	}
	if !res.Fallback {
		t.Error("want the dramaturg fallback to answer")
	}
	if res.Err == nil {
		t.Error("want the clai failure reported on the result")
	}
	// The fallback posted a valid brief to the board, exactly like write_brief.
	board, err := co.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Entries) != 1 || board.Entries[0].Kind != "brief" || board.Entries[0].Author != "dramaturg" {
		t.Errorf("board = %+v, want the fallback brief posted by the dramaturg", board.Entries)
	}
}

// The playwright's structured-output contract (machine fix, 2026-08-03): the
// production playwright's loop carries the story response format (json_schema,
// strict), so the API enforces the story shape; a consulted playwright
// answers in place and other roles deliver through tools, so their loops stay
// free text.
func TestRunner_ResponseFormatOnlyForProductionPlaywright(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner, _ := stubRunner(t, stage, nil)

	var formats []*models.ResponseFormat
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		formats = append(formats, p.rf)
		return llmOutcome{text: storyJSON(t)}, nil
	}

	// Production playwright (depth 0): structured story.
	if _, err := runner.Run(context.Background(), Invocation{Role: "playwright", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if len(formats) != 1 || formats[0] == nil || formats[0].Type != "json_object" || formats[0].Schema != nil {
		t.Fatalf("playwright response format = %+v, want json_object (the deepseek endpoint supports json_object only)", formats)
	}

	// Consulted playwright (depth 1): answers in place, free text.
	broker := NewBroker(co, stage, runner)
	runner.WireBroker(broker)
	if _, err := broker.Consult(context.Background(), "director", "playwright", "what is the draft?", 0); err != nil {
		t.Fatal(err)
	}
	if len(formats) != 2 || formats[1] != nil {
		t.Fatalf("consulted playwright response format = %+v, want nil (free text)", formats[1])
	}

	// Another production role: its deliverable is a tool, not the final answer.
	if _, err := runner.Run(context.Background(), Invocation{Role: "dramaturg", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if len(formats) != 3 || formats[2] != nil {
		t.Fatalf("dramaturg response format = %+v, want nil (free text)", formats[2])
	}
}

// A playwright final answer that is not a playable story gets one bounded
// revision round with the exact validation error (the writeDraft gate's
// schema hint), then the corrected story lands in the working file — the
// backstop for endpoints that enforce the story schema weakly.
func TestRunner_PlaywrightInvalidStoryRevision(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner, _ := stubRunner(t, stage, nil)

	calls := 0
	var feedback string
	runner.runLLM = func(_ context.Context, p llmParams) (llmOutcome, error) {
		calls++
		if calls == 1 {
			// The exact shape the production playwright produced on 09:16:
			// cast entries with the wrong field names.
			return llmOutcome{text: `{"title":"The Cheese Inspection","durationMs":8000,"scene":{"backdrop":"kitchen"},"cast":[{"role":"cat","species":"cat"}],"beats":[{"t":0,"actor":"ina","action":"enter"}]}`}, nil
		}
		feedback = p.prompt
		return llmOutcome{text: storyJSON(t)}, nil
	}
	if _, err := runner.Run(context.Background(), Invocation{Role: "playwright", Task: "t", Budget: 8}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("llm calls = %d, want 2 (original + one revision)", calls)
	}
	if !strings.Contains(feedback, "no valid cast") || !strings.Contains(feedback, "character") {
		t.Errorf("revision feedback lacks the validation error and the schema hint: %q", feedback)
	}
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if w.Story.Title != "The Test Night" || w.Story.Origin != "llm" {
		t.Errorf("story = {title %q, origin %q}, want the revised deliverable", w.Story.Title, w.Story.Origin)
	}
}

// storyWithCanon is storyJSON plus a canon array — the structured story the
// playwright delivers when it leaves canon facts behind (the soft-continuity
// seam rides on the story's "canon" field, machine fix 2026-08-03).
func storyWithCanon(t *testing.T, facts ...string) string {
	t.Helper()
	b, err := json.Marshal(validStory())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["canon"] = facts
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

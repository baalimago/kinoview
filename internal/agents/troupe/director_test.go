package troupe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
)

// newTestDirector builds a director over a fake swarm and a seeded
// submitter worktree.
func newTestDirector(t *testing.T, swarm Swarm, opts ...DirectorOption) (*Director, *Submitter) {
	t.Helper()
	sub := newTestSubmitter(t)
	all := append([]DirectorOption{
		WithSwarm(swarm),
		WithSubmitter(sub),
		WithDirectorModel("gpt-5"),
		WithDirectorConfigDir(t.TempDir()),
	}, opts...)
	d, err := NewDirector(all...)
	if err != nil {
		t.Fatalf("NewDirector: %v", err)
	}
	return d, sub
}

// findTool locates one tool in a director's tool set by its canonical name.
func findTool(t *testing.T, tools []models.LLMTool, name string) models.LLMTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Specification().Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not in the director's tool set", name)
	return nil
}

// TestNewDirector_Errors pins the required options: a director without a
// swarm, a submitter, a model or a config dir is refused before anything
// runs.
func TestNewDirector_Errors(t *testing.T) {
	cases := []struct {
		name string
		opts []DirectorOption
		want string
	}{
		{"no swarm", []DirectorOption{WithSubmitter(newTestSubmitter(t)), WithDirectorModel("m"), WithDirectorConfigDir(t.TempDir())}, "swarm can't be nil"},
		{"no submitter", []DirectorOption{WithSwarm(&fakeSwarm{}), WithDirectorModel("m"), WithDirectorConfigDir(t.TempDir())}, "submitter can't be nil"},
		{"no model", []DirectorOption{WithSwarm(&fakeSwarm{}), WithSubmitter(newTestSubmitter(t)), WithDirectorConfigDir(t.TempDir())}, "model can't be empty"},
		{"no config dir", []DirectorOption{WithSwarm(&fakeSwarm{}), WithSubmitter(newTestSubmitter(t)), WithDirectorModel("m")}, "config dir can't be empty"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDirector(tt.opts...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestDirector_ToolSet pins the director's instrument set: the closed
// registry plus submit_play — file tools as exact clai globs, notebook tools
// only when the notebook is on, spawn_role bound to the swarm, and
// submit_play on top (never selectable by a role note).
func TestDirector_ToolSet(t *testing.T) {
	d, _ := newTestDirector(t, &fakeSwarm{})

	globs, tools := d.toolSet()
	for _, want := range []string{"cat", "rows_between", "ls", "rg", "write_file", "apply_patch", "mkdir"} {
		if !slices.Contains(globs, want) {
			t.Errorf("globs missing the file tool %q: %v", want, globs)
		}
	}
	for _, absent := range []string{"mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit"} {
		if slices.Contains(globs, absent) {
			t.Errorf("globs carry %q without the notebook: %v", absent, globs)
		}
	}
	got := make([]string, len(tools))
	for i, tool := range tools {
		got[i] = tool.Specification().Name
	}
	if !slices.Contains(got, "spawn_role") || !slices.Contains(got, "submit_play") {
		t.Errorf("tools = %v, want spawn_role and submit_play", got)
	}

	t.Run("notebook adds the mcp globs", func(t *testing.T) {
		server := slivingdoc.Server(slivingdoc.NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")
		d, _ := newTestDirector(t, &fakeSwarm{}, WithDirectorNotebook(server, "/cache/slivingdoc"))
		globs, _ := d.toolSet()
		for _, want := range []string{"mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit"} {
			if !slices.Contains(globs, want) {
				t.Errorf("globs missing %q with the notebook on: %v", want, globs)
			}
		}
	})
}

// TestDirector_Prompt pins the assembled prompt: the fixed workflow naming
// the submit step, the current UTC stamped in the play-id format (the
// director authors story_<UTC> ids and the registry carries no clock tool),
// and the shared NOTES partial when the notebook is on.
func TestDirector_Prompt(t *testing.T) {
	d, _ := newTestDirector(t, &fakeSwarm{})
	p := d.prompt()
	for _, want := range []string{"WORKFLOW", "spawn_role", "submit_play", "single writer of plays"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	nowRe := regexp.MustCompile(`NOW\n\d{8}T\d{6}Z UTC\. Author the play under plays/story_\d{8}T\d{6}Z\.json`)
	if !nowRe.MatchString(p) {
		t.Errorf("prompt must stamp the current UTC in the play-id format:\n%s", p)
	}
	if strings.Contains(p, "NOTES") {
		t.Errorf("no-notebook prompt must omit the NOTES partial:\n%s", p)
	}

	t.Run("notebook names the workspace", func(t *testing.T) {
		server := slivingdoc.Server(slivingdoc.NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")
		d, _ := newTestDirector(t, &fakeSwarm{}, WithDirectorNotebook(server, "/cache/slivingdoc"))
		p := d.prompt()
		if !strings.Contains(p, "Pull the shared notebook into /cache/slivingdoc before you start.") {
			t.Errorf("prompt must carry the NOTES partial naming the workspace:\n%s", p)
		}
	})
}

// TestDirector_GenerationSubmits is the phase-7 headline: a generation with
// a mocked swarm assembles and submits a play, and the submit path is
// exercised end to end — the scripted director spawns a sub-agent through
// the swarm, writes the assembled play into the worktree, submits it through
// the real submit_play gate, and the resolved play lands on disk with its
// index entry.
func TestDirector_GenerationSubmits(t *testing.T) {
	swarm := &fakeSwarm{script: map[string]string{"sculptor": "carved the cat"}}
	const playID = "story_20260821T093000Z"

	d, sub := newTestDirector(t, swarm)
	d.runDirector = func(ctx context.Context, p directorParams) (string, error) {
		// The scripted director agent: spawn one sub-agent, assemble the play
		// from what the swarm left, then submit.
		if p.prompt == "" || len(p.globs) == 0 || len(p.tools) == 0 {
			t.Fatal("the director loop must receive the prompt, the globs and the tools")
		}
		spawn := findTool(t, p.tools, "spawn_role")
		out, err := spawn.Call(map[string]any{"role": "sculptor", "task": "carve the cat"})
		if err != nil {
			t.Fatalf("spawn_role: %v", err)
		}
		if out != "carved the cat" {
			t.Errorf("spawn_role out = %q, want the swarm's gathered final message", out)
		}
		writeDraft(t, sub.worktree, playID) // the assemble step
		submit := findTool(t, p.tools, "submit_play")
		if _, err := submit.Call(map[string]any{"play": playID}); err != nil {
			t.Fatalf("submit_play: %v", err)
		}
		return "the play is good", nil
	}

	outcome, err := d.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Submitted || outcome.PlayID != playID {
		t.Errorf("outcome = %+v, want submitted %s", outcome, playID)
	}

	// The swarm received exactly the commission the director gave it.
	calls := swarm.callsSnapshot()
	if len(calls) != 1 || calls[0].role != "sculptor" || calls[0].task != "carve the cat" {
		t.Errorf("commissions = %+v, want the one sculptor commission", calls)
	}

	// The submit path landed end to end: the resolved play is durably on
	// disk and the index carries its entry.
	playPath := filepath.Join(sub.worktree, "plays", playID+".json")
	data, err := os.ReadFile(playPath)
	if err != nil {
		t.Fatalf("resolved play on disk: %v", err)
	}
	if !isResolvedPlay(data) {
		t.Error("the persisted play is not the resolved served artifact")
	}
	idx, err := readIndex(filepath.Join(sub.worktree, playIndex))
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx.Index) != 1 || idx.Index[0].ID != playID {
		t.Errorf("index = %+v, want exactly the %s entry", idx.Index, playID)
	}

	// The director admitted at depth 0 and released: the generation budget
	// is back to rest.
	if got := d.budget.Stats(); got.Depth != 0 || got.Reserved != 0 {
		t.Errorf("budget after generation = %+v, want depth and reservation released", got)
	}
}

// TestDirector_GenerationExhausts pins the no-seed rule: exhaustion — the
// swarm produces nothing, or the director never submits — ships nothing. No
// play file is written, no index is created, and the stage stays empty.
func TestDirector_GenerationExhausts(t *testing.T) {
	cases := []struct {
		name  string
		swarm Swarm
		run   func(t *testing.T, p directorParams)
	}{
		{
			name:  "swarm produces nothing",
			swarm: &fakeSwarm{err: errors.New("the swarm produced nothing")},
			run: func(t *testing.T, p directorParams) {
				spawn := findTool(t, p.tools, "spawn_role")
				if _, err := spawn.Call(map[string]any{"role": "sculptor", "task": "carve the cat"}); err == nil {
					t.Error("spawn_role must surface the swarm's failure")
				}
				// Nothing came back to assemble; the director ends without
				// submitting.
			},
		},
		{
			name:  "director never submits",
			swarm: &fakeSwarm{},
			run: func(t *testing.T, p directorParams) {
				// The director burns its budget and ends without a submit.
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			d, sub := newTestDirector(t, tt.swarm)
			d.runDirector = func(_ context.Context, p directorParams) (string, error) {
				tt.run(t, p)
				return "budget spent", nil
			}

			outcome, err := d.Run(t.Context())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if outcome.Submitted {
				t.Errorf("outcome = %+v, want no submission", outcome)
			}

			// Nothing shipped: the fixture worktree holds no index and the
			// only play file is the conformance fixture, byte-identical.
			if _, err := os.Stat(filepath.Join(sub.worktree, playIndex)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("index exists despite exhaustion (stat err = %v)", err)
			}
			entries, err := os.ReadDir(filepath.Join(sub.worktree, "plays"))
			if err != nil {
				t.Fatalf("read plays dir: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "story_20260820T161500Z.json" {
				t.Errorf("plays dir = %v, want only the fixture play", entries)
			}
		})
	}
}

// TestDirector_GenerationBudgetRefusals pins the phase-5 wiring on the
// generation: the director admits against the generation budget before the
// agent runs, so a generation is refused outright once a guard is spent —
// and the exact guard error returns.
func TestDirector_GenerationBudgetRefusals(t *testing.T) {
	cases := []struct {
		name   string
		budget *Budget
		seed   func(t *testing.T, b *Budget)
		want   error
	}{
		{
			name:   "call max",
			budget: NewBudget(),
			seed: func(t *testing.T, b *Budget) {
				t.Helper()
				for range maxGenerationCalls {
					if err := b.Record(t.Context(), budgetedCall(1)); err != nil {
						t.Fatalf("seed Record: %v", err)
					}
				}
			},
			want: ErrCallMax,
		},
		{
			name:   "stoploss",
			budget: NewBudget(WithStoploss(100)),
			seed: func(t *testing.T, b *Budget) {
				t.Helper()
				if err := b.Record(t.Context(), budgetedCall(100)); err != nil {
					t.Fatalf("seed Record: %v", err)
				}
			},
			want: ErrStoploss,
		},
		{
			name:   "depth cap",
			budget: NewBudget(),
			seed: func(t *testing.T, b *Budget) {
				t.Helper()
				for range maxSpawnDepth {
					if _, err := b.Admit(); err != nil {
						t.Fatalf("seed admit: %v", err)
					}
				}
			},
			want: ErrDepthCap,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.seed(t, tt.budget)
			d, _ := newTestDirector(t, &fakeSwarm{}, WithGenerationBudget(tt.budget), WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
				t.Fatal("the refused generation must never reach the agent loop")
				return "", nil
			}))
			_, err := d.Run(t.Context())
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestDirector_AgentError pins that a failing agent loop fails the
// generation: the run error propagates wrapped, and the admission is
// released.
func TestDirector_AgentError(t *testing.T) {
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		return "", errors.New("the model exploded")
	}))
	_, err := d.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "the model exploded") {
		t.Fatalf("err = %v, want the agent-loop error", err)
	}
	if got := d.budget.Stats(); got.Depth != 0 || got.Reserved != 0 {
		t.Errorf("budget after a failed generation = %+v, want depth and reservation released", got)
	}
}

// TestDirector_RecordingSubmit pins the outcome wrapper: submit_play records
// the submitted id only on success, and a failed submit leaves the
// generation unsubmitted.
func TestDirector_RecordingSubmit(t *testing.T) {
	d, sub := newTestDirector(t, &fakeSwarm{})
	tool := d.recordingSubmitTool()

	// A failed submit records nothing.
	if _, err := tool.Call(map[string]any{"play": "cat@1"}); err == nil {
		t.Fatal("malformed play id must fail the inner gate")
	}
	if d.submitted {
		t.Error("failed submit must not mark the generation submitted")
	}

	// A successful submit records the id.
	const playID = "story_20260821T093000Z"
	writeDraft(t, sub.worktree, playID)
	if _, err := tool.Call(map[string]any{"play": playID}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !d.submitted || d.submittedID != playID {
		t.Errorf("recorded = %v/%s, want submitted %s", d.submitted, d.submittedID, playID)
	}

	// The wrapper preserves the inner tool's spec.
	if spec := tool.Specification(); spec.Name != "submit_play" {
		t.Errorf("Specification = %q, want submit_play", spec.Name)
	}
}

// TestDirector_DefaultBudget pins that a director without WithGenerationBudget
// runs under a default budget: the hardcoded guards are on, and the gate
// precedes the agent loop.
func TestDirector_DefaultBudget(t *testing.T) {
	d, _ := newTestDirector(t, &fakeSwarm{})
	if d.budget == nil {
		t.Fatal("director without a budget option must carry a default budget")
	}
	for range maxGenerationCalls {
		if err := d.budget.Record(t.Context(), budgetedCall(1)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	_, err := d.Run(t.Context())
	if !errors.Is(err, ErrCallMax) {
		t.Fatalf("err = %v, want ErrCallMax from the default budget", err)
	}
}

// TestDirector_GenerationOutcomeRepeats pins that each generation re-arms:
// the recorded outcome resets per Run, so a second generation over the same
// worktree submits a fresh play and reports it while the first stays on
// disk.
func TestDirector_GenerationOutcomeRepeats(t *testing.T) {
	const first = "story_20260821T093000Z"
	const second = "story_20260821T100000Z"
	var submits int
	d, sub := newTestDirector(t, &fakeSwarm{})
	d.runDirector = func(_ context.Context, p directorParams) (string, error) {
		id := first
		if submits > 0 {
			id = second
		}
		submits++
		submit := findTool(t, p.tools, "submit_play")
		_, err := submit.Call(map[string]any{"play": id})
		return "", err
	}
	writeDraft(t, sub.worktree, first)
	writeDraft(t, sub.worktree, second)

	outcome, err := d.Run(t.Context())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !outcome.Submitted || outcome.PlayID != first {
		t.Errorf("first outcome = %+v, want %s", outcome, first)
	}

	outcome, err = d.Run(t.Context())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !outcome.Submitted || outcome.PlayID != second {
		t.Errorf("second outcome = %+v, want %s", outcome, second)
	}

	idx, err := readIndex(filepath.Join(sub.worktree, playIndex))
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx.Index) != 2 {
		t.Errorf("index has %d entries, want 2 — both plays stay on disk", len(idx.Index))
	}
}

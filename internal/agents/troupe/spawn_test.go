package troupe

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
)

// fakeRoles is an in-memory RoleSource for spawner tests: the contract the
// snapshot-backed source implements, without the filesystem.
type fakeRoles map[string]Role

func (f fakeRoles) Role(id string) (Role, error) {
	r, ok := f[id]
	if !ok {
		return Role{}, fmt.Errorf("no such role %s in roles/", id)
	}
	return r, nil
}

// newTestSpawner builds a spawner over fakeRoles with the required options
// set.
func newTestSpawner(t *testing.T, roles RoleSource, opts ...SpawnOption) *Spawner {
	t.Helper()
	all := append([]SpawnOption{
		WithModel("gpt-5"),
		WithConfigDir(t.TempDir()),
		WithRoleSource(roles),
	}, opts...)
	s, err := NewSpawner(all...)
	if err != nil {
		t.Fatalf("NewSpawner: %v", err)
	}
	return s
}

// TestNewSpawner_Errors pins the required options: a spawner without a role
// source, model or config dir is refused before anything runs.
func TestNewSpawner_Errors(t *testing.T) {
	cases := []struct {
		name string
		opts []SpawnOption
		want string
	}{
		{"no role source", []SpawnOption{WithModel("m"), WithConfigDir(t.TempDir())}, "role source can't be nil"},
		{"no model", []SpawnOption{WithConfigDir(t.TempDir()), WithRoleSource(fakeRoles{})}, "model can't be empty"},
		{"no config dir", []SpawnOption{WithModel("m"), WithRoleSource(fakeRoles{})}, "config dir can't be empty"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSpawner(tt.opts...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestSpawner_ToolSet pins the core contract: a spawned role gets exactly
// its selected tools — the file and notebook tools as exact clai globs, the
// recursive spawn_role as a dynamic tool — and nothing else.
func TestSpawner_ToolSet(t *testing.T) {
	cases := []struct {
		name     string
		tools    []string
		notebook bool
		wantGlob []string
		wantDyn  []string
		wantErr  string
	}{
		{
			name:     "file tools only",
			tools:    []string{"cat", "rg"},
			wantGlob: []string{"cat", "rg"},
		},
		{
			name:    "recursive only",
			tools:   []string{"spawn_role"},
			wantDyn: []string{"spawn_role"},
		},
		{
			name:     "mixed",
			tools:    []string{"cat", "spawn_role", "write_file"},
			wantGlob: []string{"cat", "write_file"},
			wantDyn:  []string{"spawn_role"},
		},
		{
			name:     "notebook tools with notebook",
			tools:    []string{"mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit"},
			notebook: true,
			wantGlob: []string{"mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit"},
		},
		{
			name:    "notebook tool without notebook",
			tools:   []string{"mcp_slivingdoc_notes_pull"},
			wantErr: "selects mcp_slivingdoc_notes_pull but the notebook is disabled",
		},
		{
			name:    "unregistered tool",
			tools:   []string{"website_text"},
			wantErr: `selects "website_text" which is not in the closed tool registry`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var s *Spawner
			if tt.notebook {
				server := slivingdoc.Server(slivingdoc.NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")
				s = newTestSpawner(t, fakeRoles{}, WithNotebook(server, "/cache/slivingdoc"))
			} else {
				s = newTestSpawner(t, fakeRoles{})
			}
			globs, dyn, err := s.toolSet(Role{ID: "clown", Tools: tt.tools})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("toolSet: %v", err)
			}
			if !slicesEqual(globs, tt.wantGlob) {
				t.Errorf("globs = %v, want %v", globs, tt.wantGlob)
			}
			gotDyn := make([]string, len(dyn))
			for i, tool := range dyn {
				gotDyn[i] = tool.Specification().Name
			}
			if !slicesEqual(gotDyn, tt.wantDyn) {
				t.Errorf("dynamic tools = %v, want %v", gotDyn, tt.wantDyn)
			}
		})
	}
}

// TestSpawner_Prompt pins the prompt assembly: the role's own definition,
// the commission appended when given, and the shared NOTES partial naming
// the workspace when the notebook is on.
func TestSpawner_Prompt(t *testing.T) {
	const rolePrompt = "You are the clown."
	const task = "Invent a pounce gag."

	t.Run("no notebook no task", func(t *testing.T) {
		s := newTestSpawner(t, fakeRoles{})
		if got := s.prompt(Role{Prompt: rolePrompt}, ""); got != rolePrompt {
			t.Errorf("prompt = %q, want the bare role prompt", got)
		}
	})
	t.Run("task appended", func(t *testing.T) {
		s := newTestSpawner(t, fakeRoles{})
		got := s.prompt(Role{Prompt: rolePrompt}, task)
		if !strings.Contains(got, rolePrompt) || !strings.Contains(got, "\n\nTASK\n"+task) {
			t.Errorf("prompt must carry the role prompt and the commission:\n%s", got)
		}
		if strings.Contains(got, "NOTES") {
			t.Errorf("no-notebook prompt must omit the NOTES partial:\n%s", got)
		}
	})
	t.Run("notebook names the workspace", func(t *testing.T) {
		server := slivingdoc.Server(slivingdoc.NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")
		s := newTestSpawner(t, fakeRoles{}, WithNotebook(server, "/cache/slivingdoc"))
		got := s.prompt(Role{Prompt: rolePrompt}, task)
		if !strings.Contains(got, "Pull the shared notebook into /cache/slivingdoc before you start.") {
			t.Errorf("prompt must carry the NOTES partial naming the workspace:\n%s", got)
		}
		if !strings.Contains(got, "Commit with mcp_slivingdoc_notes_commit with path /cache/slivingdoc when done.") {
			t.Errorf("prompt must name the commit step with the workspace:\n%s", got)
		}
	})
}

// TestSpawner_NotebookWorkspace pins the workspace resolution: the explicit
// option wins, the callsign args are the fallback.
func TestSpawner_NotebookWorkspace(t *testing.T) {
	server := slivingdoc.Server(slivingdoc.NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")

	s := newTestSpawner(t, fakeRoles{}, WithNotebook(server, "/explicit"))
	if got := s.notebookWorkspace(); got != "/explicit" {
		t.Errorf("notebookWorkspace = %q, want the explicit option", got)
	}

	s = newTestSpawner(t, fakeRoles{}, WithNotebook(server, ""))
	if got := s.notebookWorkspace(); got != "/cache/slivingdoc" {
		t.Errorf("notebookWorkspace = %q, want the path read back from the callsign args", got)
	}
}

// TestSpawnRoleTool_Call pins the tool's input validation: role and task are
// both required, non-empty strings. The full spawn needs an LLM; the
// validation surface is the exact contract the model sees.
func TestSpawnRoleTool_Call(t *testing.T) {
	s := newTestSpawner(t, fakeRoles{})
	tool := newSpawnRoleTool(s)

	if _, err := tool.Call(map[string]any{}); err == nil || !strings.Contains(err.Error(), "role must be a non-empty string") {
		t.Errorf("Call without role: err = %v, want a role error", err)
	}
	if _, err := tool.Call(map[string]any{"role": "clown"}); err == nil || !strings.Contains(err.Error(), "task must be a non-empty string") {
		t.Errorf("Call without task: err = %v, want a task error", err)
	}
	if _, err := tool.Call(map[string]any{"role": 7, "task": "x"}); err == nil || !strings.Contains(err.Error(), "role must be a non-empty string") {
		t.Errorf("Call with a non-string role: err = %v, want a role error", err)
	}
}

// TestSpawn_MissingRole pins the spawn error for an absent role note: the
// exact refusal flows back to the spawning agent.
func TestSpawn_MissingRole(t *testing.T) {
	s := newTestSpawner(t, fakeRoles{})
	_, err := s.Spawn(t.Context(), "ghost", "do a thing")
	if err == nil || !strings.Contains(err.Error(), "troupe: spawn ghost: no such role ghost in roles/") {
		t.Fatalf("err = %v, want a missing-role refusal", err)
	}
}

// TestSpawn_BudgetRefusals pins the phase-5 wiring: the termination
// authority hangs on the runner, and a refused spawn never reaches the role
// source. The exact guard error flows back to the spawning agent.
func TestSpawn_BudgetRefusals(t *testing.T) {
	roles := fakeRoles{"clown": {ID: "clown", Prompt: "You are the clown.", Tools: []string{"cat"}}}

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
				for i := range maxSpawnDepth {
					if _, err := b.Admit(); err != nil {
						t.Fatalf("seed admit %d: %v", i, err)
					}
				}
			},
			want: ErrDepthCap,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.seed(t, tt.budget)
			s := newTestSpawner(t, roles, WithBudget(tt.budget))
			_, err := s.Spawn(t.Context(), "clown", "do a thing")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestSpawn_ReleaseOnError pins that a failed spawn returns its admission:
// the depth and the reserved allowance are released even when the role is
// missing, so the termination authority never leaks a slot.
func TestSpawn_ReleaseOnError(t *testing.T) {
	b := NewBudget(WithStoploss(1000))
	s := newTestSpawner(t, fakeRoles{}, WithBudget(b))
	if _, err := s.Spawn(t.Context(), "ghost", "do a thing"); err == nil {
		t.Fatal("want a missing-role error")
	}
	if got := b.Stats(); got.Depth != 0 || got.Reserved != 0 {
		t.Fatalf("after failed spawn: %+v, want depth and reservation released", got)
	}
}

// TestSpawner_DefaultBudget pins that a spawner without WithBudget runs
// under a default budget: the hardcoded guards are on (the call max refuses
// a spawn), and the gate precedes the role read.
func TestSpawner_DefaultBudget(t *testing.T) {
	s := newTestSpawner(t, fakeRoles{"clown": {ID: "clown", Prompt: "p", Tools: []string{"cat"}}})
	if s.budget == nil {
		t.Fatal("spawner without WithBudget must carry a default budget")
	}
	for range maxGenerationCalls {
		if err := s.budget.Record(t.Context(), budgetedCall(1)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	_, err := s.Spawn(t.Context(), "clown", "do a thing")
	if !errors.Is(err, ErrCallMax) {
		t.Fatalf("err = %v, want ErrCallMax from the default budget", err)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

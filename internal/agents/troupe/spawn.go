package troupe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
)

// Spawner runs role notes as bounded clai agents: read the role, build an
// agent whose tool set is exactly the role's selection from the closed
// registry, and run it. The runner is recursive and uniform — the director's
// own spawns go through the same path as every other role, and spawn_role is
// just another registry tool. The termination authority (depth cap, global
// call max, token stoploss) hangs on this runner: every Spawn admits against
// the generation budget and every spawned agent accounts its model calls
// into it.
type Spawner struct {
	model     string
	configDir string
	server    models.McpServer // zero value: notebook disabled
	workspace string           // materialised worktree path
	roles     RoleSource
	budget    *Budget // the generation's termination authority
	logger    *slog.Logger
}

// SpawnOption configures one Spawner.
type SpawnOption func(*Spawner)

// WithModel sets the model every spawned agent runs on (the -troupeModel
// flag).
func WithModel(m string) SpawnOption {
	return func(s *Spawner) { s.model = m }
}

// WithConfigDir sets the clai config dir every spawned agent uses.
func WithConfigDir(dir string) SpawnOption {
	return func(s *Spawner) { s.configDir = dir }
}

// WithNotebook enables the shared slivingdoc notebook: the MCP callsign the
// spawned agents pull and commit through, and the shared worktree path the
// NOTES prompt partial names. A zero server (the default) disables the
// notebook: roles selecting mcp_slivingdoc_* tools are refused at spawn.
func WithNotebook(server models.McpServer, workspace string) SpawnOption {
	return func(s *Spawner) {
		s.server = server
		s.workspace = workspace
	}
}

// WithRoleSource sets the seam the spawner reads role notes through.
func WithRoleSource(rs RoleSource) SpawnOption {
	return func(s *Spawner) { s.roles = rs }
}

// WithBudget sets the generation budget the spawner enforces: the
// termination authority one generation runs under, shared by every spawn and
// every spawned agent's model-call accounting. When unset, the spawner
// builds a default — the hardcoded caps in force, the token stoploss off
// (phase 9's serve wiring passes the -troupeTokenStoploss value here).
func WithBudget(b *Budget) SpawnOption {
	return func(s *Spawner) { s.budget = b }
}

// WithLogger attaches a slog logger to every spawned agent, so concurrent
// spawns write to their own sinks instead of racing on stdout.
func WithLogger(l *slog.Logger) SpawnOption {
	return func(s *Spawner) { s.logger = l }
}

// NewSpawner builds a spawn runner. The role source, model and config dir
// are required; the notebook is optional and the budget defaults to the
// hardcoded guards with the stoploss off.
func NewSpawner(opts ...SpawnOption) (*Spawner, error) {
	s := &Spawner{}
	for _, o := range opts {
		o(s)
	}
	if s.budget == nil {
		s.budget = NewBudget()
	}
	switch {
	case s.roles == nil:
		return nil, errors.New("troupe: spawner: role source can't be nil")
	case s.model == "":
		return nil, errors.New("troupe: spawner: model can't be empty")
	case s.configDir == "":
		return nil, errors.New("troupe: spawner: config dir can't be empty")
	}
	return s, nil
}

// notebookEnabled reports whether the slivingdoc callsign is configured.
func (s *Spawner) notebookEnabled() bool {
	return s.server.Name != ""
}

// notebookWorkspace is the worktree path substituted into the NOTES prompt
// partial: the explicit option, or the path read back from the callsign
// args.
func (s *Spawner) notebookWorkspace() string {
	if s.workspace != "" {
		return s.workspace
	}
	return slivingdoc.WorkspaceRoot(s.server)
}

// Spawn runs one role note: read the role, validate its tool selection, build
// the bounded agent with exactly the selected tools and run it to
// completion. task is the commission the spawning agent assigned; it is
// appended to the role's own prompt. Spawn is the choke point of the
// termination authority: it admits against the generation budget before
// anything runs, and a refused spawn is never spawned.
func (s *Spawner) Spawn(ctx context.Context, roleID, task string) (string, error) {
	// Phase 5: the termination authority. Admit checks the depth cap, the
	// global call max and the token stoploss and reserves this spawn's token
	// allowance under one lock; release runs when the spawn finishes, even on
	// error, so the depth and the allowance always return.
	release, err := s.budget.Admit()
	if err != nil {
		return "", fmt.Errorf("troupe: spawn %s: %w", roleID, err)
	}
	defer release()

	role, err := s.roles.Role(roleID)
	if err != nil {
		return "", fmt.Errorf("troupe: spawn %s: %w", roleID, err)
	}
	globs, tools, err := s.toolSet(role)
	if err != nil {
		return "", fmt.Errorf("troupe: spawn %s: %w", roleID, err)
	}
	a := s.newAgent(role, task, globs, tools)
	if err := a.Setup(ctx); err != nil {
		return "", fmt.Errorf("troupe: spawn %s: %w", roleID, err)
	}
	out, err := a.Run(ctx)
	if err != nil {
		return "", fmt.Errorf("troupe: spawn %s: %w", roleID, err)
	}
	return out, nil
}

// toolSet materialises a role's selected tools: the exact clai globs for the
// built-in and MCP tools plus the dynamic tools (spawn_role). ParseRole
// already refused anything outside the registry, so the only refusal left is
// an MCP tool with the notebook disabled.
func (s *Spawner) toolSet(role Role) (globs []string, tools []models.LLMTool, err error) {
	globs = make([]string, 0, len(role.Tools))
	tools = make([]models.LLMTool, 0, len(role.Tools))
	for _, name := range role.Tools {
		e, ok := lookupTool(name)
		if !ok {
			return nil, nil, fmt.Errorf("troupe: role %s selects %q which is not in the closed tool registry", role.ID, name)
		}
		if e.glob != "" {
			if e.notebook && !s.notebookEnabled() {
				return nil, nil, fmt.Errorf("troupe: role %s selects %s but the notebook is disabled", role.ID, name)
			}
			globs = append(globs, e.glob)
			continue
		}
		tools = append(tools, e.build(s))
	}
	return globs, tools, nil
}

// newAgent assembles the bounded clai agent for one spawn: the role's prompt
// plus its commission and the shared NOTES partial when the notebook is on,
// the role's budget as the tool-call cap, and exactly the selected tools.
// The agent accounts every budgeted model call into the generation budget
// through the usage recorder, so the termination authority sees the real
// work of every spawn.
func (s *Spawner) newAgent(role Role, task string, globs []string, tools []models.LLMTool) agent.Agent {
	opts := []agent.Option{
		agent.WithModel(s.model),
		agent.WithConfigDir(s.configDir),
		agent.WithPrompt(s.prompt(role, task)),
		agent.WithMaxToolCalls(role.Budget),
		agent.WithToolGlobs(globs...),
		agent.WithTools(tools),
		agent.WithUsageRecorder(s.budget),
	}
	if s.notebookEnabled() {
		opts = append(opts, agent.WithMcpServers([]models.McpServer{s.server}))
	}
	if s.logger != nil {
		opts = append(opts, agent.WithLogger(s.logger))
	}
	return agent.New(opts...)
}

// prompt assembles the agent prompt: the role's own definition, the task the
// spawning agent commissioned, and the shared NOTES partial when the
// notebook is enabled.
func (s *Spawner) prompt(role Role, task string) string {
	p := role.Prompt
	if task != "" {
		p += "\n\nTASK\n" + task
	}
	if s.notebookEnabled() {
		p += "\n" + slivingdoc.NotesPartial(s.notebookWorkspace())
	}
	return p
}

// spawnRoleTool is the recursive spawn_role tool: it reads the named role
// note and runs it through the swarm, so a role that selects spawn_role may
// spawn further sub-agents through the same runner. The stage bounds the
// recursion: a spawn past the depth cap, the global call max or the token
// stoploss is refused and the exact refusal returns to the spawning agent.
type spawnRoleTool struct {
	swarm Swarm
}

// newSpawnRoleTool builds the spawn_role tool over a swarm — the Spawner in
// production, a fake in generation tests.
func newSpawnRoleTool(swarm Swarm) models.LLMTool {
	return &spawnRoleTool{swarm: swarm}
}

func (t *spawnRoleTool) Call(input models.Input) (string, error) {
	role, ok := input["role"].(string)
	if !ok || role == "" {
		return "", errors.New("spawn_role: role must be a non-empty string")
	}
	task, ok := input["task"].(string)
	if !ok || task == "" {
		return "", errors.New("spawn_role: task must be a non-empty string")
	}
	out, err := t.swarm.Spawn(context.Background(), role, task)
	if err != nil {
		return "", err
	}
	return out, nil
}

func (t *spawnRoleTool) Specification() models.Specification {
	return models.Specification{
		Name:        "spawn_role",
		Description: "Spawn a sub-agent executing a role note from the notebook (roles/<id>.json). The role note selects this sub-agent's tools from the closed registry and sets its own budget; the sub-agent runs with exactly those tools. Recursion is allowed: a role that selects spawn_role may spawn further sub-agents. The stage bounds the generation: a spawn past the depth cap, the global call max or the token stoploss is refused. Returns the sub-agent's final message.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"role": {
					Type:        "string",
					Description: "The role note id to spawn, e.g. clown for roles/clown.json",
				},
				"task": {
					Type:        "string",
					Description: "The commission: what this sub-agent is asked to do and what to return",
				},
			},
			Required: []string{"role", "task"},
		},
	}
}

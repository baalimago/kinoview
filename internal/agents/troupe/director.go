package troupe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
)

// directorToolCalls is the director's per-agent tool-call cap: the same
// hardcoded global maximum that bounds the generation's budgeted model
// calls. The director's loop is one agent of the generation — it can never
// out-last the stage's bound, and its spawns admit against the same budget,
// so the director spending its whole cap leaves nothing for the swarm (the
// director is sovereign; the stage bounds the generation regardless).
const directorToolCalls = maxGenerationCalls

// directorPrompt is the fixed director prompt: the production loop as
// guidance, short-imperative like every other role note (implementation
// note 3). It names the submit step; the tool descriptions carry the
// argument lists and the workspace layout. The current UTC time is stamped
// at generation time (prompt), because the play id is a story_<UTC> datetime
// the director must author and the closed registry carries no clock tool.
const directorPrompt = `You are the director of a tiny self-contained theatre on a media server. The
repertoire lives as notes in the shared notebook: roles/, models/, clips/,
voices/, sounds/, gags/ and plays/. You are the mastermind, not a worker:
you decide what to author, who authors it, and when the play is good enough
to submit.

WORKFLOW — run these phases in order:

1. PULL — pull the shared notebook and read the viewer feedback in feedback/
   (ls feedback/, cat the notes). Read what the critic left for the last
   generation.
2. DECOMPOSE — split the work into smaller concerns: which models, clips,
   voices, sounds and gags need authoring or revision, and what the play
   should be. Author a role note for each concern (write_file roles/<id>.json)
   or reuse an existing one.
3. SWARM — spawn the swarm: call spawn_role for each commissioned role. Every
   sub-agent runs with exactly the tools its role note selects and under the
   stage's budget. Sub-agents author and revise assets and leave notes; the
   play is never theirs to write.
4. ASSEMBLE — read what the swarm left with the file tools and assemble the
   play: write plays/story_<UTC>.json with model instantiations and a timed
   timeline, references intact ("model": "cat@1"), status "draft", author
   "director", provenance "generation <play id>".
5. SUBMIT — call submit_play with the play id. It validates that every
   reference resolves and persists the resolved play; exact errors return for
   you to fix and resubmit.

You are the single writer of plays/: the swarm writes everything except the
play, and you alone submit. Submit as soon as the play is good — do not burn
budget. There is no seed and no fallback: if you do not submit, nothing
renders, and an empty stage is the signal to investigate.`

// Director is the mastermind: a fixed, sovereign stage role that reads
// viewer feedback, decomposes the work into smaller concerns, spawns a swarm
// of concurrent sub-agents by role note, assembles the play and submits it
// through the submit_play gate. One generation is one Director.Run. The
// director is the single writer of plays/; the swarm writes everything else
// concurrently, and exhaustion without a submit ships nothing — there is no
// seed and no offline floor.
type Director struct {
	swarm     Swarm      // spawn sub-agents by role note + gather
	submitter *Submitter // the single-writer play gate
	budget    *Budget    // the generation's termination authority (phase 5)

	model     string
	configDir string
	server    models.McpServer // zero value: notebook disabled
	workspace string           // materialised worktree path
	logger    *slog.Logger

	// runDirector is the seam between the director and clai: production
	// builds the bounded clai agent (Setup + Run) exactly like every other
	// spawn; tests inject a scripted fake that drives the tool objects
	// directly, so a whole generation runs without a model configured.
	runDirector func(ctx context.Context, p directorParams) (string, error)

	// submitted records the outcome: set by the recording submit wrapper when
	// submit_play succeeds. The agent loop is single-threaded, so plain
	// fields are safe without a lock.
	submitted   bool
	submittedID string
}

// directorParams is everything one director agent loop needs: the fixed
// prompt (stamped with the current UTC), the registry tool globs and the
// tool objects (spawn_role over the swarm, submit_play over the submitter).
type directorParams struct {
	prompt string
	globs  []string
	tools  []models.LLMTool
}

// Outcome is the result of one generation: whether the director submitted a
// play, and the play id when it did. Exhaustion — the swarm produced nothing
// or the director never submitted — ships nothing: the stage stays empty and
// the served play stays whatever was last durably on disk.
type Outcome struct {
	Submitted bool
	PlayID    string
}

// DirectorOption configures one Director.
type DirectorOption func(*Director)

// WithSwarm sets the swarm the director spawns through: the Spawner in
// production (sharing this generation's budget), a fake in generation tests.
// The swarm must operate on the same worktree as the submitter.
func WithSwarm(s Swarm) DirectorOption {
	return func(d *Director) { d.swarm = s }
}

// WithSubmitter sets the single-writer play gate the director's submit_play
// tool persists through. It must operate on the same worktree the swarm
// writes into.
func WithSubmitter(s *Submitter) DirectorOption {
	return func(d *Director) { d.submitter = s }
}

// WithGenerationBudget sets the generation budget the director runs under: the
// director admits at depth 0, and the agent accounts every budgeted model
// call into the same budget, so the generation is bounded regardless of
// swarm size (phase 5). When unset, the director builds a default — the
// hardcoded guards in force, the token stoploss off (phase 9's serve wiring
// passes the -troupeTokenStoploss value here).
func WithGenerationBudget(b *Budget) DirectorOption {
	return func(d *Director) { d.budget = b }
}

// WithDirectorModel sets the director's clai model (the -troupeModel flag).
func WithDirectorModel(m string) DirectorOption {
	return func(d *Director) { d.model = m }
}

// WithDirectorConfigDir sets the clai config dir the director's agent uses.
func WithDirectorConfigDir(dir string) DirectorOption {
	return func(d *Director) { d.configDir = dir }
}

// WithDirectorNotebook enables the shared slivingdoc notebook: the MCP callsign the
// director pulls and commits through, and the shared worktree path the NOTES
// prompt partial names. A zero server (the default) disables the notebook:
// the director's tool set drops the mcp tools and the prompt omits the NOTES
// partial.
func WithDirectorNotebook(server models.McpServer, workspace string) DirectorOption {
	return func(d *Director) {
		d.server = server
		d.workspace = workspace
	}
}

// WithDirectorLogger attaches a slog logger to the director's agent, so the
// generation's model steps land in a log sink instead of racing on stdout.
func WithDirectorLogger(l *slog.Logger) DirectorOption {
	return func(d *Director) { d.logger = l }
}

// WithRunDirector overrides the director's agent seam (tests): the scripted
// fake receives the assembled prompt, globs and tools and drives them
// directly, so a whole generation runs without an LLM. Production leaves it
// unset and the director builds and runs the bounded clai agent.
func WithRunDirector(fn func(ctx context.Context, p directorParams) (string, error)) DirectorOption {
	return func(d *Director) { d.runDirector = fn }
}

// NewDirector builds the fixed director role. The swarm, the submitter, the
// model and the config dir are required; the notebook is optional and the
// budget defaults to the hardcoded guards with the stoploss off.
func NewDirector(opts ...DirectorOption) (*Director, error) {
	d := &Director{}
	for _, o := range opts {
		o(d)
	}
	switch {
	case d.swarm == nil:
		return nil, errors.New("troupe: director: swarm can't be nil")
	case d.submitter == nil:
		return nil, errors.New("troupe: director: submitter can't be nil")
	case d.model == "":
		return nil, errors.New("troupe: director: model can't be empty")
	case d.configDir == "":
		return nil, errors.New("troupe: director: config dir can't be empty")
	}
	if d.budget == nil {
		d.budget = NewBudget()
	}
	if d.runDirector == nil {
		d.runDirector = d.runAgent
	}
	return d, nil
}

// ResetBudget returns the generation budget to a fresh state for a new
// generation. The facade calls it at the top of Prepare before Run, so the
// termination authority bounds one generation — never the accumulated
// history of many. The director and the spawner share the one budget
// pointer, so a single reset covers the whole generation.
func (d *Director) ResetBudget() {
	d.budget.Reset()
}

// Run executes one generation: the director's bounded agent loop from
// feedback-read through decompose → swarm → assemble → submit, or
// exhaustion. Run admits against the generation budget first — the
// director's own run is depth 0 — so a generation is refused outright once
// the stage's bounds are spent; the agent then accounts every budgeted model
// call into the same budget. The outcome reports whether a play was
// submitted; exhaustion ships nothing.
func (d *Director) Run(ctx context.Context) (Outcome, error) {
	release, err := d.budget.Admit()
	if err != nil {
		return Outcome{}, fmt.Errorf("troupe: generation: %w", err)
	}
	defer release()

	d.submitted, d.submittedID = false, ""
	globs, tools := d.toolSet()
	_, err = d.runDirector(ctx, directorParams{
		prompt: d.prompt(),
		globs:  globs,
		tools:  tools,
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("troupe: generation: %w", err)
	}
	if !d.submitted {
		return Outcome{}, nil
	}
	return Outcome{Submitted: true, PlayID: d.submittedID}, nil
}

// toolSet is the director's instrument set: the closed registry plus
// submit_play. The registry's file tools become exact clai globs; the
// notebook tools are included only when the notebook is enabled (the
// director is fixed — a disabled notebook drops them silently, unlike a role
// note, which is refused at spawn); spawn_role is bound to the swarm; and
// submit_play is the director-only recording wrapper over the submitter.
func (d *Director) toolSet() (globs []string, tools []models.LLMTool) {
	for _, e := range toolRegistry {
		if e.glob != "" {
			if e.notebook && !d.notebookEnabled() {
				continue
			}
			globs = append(globs, e.glob)
			continue
		}
		tools = append(tools, e.build(d.swarm))
	}
	tools = append(tools, d.recordingSubmitTool())
	return globs, tools
}

// prompt assembles the director's system prompt: the fixed workflow, the
// current UTC time in the play-id format (the director authors the play
// under plays/story_<UTC>.json, and the closed registry carries no clock
// tool), and the shared NOTES partial when the notebook is on.
func (d *Director) prompt() string {
	now := time.Now().UTC().Format("20060102T150405Z")
	p := directorPrompt + "\n\nNOW\n" + now + " UTC. Author the play under plays/story_" + now + ".json or a later story_YYYYMMDDTHHMMSSZ id — submit_play refuses an id already on disk.\n"
	if d.notebookEnabled() {
		p += "\n" + slivingdoc.NotesPartial(d.notebookWorkspace())
	}
	return p
}

// notebookEnabled reports whether the slivingdoc callsign is configured.
func (d *Director) notebookEnabled() bool {
	return d.server.Name != ""
}

// notebookWorkspace is the worktree path substituted into the NOTES prompt
// partial: the explicit option, or the path read back from the callsign
// args.
func (d *Director) notebookWorkspace() string {
	if d.workspace != "" {
		return d.workspace
	}
	return slivingdoc.WorkspaceRoot(d.server)
}

// runAgent is the production director seam: the bounded clai agent with the
// fixed prompt, the director's tool set, the generation budget as the usage
// recorder and the notebook callsign when enabled — exactly the shape every
// spawned agent gets, so the director's own loop is one bounded agent of the
// generation.
func (d *Director) runAgent(ctx context.Context, p directorParams) (string, error) {
	opts := []agent.Option{
		agent.WithModel(d.model),
		agent.WithConfigDir(d.configDir),
		agent.WithPrompt(p.prompt),
		agent.WithTools(p.tools),
		agent.WithMaxToolCalls(directorToolCalls),
		agent.WithUsageRecorder(d.budget),
	}
	if len(p.globs) > 0 {
		opts = append(opts, agent.WithToolGlobs(p.globs...))
	}
	if d.notebookEnabled() {
		opts = append(opts, agent.WithMcpServers([]models.McpServer{d.server}))
	}
	if d.logger != nil {
		opts = append(opts, agent.WithLogger(d.logger))
	}
	a := agent.New(opts...)
	if err := a.Setup(ctx); err != nil {
		return "", fmt.Errorf("troupe: director setup: %w", err)
	}
	out, err := a.Run(ctx)
	if err != nil {
		return "", fmt.Errorf("troupe: director run: %w", err)
	}
	return out, nil
}

// recordingSubmitTool wraps submit_play so the generation knows whether the
// director submitted: the inner tool validates and persists, and the wrapper
// records the submitted play id on success. The agent loop is
// single-threaded, so the director's fields are safe without a lock.
type recordingSubmitTool struct {
	inner models.LLMTool
	d     *Director
}

// recordingSubmitTool builds the director's submit_play: the submitter's
// gate wrapped in the outcome recorder.
func (d *Director) recordingSubmitTool() models.LLMTool {
	return &recordingSubmitTool{inner: d.submitter.newSubmitPlayTool(), d: d}
}

func (t *recordingSubmitTool) Call(input models.Input) (string, error) {
	out, err := t.inner.Call(input)
	if err != nil {
		return "", err
	}
	if id, ok := input["play"].(string); ok && id != "" {
		t.d.submitted = true
		t.d.submittedID = id
	}
	return out, nil
}

func (t *recordingSubmitTool) Specification() models.Specification {
	return t.inner.Specification()
}

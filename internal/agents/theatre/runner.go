package theatre

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// Result is the outcome of one role invocation: the final deliverable text
// and how it was produced.
type Result struct {
	Text     string
	Fallback bool  // true when the role's deterministic fallback answered
	Err      error // the LLM failure, when the fallback answered
}

// Invocation describes one role invocation: the role, its task, its call
// budget and the hop depth it runs at (0 for the director's spawns; every
// consultation adds one hop, capped at ConsultHopCap).
type Invocation struct {
	Role   string
	Task   string
	Budget int
	Depth  int
}

// Runner runs one role invocation as a bounded mini clai agent: the
// working-context standard assembled into the prompt, the role's tool set, a
// call budget, a per-role session log, ledger telemetry and SSE streaming.
// It resolves the collaborations the deliverable requests (decision D4) and
// answers with the role's deterministic fallback when the LLM fails — the
// composer floor stands below everything (decision D11).
type Runner struct {
	company *Company
	stage   *Stage
	broker  *Broker

	model     string
	configDir string
	cacheDir  string

	// registry is the costumer's book: lineups are validated against it, the
	// dramaturg's fallback draws its cast from it and the wardrobe's fallback
	// answers from it. Nil in unit fixtures without a registry; the fallbacks
	// degrade gracefully.
	registry *Registry

	// rnd seeds the deterministic fallbacks (the composer path). Tests inject
	// a fixed source to make the floor reproducible.
	rnd *rand.Rand

	// fallback is the injected deterministic seam (WithFallback, tests); nil
	// routes through the internal per-role dispatcher (fallbackFor).
	fallback func(role, task string) (string, error)

	// directorTools builds the director's tool set for one invocation. The
	// facade wires it after construction (the tools close over the
	// production); without it a director invocation gets the shared tools
	// only — defensive, never wired that way in production.
	directorTools func(ctx context.Context) []models.LLMTool

	mu     sync.Mutex
	logSeq int

	// runLLM is the seam between the runner and clai: production builds the
	// clai agent exactly like the concierge (agent.New with WithModel,
	// WithPrompt, WithTools and WithMaxToolCalls); tests inject a scripted
	// fake, so the machinery runs without a model configured.
	runLLM func(ctx context.Context, p llmParams) (llmOutcome, error)
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// llmParams is everything one bounded agent loop needs. The tools arrive
// already wrapped in the call counter; onCall accounts every tool execution
// into the ledger.
type llmParams struct {
	prompt string
	tools  []models.LLMTool
	budget int
	out    io.Writer
	onCall func(toolName string)
}

// llmOutcome is what a bounded agent loop returns: the final text and the
// token usage, when the querier reports it.
type llmOutcome struct {
	text   string
	tokens int
}

// WithModel sets the clai model for the mini-agents.
func WithModel(m string) RunnerOption {
	return func(r *Runner) { r.model = m }
}

// WithConfigDir sets the clai config dir (credentials, conversations).
func WithConfigDir(d string) RunnerOption {
	return func(r *Runner) { r.configDir = d }
}

// WithCacheDir sets the cache dir; the per-role session logs live under
// <cacheDir>/intro/company/logs/.
func WithCacheDir(d string) RunnerOption {
	return func(r *Runner) { r.cacheDir = d }
}

// WithRegistry gives the runner the costumer's book (decision D7): lineups
// are validated against it, the dramaturg's fallback draws its cast from it
// and the wardrobe's fallback answers from it.
func WithRegistry(reg *Registry) RunnerOption {
	return func(r *Runner) { r.registry = reg }
}

// WithFallback overrides the deterministic floor under every role (decision
// D11). Without it the runner routes through the internal per-role
// dispatcher (fallback.go) — the composer scenes, the registry answers and
// the working-file side effects.
func WithFallback(fn func(role, task string) (string, error)) RunnerOption {
	return func(r *Runner) { r.fallback = fn }
}

// NewRunner builds the mini-agent runner. The consultation broker is wired
// in afterwards (WireBroker), because the broker and the runner reference
// each other: the runner resolves collaborations through the broker, the
// broker spawns consultations through the runner.
func NewRunner(company *Company, stage *Stage, opts ...RunnerOption) *Runner {
	r := &Runner{
		company: company,
		stage:   stage,
		rnd:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	r.runLLM = r.runClai
	for _, o := range opts {
		o(r)
	}
	return r
}

// WireBroker attaches the consultation broker.
func (r *Runner) WireBroker(b *Broker) { r.broker = b }

// Run executes one role invocation and returns its deliverable. It never
// panics: a panicking agent loop is recovered and reported as an error, so
// the production continues.
func (r *Runner) Run(ctx context.Context, inv Invocation) (res Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			ancli.Errf("theatre: runner recovered from panic in %s: %v", inv.Role, rec)
			err = fmt.Errorf("runner recovered from panic in %s: %v", inv.Role, rec)
			r.stage.RecordFailure(inv.Role, err.Error())
		}
	}()

	role := strings.ToLower(strings.TrimSpace(inv.Role))
	if !ValidRoles[role] {
		return Result{}, fmt.Errorf("unknown role %q", role)
	}
	if inv.Budget <= 0 {
		inv.Budget = DefaultSubagentBudget
	}

	// Budget and deadline gates: a generation past its cap or deadline
	// refuses new work instead of burning calls.
	used, max, deadline := r.stage.BudgetSnapshot()
	if used >= max {
		msg := fmt.Sprintf("refused %s: global call budget exhausted (%d/%d)", role, used, max)
		r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: msg, Level: "warning"})
		return Result{}, fmt.Errorf("%s", msg)
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		msg := fmt.Sprintf("refused %s: wall-clock deadline exceeded", role)
		r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: msg, Level: "warning"})
		return Result{}, fmt.Errorf("%s", msg)
	}

	// The working-context standard (phase 1): generation, theme, board
	// excerpt, working summary, role prompt and task. A board read failure
	// degrades to the empty board — the subagent gets an empty excerpt and
	// the generation continues.
	board, err := r.company.LoadBoard()
	if err != nil {
		board = Board{}
	}
	working, err := r.company.LoadWorking()
	if err != nil {
		working = Working{}
	}
	prompt := AssembleContext(r.stage.gen, board.Theme, board, working.Summary(), RolePrompt(role), inv.Task)
	prompt = r.withRegistryContext(prompt)
	prompt = r.withDocsContext(prompt, role)

	out, closeLog := r.sessionLog(role)
	defer closeLog()
	fmt.Fprintf(out, "=== theatre session: %s (%s) ===\n\n%s\n\n", r.stage.gen, role, prompt)

	r.stage.Log(model.INFO, role, fmt.Sprintf("invocation started (budget %d)", inv.Budget))
	r.stage.SetActorBudget(role, inv.Budget)

	tools := r.roleTools(ctx, role, inv.Depth)

	res.Text, res.Fallback, res.Err = r.runOnce(ctx, role, inv.Task, prompt, tools, inv.Budget, out, inv.Depth)
	if res.Err != nil && !res.Fallback {
		// Both the LLM and the fallback failed; nothing to deliver.
		return Result{}, res.Err
	}

	// Collaborations (D4): subagent deliverables may request consultations;
	// the wrapper resolves them before the deliverable is final. The
	// director's own consultations flow through its tools, never through a
	// deliverable, so its final statement is not re-resolved.
	if role != "director" {
		res.Text, err = r.resolveCollaborations(ctx, inv, prompt, tools, out, res)
		if err != nil {
			return Result{}, err
		}
	}

	// The playwright floor (error table): a loop that ends without a
	// playable draft in the working file is answered by the composer draft,
	// so the director always has a working file to build on. A consulted
	// playwright never drafts — its answers flow through the fallback
	// dispatcher at its own depth.
	if role == "playwright" && inv.Depth == 0 {
		if _, werr := r.company.LoadWorking(); werr != nil {
			text, ferr := r.fallbackDraft()
			if ferr != nil {
				return Result{}, fmt.Errorf("playwright delivered no playable draft and the fallback failed: %w", ferr)
			}
			res.Text = text
			res.Fallback = true
			msg := "playwright delivered no playable draft — composer draft offered"
			r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: msg, Level: "warning"})
			r.stage.Log(model.WARNING, "playwright", msg)
		}
	}

	r.stage.Emit(TranscriptEvent{Kind: "deliver", From: role, To: artifactName(role), Body: res.Text})
	r.stage.Log(model.INFO, role, fmt.Sprintf("delivered %s: %s", artifactName(role), truncateRunes(res.Text, 80)))
	return res, nil
}

// runOnce runs one bounded agent loop and answers with the role's
// deterministic fallback when the LLM fails (decision D11). The fallback
// activation is noted on the transcript and streamed; the failure rides back
// on Result.Err so the caller can report it.
func (r *Runner) runOnce(ctx context.Context, role, task, prompt string, tools []models.LLMTool, budget int, out io.Writer, depth int) (string, bool, error) {
	onCall := func(name string) { r.stage.RecordCall(role, name) }
	// Tools are wrapped before the loop, so every tool execution is accounted
	// into the ledger whichever LLM path runs — the stub seam counts exactly
	// like the clai agent.
	wrapped := make([]models.LLMTool, len(tools))
	for i, t := range tools {
		wrapped[i] = countingTool{t, onCall}
	}
	outcome, err := r.runLLM(ctx, llmParams{prompt: prompt, tools: wrapped, budget: budget, out: out, onCall: onCall})
	text, tokens := outcome.text, outcome.tokens
	if tokens > 0 {
		r.stage.RecordTokens(role, tokens)
	}
	if err != nil {
		r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: fmt.Sprintf("fallback: %s llm failed: %v", role, err), Level: "warning"})
		r.stage.Log(model.WARNING, role, fmt.Sprintf("llm failed: %v — using fallback", err))
		// The failure is noted in the ledger (error table): the invocation
		// failed its LLM path even though the floor answered.
		r.stage.RecordFailure(role, fmt.Sprintf("llm failed: %v — fallback used", err))
		text, ferr := r.fallbackFor(role, task, depth)
		if ferr != nil {
			return "", false, fmt.Errorf("%s llm failed: %w (fallback: %v)", role, err, ferr)
		}
		return text, true, err
	}
	// Every tool execution was accounted by the wrapper; the final answer is
	// the loop's terminal roundtrip, not a budgeted tool call — it must never
	// push an actor or the generation budgets over their caps (review 3,
	// R3-03).
	r.stage.RecordAnswer(role)
	return text, false, nil
}

// runClai builds the clai agent exactly like the concierge and runs one
// bounded loop: WithModel, WithPrompt, WithTools (already wrapped in the call
// counter by runOnce), WithMaxToolCalls(budget) and, when a session log
// exists, WithOutputTo. Tokens come from the returned chat, so the ledger
// records real usage.
func (r *Runner) runClai(ctx context.Context, p llmParams) (llmOutcome, error) {
	opts := []agent.Option{
		agent.WithModel(r.model),
		agent.WithConfigDir(r.configDir),
		agent.WithPrompt(p.prompt),
		agent.WithTools(p.tools),
		agent.WithMaxToolCalls(p.budget),
	}
	if p.out != nil {
		opts = append(opts, agent.WithOutputTo(p.out))
	}
	a := agent.New(opts...)
	if err := a.Setup(ctx); err != nil {
		return llmOutcome{}, fmt.Errorf("setup agent: %w", err)
	}
	chat, err := a.Query(ctx, models.Chat{
		Created:  time.Now(),
		ID:       fmt.Sprintf("theatre_%s_%d", r.stage.gen, time.Now().UnixNano()),
		Messages: []models.Message{{Role: "user", Content: p.prompt}},
	})
	if err != nil {
		return llmOutcome{}, fmt.Errorf("agent query: %w", err)
	}
	out := llmOutcome{}
	if chat.TokenUsage != nil {
		out.tokens = chat.TokenUsage.TotalTokens
	}
	msg, _, err := chat.LastOfRole("assistant")
	if err != nil {
		return out, fmt.Errorf("no assistant reply: %w", err)
	}
	out.text = msg.String()
	return out, nil
}

// countingTool decorates an LLMTool so every execution is accounted into the
// ledger — one LLM call per tool execution.
type countingTool struct {
	models.LLMTool
	onCall func(toolName string)
}

func (t countingTool) Call(input models.Input) (string, error) {
	t.onCall(t.Specification().Name)
	return t.LLMTool.Call(input)
}

// sessionLog opens the role's per-role session log file (the existing
// SetOutput pattern: agent output goes to a file, never stdout). Files live
// under <cacheDir>/intro/company/logs/<gen>-<role>-<n>.txt; without a cache
// dir the session is discarded, so tests and composer-only runs stay quiet.
func (r *Runner) sessionLog(role string) (io.Writer, func()) {
	if r.cacheDir == "" {
		return io.Discard, func() {}
	}
	r.mu.Lock()
	r.logSeq++
	n := r.logSeq
	r.mu.Unlock()
	dir := filepath.Join(r.cacheDir, CompanyDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		ancli.Errf("theatre: session log dir: %v", err)
		return io.Discard, func() {}
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%d.txt", r.stage.gen, role, n))
	f, err := os.Create(path)
	if err != nil {
		ancli.Errf("theatre: session log: %v", err)
		return io.Discard, func() {}
	}
	return f, func() { f.Close() }
}

// withRegistryContext appends the costumer's book to the working context, so
// every role reads the canonical looks it must not contradict. A missing or
// empty registry adds nothing.
func (r *Runner) withRegistryContext(prompt string) string {
	if r.registry == nil || r.registry.Size() == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString("\nCharacter registry (canonical looks):\n")
	for _, id := range r.registry.IDs() {
		look, _ := r.registry.Lookup(id)
		coat := look.Coat
		if coat == "" {
			coat = "unpinned"
		}
		fmt.Fprintf(&b, "  %s: %s (%s)", id, look.Character, coat)
		if variants := r.registry.Variants(id); len(variants) > 0 {
			fmt.Fprintf(&b, " — wardrobe: %s", strings.Join(variants, ", "))
		}
		b.WriteString("\n")
	}
	return prompt + b.String()
}

// withDocsContext appends the company's durable memory to the working
// context (phase 6): the bulletin reaches every role, and each role reads
// its own document — the premises no-repeat list for the dramaturg, the
// repertoire and canon facts for the playwright, the set recipes for the
// scenographer, the directing lessons for the director. A missing or empty
// library adds nothing.
func (r *Runner) withDocsContext(prompt, role string) string {
	lib := r.company.LoadLibrary()
	var b strings.Builder
	if s := lib.Bulletin.context(); s != "" {
		b.WriteString(s)
	}
	switch role {
	case "dramaturg":
		b.WriteString(lib.Premises.context())
	case "playwright":
		b.WriteString(lib.Repertoire.context())
	case "scenographer":
		b.WriteString(lib.Sets.context())
	case "director":
		b.WriteString(lib.Director.context())
	}
	if b.Len() == 0 {
		return prompt
	}
	return prompt + b.String()
}

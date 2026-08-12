package theatre

import (
	"context"
	"encoding/json"
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
// into the ledger. rf is the structured-output contract for the final
// answer: the playwright's story schema at production depth (nil for every
// other role — their final answers are free text).
type llmParams struct {
	prompt string
	tools  []models.LLMTool
	budget int
	out    io.Writer
	onCall func(toolName string)
	rf     *models.ResponseFormat
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

	// The playwright's deliverable is the structured story (machine fix,
	// 2026-08-03): its final answer is the complete story JSON (json_object
	// response format — the API forces a JSON object, the field rules in the
	// prompt plus the writeDraft gate enforce the shape), and the runner
	// persists it into the working file with the same gates as write_draft.
	// A shape slip gets one bounded revision round with the exact validation
	// error; the composer floor answers when that also fails.
	if role == "playwright" && inv.Depth == 0 {
		res.Text = r.deliverDraft(ctx, inv, prompt, tools, out, res.Text, res.Fallback)
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
	params := llmParams{prompt: prompt, tools: wrapped, budget: budget, out: out, onCall: onCall}
	// The production playwright's final answer is the story: the json_object
	// response format forces a JSON object, so the playwright cannot bury
	// the story in prose or code fences. A consulted playwright answers in
	// place — free text.
	if role == "playwright" && depth == 0 {
		params.rf = playwrightResponseFormat()
	}
	outcome, err := r.runLLM(ctx, params)
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

// deliverDraft persists the playwright's structured story deliverable: the
// final answer is the complete story JSON (json_object response format, see
// playwrightResponseFormat), and the runner writes it into the working file
// with the same gates as write_draft. On a shape slip — json_object only
// guarantees a JSON object, so the shape is enforced here — one bounded
// revision round re-runs the playwright with the exact validation error, so
// the story self-corrects in a single roundtrip instead of the 24-call
// guessing loop the 09:16 production burned. The deterministic floor has
// already persisted the composer draft when fallback is set, so its report
// is never treated as a story. The returned text is the compact report the
// director and the transcript see; the working file is the authoritative
// artifact (D3).
func (r *Runner) deliverDraft(ctx context.Context, inv Invocation, prompt string, tools []models.LLMTool, out io.Writer, text string, fallback bool) string {
	if fallback {
		// The playwright's deterministic floor already saved the composer
		// draft; the final text is its draft report, not a story.
		return text
	}
	if _, err := r.writeDraft(text, ""); err == nil {
		return r.draftReport(text)
	} else {
		// The writeDraft gate's message carries the schema hint; the revision
		// round feeds it back so the playwright self-corrects.
		feedback := "\n\nYour story was rejected: " + err.Error() +
			"\nReturn the corrected complete story JSON as your final answer."
		revised, revFallback, llmErr := r.runOnce(ctx, inv.Role,
			inv.Task+feedback, prompt+feedback, tools, inv.Budget, out, inv.Depth)
		if llmErr != nil && !revFallback {
			r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: fmt.Sprintf("playwright revision failed: %v", llmErr), Level: "warning"})
			return text
		}
		if _, werr := r.writeDraft(revised, ""); werr == nil {
			return r.draftReport(revised)
		}
	}
	// Neither the original nor the revision was a playable story; the floor
	// check in Run answers with the composer draft.
	return text
}

// draftReport renders the compact report of a story the playwright just
// delivered — the text the director and the transcript see instead of the
// full story JSON.
func (r *Runner) draftReport(text string) string {
	var s model.Story
	if err := json.Unmarshal([]byte(extractJSON(text)), &s); err != nil {
		return text
	}
	return fmt.Sprintf("draft written: %q — %d beats, %d cast, %d props", s.Title, len(s.Beats), len(s.Cast), len(s.Props))
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
	// Structured output (clai's WithResponseFormat, json_object): the
	// playwright's final answer must be a single JSON object — the API
	// refuses prose around the story, and the story shape is enforced by the
	// prompt's field rules, the writeDraft gate and the revision round. This
	// is the machine fix for the schema-guessing loop that wasted the 09:16
	// production.
	if p.rf != nil {
		opts = append(opts, agent.WithResponseFormat(*p.rf))
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
// its own document — the premises no-repeat list and the audience feedback
// for the dramaturg, the repertoire and canon facts for the playwright, the
// set recipes for the scenographer, the directing lessons and the audience
// feedback for the director. A missing or empty library adds nothing.
func (r *Runner) withDocsContext(prompt, role string) string {
	lib := r.company.LoadLibrary()
	var b strings.Builder
	if s := lib.Bulletin.context(); s != "" {
		b.WriteString(s)
	}
	switch role {
	case "dramaturg":
		b.WriteString(lib.Premises.context())
		b.WriteString(lib.Audience.context())
	case "playwright":
		b.WriteString(lib.Repertoire.context())
	case "scenographer":
		b.WriteString(lib.Sets.context())
	case "director":
		b.WriteString(lib.Director.context())
		// The repertoire's earlier-production list reaches the director too:
		// the company's head must see what has already been staged, or it
		// cannot know it is about to repeat it (the cold-case loop). The
		// canon facts stay with the playwright — the director does not riff
		// on continuity, it decides what is new.
		b.WriteString(lib.Repertoire.summariesContext())
		b.WriteString(lib.Audience.context())
	}
	if b.Len() == 0 {
		return prompt
	}
	return prompt + b.String()
}

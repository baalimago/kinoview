# Phase 3 — Subagent Infrastructure

**Status:** Not Started | [README](./README.md)

## Goal

Build the machinery that runs subagents as bounded mini clai agents: the mini-agent
runner, the per-role tool sets, and the consultation broker with cycle guards.

## Specification

**Mini-agent runner** — `internal/agents/theatre/runner.go`:

```go
type Runner struct { model models.Configurations; board Board; ledger *Ledger; ... }
func (r *Runner) Run(ctx, role, task string, tools []models.LLMTool, budget int) (Result, error)
```

- Builds the clai agent exactly like the concierge (`agent.New` with `WithModel`,
  `WithPrompt`, `WithTools`, `WithMaxToolCalls(budget)`).
- Assembles the prompt from the working-context standard (phase 1): generation +
  theme + board excerpt + working summary + the role's system prompt (phase 5) + task.
- Writes the session to the per-role log file via `SetOutput` (existing pattern) and
  streams SSE entries tagged `theatre.<role>` with the generation id as `corrID`.
- Counts calls, tokens and wall time into the ledger (phase 2).
- Returns the agent's final text; the wrapper parses the role's artifact from it.

**Per-role tool sets** — each role gets the shared tools plus its deliverable writer:

| Role | Shared tools | Own tools |
|---|---|---|
| dramaturg | `post_to_board`, `read_board`, `consult` | `write_brief` |
| playwright | `post_to_board`, `read_board`, `consult` | `write_draft` (writes `working.json`), `append_canon` |
| scenographer | `post_to_board`, `read_board`, `consult` | `write_scene` (writes scene into `working.json`) |
| wardrobe consultant | `post_to_board`, `read_board` | `advise` (answers in-text; no own writer) |

`post_to_board(author, kind, to, body)` appends a validated entry. `read_board()`
returns the current excerpt. `consult(role, question)` spawns the consulted role via
the broker (below) and returns its answer; the wrapper also posts question + answer
to the board. Tool specs live in `internal/agents/theatre/tools/` and follow the
`internal/agents/tools` contract (`Call(input models.Input) (string, error)` +
`Specification()`).

**Consultation broker** — the cycle guard layer:

- **Hop cap 2**: every consult carries a `depth`; a consult at depth 2 returns
  "consultation depth exceeded" instead of spawning.
- **Consultation table**: `{questioner, role, questionHash}` — a repeat consult
  returns the previous answer from the board instead of a fresh spawn.
- **Budget ledger**: each spawn charges the global per-generation call cap (~200) and
  the wall-clock deadline; a spawn past either is refused and the caller is told.
- Subagents never consult the director (enforced by tool input schema: `role` is
  restricted to the four production roles).
- Subagent-initiated consultation flow (decision D4): the role's deliverable may
  include a `collaborations` field; the stage-manager wrapper resolves it — spawns
  the consulted role, posts question + answer to the board, and re-invokes the
  original role once with the answer injected into its task. Max 2 such rounds per
  invocation.

**Affected paths**: `internal/agents/theatre/` (runner, tools,
broker). No changes to existing `internal/agents/tools` (concierge's tools are
untouched). The director itself arrives in phase 4.

## Integration contract

| Trigger | Collaborator | Observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| `Runner.Run` with a fixture role prompt | fake LLM? no — fixture via clai's real querier only where cheap; otherwise a stub at the Runner seam | role artifact returned or error | session log file written, ledger counters updated, SSE entries emitted | stdout writes from the runner |
| `post_to_board` | board | entry appended, visible in next `read_board` | board file written atomically | entry with unknown kind/role |
| `consult(role, q)` at depth < 2 | consulted role spawn | answer string returned + board entries for question and answer | ledger consult count + hop depth updated | recursion past depth 2 |
| `consult` repeat question | consultation table | previous answer returned, no spawn | — | — |
| `consult` at depth 2 | broker | refusal message, no spawn | ledger records refusal | — |
| deliverable with `collaborations` | wrapper | original role re-invoked once with the answer injected | two board posts (question, answer) | more than 2 collaboration rounds |

## Acceptance criteria

- [ ] Runner produces a valid artifact for each of the four roles against a fixture
      board (real clai call only if a model is configured; otherwise the stub seam).
- [ ] A consult chain of depth 3 terminates: the third consult is refused, ledger
      records hop depth 2 as the max.
- [ ] Repeat consult returns the board answer without a second spawn (spawn counter
      in ledger unchanged).
- [ ] `collaborations` in a deliverable yields exactly one re-invocation with the
      answer present in the task.
- [ ] Global call cap enforcement: a fixture run that would exceed ~200 calls stops
      at the cap with a clear refusal.
- [ ] Every tool spec validates (spec-shape tests like the existing tools package).
- [ ] Existing concierge/classifier/storyteller tests unaffected.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| Subagent LLM call fails | runner falls back to the role's deterministic fallback (phase 5 seam) and reports it | stub-failure test |
| Board read fails | subagent gets empty board excerpt; generation continues | injected-error test |
| Consult spawn refused (cap/depth) | refusal message returned to caller, no panic | broker test |
| Tool call malformed input | tool returns error string; agent loop continues | tool-shape test |
| Runner panic mid-loop | recovered, error returned, ledger records failure | panic-recovery test |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 3 of the playwright-company worklog).

**Delivered** — the subagent machinery, all in `internal/agents/theatre/` plus
the `tools/` subpackage:

| File | Contents |
|---|---|
| `runner.go` | `Runner` + `Invocation` + `Result`: the bounded mini-agent runner. `Run` gates on the generation's budget/deadline, assembles the working context (phase 1), runs one bounded clai loop through the `runLLM` seam, accounts calls/tokens into the ledger, streams SSE entries tagged `theatre.<role>` with the gen as corrID, writes the per-role session log, resolves collaborations, emits the deliver event, recovers panics, and falls back to the role's deterministic answer (phase-5 seam) when the LLM fails. |
| `broker.go` | `Broker.Consult`: spawns the consulted role as a bounded mini-agent (budget `DefaultConsultBudget`, depth+1), returns its answer, posts question+answer to the board and transcript, and guards the cycle: hop cap `ConsultHopCap`, a `{questioner, role, questionHash}` consultation table (repeats answered without a second spawn), and the global call cap + wall-clock deadline. Refusals are message strings, never panics. |
| `collab.go` | The deliverable envelope (`report` + `collaborations`), `parseReport`, and `resolveCollaborations` — the stage-manager wrapper: at most `CollabMaxRounds` (2) rounds, each consulting the requested role once and re-invoking the original role once with the answer injected into its task (decision D4). |
| `roles.go` | The four role prompts (phase-5 seam — minimal working versions, phase 5 writes the full scope), `RolePrompt`/`artifactName`, `roleTools` (shared tools + per-role deliverable writer, author/questioner/depth pinned at construction), and the writer implementations (`postToBoard`, `readBoardExcerpt`, `writeDraft`, `writeScene`, `appendCanon`). |
| `tools/` (subpackage) | The eight mini-agent tools (`post_to_board`, `read_board`, `consult`, `write_brief`, `write_draft`, `append_canon`, `write_scene`, `advise`), each a thin adapter over a callback from the theatre package — the import graph stays one-way (theatre → tools) and every tool is testable with a spy. Malformed input and callback failures are message strings with nil error, so the agent loop continues (the error table's contract). |
| `stage.go` (changed) | `BudgetSnapshot` (global used/max + deadline, for the gates) and `RecordFailure` (marks a role's invocation failed in the ledger). |
| `working.go` (changed) | `Working.Canon` — the playwright's canon facts accumulate per generation (capped count/length), distilled into the repertoire doc in phase 6. |
| `board.go` (changed) | `appendBoardEntry` — shared atomic board-append helper (the runner's tool path returns the error to the model; the broker logs best-effort). |

**Material decisions (recorded for chronology):**

- **D-P3-1 — `Run` takes an `Invocation`, not the spec's bare parameter list.**
  The spec sketched `Run(ctx, role, task, tools, budget)`; the implementation
  folds role/task/budget/depth into `Invocation` and constructs the tools
  internally. Three reasons: the hop depth must ride on the invocation (the
  wrapper resolves collaborations at the invocation's depth); the tools are
  per-role and per-depth and pin author/questioner at construction, so a
  caller-passed tool set could impersonate; and the ctx must reach the tool
  closures, which only happens when the runner builds them inside `Run`.
- **D-P3-2 — the LLM is a seam, not a mock.** `Runner.runLLM` is a function
  field: production is `runClai` (the concierge pattern — `agent.New` with
  WithModel/WithPrompt/WithTools/WithMaxToolCalls/WithOutputTo), tests inject
  a scripted fake. The phase-3 contract's "stub at the Runner seam" is
  structural: the whole machinery — tools, broker, collaborations, ledger —
  runs without a model configured. `runClai` itself is exercised only for its
  no-model failure path (Setup fails cleanly; the fallback answers).
- **D-P3-3 — tool call counting lives in the runner, not the clai path.** The
  tools are wrapped in `countingTool` before the loop, so the ledger accounts
  every tool execution identically whether the stub or the clai agent runs;
  the final answer is one more recorded call. Token usage rides on the chat's
  `TokenUsage` in production and on the seam's outcome in tests.
- **D-P3-4 — `post_to_board` pins the author; the spec's `author` input is
  dropped.** The board is the shared trust surface every role reads; letting
  an LLM choose its author would let one role post as another. The kind is
  validated in the runner so the tool can tell the model a rejected entry was
  rejected (the board gate would silently drop it).
- **D-P3-5 — refusals are strings, failures are errors.** A refused consult
  (depth, budget, deadline, non-production role) returns a message string
  with nil error — the calling agent reads it and adapts. A spawn that
  actually fails (runner error) is a real error. The tool Call contract
  follows suit: malformed input and callback failures are message strings, so
  the agent loop continues per the error table.
- **D-P3-6 — the refusal still lands in the ledger.** A refused consult calls
  `RecordConsult(questioner, depth)`, so the telemetry shows how deep a chain
  tried to go (the depth-3 chain test asserts hop depth 2 as the recorded
  max, refusal included). A repeat consult records nothing — no spawn
  happened.
- **D-P3-7 — a refused collaboration re-invokes the role with the refusal as
  the answer.** The wrapper treats the refusal message exactly like an
  answer: the role is told "consultation depth exceeded" and asked to
  finalize without the collaboration. The re-invocation's own collaboration
  requests are not re-resolved (the loop iterates the original list, capped
  at 2 rounds); the final text is re-parsed for the report.
- **D-P3-8 — the broker and the runner reference each other and are wired
  explicitly.** `NewRunner(...)` then `NewBroker(company, stage, runner)`
  then `runner.WireBroker(broker)` — phase 4 wires the trio at startup, and
  a nil broker refuses consults with a clear message rather than panicking.
- **D-P3-9 — the deliverable envelope is the phase-3 shared shape.**
  `{report, collaborations:[{role, question}]}` is the minimal common
  artifact the wrapper parses; phase 5 refines the per-role schemas on top of
  it (brief.json, draft-report.json, scene-report.json). A deliverable
  without a parseable envelope is the report itself — lenient, floor first.
- **D-P3-10 — the writer tools are plumbing now, schemas in phase 5.**
  `write_draft` parses a story JSON and saves it through
  `model.Story.Validate` with the generation id as the story id;
  `write_scene` dresses the backdrop (cells/props placement is phase 5+7);
  `append_canon` accumulates into `Working.Canon` (capped); `advise` is the
  wardrobe's in-text answer confirmation. All degrade to messages the model
  can adapt to, never hard failures.

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` (before changes) | pass — baseline green |
| `go test ./internal/agents/theatre/ ./internal/agents/storyteller/ ./internal/agents/tools/` (before changes) | pass — phase 1+2 baseline |
| `go test ./internal/agents/theatre/ ./internal/agents/theatre/tools/ -v` | 59 theatre + 11 tools tests pass |
| `go test ./... -race -count=1 -timeout=180s` | pass — full suite, no races |
| `go test ./internal/agents/theatre/ -cover` | 91.2% (phase 2: 93.3%; new code pulls the % down, tools package measured separately) |
| `go test ./internal/agents/theatre/tools/ -cover` | 92.3% |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre/` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/` | 0 clone groups |
| grep `fmt.Print*`/`os.Stdout` in `internal/agents/theatre/` | no matches — the company stays ancli-only |

**Acceptance check** — all criteria met: the runner produces a valid artifact
for each of the four roles against a fixture board (stub seam,
`TestRunner_ProducesArtifactForEachRole`); a consult chain of depth 3
terminates with the third consult refused and hop depth 2 recorded as the max
(`TestBroker_ConsultChainDepth3Terminates`); a repeat consult returns the
board answer with the spawn counter unchanged
(`TestBroker_RepeatConsultReturnsCachedAnswer`); `collaborations` in a
deliverable yields exactly one re-invocation with the answer present in the
task (`TestRunner_CollaborationsResolvedOnce`, capped at two rounds by
`TestRunner_CollaborationsCappedAtTwoRounds`); the global call cap stops a
run past it with a clear refusal (`TestRunner_GlobalBudgetCapRefused`,
`TestBroker_GlobalBudgetExhaustedRefusal`); every tool spec validates
(`TestTools_SpecificationsValid`, including the consult role enum restricted
to the four production roles); existing concierge/classifier/storyteller
tests unaffected (full suite green). Error coverage: LLM failure falls back
and is reported (`TestRunner_LLMFailureFallsBack`, both-fail path
`TestRunner_LLMAndFallbackBothFail`), board read failure degrades to the
empty excerpt (`TestRunner_BoardReadFailureGetsEmptyBoard`), consult refusals
return messages without panics (broker tests), malformed tool input is a
message the loop continues from (`TestTools_MalformedInputReturnsMessage`),
and a panicking loop is recovered with the failure recorded in the ledger
(`TestRunner_PanicRecovered`). Session logs, ledger counters and SSE entries
are asserted (`TestRunner_SessionLogWritten`, `TestRunner_StreamsTaggedLogEntries`).

**Docs** — AGENTS.md package map gained runner/broker/collab/roles/tools and
a subagent-infrastructure key insight.

## Review findings

### Review 1 — 2026-08-02 (holistic review; worker: imago)

**R1-02 — the wardrobe carries the `consult` tool contrary to its scope (Low — fix tracked in phase 11).**

- Reference: `internal/agents/theatre/roles.go` `roleTools` — the shared list (`post_to_board`, `read_board`, `consult`) is appended for every role, including the wardrobe. The phase-3 spec table gives the wardrobe only `post_to_board`/`read_board` + `advise`, and the wardrobe prompt says "You ask: nothing."
- Failure scenario: a consulted wardrobe agent spends its consult budget spawning dramaturg/playwright/scenographer chains (each one hop deeper), wasting global budget and board space against its own scope prompt.
- Fix (checkbox): drop `consult` from the wardrobe's shared set (parameterize the shared list per role).

**R1-04 — a post to an invalid addressee diverges board and transcript (Low — fix tracked in phase 11).**

- Reference: `internal/agents/theatre/roles.go` `postToBoard` — the board gate clears an invalid `to` and keeps the entry, while `TranscriptEvent.valid()` drops the same event; the tool reports success either way. The board and the transcript (and therefore the debug renderer) disagree about the event.
- Failure scenario: an LLM posts with `to: "costume"` (the retired role name, which the phase-2 feed examples themselves used); the board keeps the note addressed to the company, the transcript misses it entirely.
- Fix (checkbox): validate `to` in `postToBoard` the way `kind` is validated (refusal message back to the model), or normalize it before the transcript emit.

Verified good for this phase: hop cap 2 and the repeat-consult table hold (depth-3 chain terminates, cached answers never re-spawn); budget/deadline refusals are returned as message strings with nil error so the calling agent reads them; subagents cannot consult the director (tool schema restricts to the four production roles); the playwright draft-floor check is depth-0 only, so a consult never rewrites the director's draft; `resolveCollaborations` is capped at two rounds and a failed revision keeps the last good text.

### Review 3 — 2026-08-02 (holistic review; worker: imago)

**R3-03 — the trailing "answer" call can show an actor over its per-invocation
budget and push the budgets one past their caps (Low — fix tracked in phase
13).** `internal/agents/theatre/runner.go:287`
(`r.stage.RecordCall(role, "answer")`) counts the loop's final answer as a
call on top of every tool execution. The phase line's denominator is the
clai `WithMaxToolCalls` budget, so a subagent that used its full tool budget
(8) displays "9/8 calls" (`stage.go` `phaseBody`), and the director's own
trailing answer can push `DirectorUsed`/`GlobalUsed` one past their caps in
the ledger — the telemetry then disagrees with the budget it is meant to
track. Failure scenario: any subagent that exhausts its tool budget shows a
fraction greater than one; a generation whose calls sum to the global cap
ends with the cap exceeded by one. Fix (checkbox): decide once whether the
final answer is a budgeted call — if it is, it belongs in the denominator;
if not, record it against the global only (or clamp the display) so the
phase line and the caps never read over budget. Verified good: the
`countingTool` wrapper accounts every tool execution before it runs (a
failing tool still counts, so telemetry cannot undercount a refusal); the
runner's `rnd` is single-goroutine per production (all invocations run on
the director's loop), so the deterministic fallbacks need no lock.

### Review 5 — 2026-08-02 (holistic review; worker: imago)

**R5-02 — a collaboration revision re-runs the actor's full per-invocation
budget (Low; does not reopen).** `internal/agents/theatre/collab.go`
`resolveCollaborations` re-invokes the original role with the full
`inv.Budget` again for each revision round (`runOnce(ctx, inv.Role, ...,
tools, inv.Budget, ...)`), and every tool execution of the revision records
into the same actor's `Calls` and the generation's `GlobalUsed` via
`countingTool`/`RecordCall`. Reproduced with a scratch test (removed after
the repro): one dramaturg invocation with budget 8 and one collaboration
round spent its full 8 calls in the main loop and 8 more in the revision —
the ledger ends `dramaturg 16/8 calls`, and with two rounds (`CollabMaxRounds
= 2`) it would read 24/8. The phase line (`phaseBody`) and the dialog
summary render `Calls/Budget`, so the phase-13 R3-03 closure (“the phase
line and dialog summary can never show over-cap”) is incomplete: it fixed
the trailing answer but not the revision loops. `GlobalUsed` can also
overshoot `GlobalMax` by up to (1+rounds)×budget + consulted budgets per
admitted invocation — R4-01's stated bound (“the last admitted invocation's
budget”) understates the collaboration case. Refusal semantics unaffected —
no NEW work starts past the cap — so cosmetic telemetry, Low. Fix
(checkbox): run the revision against the remaining per-invocation allowance
(`inv.Budget − calls already spent`), or account revision calls separately,
so `Actor.Calls ≤ Actor.Budget` holds on every path.

Verified good for this phase (review 5): the collaboration flow still caps
at `CollabMaxRounds`; a failed consult or revision keeps the last good text;
the wardrobe's tool set still lacks `consult`; `postToBoard` still refuses
an invalid `to` before board or transcript; the hop cap and the repeat-consult
table still hold under `-race`.

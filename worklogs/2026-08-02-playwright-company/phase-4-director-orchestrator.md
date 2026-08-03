# Phase 4 — Director Orchestrator

**Status:** ✅ Complete | [README](./README.md)

## Goal

Build the director superagent and rewire `Teller` to run the production: the
director's tools, the production-flow prompt, budget and wall-clock enforcement,
last-validated-draft fallback, and the composer floor — preserving cooldown,
single-flight, `Warm`/`Next` and disk persistence.

## Specification

**Director agent** — a clai agent exactly like the concierge, 50 tool calls
(`WithMaxToolCalls(50)`, flag `-storytellerMaxCalls`, default 50). Its tools:

| Tool | Purpose |
|---|---|
| `dramaturg_brief` | run the dramaturg mini-agent; returns its brief report |
| `draft_story` | run the playwright mini-agent with brief + the director's notes; returns the compact report |
| `dress_set` | run the scenographer mini-agent with draft + notes; returns the dressing report |
| `read_story` | return the working-file draft or a requested part (cast, beats, scene, title) |
| `validate_story` | run `model.Story.Validate()` on the working file; return normalized story or exact errors |
| `pin_identity` | deterministic: pin canonical coats/species per character id (costumer registry, phase 6) |
| `post_to_board` | director-level post |
| `consult` | director-level consult (same broker as subagents, depth 0) |
| `submit_story` | final gate: validate, persist `intro_story.json`, distill company docs + bulletin (phase 6), return summary |

**Production-flow prompt** — the director's system prompt defines the workflow
suggested flow: brief → draft → dress → validate → pin → iterate on notes → submit. It
is phrased as *guidance, not law* (decision D1): the director may deviate, revisit,
or consult at will. It is instructed to work from stage-manager reports, request
script pages via `read_story` only when scrutiny is needed, and to submit as soon as
the piece is good — not to burn budget.

**Enforcement** — all in the teller/broker, not the prompt:

- Wall-clock cap ~10 min per generation (flag `-storytellerWallClock`); the broker
  refuses spawns past the deadline.
- Global call cap ~200 per generation (flag `-storytellerGlobalCalls`).
- On budget exhaustion: the last `validate_story`-clean working file ships; with
  none, `ComposeThemed` ships. The composer floor is unchanged.

**Teller rewiring** — `teller.generate` becomes `theatre.Theatre.RunProduction`;
the theatre implements `storyteller.Teller` so index wiring stays put:

- Builds the director agent with the shared `-storyteller` model config (the same
  `models.Configurations`; subagents clone it, like the classifier).
- Generation id (`newID`) doubles as the production id and `corrID` for all logs.
- Persists: the final story to `intro_story.json` (existing atomic path), company
  docs and bulletin (phase 6), transcript + ledger (phases 1–2).
- Unchanged semantics: cooldown (10 min default, flag), single-flight, `Warm` two-step
  seed-then-upgrade, `Next` synchronous compose fallback, `loadFromDisk` validation
  and mtime-seeded cooldown, composer-only mode when no model is configured.
- Composer-only mode: no director agent is built; `Next`/`Warm` behave exactly as
  today (regression surface for phase tests).

**Affected paths**: `internal/agents/theatre/theatre.go` (the facade implementing
`agents.Teller`), `internal/agents/theatre/` (director tools),
`cmd/serve/serve_setup.go` (new flags), `cmd/serve/serve_setup_test.go`.
`internal/agents/storyteller/` (composer, staging, muse, `Teller`) is unchanged
until phase 9 migrates it into the theatre and deletes the package.

## Integration contract

| Trigger | Collaborator | Observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| `Prepare` with model configured | director agent | story generated, `Prepare` returns true | `intro_story.json` written, company files written, transcript flushed | generation during cooldown or while in flight |
| Director runs out of budget | broker | last validated working file ships (or composer) | ledger records exhaustion | story with unvalidated beats shipped |
| `validate_story` fails | model.Story.Validate | exact errors returned to director | — | invalid story reaching `submit_story` |
| `submit_story` | gate | validated story persisted | company docs + bulletin distilled, ledger final | second submit for same generation |
| No model configured | teller | composer-only path, byte-identical behavior to today | — | director agent built |
| Restart with cached LLM story | loadFromDisk | cooldown seeded from mtime (existing behavior) | — | fresh generation inside cooldown |

## Acceptance criteria

- [ ] A fixture production (stub subagents) runs: brief → draft → dress → validate →
      pin → submit, with the transcript and ledger recording every step.
- [ ] Budget exhaustion yields the last validated draft, never an invalid one.
- [ ] `submit_story` refuses a story that fails `model.Story.Validate`, returning the
      errors to the director.
- [ ] Composer-only mode (no model) reproduces current behavior exactly — all
      existing storyteller tests pass unchanged.
- [ ] Cooldown, single-flight, `Warm`, `Next`, `loadFromDisk` and atomic-save tests
      pass unchanged.
- [ ] Director tool set registered: 9 tools, specs validate.
- [ ] Flags `-storytellerMaxCalls`, `-storytellerWallClock`, `-storytellerGlobalCalls`
      documented in help text with defaults.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| Director agent construction fails | teller falls back to composer-only generation, error logged | stub-construction test |
| Subagent fails mid-production | role fallback result used; production continues | stub-failure test |
| `submit_story` called twice | second call refused with clear error | broker test |
| Wall clock exceeded | spawns refused; last validated draft ships | deadline test |
| Working file unreadable at submit | composer story ships; error logged | injected-error test |
| Director never calls submit | budget exhaustion path ships last validated draft | exhaustion test |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 4 of the playwright-company worklog).

**Delivered** — the director superagent and the teller rewiring:

| File | Contents |
|---|---|
| `theatre.go` | The `Theatre` facade implementing the teller contract (`Next`/`Prepare`/`Warm`), so `media.WithStoryteller` wiring stays put. Cooldown, single-flight, Warm's two-step seed-then-upgrade, `Next`'s synchronous compose, `loadFromDisk` validation and mtime-seeded cooldown are preserved verbatim from the storyteller (decision D12). `generate` composes directly when no model is configured — no director is ever built (the composer-only regression surface). `saveStory` is the atomic intro_story.json writer; `newID` mirrors the storyteller's id format so a pre-migration cache still loads. |
| `director.go` | The `production` struct (one generation's wiring: company, stage, runner, broker), `openProduction`, `runProduction`, `finish` (the resolution point), the director's nine-tool set with callbacks, the production-flow prompt, and the working-file gates: `readStory`, `validateStory`, `pinIdentity`, `submitStory`. |
| `runner.go` (changed) | The `directorTools` seam: a director invocation runs through the same runner as the subagents, with the facade's tool set injected after construction. The director's final statement is never re-resolved for collaborations (its consultations flow through its own tools). |
| `roles.go` (changed) | `RolePrompt("director")` returns the production-flow prompt; `roleTools` gains the director case (injected tool set, shared tools as defensive fallback). |
| `stage.go` (changed) | `DefaultWallClock` (10 min); `phaseOrder` reordered to brief → draft → dress → validate → pin → submit, matching the production prompt's suggested flow. |
| `tools/director.go` | The seven director tools (`dramaturg_brief`, `draft_story`, `dress_set`, `read_story`, `validate_story`, `pin_identity`, `submit_story`), each a thin adapter over a production callback with the message-string contract; `optString` for the optional notes/part inputs. |
| `loghandler` (changed) | `Print` extracted from the POST handler — the theatre's session sink reuses it, so server-side agent sessions and web-posted logs share the house format (phase 2's serve-side hookup). |
| `cmd/serve` (changed) | Flags `-storytellerMaxCalls` (50), `-storytellerGlobalCalls` (200), `-storytellerWallClock` (10m) with help text; `serve_setup` constructs `theatre.New` with the shared `-storyteller` model config, the budget flags, the wall clock and `WithSessionSink(loghandler.Print)`. `storyteller.LatestTheme` still supplies the muse until phase 9 migrates it. |

**Material decisions (recorded for chronology):**

- **D-P4-1 — the director is a runner invocation, not a separate loop.** The
  director runs as `Invocation{Role: "director", Budget: directorMax}` through
  the same bounded runner as the subagents, so it inherits the working-context
  standard, session logs, ledger accounting, SSE streaming, panic recovery and
  the budget gates for free. The facade injects the nine-tool set through the
  runner's `directorTools` seam — the same wire-after-construction pattern as
  the broker.
- **D-P4-2 — the test seam rides the facade, not the runner.** `Theatre.runLLM`
  overrides the runner's seam when set; production leaves it nil and the
  runner's own clai path builds the director and every subagent from the
  shared model config (a fresh `agent.New` per invocation — the classifier's
  clone pattern). Tests inject one scripted fake, so a whole production runs
  without a model.
- **D-P4-3 — the working file is the resolution point, not the director's
  exit.** `finish` decides what a generation produced: a submitted story ships
  as-is; any other readable draft is the last validated draft and ships with a
  warning note (budget/wall-clock exhaustion); nothing readable is an error
  the caller answers with the composer floor. The "never an invalid one"
  guarantee is structural: `SaveWorking` and `LoadWorking` run the same
  `model.Story.Validate` gate, so an unplayable draft can never reach the
  exhaustion path.
- **D-P4-4 — submit's double-call gate is a production flag.** `production.submitted`
  (the director's loop is single-threaded, so no lock) refuses a second submit
  with a clear message; the working-file status "submitted" is the side effect
  `finish` reads. The story is persisted exactly once, by `submitStory` —
  `finish` only rings the stage bell for the submitted path.
- **D-P4-5 — phaseOrder matches the suggested flow.** The phase line reads
  brief → draft → dress → validate → pin → submit, the flow the production
  prompt suggests; the working statuses stay the -ed set. The validate-before-
  pin order comes from the phase-4 spec's production flow.
- **D-P4-6 — pin_identity's registry is in-memory (phase-6 seam).** The first
  look seen per cast id becomes the pin; later generations apply it. Phase 6
  makes the registry durable across generations.
- **D-P4-7 — the session sink is the loghandler's own renderer.** `loghandler.Print`
  is extracted from the POST endpoint; `WithSessionSink(loghandler.Print)` in
  serve gives the theatre the house format `[theatre.<role>]: corrID: <gen> — …`.
- **D-P4-8 — the facade options are named for the serve flags.** `WithCallBudgets`,
  `WithWallClock`, `WithSessionSink` (the stage's `WithBudgets`/`WithLogSink`
  own the natural names; Go package-level names cannot collide).
- **D-P4-9 — composer-only parity is a mirrored test surface.** The storyteller
  package is untouched (its tests pass unchanged); the facade mirrors the
  cooldown/single-flight/Warm/Next/loadFromDisk/atomic-save semantics with its
  own tests so the phase-9 migration has a regression net.

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` (before changes) | pass — baseline green |
| `go test ./internal/agents/theatre/... ./internal/agents/storyteller/...` (before changes) | pass — phase 1–3 baseline |
| `go test ./internal/agents/theatre/ -v` | 103 top-level tests pass (76 pre-existing + 27 new: 20 facade, 6 production/error, 1 runner director-seam); the new `TestTheatre_*` suite covers the fixture flow, budgets, gates and the composer-only mirror |
| `go test ./internal/agents/theatre/tools/ -v` | 53 pass lines (15 tool set + 4 behavior + 34 spec/malformed/error subtests), director tools included |
| `go test ./...` | pass — full suite, storyteller tests unchanged |
| `go test ./... -race -count=1 -timeout=300s` | pass — no races |
| `go test ./internal/agents/theatre/ -cover` | 90.0% (new director/facade code; `runClai` production path stays seam-covered, same pattern as phase 3) |
| `go test ./internal/agents/theatre/tools/ -cover` | 93.5% |
| `go test ./cmd/serve/ -cover` | 75.7% |
| `go test ./internal/loghandler/ -cover` | 88.9% |
| `go run mvdan.cc/gofumpt@latest -l .` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/ cmd/serve/ internal/loghandler/` | 0 clone groups |
| grep `fmt.Print*`/`os.Stdout` in `internal/agents/theatre/` | no matches — the company stays ancli-only |

**Acceptance check** — all criteria met: the fixture production runs the full
flow with the transcript and ledger recording every step
(`TestTheatre_FixtureProductionRunsFlow`); budget exhaustion yields the last
validated draft, never an invalid one (`TestTheatre_BudgetExhaustionShipsLastValidatedDraft`);
`submit_story` refuses an invalid draft with the exact errors
(`TestTheatre_SubmitRefusesInvalidDraft`) and a second submit
(`TestTheatre_SubmitTwiceRefused`); composer-only mode reproduces current
behaviour — all existing storyteller tests pass unchanged and the facade's
mirrored surface covers cooldown, single-flight, `Warm`, `Next`, `loadFromDisk`
and atomic saves; the director tool set is the spec's nine, all specs validate
(`TestTheatre_DirectorToolSetRegistered`, `TestTools_SpecificationsValid`); the
flags `-storytellerMaxCalls`, `-storytellerWallClock`, `-storytellerGlobalCalls`
are documented in help text with defaults (`serve_setup_test.go`). Error
coverage: director construction failure falls back to composer with the error
logged (`TestTheatre_DirectorConstructionFailsFallsBackToComposer`); a failing
subagent uses its fallback and the production continues
(`TestTheatre_SubagentFailureFallsBackAndContinues`); the wall clock refuses
spawns and the pre-deadline validated draft ships
(`TestTheatre_WallClockRefusesSpawnsPastDeadline`); a working file that dies at
submit time ships the composer floor with the failure logged
(`TestTheatre_WorkingFileUnreadableShipsComposer`); a director that never
submits hits the exhaustion path (`TestTheatre_BudgetExhaustionShipsLastValidatedDraft`,
`TestTheatre_NoDraftFailsToComposer`).

## Review findings

### Review 1 — 2026-08-02 (holistic review; worker: imago)

**R1-01 — data race on `Theatre.rnd` between `Next` and `Prepare`/`Warm` (Medium — fix tracked in phase 11).**

- Reference: `internal/agents/theatre/theatre.go` — `Next` composes under `t.mu` (the `current == nil` branch), while `Prepare`→`generate` (composer-only path and the LLM-failure fallback compose) and `Warm`'s synchronous seed compose run with no lock at all.
- Failure scenario: composer-only mode (or any LLM-failure fallback), a background `Prepare` (startup upgrade, or the post-visit "story consumed" prepare) composing while a `Next` composes on an empty cache. Reproduced with `-race`: fresh `Theatre` per iteration, concurrent `Next()` + `Prepare()`, 400 iterations → `WARNING: DATA RACE` in `math/rand.(*rngSource).Uint64` via `ComposeThemed` on both goroutines. The race detector is quiet only because the existing tests never exercise the two composers concurrently.
- Provenance: **pre-existing** — the pre-migration storyteller had the identical locking pattern (`Next` under `mu`, `Prepare`/`Warm` outside; verified against `storyteller.go` at c61c86e). Phase 4's contract was to preserve those semantics, so this is not a regression the phase introduced; the defect was carried over by the migration and is being routed to a fix phase rather than silently left on the board.
- Latency in production: the serve wiring seeds via `Warm` before serving, so `Next`'s compose branch is rarely hit after startup; the race is real at the `agents.Teller` API boundary (the contract explicitly allows concurrent `Next` + `Prepare`) and becomes live the moment any caller composes on an empty cache concurrently with a background prepare.
- Fix (checkbox): guard `t.rnd` with a dedicated mutex (a separate `rndMu`, since `Prepare` must not hold `t.mu` across an LLM production) or serialize every compose under `t.mu`; add a `-race` regression test that composes concurrently via `Next` + `Prepare`.

Verified good for this phase: cooldown/single-flight/`Warm` two-step/`Next` fallback semantics are byte-for-byte the pre-migration flow; `loadFromDisk` validation and mtime-seeded cooldown are unchanged; wall-clock enforcement holds at both the runner/broker spawn gates and the director's `context.WithTimeout`; the last-validated-draft path ships any readable draft and `LoadWorking` runs the same `model.Story.Validate` gate, so "validated" is exact by construction.

### Review 2 — 2026-08-02 (holistic review; worker: imago)

**R2-01 — `openProduction` draws the generation id from `t.rnd` without `rndMu`
(Medium — fix tracked in phase 12).** The R1-01 fix serialized every *compose*
path under `rndMu` but missed the production path's generation-id draw:
`openProduction` (`director.go:88`) calls `newID(t.rnd)` unlocked, racing any
concurrent compose (`Next` on an empty cache, `Warm`'s seed). Reproduced with
`-race` on the model-configured path (see the phase-11 file for the full
finding). This file's `runProduction`/`openProduction` are where the defect
lives; the fix (guard the draw via a locked helper) and its regression test
are spec'd in phase 12.

### Review 3 — 2026-08-02 (holistic review; worker: imago)

**R3-01 — the serve-side 2-minute context cap overrides `-theatreWallClock`
on HTTP-triggered generations (Medium — fix tracked in phase 13).** This
phase delivered the wall-clock contract (`-theatreWallClock`, default 10m,
help text "wall-clock cap for one intro-story generation") and enforced it at
the broker spawn gate and the director's `context.WithTimeout(ctx,
wallClock)` (`director.go` `runProduction`). But the serve wiring this phase
also touched (`cmd/serve` constructs the theatre) triggers generations
through `internal/media/index_handlers.go:348-354`
(`prepareNextStory` → `context.WithTimeout(context.Background(), 2*time.Minute)`
→ `Prepare`). `WithTimeout` inherits the earlier parent deadline, so every
story-consumed/session-end generation is hard-cancelled at 2 minutes
whatever the flag says — only the startup `Warm` path gets the flag's full
value. The broker's deadline gate (`stage.WallDeadline = now + wallClock`)
is never reached on the common path; raising the flag above 2 minutes does
nothing. Failure mode is graceful (cancelled LLM calls fall back; the last
validated draft ships), so the show never breaks — but the flag's contract
is unmet and the advertised ~10-minute window is inert for the trigger that
runs most often. Fix (checkbox): drop the serve-side cap and let the
theatre's own gates bound the goroutine, or derive it from the configured
wall clock plus a margin — never a fixed constant (strategy item 15). A
regression test must pin the effective deadline of the trigger path to the
flag.

Verified good for this phase (review 3): the facade's two draws from `t.rnd`
both run under `rndMu` (`compose`, `newGenID`); `finish`'s three resolution
paths (submitted → ship; readable draft → ship + save; nothing → composer
floor via the caller) hold on the failure branches; `submitStory` persists
the story before marking the working file submitted, so docs never precede
the story; the R2-01 regression test exercises the model-configured
production path under `-race` and passes at `-count=3`.

## Review findings (review 7, 2026-08-03)

**R7-01 — exhaustion ships an unvalidated draft (High; closed in phase 14).**
`internal/agents/theatre/director.go:154-173` selects the readable-working-file
branch on `werr == nil` alone. `writeDraft` leaves `Working.Status` as `draft`
until the separate `validate_story` tool changes it to `validated`, so the
branch does not implement the phase contract's “last validated draft” gate.

Failure scenario: the playwright successfully writes a playable story, then
the director reaches its call or wall-clock limit before calling
`validate_story`. `finish` persists and submits that unvalidated draft instead
of using an earlier validated snapshot or returning to the composer floor.

Fix (closed): an explicit `Working.Validated` flag — set only by
`validate_story`, cleared by every draft-rewriting writer — gates the
exhaustion branch; regression tests `TestTheatre_DraftOnlyExhaustionFallsToComposer`
and `TestTheatre_ValidatedThenRewrittenDraftDoesNotShip` (see [phase 14](./phase-14-review-fixes.md)). **[x]**

**R7-02 — submit ignores story persistence failure (High; closed in phase 14).**
`internal/agents/theatre/director.go:338-370` calls `p.theatre.saveStory(w.Story)`
without an error result, while `theatre.go:346-380` logs marshal, mkdir,
temporary-file and rename failures and returns nothing. The submit then marks
`working.json` submitted and runs distillation regardless of that failure.

Failure scenario: `intro_story.json` cannot be created or replaced (for
example, a full or read-only cache volume) while the company paperwork remains
writable. The director receives a successful submission, `working.json` says
submitted, and library docs can be updated although the story is absent or
stale.

Fix (closed): `saveStory` returns its error and `submit_story` aborts the
submitted transition and distillation when the story cannot be persisted;
failure-injection regression test
`TestTheatre_SubmitAbortsWhenStoryNotPersisted` (see [phase 14](./phase-14-review-fixes.md)). **[x]**

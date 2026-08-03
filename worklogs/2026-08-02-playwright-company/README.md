# 2026-08-02: Playwright Company — Director-Superagent Storyteller

**Status:** ✅ Phase 14 closed review-7 findings R7-01/R7-02 (High) — phases 4 and 6 complete. Reviews 4–8 filed R4-01/R5-01/R5-02/R6-01 (Low, non-reopening; review 8 re-confirmed all four open as filed) | [Phase list](#phase-status)

## Summary

Upgrade the intro splash storyteller into a multi-agent **theatre company**. A director
superagent orchestrates mini-agent subagents (dramaturg, playwright, scenographer,
wardrobe consultant) via tool calls over a shared production board, with a consultation
broker for cross-agent collaboration, per-role persistent libraries that grow across
generations, a durable bulletin, soft-continuity canon facts, an expanded vocabulary,
and a new bird species. The deterministic composer stays the floor under everything.
Observability: a per-generation transcript, a compact single-writer stdout feed, and a
progress ledger.

This worklog supersedes the single-agent design in
[`agent_notebook/2026-07-25_intro-story-engine.md`](../../agent_notebook/2026-07-25_intro-story-engine.md).
The existing guarantees (cooldown, single-flight, `Warm`/`Next`, atomic persistence,
`model.Story.Validate`, composer fallback) are preserved, not replaced.

## Strategy

The pipeline, as decided in the planning interview:

1. **Director is a clai superagent** (the `internal/agents/concierge` pattern): tools,
   conversation-held state, **50 tool calls per generation**.
2. **Subagents are mini clai agents** spawned per invocation — `dramaturg_brief`,
   `draft_story`, `dress_set` each run a bounded agent loop (~8 calls, tunable per
   role) with its own tools: `consult`, `post_to_board`, `read_board`, and its
   deliverable writer.
3. **Consultation broker**: `consult(role, question)` spawns a fresh instance of that
   role with the standard preamble + the director's task + the question, and returns
   the answer onto the board. Hop cap **2**, a consultation table dedupes repeats,
   subagents never consult the director (they post; the director reads).
4. **Working context standard**: one assembly function builds every agent's context —
   generation id + theme + board excerpt (last ~20 entries) + working-file summary +
   role prompt + task. No agent runs outside it.
5. **Stage-manager working file** (`company/working.json`) holds the draft. Subagents
   return compact reports; `read_story` fetches detail on demand.
6. **Production board** (`company/board.json`): the per-generation shared worklog —
   entries `{author, kind, to, body}`. Ephemeral; distilled into company docs and the
   bulletin at submit.
7. **Soft continuity**: canon facts persist across generations; the playwright riff on
   them. Looks are pinned by the costumer registry so characters never drift.
8. **Costumer splits in two**: deterministic registry (`pin_identity` — wardrobe
   maintenance) + consultable **wardrobe consultant** role (the advisor).
9. **Budgets and circuit breakers**: director 50 calls; subagent ~8 per invocation;
   consult ~4 per spawn; global ~200 LLM calls per generation; ~10-minute wall clock.
   On exhaustion the last validated draft ships; with none, the composer floor.
   All numbers are flags, tuned later from telemetry.
10. **Vocabulary expansion**: new pieces (fireplace, bookshelf, door, log), props
    (ball, bone, cushion, bowl), backdrops (kitchen, forest, rain), actions (yawn,
    sniff, jump), 2–3 new composer templates.
11. **New species: bird** — player art, CSS, formant voice, coats, composer scenes.
12. **Persistence (self-developing library)**: per-role company docs (premises,
    repertoire/canon facts, set usage, character registry, director lessons) plus a
    durable cross-generation bulletin. Trimmed to caps, validated on load, atomic
    writes, injected into the relevant role's prompt next generation.
13. **Observability**: per-generation transcript JSONL (single writer), compact stdout
    feed (one line per inter-agent event), progress ledger with phase lines, SSE
    loghandler streaming with role+generation tags, telemetry counters feeding
    `cmd/llm`, and a `debug production <genID>` dialog renderer.
14. **The facade's random source is internally synchronized** (review 1, R1-01;
    extended by review 2, R2-01): the `Teller` contract permits concurrent
    `Next` + `Prepare`, so every draw from the theatre's random source — the
    compose paths AND the generation-id draw in `openProduction` — must be
    guarded by `rndMu`, a fix in the facade, never in callers. Future callers
    of `agents.Teller` may assume concurrent use is safe.
15. **The theatre's own gates are the budget authority** (review 3, R3-01):
    callers that wrap `Prepare` in their own context timeout must not undercut
    the theatre's wall-clock flag — the effective generation window is the
    earlier of the two deadlines, and a smaller caller-side cap silently
    disables `-theatreWallClock` on that trigger path. The theatre bounds
    every generation itself (wall clock + single-flight + budget gates), so a
    caller-side cap is redundant at best; when one exists it must be derived
    from the theatre's configured wall clock, never a fixed constant.
16. **The global budget gate admits whole invocations** (review 4, R4-01):
    the gate refuses new work once the cap is spent, but an invocation
    already admitted finishes its budgeted calls — so `GlobalUsed` can
    tail-overshoot `GlobalMax` by the last admitted invocation's budget. The
    per-actor counters and the director budget never exceed their caps; the
    global counter's tail-overshoot is bounded, cosmetic telemetry. A future
    fix may reserve the invocation's budget at the gate
    (`used + inv.Budget > max`) to make `GlobalUsed ≤ GlobalMax` provable.
    Review 5 (R5-02) extends the bound: an invocation with collaboration
    rounds re-runs its budget per round, so the overshoot can be up to
    (1+rounds)×budget + consulted budgets.
17. **The per-invocation budget is the whole invocation, collaboration
    revisions included** (review 5, R5-02): `resolveCollaborations`
    re-invokes the original role with the full `inv.Budget` again, so one
    invocation can record up to (1+CollabMaxRounds)×budget calls against the
    actor's `Budget` — the phase line and dialog summary then show over-cap,
    the exact display R3-03 closed for the trailing answer. A fix must make
    the revision consume the remaining per-invocation allowance, or account
    revisions separately, so `Actor.Calls ≤ Actor.Budget` holds on every
    path.
18. **The brief is captured at source, never at draft-write time** (review
    5, R5-01): the R3-02 out-of-band capture reads the board at draft-write
    time (`boardBrief()`), but the pre-draft window is not provably closed —
    a consult posts question + answer and the consulted role posts up to its
    budget, so a budget-respecting generation can trim the brief before the
    playwright writes. The capture must happen when the brief is posted
    (`writeBrief`/`fallbackBrief`), where the board is guaranteed to still
    hold it; the board scan stays a fallback for older files, never the
    primary copy.
19. **The phase line compares like with like** (review 6, R6-01): the
    ledger's `Actor.Calls` is generation-cumulative while `Actor.Budget` is
    the most recent invocation's per-invocation cap, and `phaseBody` renders
    `Calls/Budget` — so any role invoked more than once in a generation (a
    role consulted after its main invocation, or consulted repeatedly)
    shows over-cap, the display R3-03 closed for the trailing answer. The
    R5-02 revision-allowance fix bounds only the collaboration path; the
    accounting itself must change for the invariant to hold on every path —
    reset `Calls` per invocation (with a separate cumulative counter),
    render the current invocation's calls, or drop the ratio after the
    first invocation. The fix belongs in `stage.go`, never in callers.

20. **Exhaustion ships only validated work** (review 7, R7-01): `finish` must require the draft to have passed `validate_story` before using the readable draft fallback. A playable file with status `draft` is not the last validated draft; it must fall through to the composer floor. Fixed in phase 14: the blessing is an explicit `Working.Validated` flag set by `validate_story` and cleared by every writer that rewrites the draft — a status label alone cannot carry it, because `pin_identity` and `write_scene` re-label the file after validation (D-P14-1).
21. **Persistence errors cross the submit boundary** (review 7, R7-02): `submit_story` may mark `working.json` submitted and distill company docs only after `intro_story.json` has been durably written. Fixed in phase 14: `saveStory` returns its error and `submit_story` aborts the submitted transition and distillation when the story cannot be persisted (D-P14-2).

## Phase Status

| Phase    | Status      | Summary                                                                                                                                                                                              |
| -------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Phase 1  | ✅ Complete | Company substrate: board, working file, ledger, transcript formats, context-standard assembly, atomic persistence utils, doc validation                                                              |
| Phase 2  | ✅ Complete — fixed in P11 | Observability: transcript writer, ancli stdout feed (theatre prefix), ledger phase lines, SSE streaming, telemetry counters, `debug production` renderer. Review 1: R1-03 (repeat consults invisible to ledger/transcript) → phase 11. Review 6: R6-01 (phase line compares cumulative Calls against per-invocation Budget) filed — Low, does not reopen |
| Phase 3  | ✅ Complete — fixed in P11 | Subagent infrastructure: mini-agent runner, per-role tool sets, consultation broker (hop cap, dedupe, budget ledger). Review 1: R1-02 (wardrobe carries consult), R1-04 (invalid-to post diverges board/transcript) → phase 11. Review 5: R5-02 (collaboration revision re-runs the actor's full budget) filed — Low, does not reopen. Review 6: R6-01 (phase line over-cap on repeat invocations) filed — Low, does not reopen |
| Phase 4  | ✅ Complete — fixed in P14 | Director orchestrator + `Teller` rewiring. Review 7: R7-01 (readable but unvalidated `working.json` is shipped as the “last validated draft”) and R7-02 (submit ignores `saveStory` failure) → phase 14. Earlier R1-01/R2-01/R3-01 fixes remain closed. |
| Phase 5  | ✅ Complete | Roles and prompts: dramaturg, playwright, scenographer, wardrobe consultant — scope prompts, artifact schemas, deterministic fallbacks, canon-fact injection                                                 |
| Phase 6  | ✅ Complete — fixed in P14 | Persistence and the self-developing library. Review 7: R7-02 (the story write failure is logged but submit still marks working state submitted and distills docs) → phase 14. Earlier R3-02/R3-04 fixes and R5-01 remain as recorded. |
| Phase 7  | ✅ Complete | Vocabulary expansion: new pieces (fireplace, bookshelf, door, log), props (ball, bone, cushion, bowl), backdrops (kitchen, forest, rain) and actions (yawn, sniff, jump) in model + player + CSS; three new composer templates (midnightsnack, birdwatching, snowed-in); theatre backdrop vocabulary follows                                                                 |
| Phase 8  | ✅ Complete | Bird species: player art, CSS, voice, coats, composer scenes, wardrobe coverage                                                                                                                      |
| Phase 9  | ✅ Complete — fixed in P11 | Storyteller removal: floor (composer, staging, muse) migrated into theatre, `Teller` moved to `agents/interfaces.go`, package deleted, every reference re-pointed, composer-only path proven byte-identical by a frozen snapshot. Review 1: R1-01 provenance (race carried over, not introduced) → phase 11 |
| Phase 10 | ✅ Complete | Quality gate: format, vet, staticcheck, race/coverage tests, fix, dupl, per-package coverage — all green (D-P10-1 sized the fallback sweeps to the 30s gate; D-P10-2 documents the pre-existing full-suite flakes) |
| Phase 11 | ✅ Complete — fixed in P12 | Review-1 fixes (addendum): R1-01 `Theatre.rnd` data race, R1-02 wardrobe consult tool, R1-03 repeat-consult accounting, R1-04 invalid-to post divergence — each with its regression test (see [feedback index](#feedback-index)). Review 2: R2-01 — R1-01's `rndMu` guard missed the production path's generation-id draw → phase 12 |
| Phase 12 | ✅ Complete | Review-2 fixes (addendum): R2-01 — `openProduction`'s generation-id draw now goes through a locked `newGenID` helper (`rndMu` guards every draw from `t.rnd`), `-race` regression test on the model-configured path (fails on the pre-fix code), plus the R1-01 test hardened against its TempDir-cleanup flake (see [feedback index](#feedback-index)) |
| Phase 13 | ✅ Complete — R5-01/R5-02 filed (does not reopen) | Review-3 fixes (addendum): R3-01 (Medium — serve-side 2-minute cap overrides `-theatreWallClock`; reopens phase 4), R3-02 (Low — board overflow silently drops the premise at distill), R3-03 (Low — trailing "answer" call can show an actor over its budget), R3-04 (Low — registry load gate trusts hand-edited variant lists). All four closed with regression tests ([phase 13](./phase-13-review-fixes.md)). Review 5: R5-01 (pre-draft brief window not provably closed), R5-02 (collaboration revision re-runs the actor's budget) filed — Low, does not reopen |
| Phase 14 | ✅ Complete | Review-7 fixes (addendum): R7-01 (High — exhaustion ships an unvalidated draft; `Working.Validated` flag, cleared by every draft-rewriting writer) and R7-02 (High — submit ignores story persistence failure; `saveStory` returns its error, `submit_story` aborts the submit). Both closed with regression tests ([phase 14](./phase-14-review-fixes.md)) |

Dependency order: 1 → 2 → 3 → 4 → 5 → 6, with 7 and 8 after 5 (7 before 8), then
9 (storyteller removal), then 10 (quality gate), then 11 (review-1 fixes, addendum),
then 12 (review-2 fixes, addendum), then 13 (review-3 fixes, addendum), then
14 (review-7 fixes, addendum).

## Severity Taxonomy

- **Critical**: OOM, data loss, process crash
- **High**: breaks a feature or creates incorrect behavior
- **Medium**: degrades observability or performance
- **Low**: cosmetic

All findings above Low reopen the phase.

## Decisions

- **D1 — Director as superagent**: the director is a clai agent with tools (concierge
  pattern), not a single query. It holds the working state; subagents are stateless.
- **D2 — Subagents as mini-agents**: each role invocation is a bounded clai loop with
  `consult`/`post_to_board`/deliverable tools. Chaos lives in the rehearsal; order at
  the door (validation gates, budgets, composer floor).
- **D3 — Shared production board**: the board is the working context standard every
  agent reads and may write. It is the shared memory that makes stateless spawns
  "conversational".
- **D4 — Consultation broker with cycle guards**: hop cap 2, consultation-table
  dedupe, no consulting the director. Subagent-initiated consultations flow through
  the structured `collaborations` field and the stage-manager wrapper.
- **D5 — Working-file draft**: the draft lives in `company/working.json` (stage
  manager); subagents return compact reports; `read_story` for detail. Keeps 50-call
  rehearsals context-cheap.
- **D6 — Soft continuity**: canon facts persist and are injected into the playwright.
  Stories stay standalone-readable but reward regulars.
- **D7 — Costumer = registry + consultant**: `pin_identity` stays deterministic
  (character identity never drifts); the wardrobe consultant is a consultable role.
- **D8 — Budgets are flags, tuned from telemetry**: 50 director calls, ~8/subagent,
  ~4/consult, ~200/generation, ~10 min wall clock. The "analyze later" is built in.
- **D9 — Vocabulary and species are hand-built walls**: the library grows characters
  and recipes freely, but new art (pieces, props, backdrops, species) is implemented
  in the player before the agents may use it.
- **D10 — Observability = ancli feed + transcript + ledger**: the stdout story is a
  compact one-line-per-event feed printed through `ancli` (house timestamps via
  `ancli.SetupSlog`, colours, mutex-protected lines) with a `[theatre <gen>]`
  prefix; one JSONL transcript per generation (single writer); and a per-generation
  progress ledger. Agents never write stdout directly.
- **D11 — Composer remains the floor**: every subagent tool degrades to its
  deterministic fallback; no LLM failure can block the splash.
- **D12 — Cooldown/single-flight/Warm/Next preserved**: the 50-call budget is per
  generation; the cooldown still gates how often a generation starts.
- **D13 — The system is named `theatre` and replaces the storyteller**: package
  `internal/agents/theatre/` hosts the director, subagents, broker, board, docs,
  observability and — after phase 9 — the migrated deterministic floor (composer,
  staging, muse). The `Teller` contract moves to `internal/agents/interfaces.go`
  (the house home of agent contracts); `internal/agents/storyteller/` is deleted.
  On-disk paperwork stays under `intro/company/` — the theatre runs the company.
  Stdout prefix `[theatre <gen>]`, SSE tags `theatre.<role>`. Cache path and
  cooldown semantics are unchanged, so a pre-migration cache still loads. Phase 9
  renamed the serve flags to `-theatre`, `-theatreCooldown`, `-theatreMaxCalls`,
  `-theatreWallClock`, `-theatreGlobalCalls` (D-P9-2) and the LLM usage
  attribution label to `theatre`.

**Review 8 — 2026-08-03 (worker: imago, holistic review session).**

All 14 phases were complete, so per the runbook this session performed the
eighth holistic review (worklog-review skill, round 8) instead of starting a
new phase. The gates were re-run independently and came out green: `go run
mvdan.cc/gofumpt@latest -l .` (v0.11.0, zero diffs), `go vet ./...` (clean),
`go run honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix
./...` (clean), `go build ./...` (ok), `go run github.com/mibk/dupl@latest
-t 80 .` (27 pre-existing clone groups, none in theatre — matching the
recorded number), `node --check intro.js` + the node harness 6/6, `rg
storyteller` over `cmd/`+`internal/` zero hits, no direct stdout in the
theatre package. The full suite `go test ./... -race -cover -count=3
-timeout=30s` hit the two documented 30-second-window timing flakes under
full-suite load — `internal/agents/theatre` (30.228s) and
`internal/media/storage` (30.165s), both the D-P10-2 marginal-wall-time
family: the theatre package passes in isolation `-race -cover -count=3
-timeout=300s` in 54.3s at 91.4% coverage (matching the recorded ~91.3%)
and storage passes `-race -count=1` in 12.2s. The targeted regression set
was re-run green: R7-01/R7-02 (`DraftOnlyExhaustionFallsToComposer`,
`ValidatedThenRewrittenDraftDoesNotShip`, `SubmitAbortsWhenStoryNotPersisted`,
`SaveStoryWriteFailureReturnsError`, `BudgetExhaustionShipsLastValidatedDraft`,
`WallClockRefusesSpawnsPastDeadline`), R1-01/R2-01
(`ConcurrentNextAndPrepareComposeSafely`, `ConcurrentNextAndPrepareProductionSafely`),
R3-01 (`TestPrepareNextStory_LeavesWallClockToTheTheatre`), R3-02
(`Distill_PremiseAndSetsSurviveBoardOverflow`), R3-03
(`Runner_FullBudgetNeverShowsOverCap`), R3-04
(`Registry_LoadDocDropsOutOfPaletteVariants`), R1-03/R1-04
(`Broker_RepeatConsult*`, `Runner_PostToBoardRejects*`), and the frozen
composer snapshot — all green. The closed findings were traced through
their branches in code: both of the facade's draws from `t.rnd` run under
`rndMu` (`compose` and `newGenID`); the wardrobe's tool set carries no
`consult`; a repeat consult accounts the ledger and notes the transcript
without re-spawning; `postToBoard` validates `kind` and `to` before the
board or transcript sees them; `prepareNextStory` passes
`context.Background()`; `Brief`/`Dressed`/`Validated` ride the working file
out of band and every draft-rewriting writer (`write_draft`, `write_scene`,
`fallbackDraft`, `fallbackScene`) clears the R7-01 blessing; `saveStory`
returns its error unlogged and `submit_story` aborts the submitted
transition and distillation on persistence failure. The four open Lows were
re-verified independently with scratch tests (removed after the repro), each
still open exactly as filed: R4-01 — the gate admits an invocation without
reserving its budget: 3 spent, admitted (7 < 10), invocation spent 8 →
ledger `global 11/10`; R5-01 — the brief was trimmed before the draft
write: brief + 60 notes on the board → brief gone, `w.Brief` empty at
`writeDraft`; R5-02 — one collaboration round re-runs the actor's full
budget: playwright `9/8` calls; R6-01 — the phase line renders cumulative
calls against the per-invocation budget: `dramaturg 12/4 calls`. No new
findings. Per the severity taxonomy the four Lows do not reopen; the board
stays green with all 14 phases complete, and the next session picks them up
if a future addendum consolidates them (or any future Medium finding).

**Review 5 — 2026-08-02 (worker: imago, holistic review session).**

All 13 phases were complete, so per the runbook this session performed the
fifth holistic review (worklog-review skill, round 5) instead of starting a
new phase. The gates were re-run independently and came out green:
`go run mvdan.cc/gofumpt@latest -l .` (zero diffs), `go vet ./...` (clean),
`go run honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix
./...` (clean), `go run github.com/mibk/dupl@latest -t 80 .` (27
pre-existing clone groups, none in theatre), `go test ./... -race -cover
-count=3 -timeout=30s` (all 23 packages ok; theatre 91.3%, tools 93.1%,
model 95.6% — matching the recorded numbers), the targeted R3-01/R1-01..
R1-04/R2-01/R3-02/R3-03/R3-04 regression set under `-race -count=3`
(green), `go build ./...` (ok), `node --check intro.js` + the node harness
6/6, the frozen composer snapshot byte-identical, zero `storyteller` hits,
no direct stdout in the theatre package. All eight prior findings
(R1-01..R1-04, R2-01, R3-01..R3-04) verified in code and under `-race`.
Two findings filed — R5-01 (Low: the phase-13 “provably closed” pre-draft
brief window is not closed — consults post question + answer and consulted
roles post up to their budget, and the dramaturg's own 8-call budget can
post 8 entries, so a budget-respecting generation trims the brief before
the playwright's draft write; reproduced with a scratch test — the brief
was gone and `w.Brief` was empty, the exact R3-02 failure mode) and R5-02
(Low: a collaboration revision re-runs the actor's full budget —
reproduced dramaturg 16/8 calls after one round, so the phase line and
dialog summary show over-cap again and `GlobalUsed` can overshoot by up to
(1+rounds)×budget, exceeding R4-01's stated bound). Per the severity
taxonomy, Low does not reopen; both are filed with checkbox fixes, the
affected invariants are promoted to strategy items 17 and 18, and R4-01's
bound note is extended. Verdict: the work is ready; the board stays green
with all 13 phases complete and R5-01/R5-02 open as non-reopening Lows for
a future addendum.

**Review 4 — 2026-08-02 (worker: imago, holistic review session).**

All 13 phases were complete after phase 13 landed, so per the runbook this
session performed the fourth holistic review (worklog-review skill, round 4)
instead of starting a new phase. The phase-13 gates were re-run independently
and came out green: `go run mvdan.cc/gofumpt@latest -l .` (v0.11.0, zero
diffs), `go vet ./...` (clean), `go run
honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix ./...`
(clean), `go run github.com/mibk/dupl@latest -t 80 .` (27 pre-existing clone
groups, none in the touched files), `go test ./... -race -cover -count=3
-timeout=30s` (every package ok except the documented pre-existing D-P10-2
`cmd/classify` context-timeout flake, green in isolation — theatre 91.3%,
tools 93.1%, model 95.6%, matching the recorded numbers), `go test -race
./internal/agents/theatre/...` (green — 7.1s, and 18.5s at `-count=3`),
`go test -race ./internal/media/... ./cmd/serve/...` (green). The R1-01..
R1-04 and R2-01 fixes were verified in code and under `-race`, and each
phase-13 fix was traced through its branches: the R3-01 trigger path carries
no caller-side deadline; the R3-02 brief reaches the working file on every
draft-creation path (the pre-draft overflow window is provably closed by the
budget gates) and `Dressed` is set only by the scenographer paths; the R3-03
answer is a non-budgeted call on the success path only; the R3-04 load gate
and `Canonize` share the species palette. One finding filed — R4-01 (Low:
the global budget gate admits an invocation without reserving its own
budget, so an invocation admitted near the cap finishes past it —
reproduced: GlobalUsed 195 + subagent (8) → ledger `global 203/200`;
cosmetic telemetry, refusal semantics unaffected). Per the severity
taxonomy, Low does not reopen; the finding is filed with a checkbox fix in
the phase-13 review section and the affected `ledger.go` comment was
corrected to state the tail-overshoot (the phase-13 “counters never exceed
their caps” holds for the per-actor counters and the director budget, not
for the global counter). The global-budget-gate invariant is promoted to
strategy item 16. Verdict: the work is ready; the board stays green with all
13 phases complete and R4-01 open as a non-reopening Low for a future
addendum.

**Review 3 — 2026-08-02 (worker: imago, holistic review session).**

All 12 phases were complete, so per the runbook this session performed the
third holistic review instead of starting a new phase (worklog-review skill,
round 3). The gates from phase 12 were re-run independently and came out
green: `go run mvdan.cc/gofumpt@latest -l .` (v0.11.0, zero diffs), `go vet
./...` (clean), `go run honnef.co/go/tools/cmd/staticcheck@latest ./...`
(clean), `go fix ./...` (clean), `go run github.com/mibk/dupl@latest -t 80 .`
(27 pre-existing clone groups, none in theatre), `go test ./... -race -cover
-count=3 -timeout=30s` (every package ok except the documented pre-existing
`cmd/classify` context-timeout flake, which passes `-race -count=3` in
isolation — D-P10-2 family; theatre 91.2%, tools 93.1%, model 95.6%,
matching the recorded numbers), `go test -race -cover -count=3
./internal/agents/theatre/...` (green — theatre 21.5s/91.2%, tools
1.0s/93.1%). Supplementary: `node --check intro.js` + the node harness 6/6;
the frozen composer snapshot test present; `rg 'newID(t\.rnd)'` over the
package returns zero hits outside the two locked helpers' shared `newID`
definition. R1-01..R1-04 and R2-01 were verified in code: both of the
facade's draws from `t.rnd` run under `rndMu` (`compose` and `newGenID`);
the wardrobe's tool set carries no `consult`; a repeat consult records in the
ledger and notes the transcript without re-spawning; `postToBoard` refuses an
invalid `to` before board or transcript sees it; the R2-01 regression test
exercises the model-configured production path and the R1-01 test waits on
`writeWG` (the P12 hardening). Verdict: the work is ready — but the review
found one genuine integration gap and three minor deviations (R3-01..R3-04,
feedback index). R3-01 is a confirmed Medium: the serve-side 2-minute
context cap in `prepareNextStory` (`index_handlers.go:348-354`) overrides
`-theatreWallClock` (default 10m) for every HTTP-triggered generation,
because `runProduction` derives its deadline from the parent ctx — raising
the flag above 2 minutes does nothing on the common trigger path, and the
broker's deadline gate is never exercised there. It reopens phase 4's
wall-clock contract per the severity taxonomy and is routed to the phase-13
addendum. Cross-cutting observation: the wall-clock budget now has two
enforcers (the theatre's gates and a caller-side constant), and the smaller
one silently wins — promoted to strategy item 15 so future callers of
`agents.Teller` derive their timeouts from the theatre's configured wall
clock, never a fixed constant.

**Review 2 — 2026-08-02 (worker: imago, holistic review session).**

Re-ran the gates independently on the phase-11 state and reproduced the claimed
green: `go run mvdan.cc/gofumpt@latest -l .` (v0.11.0, zero diffs; the locally
installed gofumpt v0.10.0 flags 6 files with pure cosmetic line-breaking —
version skew, not a defect, and not the gate's tool), `go vet ./...` (clean),
`go run honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean),
`go test ./... -race -cover -count=3 -timeout=30s` (all 23 packages ok —
including the documented `internal/media/watcher` flake, which passed this
run; theatre 91.1%, tools 93.1%, model 95.6% — matching the recorded
numbers), `go fix ./...` (clean), `go run github.com/mibk/dupl@latest -t 80 .`
(27 pre-existing groups, none in theatre). Supplementary: `rg storyteller`
over `cmd/`+`internal/` → zero hits; no direct stdout in the theatre package;
`node --check intro.js` + the node harness 6/6; the frozen composer snapshot
byte-identical; the three-layer vocabulary grep present (intro.js registries +
style.css 75 hits); R1-01..R1-04 fixes verified in code, each regression test
asserting exactly what the phase file claims (the R1-01 test runs 400
composer-only theatres; the R1-03 test pins spawn counter + ledger + note;
the R1-04 test pins board/transcript agreement). Verdict: not ready to close
— one Medium finding (R2-01, feedback index) reopens phases 4 and 11. The
R1-01 fix serialized every *compose* path under `rndMu` but missed the
production path's generation-id draw: `openProduction` calls `newID(t.rnd)`
unlocked (`director.go:88`), so concurrent `Next` (compose, under `rndMu`) +
`Prepare` (production) still race on `math/rand`. Reproduced independently
with a scratch `-race` test (400 model-configured theatres, concurrent
`Next`+`Prepare` → `DATA RACE` in `math/rand.(*rngSource)` via `newID` at
`theatre.go:373` vs `ComposeThemed` at `floor.go:26`); the scratch test was
removed after the repro. The R1-01 regression test cannot catch it —
`newTestTheatre` is composer-only, so `openProduction` is never reached. The
R1-01 verdict's own guarantee ("every future caller of `agents.Teller` may
assume concurrent use is safe") is unmet on this path; the fix is spec'd in
phase 12. Cross-cutting observation: the facade owns exactly two draws from
`t.rnd` (compose and the generation-id draw) — the rnd serialization
invariant is now stated in full (strategy item 14) so no third draw can
appear unlocked.

**Review 1 — 2026-08-02 (worker: imago, holistic review session).**

Re-ran the phase-10 gates independently and reproduced the claimed green: `go run mvdan.cc/gofumpt@latest -l .` (zero diffs), `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go vet ./...` (clean), `go test ./... -race -cover -count=3 -timeout=30s` (all 23 packages ok on a quiet machine; coverage matches the recorded numbers — theatre 91.2%, tools 93.1%, model 95.6%), `go fix ./...` (clean), `go run github.com/mibk/dupl@latest -t 80 .` (27 pre-existing groups, none in theatre). Supplementary: `rg storyteller` over `cmd/`+`internal/` → zero hits; no direct stdout in `internal/agents/theatre/`; `node --check intro.js` + the node harness 6/6; the frozen composer snapshot byte-identical; the three-layer vocabulary grep present. Verdict: the gates are real and the implementation is faithful to the design — but the review found one genuine defect and three minor deviations (R1-01..R1-04, feedback index). R1-01 is a confirmed data race on `Theatre.rnd`, **pre-existing** (identical lock pattern in the pre-migration storyteller, preserved by phase 9's behavioral-equivalence contract) and latent in the current serve wiring (Warm seeds before serving); it reopens the facade's concurrency surface per the severity taxonomy (Medium > Low) and is routed to the phase-11 addendum. Cross-cutting observation: the `Teller` contract permits concurrent `Next`+`Prepare`, and every future caller of that contract inherits the R1-01 race until it is fixed — the fix belongs in the facade, not in callers.

## Feedback Index

| ID  | Severity | Phase | Status | Summary    |
| --- | -------- | ----- | ------ | ---------- |
| R1-01 | Medium | 4, 9 (fixed in P11) | ✅ Closed | Data race on `Theatre.rnd`: `Next` composes under `t.mu`, `Prepare`/`Warm` compose unlocked (pre-existing — carried over by the phase-9 migration; reproduced with `-race`) |
| R1-02 | Low | 3 (fixed in P11) | ✅ Closed | The wardrobe carries the `consult` tool contrary to the spec table and its own "You ask: nothing" prompt |
| R1-03 | Low | 2, 3 (fixed in P11) | ✅ Closed | A repeat consult answers from the table without ledger or transcript accounting, so telemetry undercounts consultations |
| R1-04 | Low | 3 (fixed in P11) | ✅ Closed | A post with an invalid `to` is kept on the board (addressee cleared) but dropped from the transcript — the two records diverge |
| R2-01 | Medium | 4, 11 (fixed in P12) | ✅ Closed | R1-01's fix is incomplete: `openProduction` draws the generation id from `t.rnd` via `newID(t.rnd)` without `rndMu`, so concurrent `Next` (compose) + `Prepare` (production) still race on `math/rand` — reproduced with `-race` on the model-configured path, which the R1-01 regression test never exercises. Fixed in phase 12: `newGenID` serializes the draw through `rndMu` |
| R3-01 | Medium | 4 (fixed in P13) | ✅ Closed | The serve-side 2-minute context cap in `prepareNextStory` (`index_handlers.go:348-354`) overrides `-theatreWallClock` (default 10m) for every HTTP-triggered generation: `runProduction` derives its deadline from the parent ctx, so consume/session-end generations are hard-cancelled at 2 minutes and the flag is inert on the common path. Reopens phase 4's wall-clock contract. Fixed in phase 13: the trigger passes `context.Background()` — the theatre's own gates are the only bound (D-P13-1); regression test `TestPrepareNextStory_LeavesWallClockToTheTheatre` fails on the pre-fix code |
| R3-02 | Low | 6 (fixed in P13) | ✅ Closed | A generation posting more than `BoardMaxEntries` (60) after the brief trims the brief off the board; `premiseFrom` then silently skips the premises doc at distill (same for `setsFrom`'s deliverable scan and `bulletinFrom`'s older decisions). Fixed in phase 13: the brief and the scenographer's dressed marker ride in the working file (D-P13-3), `premiseFrom` reads the out-of-band copy first and `setsFrom` checks `Dressed`; regression test `TestDistill_PremiseAndSetsSurviveBoardOverflow` with a 61+-entry board |
| R3-03 | Low | 2, 3 (fixed in P13) | ✅ Closed | The trailing "answer" call (`runner.go:287`) is counted against the actor's per-invocation budget, so a subagent that used its full tool budget displays "9/8 calls" in the phase line; the director's own trailing answer can push `DirectorUsed`/`GlobalUsed` one past their caps. Fixed in phase 13: the final answer is not a budgeted call — `RecordAnswer` updates the last action only, never a counter (D-P13-2); regression test `TestRunner_FullBudgetNeverShowsOverCap` pins 8/8 |
| R3-04 | Low | 6 (fixed in P13) | ✅ Closed | `trimRegistry` keeps hand-edited `Variants` without checking them against the species palette (docs.go:220), while `Canonize` restricts to the palette; an out-of-palette coat then surfaces in every working context and survives story validation (the player silently replaces it). Fixed in phase 13: the load gate drops out-of-palette variants and coats via `filterVariants` — load and canonize agree on what a coat may be (D-P13-4); regression test `TestRegistry_LoadDocDropsOutOfPaletteVariants` |
| R4-01 | Low | 2, 3 (filed in review 4) | ⬜ Open (does not reopen) | The global budget gate admits an invocation when `used < max` without reserving the invocation's own budget, so an invocation admitted near the cap finishes its budgeted calls past it — reproduced: GlobalUsed 195 + subagent (8) → ledger `global 203/200`. The dialog/ledger render `GlobalUsed/GlobalMax`, so the phase-13 “counters never exceed their caps” holds for `Actor.Calls`/`DirectorUsed` but not `GlobalUsed` (the `ledger.go` comment now states the tail-overshoot). Refusal semantics unaffected — no new work starts past the cap — so it is cosmetic telemetry, Low, and does not reopen; fix (reserve `inv.Budget` at the runner gate) spec'd in [phase 13](./phase-13-review-fixes.md)'s review section. Review 5 extends the overshoot bound: collaboration revisions re-run the budget (R5-02) |
| R5-01 | Low | 6, 13 (filed in review 5) | ⬜ Open (does not reopen) | The phase-13 “provably closed” pre-draft window is not closed: the proof counts one board entry per director call, but a consult posts question + answer and the consulted role posts up to its budget (up to 6 entries per director call), and the dramaturg's own 8-call budget can post 8 entries before the brief lands in the working file. Reproduced with a scratch test: dramaturg 7 notes + brief, 33 budget-respecting consults → board trimmed to 60, brief gone, `w.Brief` empty at draft-write time → the premise is silently skipped at distill, the exact R3-02 failure mode. Fix (checkbox): capture the brief at brief-post time (writeBrief/fallbackBrief), not at draft-write time — the board scan is a racy fallback |
| R5-02 | Low | 3, 13 (filed in review 5) | ⬜ Open (does not reopen) | A collaboration revision re-runs the actor's full budget: `resolveCollaborations` re-invokes `runOnce` with `inv.Budget` again, and every tool execution records into the same actor's `Calls` and `GlobalUsed`. Reproduced: dramaturg 16/8 calls after one round (24/8 after two) — the phase line and dialog summary show over-cap, the exact display R3-03 closed for the trailing answer, and `GlobalUsed` can tail-overshoot by up to (1+rounds)×budget + consulted budgets, exceeding R4-01's stated bound. Fix (checkbox): run the revision against the remaining per-invocation budget (`inv.Budget − calls already spent`) or account revisions separately |
| R6-01 | Low | 2, 3, 13 (filed in review 6) | ⬜ Open (does not reopen) | The phase line compares a generation-cumulative call counter against a per-invocation budget: `RecordCall` accumulates `Actor.Calls` across every invocation of a role, while `SetActorBudget` overwrites `Actor.Budget` per invocation, and `phaseBody` renders `Calls/Budget`. Any role invoked more than once — the director consults a role after its main invocation, or consults the same role twice — shows over-cap: reproduced with a scratch test — dramaturg main (8 calls, budget 8) + one consult (4 calls, budget 4) renders `dramaturg 12/4 calls`, the exact display R3-03 closed for the trailing answer, on a path R5-02's fix does not close (the revision-allowance fix bounds collaborations, not repeat consultations). The dialog summary is unaffected (renders `Calls` without a ratio). Fix (checkbox): make the phase line compare like with like — reset `Actor.Calls` at `SetActorBudget` (keep a separate cumulative counter), render the current invocation's calls, or drop the ratio after the first invocation; the fix belongs in `stage.go` |

| R7-01 | High | 4 (fixed in P14) | ✅ Closed | `production.finish` ships every readable `working.json` as the “last validated draft” without checking that the draft passed `validate_story`. If the playwright writes a playable draft and the director exhausts before `validate_story`, the unvalidated draft is persisted and shown instead of the composer floor or an earlier validated snapshot. Fixed in phase 14: an explicit `Working.Validated` flag — set only by `validate_story`, cleared by every draft-rewriting writer (`write_draft`, `write_scene`, `fallbackDraft`, `fallbackScene`) — gates the exhaustion branch; regression tests `TestTheatre_DraftOnlyExhaustionFallsToComposer` and `TestTheatre_ValidatedThenRewrittenDraftDoesNotShip` fail on the pre-fix code |
| R7-02 | High | 4, 6 (fixed in P14) | ✅ Closed | `submitStory` ignores the error returned by `Theatre.saveStory` (`theatre.go:346-380` logs only), then marks `working.json` submitted and distills the library. A story-file write failure can therefore report a successful submission and durable docs while `intro_story.json` is absent or stale. Fixed in phase 14: the atomic story writer returns its error (unlogged — callers act on it, D-P14-2) and `submit_story` aborts the submitted transition and distillation when it fails; regression test `TestTheatre_SubmitAbortsWhenStoryNotPersisted` (intro_story.json is a directory) fails on the pre-fix code |

## Session Journal

### 2026-08-03 — Phase 14 executed

Phase 14 (review-7 fixes, addendum) implemented by imago: R7-01 and R7-02,
the two High findings that reopened phases 4 and 6. R7-01 — the exhaustion
path shipped any readable draft, validated or not — is closed by an explicit
`Working.Validated` flag: `validate_story` is the only writer of the blessing,
and every draft-rewriting writer (`write_draft`, `write_scene`, `fallbackDraft`,
`fallbackScene`) clears it, so `finish`'s exhaustion branch ships exactly the
content that passed the gate (D-P14-1; the status label alone cannot carry it,
because `pin_identity` and `write_scene` re-label a validated file). R7-02 —
submit ignored story-persistence failure — is closed by making `saveStory`
return its error (the house "saves return errors unlogged" pattern, D-P14-2):
`Next`/`Prepare`/`Warm`/`finish` log it and keep serving from memory, while
`submit_story` aborts the submitted transition and distillation. Three
regression tests (two for R7-01, one for R7-02) fail on the pre-fix code; the
full QA gate is green, including `-race` and the pre-existing exhaustion,
wall-clock and composer-floor tests unchanged. Implementation notes, decisions
D-P14-1..2, and the validation table live in the phase file.

### 2026-08-03 — Review 7 (holistic review) performed

All 13 phases were marked complete, so this session performed another holistic
review. The independent gates were mixed: `go run mvdan.cc/gofumpt@latest -l .`,
`go vet ./...`, `go run honnef.co/go/tools/cmd/staticcheck@latest ./...`,
`go build ./...`, and the isolated `go test ./internal/agents/theatre -race
-cover -count=3 -timeout=30s` passed. The full `go test ./... -race -cover
-count=3 -timeout=30s` failed because the theatre package reached 30.336s and
hit the 30-second package timeout; a subsequent isolated run completed in
23.472s. This is a quality-gate timing regression/flake, but the two review
findings below are independent correctness defects.

R7-01 is High: `finish` treats any readable working file as the last validated
draft, although `writeDraft` leaves its status as `draft` until the separate
`validate_story` gate. A playwright can write a playable draft, exhaust the
director before validation, and bypass the promised composer-floor fallback.
R7-02 is High: `submitStory` calls the no-error-returning `saveStory`, which only
logs persistence failures, then marks the working file submitted and distills
docs. This violates the submit persistence boundary and can publish a success
paper trail for an absent or stale story. Both findings reopen their affected
phases under the severity taxonomy. Verdict: not ready to close.

### 2026-08-03 — Review 8 (holistic review) performed

All 14 phases were complete after phase 14 landed, so per the runbook this
session performed the eighth holistic review (worklog-review skill, round 8)
instead of starting a new phase. The gates were re-run independently and
reproduced green: gofumpt (zero diffs), vet, staticcheck, fix, build,
dupl (27 pre-existing clone groups, none in theatre), `node --check
intro.js` + the node harness 6/6, zero `storyteller` hits over
`cmd/`+`internal/`, no direct stdout in the theatre package. The full
`-race -cover -count=3 -timeout=30s` suite hit the two documented
30-second-window timing flakes under full-suite load (theatre 30.228s,
storage 30.165s — D-P10-2 marginal-wall-time family; both pass in
isolation: theatre 54.3s at 91.4% coverage with `-race -cover -count=3`,
storage 12.2s `-race -count=1`). The R1-01..R1-04, R2-01, R3-01..R3-04 and
R7-01/R7-02 fixes were verified in code and their regression tests re-run
green; the phase-14 `Validated` flag was traced through every writer
(`write_draft`, `write_scene`, `fallbackDraft`, `fallbackScene` all clear
it) and `saveStory`'s error crosses the submit boundary. The four open Lows
(R4-01, R5-01, R5-02, R6-01) were reproduced independently with scratch
tests (removed after the repro) and confirmed still open exactly as filed:
`global 11/10`, brief trimmed before the draft write (`w.Brief` empty),
playwright `9/8` calls after one collaboration round, `dramaturg 12/4
calls` phase line. None reopen per the taxonomy. No new findings. Verdict:
the work is ready; the board stays green with all 14 phases complete, and
the next session picks up R4-01/R5-01/R5-02/R6-01 if a future addendum
consolidates them (or any future Medium finding).

### 2026-08-02 — Review 6 (holistic review) performed

All 13 phases were complete after phase 13 landed, so per the runbook this
session performed the sixth holistic review (worklog-review skill, round 6)
instead of starting a new phase. The gates were re-run independently and
reproduced green: `go run mvdan.cc/gofumpt@latest -l .` (v0.11.0, zero
diffs), `go vet ./...` (clean), `go run
honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix ./...`
(clean), `go run github.com/mibk/dupl@latest -t 80 .` (27 pre-existing
clone groups, none in theatre), `go test ./... -race -cover -count=3
-timeout=30s` (all packages ok except the documented pre-existing D-P10-2
`cmd/classify` context-timeout flake — green in isolation under
`-race -count=3`; theatre 91.3%, tools 93.1%, model 95.6% — matching the
recorded numbers), `go test -race ./internal/agents/theatre/...
./internal/media/... ./cmd/serve/... ./cmd/debug/...` (green), the
targeted regression set (R3-01, the frozen composer snapshot, both race
tests, R3-02, R3-03, R3-04) green under `-race`, `node --check intro.js` +
the node harness 6/6, zero `storyteller` hits, no direct stdout in the
theatre package. The R1-01..R1-04, R2-01 and R3-01..R3-04 fixes were
verified in code and under `-race`, and each phase-13 fix was traced
through its branches; the three open Lows (R4-01, R5-01, R5-02) were
confirmed still open as filed. One new finding filed — R6-01 (Low: the
phase line compares a generation-cumulative `Actor.Calls` against the most
recent invocation's per-invocation `Actor.Budget`, so any role invoked more
than once — the director consults a role after its main invocation, or
consults the same role twice — renders over-cap, e.g. `dramaturg 12/4
calls`, the exact display R3-03 closed for the trailing answer, on a path
R5-02's fix does not close; reproduced with a scratch test, removed after
the repro). Low does not reopen per the taxonomy, so the board stays green:
the finding is filed with a checkbox fix in the phase-13 review section and
the invariant is promoted to strategy item 19. The next session picks up
R4-01/R5-01/R5-02/R6-01 if a future addendum consolidates them (or any
future Medium finding).

### 2026-08-02 — Review 5 (holistic review) performed

All 13 phases were complete after phase 13 landed, so per the runbook this
session performed the fifth holistic review (worklog-review skill, round 5)
instead of starting a new phase. The gates were re-run independently and
reproduced green (gofumpt, vet, staticcheck, fix, dupl 27 pre-existing
groups, full-suite `-race -cover -count=3` all 23 packages ok — theatre
91.3%, tools 93.1%, model 95.6% — the targeted regression set green under
`-race -count=3`, node syntax + harness 6/6, frozen composer snapshot
byte-identical). The R1-01..R1-04 and R2-01 fixes were verified in code and
under `-race`, and each phase-13 fix was traced through its branches. Two
findings filed — R5-01 (Low: the phase-13 “provably closed” pre-draft brief
window is not closed — a consult posts question + answer and the consulted
role posts up to its budget, and the dramaturg's own 8-call budget can post
8 entries, so a budget-respecting generation trims the brief before the
playwright's draft write; reproduced with a scratch test — brief gone,
`w.Brief` empty) and R5-02 (Low: a collaboration revision re-runs the
actor's full budget — reproduced dramaturg 16/8 calls after one round, so
the phase line and dialog summary show over-cap again and `GlobalUsed` can
overshoot by up to (1+rounds)×budget). Low does not reopen per the
taxonomy, so the board stays green: both findings are filed with checkbox
fixes in the phase-13 review section, the affected invariants are promoted
to strategy items 17 and 18, and R4-01's bound note is extended. The next
session picks up R5-01/R5-02 if a future addendum consolidates them (or any
future Medium finding).

### 2026-08-02 — Review 4 (holistic review) performed

All 13 phases were complete after phase 13 landed, so per the runbook this
session performed the fourth holistic review (worklog-review skill, round 4)
instead of starting a new phase. The phase-13 gates were re-run independently
and reproduced green (gofumpt, vet, staticcheck, fix, dupl 27 pre-existing
groups, full-suite `-race -cover -count=3` — only the documented D-P10-2
`cmd/classify` context-timeout flake, green in isolation — and dedicated
`-race` runs of theatre/media/serve; theatre 91.3%, tools 93.1%). The
R1-01..R1-04 and R2-01 fixes were verified in code and under `-race`, and
each phase-13 fix was traced through its branches. One finding filed —
R4-01 (Low: the global budget gate admits an invocation without reserving
its own budget, so an invocation admitted near the cap finishes past it —
reproduced `global 203/200`; cosmetic telemetry, refusal semantics
unaffected). Low does not reopen per the taxonomy, so the board stays green:
the finding is filed with a checkbox fix in the phase-13 review section, the
`ledger.go` comment was corrected to state the tail-overshoot, and the
global-budget-gate invariant is promoted to strategy item 16. The next
session picks up R4-01 if a future addendum consolidates it (or any future
Medium finding).

### 2026-08-02 — Phase 13 (review-3 fixes) executed

Phase 13 (the review-3 addendum) implemented by imago, closing all four
findings R3-01..R3-04. Each fix lands with its regression test: R3-01 drops
the serve-side 2-minute cap in `prepareNextStory` (the trigger now passes
`context.Background()`, so `-theatreWallClock` is the only deadline on
HTTP-triggered generations; the regression test pins that `Prepare` receives
no caller-side deadline and fails on the pre-fix code with the review's
exact `1m59.99s` cap, D-P13-1); R3-02 carries the brief and the
scenographer's dressed marker out of band in the working file (`Brief`
captured at draft-write time, `Dressed` set only by the scenographer paths),
so a board overflow can never silently lose a generation's premise
(D-P13-3); R3-03 makes the final answer a non-budgeted call — a new
`Stage.RecordAnswer` updates the last action only, never `Actor.Calls`,
`DirectorUsed` or `GlobalUsed`, so the phase line and the dialog summary
never show over-cap (D-P13-2); R3-04 runs the registry load gate through the
same species-palette check as `Canonize` — `filterVariants` drops
out-of-palette variants and an out-of-palette coat degrades to unpinned
(D-P13-4). Three runner/broker tests that counted the answer as a call now
count tool executions (the questioner-specific spawn test counts LLM
prompts instead). QA: gofumpt clean, vet clean, staticcheck clean, fix
clean, dupl 27 pre-existing groups (none in touched files), `go test -race
./internal/agents/theatre/...` green (theatre 91.3%, tools 93.1%),
`-race -count=3` on the package green (18.5s), `go test -race
./internal/media/... ./cmd/serve/...` green, full suite `-race -cover
-count=3 -timeout=30s` green except the documented pre-existing D-P10-2
`cmd/classify` context-timeout flake (passes in isolation, untouched); the
frozen composer snapshot stays byte-identical. All four feedback-index rows
closed; the phase file records the decisions, the validation table and the
pre-fix repro.

### 2026-08-02 — Review 3 (holistic review) performed

All 12 phases were complete, so per the runbook this session performed the
third holistic review instead of starting a new phase (worklog-review skill,
round 3). The phase-12 gates were re-run independently and reproduced green
(gofumpt, vet, staticcheck, fix, dupl 27 pre-existing groups, full-suite
`-race -cover -count=3` — only the documented D-P10-2 `cmd/classify`
context-timeout flake, green in isolation — and a dedicated `-race -cover
-count=3` theatre run at 91.2%/93.1%); the node harness is 6/6 and the frozen
composer snapshot test is present. The R1-01..R1-04 and R2-01 fixes were
verified in code with their regression tests, and the `rndMu` invariant holds
on both of the facade's draws. Four findings filed — R3-01 (Medium: the
serve-side 2-minute context cap in `prepareNextStory` overrides
`-theatreWallClock` on HTTP-triggered generations; reopens phase 4), R3-02
(Low: board overflow silently drops the premise at distill), R3-03 (Low: the
trailing "answer" call can show an actor over its budget), R3-04 (Low: the
registry load gate trusts hand-edited variant lists). Per the severity
taxonomy, Medium reopens; the fixes are consolidated into the phase-13
addendum (spec'd in this review), the affected board rows point at it, and
the cross-phase wall-clock invariant is promoted to strategy item 15. The
next session picks up phase 13.

### 2026-08-02 — Phase 12 (review-2 fixes) executed

Phase 12 (the review-2 addendum) implemented by imago, closing R2-01 — the
incomplete R1-01 fix. `openProduction` drew the generation id from `t.rnd`
unlocked while every compose path drew under `rndMu`; the fix adds a locked
`newGenID` helper (lock, draw, unlock — never held across the production) and
routes the production's draw through it, so both of the facade's draws from
the random source are serialized (D-P12-1). TDD: the regression test
(`TestTheatre_ConcurrentNextAndPrepareProductionSafely`, 400 model-configured
theatres with a terminal-text scripted `runLLM`, concurrent `Next`+`Prepare`)
was written first and fails on the pre-fix code with the exact `DATA RACE`
the review reproduced (`newID` vs `ComposeThemed` on `math/rand.(*rngSource)`),
then passes with the fix. The frozen composer snapshot stays byte-identical.
While landing the test, the R1-01 test's TempDir-cleanup flake surfaced (its
400 background `saveStory` goroutines raced the tempdir teardown, D-P10-2
family); `Next`'s fire-and-forget write is now tracked in a `writeWG` and both
race tests wait on it (D-P12-2). QA: gofumpt clean, vet clean, staticcheck
clean, fix clean, `go test -race ./internal/agents/theatre/...` green (theatre
91.2%, tools 93.1% — matching the phase-11 numbers), `-race -count=3` on the
two race tests and the full package green, dupl 27 pre-existing groups (none
in theatre). The phase file records the decisions and the exact commands;
AGENTS.md now states the serialization invariant on the facade insight.

All phases 1–11 were complete, so per the runbook this session performed the
second holistic review instead of starting a new phase (worklog-review skill,
round 2). The gates from phase 11 were re-run independently and came out
green (see the Review 2 decisions entry for exact commands and results); the
R1-01..R1-04 fixes were verified in code and their regression tests confirmed
to assert what the phase file claims. One finding filed — R2-01 (the R1-01
fix is incomplete: `openProduction` draws the generation id from `t.rnd`
without `rndMu`, racing every compose path; reproduced with a scratch
`-race` test on the model-configured path, since the R1-01 regression test
only covers composer-only mode) — Medium, with a precise reference, a failure
scenario and a checkbox fix. Per the severity taxonomy, Medium reopens; the
fix is routed to the phase-12 addendum (one fix, one regression test, local
to the facade), and the affected board rows point at it. Full finding lives
in the review sections of the phase-4 and phase-11 files and the phase-12
spec; the next session picks up phase 12.

### 2026-08-02 — Phase 11 (review-1 fixes) executed

Phase 11 (the review-1 addendum) implemented by imago, closing all four
findings R1-01..R1-04. Each fix is small, independent and lands with its
regression test, written first and confirmed to fail on the pre-fix code:
R1-01 gets a dedicated `rndMu` serializing every compose path through a
`compose` helper (`Theatre.rnd` race reproduced under `-race`, then fixed;
the frozen composer snapshot stays byte-identical, D-P11-1); R1-02 drops
`consult` from the wardrobe's tool set (post_to_board/read_board/advise only,
D-P11-2's sibling); R1-03 accounts a consultation-table hit in the ledger and
notes it on the transcript without re-spawning (D-P11-3); R1-04 validates `to`
in `postToBoard` the way `kind` is validated, so board and transcript agree on
every accepted post. QA: gofumpt clean, vet clean, staticcheck clean, fix
clean, dupl 27 pre-existing groups (none in theatre), `go test -race
./internal/agents/theatre/...` green, full suite `-race -cover -count=3
-timeout=30s` green except the documented pre-existing `internal/media/watcher`
`Test_walkDo` timing flake (D-P10-2, untouched, passes in isolation); theatre
coverage 91.1%, tools 93.1%. All four feedback-index rows closed; the phase
file records the decisions and the exact commands.

### 2026-08-02 — Review 1 (holistic review) executed

All ten phases were already complete, so per the runbook this session performed the
holistic review instead of starting a new phase (worklog-review skill, round 1). The
gates from phase 10 were re-run independently and came out green (see the Review 1
decisions entry for exact commands and results). Four findings filed — R1-01
(`Theatre.rnd` data race, Medium, pre-existing), R1-02 (wardrobe consult tool, Low),
R1-03 (repeat-consult accounting, Low), R1-04 (invalid-to post divergence, Low) —
each with a precise reference, a failure scenario and a checkbox fix. Per the
severity taxonomy, R1-01 (Medium) reopens; the fixes are consolidated into the
phase-11 addendum (they are small, mostly independent and cross phases 2/3/4/9),
and the affected board rows point at it. Full findings live in the review sections
of the phase files; the next session picks up phase 11 — which landed the same day (see the phase-11 entry above).

### 2026-08-02 — Phase 10 executed

Phase 10 (quality gate) implemented by imago: all six gates from AGENTS.md ran
over the full tree and came out green — gofumpt (zero diffs), staticcheck
(clean), vet (clean), `go test ./... -race -cover -count=3 -timeout=30s`
(all 23 packages ok), fix (clean), dupl (27 pre-existing clone groups, none in
touched files) — plus `make qa` (exit 0, a second consecutive green full
suite). The first full-suite run caught a real in-scope defect: the theatre
package itself took 31.08s under the gate's flags, over its 30s per-package
timeout (D-P10-1). The CPU profile blamed the two file-I/O-bound fallback seed
sweeps in `fallback_test.go`; trimming 400→250 and 200→120 seeds kept
coverage at 91.1% and every template drawn ~30×/~15× per run, and the package
now finishes 16.3s idle / 21.2s under full-suite load. The other four
non-green runs each tripped one pre-existing, untouched, isolation-passing
timing test (`cmd/classify` context-timeout, `internal/media`
`TestEventStreamAndSuggestions`, `internal/media/watcher` `Test_walkDo`, and
the `internal/media/storage` package's marginal 28.2–30.6s wall time) —
documented with references per the error-coverage table, not fixed (D-P10-2).
Per-package coverage recorded for every touched package against the 2026-07-25
baseline: no regressions (model +0.5, serve +1.0, media +1.5, theatre +0.5 vs
phase 6; llm flat). Supplementary checks re-run: zero `storyteller` hits,
ancli-only theatre stdout, node syntax + harness, frozen composer snapshot
byte-identical, three-layer vocabulary grep. Implementation notes, decisions
D-P10-1..2, the dupl triage and the full run ledger live in the phase file.

### 2026-08-02 — Phase 9 executed

Phase 9 (storyteller removal) implemented by imago: the deterministic floor
moved wholesale into the theatre — `composer.go` → `floor.go`, `staging.go`,
`muse.go` (duplicate `newID`/`idAlphabet`, `Muse`/`MuseFunc` and `pick`
definitions collapsed to the single ones), the composer tests extracted from
the old `storyteller_test.go` into `floor_test.go`, and the old LLM teller
deleted (the facade already reimplemented it; every teller-level test has a
`TestTheatre_*` counterpart). The `Teller` contract moved to
`internal/agents/interfaces.go` as `agents.Teller` with a compile-time
assertion on `*Theatre`; `index.go` wires `WithTheatre`; serve flags renamed
`-storyteller*` → `-theatre*`; `cmd/llm` attribution now labels theatre
sessions "theatre"; comments and AGENTS.md updated; `rg storyteller` over
`cmd/` and `internal/` returns zero hits. The composer-only path is proven
byte-identical by the frozen snapshot
(`testdata/composer_snapshot.json`, captured pre-migration, seeds
1/2/3/5/8/13/21/34/55/89/144/233, theme "The Great Mouse Hunt"). A
`-race` finding in the phase-2 feed test harness (ancli globals swapped while
the feed goroutine emitted) was fixed: the globals are now pinned before any
feed activity. Implementation notes, decisions D-P9-1..4, and the validation
table live in the phase file. All QA green for the touched packages (build,
tests incl. race ×3, vet, staticcheck, gofumpt, fix, dupl, node checks,
ancli-only grep, zero-hit rg); the full-suite race runs hit pre-existing
load-sensitive flakes in `cmd/classify` and `internal/media` while the
machine ran at load ~108/8 cores — each flaky test passes `-race -count=5`
in isolation, and phase 10 should re-run the full suite idle.

### 2026-08-02 — Planning session (setup only)

Interview outcomes recorded as decisions D1–D12 and the phase list above. No code
written. The user's asks captured in the phases:

- More variety → phases 7 (vocabulary) and 8 (bird).
- Character consistency → phase 6 costumer registry + soft continuity (D6, D7).
- Higher quality → director revision loop (phase 4) and scope prompts (phase 5).
- Cross-agent collaboration → board + consultation broker (phases 1, 3).
- Self-developing library → per-role persistence + bulletin (phase 6).
- Observability → transcript, ancli feed, ledger, SSE, telemetry (phase 2).
- House logging → the feed goes through `ancli` with a `[theatre <gen>]` prefix
  (phase 2 update).
- System naming → the system is `theatre` (D13).

### 2026-08-02 — Phase 1 executed

Phase 1 (company substrate) implemented by imago: new package
`internal/agents/theatre/` with the board, working file, ledger, transcript,
`AssembleContext` working-context standard, atomic persistence helper and doc
validation. Implementation notes, decisions D-P1-1..9, and the validation table
live in the phase file. AGENTS.md package map gained the `theatre/` entry.
All QA green (tests, race, vet, staticcheck, gofumpt, dupl, 92.6% coverage).

### 2026-08-02 — Phase 2 executed

Phase 2 (observability) implemented by imago: the feed goroutine
(`feed.go` — one ancli line per event, `[theatre <gen>]` prefix, ancli-only),
the stage-manager wrapper (`stage.go` — single-writer transcript, phase lines,
ledger telemetry: per-role calls/tokens/consults/hop depth, submit/fail
summaries, SSE log sink), the dialog renderer (`dialog.go`), and the
`kinoview debug production <genID>` subcommand. Implementation notes,
decisions D-P2-1..8, and the validation table live in the phase file. AGENTS.md
gained the stage/feed/dialog entries and the single-writer observability
insight. All QA green (tests incl. race, vet, staticcheck, gofumpt, dupl,
93.3% coverage, ancli-only grep check, live smoke test).

### 2026-08-02 — Phase 3 executed

Phase 3 (subagent infrastructure) implemented by imago: the mini-agent runner
(`runner.go` — bounded clai loops through a `runLLM` seam, budget/deadline
gates, session logs, ledger telemetry, SSE streaming, panic recovery, the
deterministic-fallback seam), the consultation broker (`broker.go` — hop cap
2, repeat-consult table, budget ledger, board+transcript posts), the
collaborations flow (`collab.go` — deliverable envelope, at most 2 rounds of
consult-and-revise), the role prompts + per-role tool wiring (`roles.go`,
phase-5 seam) and the `tools/` subpackage (eight callback-driven tools with
message-string error contracts). Implementation notes, decisions D-P3-1..10,
and the validation table live in the phase file. AGENTS.md gained the
runner/broker/collab/roles/tools entries and a subagent-infrastructure
insight. All QA green (tests incl. race, vet, staticcheck, gofumpt, dupl,
91.2% theatre + 92.3% tools coverage, ancli-only grep check).

### 2026-08-02 — Phase 4 executed

Phase 4 (director orchestrator + teller rewiring) implemented by imago: the
`Theatre` facade (`theatre.go` — `Next`/`Prepare`/`Warm` with the storyteller's
cooldown, single-flight, loadFromDisk and atomic-save semantics preserved), the
director superagent (`director.go` — production struct, `runProduction`,
`finish` resolution, the nine-tool set, the production-flow prompt, the
working-file gates), the runner's director seam, the seven director tools in
`tools/director.go`, the `loghandler.Print` extraction (the session sink),
serve flags `-storytellerMaxCalls`/`-storytellerGlobalCalls`/`-storytellerWallClock`
and the serve rewiring to `theatre.New`. Decisions D-P4-1..9 and the
validation table live in the phase file. AGENTS.md gained the director/facade
entries and a director-superagent insight. All QA green (tests incl. race,
vet, staticcheck, gofumpt, fix, dupl, 90.0% theatre + 93.5% tools coverage,
ancli-only grep check).

### 2026-08-02 — Follow-up: replace the storyteller

Decision D13 updated: the theatre replaces `internal/agents/storyteller/` rather
than importing it. Added phase 9 (storyteller removal — migrate the floor, move
`Teller` to `agents/interfaces.go`, delete the package, re-point references,
update AGENTS.md) and renumbered the quality gate to phase 10. The composer-only
path must be behaviorally identical before and after the migration (snapshot test
in phase 9).

### 2026-08-02 — Phase 5 executed

Phase 5 (roles and prompts) implemented by imago: the four production roles in
full — three-section scope prompts (decides / asks / stops, compile-time
constants), the artifact schemas validated at the writer boundary
(`artifacts.go`: brief, draft-report, scene-report), the costumer registry
(`registry.go`: permanent-cast seed, pin/apply, lookup) and the deterministic
floors (`fallback.go`: composer draft into the working file, registry advice,
in-place answers for consulted roles). The playwright's draft report is stored
beside the draft and its acts supersede the derived count; canon facts
round-trip through the working file; the scenographer's floor reuses the
composer's staging rules via the exported `storyteller.DressDraft`; a playwright
that never produces a playable draft is answered by the composer draft with a
warning note. Implementation notes, decisions D-P5-1..10, and the validation
table live in the phase file. AGENTS.md gained the artifacts/fallback/registry
entries and a scoped-roles key insight. All QA green (tests incl. race, vet,
staticcheck, gofumpt, fix, dupl, 89.5% theatre coverage, ancli-only grep check).

### 2026-08-02 — Phase 6 executed

Phase 6 (persistence and the self-developing library) implemented by imago: the
six durable company docs (`docs.go` — premises, repertoire, sets, registry,
director lessons, bulletin; each atomic-written, validated on load, trimmed to
its cap oldest-first), the durable registry (`registry.go` — canonical coat/species
seeds per the spec table, `Canonize` as the only place identities are born,
explicit director approval at submit, registry.json round-trip at startup),
submit-time distillation (`distill.go` — deterministic extraction from the board
+ working file into all six docs, missing artifacts skip their doc, count-bumped
set recipes), the submit gate carrying notes + characters
(`submit_story(notes, characters)`), per-role doc context injection
(`withDocsContext`), the dramaturg floor's premises no-repeat list, and the
feed-drain fix in `stage.go` (no goroutine outlives a generation).
Implementation notes, decisions D-P6-1..9, and the validation table live in the
phase file. AGENTS.md gained docs/distill entries and a self-developing-library
insight. All QA green (tests incl. race ×3, vet, staticcheck, gofumpt, fix,
dupl, 90.6% theatre + 93.1% tools coverage).

### 2026-08-02 — Phase 7 executed

Phase 7 (vocabulary expansion) implemented by imago: the hand-built art walls
(decision D9) raised in all three layers — `internal/model/story.go` gains
kitchen/forest/rain backdrops, fireplace/bookshelf/door/log pieces, ball/bone/
cushion/bowl props and yawn/sniff/jump actions (jump joins the target-required
set, sniff stays optional); `cmd/serve/frontend/intro.js` gains the player
registries and the three actions plus the `clampStage`/`targetPx` helpers
(stareoff now faces props too); `cmd/serve/frontend/style.css` gains the art,
the three action animations and the low-perf kill rules; the composer gains
three new templates (`midnightsnack`, `birdwatching`, `snowed-in`) and
per-backdrop dresser lists so the new pieces actually stand, with bone/cushion
props added to existing templates so every render path is proven; the theatre's
`write_scene` backdrop list and the wardrobe floor's `backdropNames` follow.
Implementation notes, decisions D-P7-1..7, and the validation table live in the
phase file. All QA green (tests incl. race ×3, vet, staticcheck, gofumpt, fix,
dupl with 5 pre-existing clone groups, node syntax check, three-layer grep,
DUMP_STORIES visual dump of the new templates).

### 2026-08-02 — Phase 8 executed

Phase 8 (the bird) implemented by imago: the fourth species in all four
layers — `bird` in `ValidCharacters`; player art, perch elevation (option (a),
D-P8-1), four coats and the highest/shortest formant voice with a 3-note
rising-middle chirp (`burstPitch` seam, D-P8-6); CSS art and the hop gait
(no bird-leg walking class, D-P8-4); the composer's `birdvisit` scene
(pip perches, teases the cat with a hop that lands short, flies off, D-P8-5);
the registry's permanent `pip`/`chaffinch` (D-P8-3) with single-word coat
variants (D-P8-2) and a bird-aware wardrobe floor. A plain Node player harness
(`cmd/serve/frontend_test/intro.test.js`, D-P8-7) asserts the perch height,
the species-base scale, the hop gait and the chirp schedule at the JS seams.
Implementation notes, decisions D-P8-1..7, and the validation table live in
the phase file. All QA green (tests incl. race ×3, vet, staticcheck, gofumpt,
fix, dupl with 5 pre-existing clone groups, node syntax checks, the node
harness 6/6, three-layer grep, DUMP_STORIES visual dump of the bird scene).

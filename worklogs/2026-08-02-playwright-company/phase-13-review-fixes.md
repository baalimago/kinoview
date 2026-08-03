# Phase 13 — Review-3 Fixes (addendum)

**Status:** ✅ Complete | [README](./README.md)

## Goal

Close the findings of review 3 (2026-08-02). The review re-ran the phase-12
gates independently, verified the R1-01..R1-04 and R2-01 fixes in code and
found one Medium finding plus three Low findings. This is an addendum phase:
the fixes are small and cross the phases the review annotated — 4 (wall-clock
enforcement), 2/3 (call accounting), 6 (distillation and the registry load
gate) — so they are consolidated here instead of reopening three phase files.
The original board rows point at this phase.

## Work items (one checkbox per finding; close in order)

- [x] **R3-01 — the serve-side 2-minute context cap overrides
      `-theatreWallClock` for HTTP-triggered generations (Medium).**
      `internal/media/index_handlers.go:348-354` (`prepareNextStory`) wraps
      every `Prepare` in `context.WithTimeout(context.Background(),
      2*time.Minute)`, and `internal/agents/theatre/director.go`
      `runProduction` derives its deadline with
      `context.WithTimeout(ctx, t.wallClock)` — the earlier parent deadline
      wins. So a generation triggered by story consumption or session end is
      hard-cancelled at 2 minutes, whatever `-theatreWallClock` (default 10m)
      says; only the startup `Warm` path gets the flag's full value. Raising
      the flag above 2 minutes does nothing on the common trigger path, and
      the broker's deadline gate (`stage.WallDeadline = now + wallClock`)
      never gets exercised there. Failure mode is graceful (cancelled LLM
      calls fall back; the last validated draft ships), but the flag's
      contract is unmet and the advertised ~10-minute window is inert.
      Fix (option a, preferred): drop the 2-minute cap and let the theatre's
      own budget gates bound the goroutine — `Prepare` is already bounded by
      the production's wall clock and single-flight. Option b: derive the
      serve-side timeout from the theatre's configured wall clock plus a
      margin (e.g. `theatreWallClock + 1m`) so the flag remains the
      authority. Either way the fix belongs in the serve wiring, with the
      theatre's wall-clock gate untouched. Add a test that pins the trigger
      path's effective deadline to the flag (or document the bound in the
      flag help). → **Fixed: option a** — `prepareNextStory` passes
      `context.Background()`; the regression test
      (`TestPrepareNextStory_LeavesWallClockToTheTheatre`,
      `internal/media/index_handlers_test.go`) pins that `Prepare` receives
      no caller-side deadline (fails on the pre-fix code with
      `deadline 1m59.99s from now`).

- [x] **R3-02 — a generation that overflows the board loses its premise at
      distill, silently (Low).** `internal/agents/theatre/distill.go`
      `premiseFrom` walks `board.Entries` backwards for the last "brief"
      entry, but `Board.Append` caps the board at `BoardMaxEntries` (60,
      constants.go). A chatty generation posting 60+ entries after the brief
      trims the brief off the board, so `premiseFrom` returns "no brief" and
      the premises doc is not updated for that generation — no warning, no
      entry. The same trim silently skips `setsFrom`'s scenographer
      deliverable scan and `bulletinFrom`'s older decisions.
      Fix: carry the brief (and the scenographer deliverable) out of band —
      e.g. record them in the working file or a distillable field at write
      time — or emit a warning note when a distill input was trimmed off the
      board. The premises doc must not silently lose a generation.
      → **Fixed: out of band in the working file.** `Working` gains `Brief`
      (captured from the board at draft-write time by `writeDraft` and the
      playwright floor) and `Dressed` (set only by the scenographer's
      `writeScene` and floor). `premiseFrom` reads `w.Brief` first (the board
      scan remains the fallback for older files); `setsFrom` checks
      `w.Dressed` instead of scanning the board. The bulletin's rolling cap
      is the doc's own trimming, not a per-generation loss, and is left
      untouched.

- [x] **R3-03 — the trailing "answer" call can push an actor over its
      per-invocation budget in the phase line and the global budget past its
      cap (Low).** `internal/agents/theatre/runner.go:287`
      (`r.stage.RecordCall(role, "answer")`) counts the final answer as a
      call on top of every tool execution, so a subagent that used its full
      `WithMaxToolCalls` budget (8) displays "9/8 calls" in the phase line
      (`stage.go` `phaseBody`), and the director's own trailing answer can
      push `DirectorUsed`/`GlobalUsed` one past their caps.
      Fix: keep the answer out of the per-invocation denominator — either
      stop counting it against the actor's `Calls` (record it against the
      global only), or display `min(calls, budget)` — and decide once whether
      the final answer is a budgeted call. Telemetry must never show an actor
      over its cap. → **Fixed: the final answer is not a budgeted call.** A
      new `Stage.RecordAnswer` accounts the answer's `LastAction` and status
      only — not `Actor.Calls`, not `DirectorUsed`, not `GlobalUsed` (all
      three would show over-cap in the phase line or the dialog summary; the
      finding itself lists `DirectorUsed`/`GlobalUsed` one past their caps as
      part of the bug). The answer's cost stays visible in the token
      telemetry. See D-P13-2 for the deviation from the "global only" option.

- [x] **R3-04 — the registry load gate trusts hand-edited variant lists (Low).**
      `internal/agents/theatre/docs.go:220` (`trimRegistry`) keeps
      `e.Variants` from the file (pattern-checked, capped at 8) without
      checking them against the species palette, while `Canonize`
      (registry.go) restricts variants to `speciesVariants`. A hand-edited or
      stale registry.json can therefore surface out-of-palette coats in every
      working context ("— wardrobe: pink") and the wardrobe's answers; a
      playwright may then write that coat, which passes `model.Story.Validate`
      (coats are checked against the id pattern only, story.go:294-297) and
      is silently replaced by a random palette coat in the player (intro.js
      `def.coats[spec.coat] || def.coats[pick(coatNames)]`).
      Fix: run the load gate through the same palette check as `Canonize`
      (drop variants not in `speciesVariants`), so load and canonize agree on
      what a coat may be. → **Fixed: the load gate and canonize now share the
      palette.** `trimRegistry` drops out-of-palette variants via a new
      `filterVariants` (palette filter + dedupe + `variantCap`) and degrades
      an out-of-palette coat to unpinned, exactly the check `Canonize` runs
      at approval.

## Acceptance criteria

- [x] `go test -race ./internal/agents/theatre/...` and `go test -race
      ./cmd/serve/...` green, including the new R3-01 regression test (fails
      on the pre-fix code). (The regression test lives in
      `internal/media/index_handlers_test.go`, where `prepareNextStory`
      lives; `./internal/media/...` is green too.)
- [x] A subagent that exhausts its tool budget never shows more calls than
      its budget in the phase line
      (`TestRunner_FullBudgetNeverShowsOverCap` — 8/8, never 9/8).
- [x] A registry.json hand-edited with an out-of-palette variant loads with
      that variant dropped
      (`TestRegistry_LoadDocDropsOutOfPaletteVariants` — also covers an
      out-of-palette coat).
- [x] The premise doc is updated even when the generation's board exceeds
      `BoardMaxEntries`
      (`TestDistill_PremiseAndSetsSurviveBoardOverflow` — 61+ entries, brief
      and scenographer deliverable trimmed off the board, premises and sets
      still distill; the working-file capture is pinned by
      `TestTheatre_FixtureProductionRunsFlow` and the round-trip by
      `TestLoadWorking_BriefAndDressedRoundTrip`).
- [x] All gates from AGENTS.md green over the touched packages; README status
      board, feedback index and session journal updated (findings closed,
      phase complete).

## Error coverage

| Failure | Expected outcome |
|---|---|
| The R3-01 fix removes the goroutine bound entirely | `Prepare`'s own wall-clock gate and single-flight still bound the goroutine; the serve-side test pins the effective deadline — `TestPrepareNextStory_LeavesWallClockToTheTheatre` asserts no caller-side deadline (zero on `ctx.Deadline()`) |
| The R3-02 fix adds a field the working file rejects | `Working.normalize` trims/caps `Brief`; `TestLoadWorking_BriefAndDressedRoundTrip` pins the round-trip and the pre-fix file shape (both fields `omitempty`); old working files still load |
| The R3-03 fix double-counts or drops a real call | the ledger totals stay equal to tool executions (answers are not budgeted calls); the phase-line test pins the display; the consulted-role tests now count spawns via the LLM prompts, not the call counter |
| The R3-04 fix breaks a canonized entry's variants | `Canonize` already stores palette variants; the load gate matches it, so the canonize/load round-trip tests stay green |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 13 — the review-3 addendum of
the playwright-company worklog). All four findings R3-01..R3-04 are closed;
each fix lands with its regression test. The work is strictly additive to the
phase-12 state: the frozen composer snapshot is byte-identical, the two
facade draws from `t.rnd` still run under `rndMu`, and the wall-clock gate in
`runProduction` is untouched (the fix is in the serve wiring, as spec'd).

**Delivered:**

| Item | What landed |
|---|---|
| R3-01 — wall-clock authority | `internal/media/index_handlers.go` `prepareNextStory` passes `context.Background()` — no caller-side cap, so `-theatreWallClock` is the only deadline on HTTP-triggered generations. `Prepare` is bounded by the production's wall clock (`runProduction`'s `context.WithTimeout(ctx, t.wallClock)`), the single-flight slot and the call budgets. New `TestPrepareNextStory_LeavesWallClockToTheTheatre` (`index_handlers_test.go`) records the context a stub `agents.Teller` receives and asserts no deadline — fails on the pre-fix code (`deadline 1m59.99s from now`), green under the fix |
| R3-02 — distill inputs out of band | `Working` gains `Brief string` (the dramaturg's brief the draft was written from, captured at draft-write time by `writeDraft` and `fallbackDraft` via a new `Runner.boardBrief()`) and `Dressed bool` (set only by the scenographer's `writeScene`/`fallbackScene`). `premiseFrom(board, w, now)` reads `w.Brief` first — the board scan stays the fallback for older files — and `setsFrom(w, now)` checks `w.Dressed`; the board param dropped. `Working.normalize` trims/caps the brief at `EntryMaxBody`, the same bound the board applies. The pre-draft overflow window was claimed provably closed by the budgets (brief entry 1, at most ~57 entries before the playwright's draft write); review 5 (R5-01) disproved the claim — consults and consulted-role posts can exceed `BoardMaxEntries` before the draft write — so the out-of-band capture should move to brief-post time in a future addendum |
| R3-03 — the answer is not a budgeted call | `runOnce` calls a new `Stage.RecordAnswer(role)` instead of `RecordCall(role, "answer")`: the answer updates the actor's `LastAction`/status and `lastRole` but no call counter, so the phase line (`Calls/Budget`) and the dialog summary (`DirectorUsed`, `GlobalUsed`) can never show over-cap. `RecordCall`'s and `Budget`'s docs now state that the budgets cap `WithMaxToolCalls` executions |
| R3-04 — the load gate shares the palette | `trimRegistry` drops variants outside `speciesVariants(species)` via a new `filterVariants` (palette filter, dedupe, `variantCap` 8) and degrades an out-of-palette coat to unpinned — the exact check `Canonize` runs at approval, so load and canonize agree on what a coat may be |
| Tests updated for the new semantics | the three runner/broker assertions that counted the answer as a call now count tool executions (`runner_test.go` writer-tool tests 2→1, director submit 2→1, consulted wardrobe 1→0 — the questioner-specific spawn test counts LLM prompts instead of ledger calls); `distillFixtureProduction` sets `Dressed`; the fixture-flow test pins the working brief |

**Material decisions (recorded for chronology):**

- **D-P13-1 — the trigger path passes a context without a deadline (R3-01,
  option a).** The spec preferred dropping the cap to deriving it from the
  flag plus a margin: the theatre bounds every generation itself (wall
  clock + single-flight + budgets), so any caller-side cap is redundant at
  best and silently undercuts the flag at worst (strategy item 15). The
  regression test pins the absence of a caller-side deadline rather than a
  derived bound — the strongest form of "the flag is the only authority",
  and the one the pre-fix code provably fails.
- **D-P13-2 — the final answer counts nowhere in the call counters (R3-03).**
  The spec offered "record it against the global only" as an option, but the
  finding itself lists `DirectorUsed`/`GlobalUsed` one past their caps as
  part of the bug, and the dialog summary renders both against their caps —
  counting the answer anywhere would move the over-cap display up a level.
  The decision: the answer is the loop's terminal roundtrip (a clai loop
  with `WithMaxToolCalls` must always be allowed to answer), so it is not a
  budgeted call at all; its cost is visible in the token telemetry and its
  occurrence in `LastAction = "answer"`. The ledger's call counters are
  exactly "budgeted tool executions", which is what the budgets cap and what
  the phase lines compare.
- **D-P13-3 — the brief rides in the working file, captured at draft-write
  time (R3-02).** The board field and a dedicated brief file were weighed;
  the working file wins because (a) the error table contemplates a working
  field, (b) the brief is semantically "what this draft was written from",
  sitting beside `Canon` and `Report`, and (c) the capture window was
  believed provably closed by the budget gates (see the delivered table).
  Review 5 (R5-01) disproved (c) — the window is not budget-closed — so the
  capture should move to brief-post time; the board scan stays as the
  fallback so pre-fix working files and out-of-order briefs still distill.
  The scenographer marker is the complementary `Dressed` flag —
  exact where the working status would over-claim if the director ever
  validated without dressing.
- **D-P13-4 — the load gate also palette-checks the coat (R3-04).** The
  finding's fix text names the variants, but "load and canonize agree on
  what a coat may be" covers the pinned coat too; `Canonize` refuses an
  out-of-palette coat, so the load gate degrades one to unpinned rather than
  surfacing it in every working context. Both paths now run `speciesVariants`
  — one named place for "what a coat may be".

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go test -race -run TestPrepareNextStory_LeavesWallClockToTheTheatre ./internal/media/` (pre-fix, cap restored) | FAIL — `Prepare received a caller-side deadline 1m59.99999412s from now`; the exact bug |
| Same, post-fix | pass |
| `go test -race -run 'TestDistill\|TestRunner_FullBudget\|TestRegistry_LoadDoc\|TestTheatre_FixtureProductionRunsFlow\|TestLoadWorking_BriefAndDressed' ./internal/agents/theatre/` | pass — all new regression tests |
| `go test -race ./internal/agents/theatre/...` | pass — theatre 7.1s/91.3%, tools 1.0s/93.1% (coverage up from 91.2%; the new tests added points) |
| `go test -race -count=3 ./internal/agents/theatre/...` | pass — theatre 18.5s, tools 1.0s |
| `go test -race ./internal/media/... ./cmd/serve/...` | pass — media 3.5s, storage 6.9s, serve 1.6s |
| `go test -race -count=3 ./cmd/classify/ -run TestCommand_startClassificationStation_context_timeout` | pass in isolation — the documented D-P10-2 pre-existing flake (failed once under full-suite load, green alone, untouched) |
| `go test ./... -race -cover -count=3 -timeout=30s` | all packages ok except the documented `cmd/classify` D-P10-2 flake; theatre 91.3%, tools 93.1%, model 95.6% |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre internal/media/index_handlers.go internal/media/index_handlers_test.go` | clean (zero diffs) |
| `go vet ./...` / `go fix ./...` | clean |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./internal/agents/theatre/... ./internal/media/... ./cmd/serve/...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 .` | 27 pre-existing clone groups, none in the touched files (same count as review 3) |
| `go test -race -run 'TestCompose_Snapshot\|TestTheatre_ConcurrentNextAndPrepare' ./internal/agents/theatre/` | pass — the frozen composer snapshot byte-identical; the R1-01/R2-01 race guards untouched |

**Acceptance check** — all criteria met: the R3-01 regression test fails on
the pre-fix code and passes under the fix; a budget-exhausted subagent shows
8/8, never 9/8; a hand-edited registry drops out-of-palette variants (and
coats); a 61+-entry board still distills its premise and set recipe; all
AGENTS.md gates are green over the touched packages; this phase file, the
README status board, the feedback index and the session journal record the
closure. R3-01..R3-04 are Closed.

## Review findings (review 4, 2026-08-02)

Holistic review of the completed worklog (round 4; all 13 phases complete, so
per the runbook the session reviewed instead of starting a new phase).

**Verified good** (re-run independently on the phase-13 state):

- All AGENTS.md gates reproduce green: `go run mvdan.cc/gofumpt@latest -l .`
  (v0.11.0, zero diffs), `go vet ./...` (clean), `go run
  honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix ./...`
  (clean), `go run github.com/mibk/dupl@latest -t 80 .` (27 pre-existing
  clone groups, none in the touched files), `go test ./... -race -cover
  -count=3 -timeout=30s` (all packages ok except the documented pre-existing
  D-P10-2 `cmd/classify` context-timeout flake, green in isolation — the
  theatre package 91.3%, tools 93.1%, model 95.6%), `go test -race
  ./internal/agents/theatre/...` (green; 18.5s at `-count=3`), `go test
  -race ./internal/media/... ./cmd/serve/...` (green).
- R1-01..R1-04 and R2-01 verified in code and under `-race` (the two race
  regression tests and the frozen composer snapshot pass; the wardrobe's
  tool set carries no `consult`; repeat consults account in the ledger and
  note the transcript; `postToBoard` refuses an invalid `to` before board or
  transcript see it).
- R3-01: `prepareNextStory` passes `context.Background()`; `rg 'WithTimeout'
  internal/media/index_handlers.go` shows no caller-side cap left on the
  trigger path; `runProduction`'s `context.WithTimeout(ctx, t.wallClock)`
  derives the deadline from the flag's own value. The regression test fails
  on the pre-fix code with the exact `1m59.99s` cap.
- R3-02 traced through every draft-creation path: `writeDraft` and
  `fallbackDraft` (both the compose path and the early-return path — the
  latter never writes, and any this-generation draft it reports was written
  by one of the two capturing writers) capture `boardBrief()`; `Dressed` is
  set only by `writeScene`/`fallbackScene` and reset only by the two draft
  writers (verified by `rg 'Dressed'` over the package). The pre-draft
  overflow window is provably closed by the budget gates: the brief is board
  entry 1 and at most ~57 entries can be posted before the playwright's
  draft write (director's remaining ≤49 calls + playwright's ≤8), so the
  brief always reaches the working file.
- R3-03: the final answer reaches `Stage.RecordAnswer` on the success path
  only (the failure path returns before it, unchanged); `RecordAnswer`
  updates `LastAction`/status/`lastRole` and no counter; `Actor.Calls ≤
  Budget` holds because clai's `WithMaxToolCalls` caps executions.
- R3-04: `trimRegistry` and `Canonize` both restrict coats and variants to
  `speciesVariants`; the permanent cast's canonical defaults override the
  file at load either way.

**Finding R4-01 — the global budget can tail-overshoot its cap by the last
admitted invocation's budget (Low; does not reopen).** `runner.go`
`Runner.Run` and `broker.go` `Broker.Consult` admit an invocation when
`used < max` without reserving the invocation's own budget, so an invocation
admitted near the cap finishes its budgeted calls past it: reproduced with a
scratch test (removed after the repro) — GlobalUsed at 195, a subagent with
budget 8 admitted, ledger ends `global 203/200`. The dialog summary and the
ledger render `GlobalUsed/GlobalMax`, so the phase-13 claim that the call
counters never exceed their caps holds for `Actor.Calls` and
`DirectorUsed` but not for `GlobalUsed`; the phase-13 `ledger.go` comment
has been corrected to state the tail-overshoot (this review). The gate's
refusal semantics are unaffected — no NEW work starts past the cap — so the
failure is cosmetic telemetry, Low per the taxonomy. Fix (checkbox): reserve
the invocation's budget at the runner gate (`used + inv.Budget > max` →
refuse with the budget-exhausted message) so `GlobalUsed ≤ GlobalMax`
provably; the broker's pre-check is then redundant but harmless. A
misconfiguration of `-theatreMaxCalls > -theatreGlobalCalls` would refuse
the director itself — acceptable, the flag combination is contradictory.
Routed to a future addendum; the board stays green (Low does not reopen).

## Review findings (review 5, 2026-08-02)

Holistic review of the completed worklog (round 5; all 13 phases complete, so
per the runbook the session reviewed instead of starting a new phase).

**Verified good** (re-run independently on the phase-13 state):

- All AGENTS.md gates reproduce green: `go run mvdan.cc/gofumpt@latest -l .`
  (zero diffs), `go vet ./...` (clean), `go run
  honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix ./...`
  (clean), `go run github.com/mibk/dupl@latest -t 80 .` (27 pre-existing
  clone groups, none in theatre), `go test ./... -race -cover -count=3
  -timeout=30s` (all 23 packages ok; theatre 91.3%, tools 93.1%, model
  95.6% — matching the recorded numbers), `go build ./...` (ok), the
  targeted regression set (`TestPrepareNextStory_LeavesWallClockToTheTheatre`,
  the frozen composer snapshot, both `-race` race tests, R3-02, R3-03,
  R3-04, R1-02, R1-03, R1-04) green under `-race -count=3`, `node --check
  intro.js` + the node harness 6/6, zero `storyteller` hits, no direct
  stdout in the theatre package.
- R1-01..R1-04 and R2-01 verified in code and under `-race`: both facade
  draws from `t.rnd` run under `rndMu` (`compose` and `newGenID`); the
  wardrobe's tool set carries no `consult`; a repeat consult records in the
  ledger and notes the transcript without re-spawning; `postToBoard` refuses
  an invalid `to` before board or transcript see it.
- R3-01..R3-04 re-traced: `prepareNextStory` still passes
  `context.Background()`; `Dressed` is still set only by
  `writeScene`/`fallbackScene` and reset only by the two draft writers;
  `RecordAnswer` still updates no counter; `trimRegistry` and `Canonize`
  still share `speciesVariants`.

**Finding R5-01 — the pre-draft brief window is not provably closed (Low;
does not reopen).** The phase-13 R3-02 closure argument (delivered table and
review-4 verified-good) claimed the brief always reaches the working file
because “at most ~57 entries can be posted before the playwright's draft
write”. That accounting counts one board entry per director call and per
playwright call, but a director `consult` posts question + answer from the
broker AND the consulted role can post up to its own budget, so one director
call can add up to 6 entries; the dramaturg's own 8-call budget can post up
to 8 entries (7 notes + the brief); the playwright can post 7 notes before
`write_draft`. Reproduced with a scratch test (removed after the repro): a
budget-respecting generation (dramaturg 7 notes + brief; 33 consults under
the 200 global cap, each posting question + answer + 4 consulted posts)
trims the board to 60 and drops the brief before the playwright's draft
write — `w.Brief` came out empty and `premiseFrom` returned no premise, the
exact R3-02 failure mode. The window between the brief post and the draft
write is not closed by the budgets. Fix (checkbox): capture the brief at
brief-post time (`writeBrief`/`fallbackBrief` write it into the working file
the moment it is posted — the board is guaranteed to hold it then), and keep
the `boardBrief()` scan as a fallback for older files. Promoted to strategy
item 18.

**Finding R5-02 — a collaboration revision re-runs the actor's full
per-invocation budget (Low; does not reopen).** `collab.go`
`resolveCollaborations` re-invokes `runOnce` with the full `inv.Budget` for
each revision round, and every tool execution records into the same actor's
`Calls` and `GlobalUsed`. Reproduced with a scratch test (removed after the
repro): one dramaturg invocation with budget 8 and one collaboration round
spent 8 calls in the main loop and 8 in the revision — ledger `dramaturg
16/8 calls` (24/8 with two rounds). The phase line and the dialog summary
render `Calls/Budget`, so the phase-13 R3-03 closure (“the phase line and
dialog summary can never show over-cap”) is incomplete on the collaboration
path, and `GlobalUsed` can overshoot by up to (1+rounds)×budget + consulted
budgets per admitted invocation — R4-01's stated bound understates it.
Refusal semantics unaffected, so cosmetic telemetry, Low. Fix (checkbox):
run the revision against the remaining per-invocation allowance
(`inv.Budget − calls already spent`), or account revision calls separately,
so `Actor.Calls ≤ Actor.Budget` holds on every path. Promoted to strategy
item 17.

R4-01 stays open as filed: its fix (reserve `inv.Budget` at the runner gate)
closes the plain-admission overshoot; R5-02 additionally bounds the
collaboration revisions. The board stays green — all 13 phases complete,
R5-01/R5-02 open as non-reopening Lows for a future addendum.

## Review findings (review 6, 2026-08-02)

Holistic review of the completed worklog (round 6; all 13 phases complete, so
per the runbook the session reviewed instead of starting a new phase).

**Verified good** (re-run independently on the phase-13 state):

- All AGENTS.md gates reproduce green: `go run mvdan.cc/gofumpt@latest -l .`
  (zero diffs), `go vet ./...` (clean), `go run
  honnef.co/go/tools/cmd/staticcheck@latest ./...` (clean), `go fix ./...`
  (clean), `go run github.com/mibk/dupl@latest -t 80 .` (27 pre-existing
  clone groups, none in theatre), `go test ./... -race -cover -count=3
  -timeout=30s` (all packages ok except the documented pre-existing D-P10-2
  `cmd/classify` context-timeout flake, green in isolation under
  `-race -count=3`), `go test -race ./internal/agents/theatre/...
  ./internal/media/... ./cmd/serve/... ./cmd/debug/...` (green), the
  targeted regression set (`TestPrepareNextStory_LeavesWallClockToTheTheatre`,
  `TestCompose_SnapshotMatchesFrozenPreMigrationOutput`,
  `TestTheatre_ConcurrentNextAndPrepareProductionSafely`,
  `TestDistill_PremiseAndSetsSurviveBoardOverflow`,
  `TestRunner_FullBudgetNeverShowsOverCap`,
  `TestRegistry_LoadDocDropsOutOfPaletteVariants`) green under `-race`,
  coverage theatre 91.3% / tools 93.1% / model 95.6% (matching the recorded
  numbers), `node --check intro.js` clean + the node harness 6/6, zero
  `storyteller` hits over `cmd/`+`internal/`, no direct stdout in the
  theatre package.
- R1-01..R1-04, R2-01, R3-01..R3-04 re-verified in code and under `-race`:
  both facade draws from `t.rnd` run under `rndMu` (`compose` and
  `newGenID`); the wardrobe's tool set carries no `consult`; repeat consults
  record in the ledger and note the transcript; `postToBoard` refuses an
  invalid `kind`/`to` before board or transcript see it; `prepareNextStory`
  passes `context.Background()`; `Dressed` is set only by
  `writeScene`/`fallbackScene` and reset only by the two draft writers;
  `RecordAnswer` updates no counter; `trimRegistry` and `Canonize` share
  `speciesVariants`/`filterVariants`.
- The three open findings R4-01, R5-01, R5-02 are still open as filed: the
  runner/broker gates admit without reserving `inv.Budget`; the brief is
  still captured at draft-write time (`writeDraft`/`fallbackDraft` set
  `w.Brief = boardBrief()`), not at brief-post time; `resolveCollaborations`
  still re-invokes `runOnce` with the full `inv.Budget`.

**Finding R6-01 — the phase line compares a generation-cumulative call
counter against a per-invocation budget, so any role invoked more than once
shows over-cap (Low; does not reopen).** `stage.go` `RecordCall` accumulates
`Actor.Calls` across every invocation of a role in the generation, while
`SetActorBudget` overwrites `Actor.Budget` with each invocation's cap; the
phase line (`phaseBody`) renders `Calls/Budget`. A role that runs more than
once — the director consults a role after its main invocation, or consults
the same role twice — therefore shows over-cap: reproduced with a scratch
test (removed after the repro) — a dramaturg main invocation with budget 8
(8 calls) followed by one consult with budget 4 (4 calls) renders
`dramaturg 12/4 calls` in the phase line, the exact display R3-03 closed
for the trailing answer, on a path R5-02's fix does not close: the revision
fix (consume the remaining per-invocation allowance) bounds the
collaboration path but not repeat consultations, so the R5-02 invariant
“`Actor.Calls ≤ Actor.Budget` holds on every path” is unachievable while
`Calls` is cumulative and `Budget` is per-invocation. The dialog summary
(`renderLedgerSummary`) is unaffected — it renders `Calls` without a ratio.
Severity: Low (cosmetic telemetry display); the refusal semantics and the
call counters themselves are untouched. Fix (checkbox): make the phase line
compare like with like — either reset `Actor.Calls` at `SetActorBudget`
(keep a separate cumulative counter for the dialog/telemetry), render only
the current invocation's calls, or drop the ratio once the actor has run
more than once. The fix belongs in `stage.go`, not in callers.

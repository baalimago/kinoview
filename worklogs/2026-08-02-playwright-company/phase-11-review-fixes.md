# Phase 11 — Review-1 Fixes (addendum)

**Status:** ✅ Complete | [README](./README.md)

## Goal

Close the findings of review 1 (2026-08-02). This is an addendum phase: the fixes
are small, mostly independent and cross the phases the review annotated — 2
(observability), 3 (subagents), 4 (director facade) and 9 (storyteller removal) —
so they are consolidated here instead of reopening four phase files. The original
board rows point at this phase.

## Work items (one checkbox per finding; close in order)

- [x] **R1-01 — data race on `Theatre.rnd` (Medium).** `internal/agents/theatre/
      theatre.go`: `Next` composes under `t.mu` while `Prepare`→`generate`
      (composer-only path and the LLM-failure fallback) and `Warm`'s synchronous
      seed compose run unlocked. Reproduced with `-race` (fresh `Theatre` per
      iteration, concurrent `Next` + `Prepare`, 400 iterations → `DATA RACE` in
      `math/rand.(*rngSource)` via `ComposeThemed`). Pre-existing (the pre-
      migration storyteller had the identical pattern; the migration preserved
      it by contract), so the fix is an improvement the worklog's own quality
      bar demands, not a regression repair.
      Fix: guard `t.rnd` with a dedicated mutex (a separate `rndMu`, since
      `Prepare` must not hold `t.mu` across an LLM production) or serialize
      every compose under `t.mu`. Add a `-race` regression test exercising
      concurrent `Next` + `Prepare` compose paths. Verify the composer-only
      path stays byte-identical to the frozen snapshot (the fix must not touch
      the draw order).

- [x] **R1-02 — the wardrobe carries `consult` (Low).** `internal/agents/theatre/
      roles.go` `roleTools`: the shared list gives the wardrobe `consult`
      against the phase-3 spec table and its own "You ask: nothing" prompt.
      Fix: parameterize the shared tool list per role so the wardrobe gets
      `post_to_board`, `read_board` and `advise` only. Keep the consult tool
      for dramaturg/playwright/scenographer.

- [x] **R1-03 — repeat consults are invisible to ledger and transcript (Low).**
      `internal/agents/theatre/broker.go` `Consult`: a consultation-table hit
      returns before `RecordConsult`, the board post and the transcript emit.
      Fix: on a table hit, record the consult in the ledger (and emit a
      transcript note) without re-spawning. The repeat answer stays cached —
      only the accounting changes.

- [x] **R1-04 — a post to an invalid addressee diverges board and transcript
      (Low).** `internal/agents/theatre/roles.go` `postToBoard`: the board gate
      clears an invalid `to` and keeps the entry, `TranscriptEvent.valid()`
      drops the same event, and the tool reports success either way. Fix:
      validate `to` in `postToBoard` the way `kind` is validated (return a
      refusal message the model can read), or normalize the addressee before
      the transcript emit — the two records must agree on every accepted post.

## Acceptance criteria

- [x] `go test -race ./internal/agents/theatre/...` green, including the new
      R1-01 concurrency regression test (fails on the pre-fix code).
- [x] The frozen composer snapshot test stays byte-identical (the R1-01 fix
      must not change the draw order).
- [x] The wardrobe's tool set no longer contains `consult`; dramaturg/
      playwright/scenographer keep it (tool-set unit test updated).
- [x] A repeated consultation is visible in the ledger (consults counter) and
      the transcript without spawning a second agent (broker unit test
      extended).
- [x] A post with an invalid `to` is either refused to the model or recorded
      consistently in board and transcript (post_to_board unit test).
- [x] All gates from AGENTS.md green over the touched packages; README status
      board and feedback index updated (findings closed, phase complete).

## Error coverage

| Failure | Expected outcome |
|---|---|
| The R1-01 fix changes the composer's draw order | snapshot test fails; the fix must serialize access, not alter the sequence |
| The R1-02 fix removes a tool another test relies on | tool-set and runner tests updated in the same change |
| The R1-03 ledger record double-counts with the spawn path | the table-hit branch records once, the spawn branch keeps its own single record |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 11 — the review-1 addendum of the
playwright-company worklog). The four review-1 findings are closed; each fix is
small, independent and lands with its regression test (TDD: every test was
written first and confirmed to fail on the pre-fix code).

**Delivered:**

| Finding | Fix | Regression test |
|---|---|---|
| R1-01 — `Theatre.rnd` data race | `theatre.go`: a dedicated `rndMu` guards the random source; a `compose` helper serializes every compose path (`Next` under `t.mu`, `Prepare`'s composer-only + LLM-failure fallback, `Warm`'s seed) through it. The lock never touches the draw order — the frozen snapshot stays byte-identical. | `TestTheatre_ConcurrentNextAndPrepareComposeSafely` (400 fresh theatres, concurrent `Next`+`Prepare`; reproduces the review's `DATA RACE` in `math/rand.(*rngSource)` on the pre-fix code) |
| R1-02 — wardrobe carries `consult` | `roles.go` `roleTools`: the shared list is now `post_to_board` + `read_board`; `consult` is appended for every role except the wardrobe, whose set is the spec's `post_to_board`/`read_board`/`advise` (its "You ask: nothing" scope). Tool order for the other roles is unchanged. | `TestRoleTools_WardrobeLacksConsult` (wardrobe has no consult and has the three spec tools; dramaturg/playwright/scenographer keep consult) |
| R1-03 — repeat consults invisible to ledger/transcript | `broker.go` `Consult`: a consultation-table hit now records the consult in the ledger (`RecordConsult` at the attempted depth, matching the refusal paths) and emits a stage note ("repeat consult answered from the table") before returning the cached answer — no second spawn, no board post, only the accounting changes. | `TestBroker_RepeatConsultReturnsCachedAnswer` extended: spawn counter unchanged, dramaturg consults = 2, transcript carries the repeat note |
| R1-04 — invalid-`to` post diverges board/transcript | `roles.go` `postToBoard`: `to` is validated the way `kind` is — trimmed, lowercased, must be a valid role or empty, else a refusal message the model can read. An accepted post records on both board and transcript; a refused one records on neither. | `TestRunner_PostToBoardRejectsUnknownAddressee` ("costume" refused, "director" accepted; board has 1 entry, transcript has 1 post event) |

**Material decisions (recorded for chronology):**

- **D-P11-1 — the facade serializes its own random source; callers stay put.**
  The `Teller` contract permits concurrent `Next` + `Prepare`, and the review
  ruled the fix belongs in the facade, never in callers. A dedicated `rndMu`
  (rather than serializing every compose under `t.mu`) is required because
  `Prepare` must not hold `t.mu` across an LLM production — `generate` can
  take minutes. The lock is leaf-ordered (acquired only inside `compose`),
  so it cannot deadlock against `t.mu`.
- **D-P11-2 — R1-04 takes the refusal route, not the normalization route.**
  The finding allowed either; validating `to` at the tool gate mirrors the
  existing `kind` validation exactly (same shape, same message contract), so
  the model can read the refusal and adapt instead of silently addressing
  the company. The board's own load-time gate (clear an invalid addressee)
  stays as defense-in-depth for hand-edited or hostile boards — the two
  layers now agree: the tool refuses first, the gate normalizes only what
  was never routed through the tool.
- **D-P11-3 — the R1-03 repeat note is a stage note, not a consult event.**
  The transcript's `consult` kind means "a consultation was spawned"; a
  table hit spawns nothing, so emitting one would overstate the run in the
  debug renderer. A `note` from the stage (the ledger's owner) records the
  accounting without implying a spawn, and the ledger's consults counter
  carries the number the telemetry actually needs.

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go test -race -count=1 -run 'TestRoleTools_WardrobeLacksConsult\|TestBroker_RepeatConsultReturnsCachedAnswer\|TestRunner_PostToBoardRejectsUnknownAddressee' ./internal/agents/theatre/` (pre-fix) | FAIL as designed — wardrobe has consult; dramaturg consults = 1; invalid `to` accepted, board/transcript diverge |
| `go test -race -count=1 -run 'TestTheatre_ConcurrentNextAndPrepareComposeSafely' ./internal/agents/theatre/` (pre-fix) | FAIL — `WARNING: DATA RACE` in `math/rand.(*rngSource)` via `ComposeThemed`, matching the review's repro |
| `go test -race -count=1 -run 'TestTheatre_ConcurrentNextAndPrepareComposeSafely\|TestRoleTools_WardrobeLacksConsult\|TestBroker_RepeatConsultReturnsCachedAnswer\|TestRunner_PostToBoardRejectsUnknownAddressee' ./internal/agents/theatre/` (post-fix) | pass |
| `go test -race ./internal/agents/theatre/...` | pass — theatre 10.7s, tools 1.1s |
| `go test -race -count=1 -run TestCompose_SnapshotMatchesFrozenPreMigrationOutput ./internal/agents/theatre/` | pass — byte-identical (draw order untouched) |
| `go run mvdan.cc/gofumpt@latest -l .` | clean (zero diffs) |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go test ./... -race -cover -count=3 -timeout=30s` | 22/23 packages ok; theatre 23.7s/91.1% (phase 10: 91.2% — noise), tools 93.1%, model 95.6% unchanged. The one failure is the documented pre-existing `internal/media/watcher` `Test_walkDo` timing flake (D-P10-2, untouched by this phase); it passes `-race -count=5` in isolation |
| `go run github.com/mibk/dupl@latest -t 80 .` | 27 pre-existing clone groups, none in the theatre package (same count as the phase-10 review) |

**Acceptance check** — all criteria met: the R1-01 regression test fails on the
pre-fix code and passes under `-race` with the fix; the frozen snapshot is
byte-identical; the wardrobe tool set is `post_to_board`/`read_board`/`advise`
and the other roles keep `consult`; a repeat consult is counted in the ledger
and noted on the transcript with no second spawn (the table-hit branch records
once, the spawn branch keeps its own single record — no double count); an
invalid `to` is refused to the model and records on neither board nor
transcript, while a valid one records on both. README status board and feedback
index updated (all four findings closed, phase complete).

## Review findings

### Review 2 — 2026-08-02 (holistic review; worker: imago)

**R2-01 — the R1-01 fix is incomplete: `openProduction` draws the generation id
from `t.rnd` without `rndMu` (Medium — fix tracked in phase 12).**

- Reference: `internal/agents/theatre/director.go:88` — `openProduction` calls
  `newID(t.rnd)` with no lock, while every compose path draws the same source
  under `rndMu` (`theatre.go:186-188` `compose`, added by the R1-01 fix). The
  facade now owns two draws from `t.rnd` (compose and the generation-id draw);
  the R1-01 fix serialized only the first.
- Failure scenario: the `Teller` contract permits concurrent `Next` + `Prepare`.
  A `Next` on an empty cache composes (under `rndMu`); a model-configured
  `Prepare` runs the production, whose `openProduction` reads `t.rnd` unlocked.
  Reproduced with `-race`: 400 fresh model-configured theatres (scripted
  `runLLM`), concurrent `Next()` + `Prepare()` → `WARNING: DATA RACE` in
  `math/rand.(*rngSource)` via `newID` at `theatre.go:373` vs `ComposeThemed`
  at `floor.go:26` (via `t.compose` under `rndMu`). Latent in the current
  serve wiring (Warm seeds before serving, single-flight serializes
  productions), but the R1-01 verdict's own guarantee — "every future caller
  of `agents.Teller` may assume concurrent use is safe" — is unmet on this
  path, exactly as it was for R1-01 itself.
- Why the R1-01 regression test cannot catch it: `TestTheatre_ConcurrentNextAndPrepareComposeSafely`
  builds each theatre via `newTestTheatre` (composer-only — no model), so
  `Prepare` → `generate` → `compose` and `openProduction` is never reached.
  The production path's generation-id draw was never exercised under `-race`.
- Fix (checkbox): guard the draw — hold `t.rndMu` around `newID(t.rnd)` in
  `openProduction` (a locked helper, e.g. `t.newGenID()`, keeps the leaf
  ordering; never hold the lock across the production). Add a `-race`
  regression test with a model-configured theatre (scripted `runLLM`) that
  exercises concurrent `Next` + `Prepare`; it must fail on the pre-fix code.
  The frozen composer snapshot must stay byte-identical (the fix only
  serializes the draw; it must not touch the compose order). Spec: phase 12.

Verified good for this phase: the R1-02 tool-set split (wardrobe =
`post_to_board`/`read_board`/`advise`, no `consult`; the other roles keep it,
order unchanged) and its test; the R1-03 table-hit accounting (ledger
`RecordConsult` at the attempted depth + transcript note, no re-spawn, no
double count) and its test; the R1-04 `to` validation at the `postToBoard`
gate (refusal message, board and transcript agree on every accepted post) and
its test; the R1-01 `rndMu` + `compose` helper on the compose paths and the
snapshot byte-identity under the fix.

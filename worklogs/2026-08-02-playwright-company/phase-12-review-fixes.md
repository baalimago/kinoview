# Phase 12 — Review-2 Fixes (addendum)

**Status:** ✅ Complete | [README](./README.md)

## Goal

Close the finding of review 2 (2026-08-02): R2-01, the incomplete R1-01 fix.
Review 1's `rndMu` guard serialized every *compose* path but missed the
production path's generation-id draw. This is an addendum phase: one fix, one
regression test, local to the facade (phase 4's `director.go` + phase 11's
fix), so it is consolidated here instead of reopening two phase files. The
original board rows point at this phase.

## Work items (one checkbox per finding; close in order)

- [x] **R2-01 — `openProduction` draws the generation id from `t.rnd`
      without `rndMu` (Medium).** `internal/agents/theatre/director.go`
      `openProduction` calls `newID(t.rnd)` unlocked (`director.go:88`,
      `theatre.go:369-375`), while every compose path draws the same source
      under `rndMu` (`theatre.go:186-188`). The `Teller` contract permits
      concurrent `Next` + `Prepare`; a `Next` compose on an empty cache
      concurrent with a model-configured `Prepare` races on `math/rand`.
      Reproduced with `-race`: 400 fresh model-configured theatres (scripted
      `runLLM`), concurrent `Next()` + `Prepare()` → `WARNING: DATA RACE` in
      `math/rand.(*rngSource)` via `newID` at `theatre.go:373` vs
      `ComposeThemed` at `floor.go:26`. The R1-01 regression test cannot
      catch it: `newTestTheatre` is composer-only (no model → no
      `openProduction`), so the production path's draw was never exercised
      under `-race`.
      Fix: guard the draw — hold `t.rndMu` around `newID(t.rnd)` in
      `openProduction` (a locked helper, e.g. `t.newGenID()`, keeps the leaf
      ordering the R1-01 fix established; the lock must never be held across
      the production). Add a `-race` regression test with a model-configured
      theatre (scripted `runLLM`, like `fixtureScript` but returning a
      terminal text) exercising concurrent `Next` + `Prepare`; it must fail
      on the pre-fix code.

## Acceptance criteria

- [x] `go test -race ./internal/agents/theatre/...` green, including the new
      R2-01 concurrency regression test (fails on the pre-fix code; the
      existing R1-01 test stays green either way).
- [x] The frozen composer snapshot test stays byte-identical (the fix must
      not touch the draw order).
- [x] The `rndMu` invariant holds on every path that draws from `t.rnd`:
      `compose` (theatre.go) and the generation-id draw (director.go).
- [x] All gates from AGENTS.md green over the touched packages; README status
      board and feedback index updated (finding closed, phase complete).

## Error coverage

| Failure | Expected outcome |
|---|---|
| The fix changes the composer's draw order | snapshot test fails; the fix must serialize access, not alter the sequence |
| The lock is held across the production | the test deadlocks; `newGenID` must lock, draw, unlock before `openProduction` continues |
| The regression test passes on pre-fix code | the test does not exercise `openProduction` — a model must be configured and `Prepare` must reach `runProduction` |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 12 — the review-2 addendum of the
playwright-company worklog). R2-01 is closed; the fix is one helper in the
facade plus one regression test, exactly as spec'd.

**Delivered:**

| Item | What landed |
|---|---|
| The fix | `theatre.go`: a `newGenID` locked helper (lock `rndMu`, draw `newID(t.rnd)`, unlock) sits next to `compose`; `director.go` `openProduction` draws through it. The lock is leaf-ordered and never held across the production. The struct comment and the `compose` doc now state the full invariant: the facade owns exactly two draws from `t.rnd` — compose and the generation-id draw — and both are serialized |
| The regression test | `TestTheatre_ConcurrentNextAndPrepareProductionSafely` (400 fresh model-configured theatres, scripted `runLLM` returning a terminal text, concurrent `Next` + `Prepare`) — fails on the pre-fix code with the review's exact `DATA RACE` (`newID` vs `ComposeThemed` on `math/rand.(*rngSource)`), green under the fix |
| Test hardening (R1-01 flake) | While landing the test, the R1-01 test's TempDir-cleanup flake surfaced (reproduced: `TempDir RemoveAll cleanup: unlinkat ...: directory not empty` — `Next`'s fire-and-forget `go t.saveStory(s)` raced the teardown). `Next`'s background write is now tracked in a `writeWG` (the goroutine runs through `writeWG.Go`, rewritten from the Add/go/Done form by `go fix`'s waitgroup fix) and both race tests wait on it — a completion signal, not a poll |

**Material decisions (recorded for chronology):**

- **D-P12-1 — one locked helper, both draws.** The review allowed either
  holding `rndMu` inline in `openProduction` or a locked helper; the helper
  (`newGenID`) mirrors the R1-01 fix's `compose` exactly (lock, draw,
  unlock), keeps the leaf ordering and gives the invariant one named place.
  The lock never spans the production — a deadlock there is the error-table
  trap and the test would catch it (the production holds `t.mu`-adjacent
  state, but `rndMu` is only ever acquired leaf-ordered).
- **D-P12-2 — a WaitGroup, not a poll, for Next's background write.** The
  first drain attempt (poll for `intro_story.json`) could not distinguish
  Prepare's synchronous write from Next's still-in-flight goroutine — the
  goroutine can be unscheduled when the poll observes the file, so the
  teardown still raced the rename. `writeWG` is the honest completion
  signal: `Next` runs the write through `writeWG.Go`, the race tests
  `Wait()` before their TempDirs tear down, and production never waits on it
  (zero cost). This is the D-P10-2 family of flake, fixed at the source
  rather than documented; it is a test-only concern, the 3-line production
  change is the seam.
- **D-P12-3 — the regression test scripts a terminal text, not the fixture
  flow.** `openProduction` draws the generation id before the runner's first
  LLM call, so a production that fails into the composer floor exercises the
  draw just as well as a full fixture run — and avoids `callTool`'s
  `t.Fatalf` from a non-test goroutine (the fixture script would call it
  from the `Prepare` goroutine, which `testing` forbids).

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go test -race -count=1 -run 'TestTheatre_ConcurrentNextAndPrepareProductionSafely' ./internal/agents/theatre/` (pre-fix) | FAIL — `WARNING: DATA RACE` in `math/rand.(*rngSource)` via `newID` vs `ComposeThemed`, matching the review's repro |
| `go test -race -count=1 -run 'TestTheatre_ConcurrentNextAndPrepareProductionSafely\|TestTheatre_ConcurrentNextAndPrepareComposeSafely' ./internal/agents/theatre/` (post-fix) | pass — 3.1s |
| `go test -race -count=3 -run 'TestTheatre_ConcurrentNextAndPrepareComposeSafely\|TestTheatre_ConcurrentNextAndPrepareProductionSafely' ./internal/agents/theatre/` | pass — 7.3s (the R1-01 TempDir flake, reproduced pre-hardening at `-count=3`, is gone) |
| `go test -race ./internal/agents/theatre/...` | pass — theatre 7.3s/91.2%, tools 1.0s/93.1% (coverage matches phase 11; no regression) |
| `go test -race -count=3 ./internal/agents/theatre/...` | pass — theatre 20.3s, tools 1.0s |
| `go test -race -count=1 -run TestCompose_SnapshotMatchesFrozenPreMigrationOutput ./internal/agents/theatre/` | pass — byte-identical (draw order untouched) |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre/` | clean (zero diffs) |
| `go vet ./internal/agents/theatre/...` / `go fix ./internal/agents/theatre/...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./internal/agents/theatre/...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 .` | 27 pre-existing clone groups, none in the theatre package (same count as phase 11) |

**Acceptance check** — all criteria met: the R2-01 regression test fails on
the pre-fix code and passes under `-race` with the fix; the R1-01 test stays
green either way (and is now flake-free); the frozen snapshot is
byte-identical; both draws from `t.rnd` go through `rndMu` (`compose` and
`newGenID` — a `rg 'newID\(t\.rnd\)'` over the package returns zero hits
outside the two locked helpers' shared `newID` definition); the README status
board, feedback index and session journal are updated (R2-01 closed, phases 4
and 11 rows point at the P12 fix, phase 12 complete); AGENTS.md states the
serialization invariant on the facade insight.

## Review findings

### Review 2 — 2026-08-02 (holistic review; worker: imago)

**R2-01 — openProduction's generation-id draw races with every compose path
(Medium).** Full finding in the phase-11 file; spec'd above. Reproduced
independently with a scratch `-race` test (400 model-configured theatres,
concurrent `Next` + `Prepare`); the scratch test was removed after the repro —
the permanent regression test lands with the fix.

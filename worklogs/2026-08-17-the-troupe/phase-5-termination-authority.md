# Phase 5 — Termination authority

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

Bound one generation regardless of swarm size or spawn depth. Three guards, of
which only one is an operator-tunable flag; the other two are hardcoded
constants (defense-in-depth, not operator surface).

## Shape (as built)

```text
internal/agents/troupe/budget.go      # stoploss + reservation + hardcoded caps
internal/agents/troupe/budget_test.go
internal/agents/troupe/spawn.go       # the runner admits every spawn + usage-recorder wiring
```

## The three guards

- **Token stoploss** — the cost guard and the only flag: `-troupeTokenStoploss`
  (its value reaches the budget as `WithStoploss`; phase 9 exposes the flag on
  the serve flagset). Once the generation's cumulative token usage crosses the
  stoploss, `spawn_role` refuses new spawns. Reservation is atomic: a spawn
  checks and reserves under one lock, so concurrent spawns cannot both pass
  the threshold.
- **Global maximum** — the hard cap on one generation's total work (budgeted
  model calls): `maxGenerationCalls = 128`, hardcoded. The counted calls are
  capped at the constant, so the total never exceeds it.
- **Depth cap** — the recursion guard: `maxSpawnDepth = 4`, hardcoded. A spawn
  past the cap is refused, never spawned; the director's own run is depth 0
  and every `spawn_role` recursion adds one.

## Implementation notes (as executed)

1. **The stoploss is a token count, not a monetary claim** — `WithStoploss`
   takes the `-troupeTokenStoploss` value; `<= 0` disables the guard, leaving
   the two hardcoded guards in force.
2. **The final answer is never budgeted.** `Budget.Record` (the
   `CallUsageRecorder` every spawned agent is wired with) accrues only
   budgeted calls — a model step that ended with a tool call accrues its
   tokens and one call; the final answer (`EndedWithReply`) and stop-ended
   steps count nothing, so telemetry never shows an actor over its cap.
3. **Reservation and the refusal check share one mutex.** `Admit` checks the
   depth cap, the global call max and the stoploss under one lock and
   reserves the spawn's token allowance: a spawn that would cross the
   threshold reserves the remaining allowance (the last crumbs) or fails
   atomically — no two concurrent spawns can both pass. The reservation is a
   concurrency claim (nominal `spawnReserveTokens` per spawn), never a cap on
   real usage: in-flight work always completes and accounts its real tokens.
   `release` runs via `defer` in `Spawn`, so the depth and the allowance
   return even when a spawn fails.
4. **The cooldown is separate** (decision 13) — it gates how often a
   generation may start (phase 9's facade), not how large it grows; the three
   work guards live in the budget.
5. **The runner is the single choke point.** `Spawner.Spawn` admits against
   the generation budget before anything runs — a refused spawn is never
   spawned — and the refused guard error (depth/call max/stoploss) flows back
   to the spawning agent. The default budget keeps the hardcoded guards on
   with the stoploss off; `WithBudget` hands the shared generation budget in
   (phase 7's director + swarm run under one).

## Tests

- **Stoploss** (`budget_test.go`): a spawn that would cross the threshold is
  refused once the allowance is spent; the final answer and stop-ended steps
  are never counted; a zero stoploss disables the guard.
- **Atomicity**: N concurrent spawns against a near-exhausted budget — exactly
  one passes (reserving the last crumbs), the rest are refused with
  `ErrStoploss`, and the claimed allowance never exceeds the stoploss at any
  point. A fresh budget stays concurrent: all depth-cap slots admit together
  with no stoploss serialization.
- **Global max**: budgeted calls are capped at the hardcoded constant and a
  spawn is refused once the cap is reached.
- **Depth cap**: a spawn past the cap is refused, never spawned; releasing a
  slot admits again.
- **Spawner wiring** (`spawn_test.go`): each guard refuses a `Spawn` with its
  exact error before the role source is read; a failed spawn releases its
  admission (depth and reservation return); a spawner without `WithBudget`
  runs under the default budget with the hardcoded guards on.
- The phase-3 arithmetic type was renamed to `ExpansionBudget` (with
  `DefaultExpansionBudget`/`WithExpansionBudget`) so `Budget` names the
  termination authority (the phase-5 doc's `budget.go`), matching the old
  theatre's call-budget precedent.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] `-troupeTokenStoploss` is the only operator flag among the three guards
      (the call max and the depth cap are hardcoded constants).

## Decision log (session 2026-08-23, clai worker)

- **D-5-1 — A budgeted call is a model step that ended with a tool call.**
  `Budget.Record` accrues tokens and one call only for `EndedWithTool`
  steps; the final answer (`EndedWithReply`) and stop-ended steps are never
  budgeted. This is the "final answer is never budgeted" rule (implementation
  note 2) and the reason telemetry never shows an actor over its cap.
- **D-5-2 — Reservation is a per-spawn nominal claim with a crumbs branch.**
  Each admitted spawn reserves `spawnReserveTokens` (10 000, hardcoded) of
  the remaining allowance; a spawn that would cross the threshold reserves
  the remaining allowance instead, or is refused atomically. The reservation
  is a concurrency claim, never a cap: real usage always accrues, and an
  in-flight spawn always completes. A full-remaining-allowance reservation
  would have serialized the swarm; a claim that capped real usage would have
  hidden cost. The claimed allowance (`used + reserved`) never exceeds the
  stoploss between admissions.
- **D-5-3 — The depth cap counts in-flight spawns.** `maxSpawnDepth = 4`
  hardcoded; `Admit` increments on entry and `release` decrements on exit, so
  the cap bounds both recursion depth and concurrent breadth. Sequential
  spawn_role tool calls (the director's loop) nest at most one deep and are
  unaffected.
- **D-5-4 — The call counter is capped, the token usage is not.** Budgeted
  calls stop counting at `maxGenerationCalls` — the hard cap, so the total
  can never exceed it — while token usage always accrues: the stoploss gates
  new spawns, it never rewrites history.
- **D-5-5 — The budget lives on the spawner, defaulted.** `WithBudget` shares
  one generation budget across every spawn; a spawner without one runs under
  a default with the hardcoded guards on and the stoploss off. Phase 9 wires
  the `-troupeTokenStoploss` flag value through `WithStoploss`.
- **D-5-6 — The arithmetic expansion type was renamed `ExpansionBudget`.**
  The phase-3 resolver type collided with the termination authority; the
  rename (with `DefaultExpansionBudget`/`WithExpansionBudget`) frees `Budget`
  for the generation guards and describes the resolver's type precisely.

## Notes for later phases

- Phase 7's director and swarm run inside these bounds: one budget per
  generation, the director at depth 0, sub-agents admitting against the same
  budget with their model calls accounted through the usage recorder.
- Phase 9 exposes `-troupeTokenStoploss` on the serve flagset (alongside
  `-troupeModel`) and passes the value through `WithStoploss`.

## Session report (2026-08-23)

Picked up phase 5 from the handover state (README status: "Phase 5
(termination authority) is next"; phases 0–4 already in the tree).

**Built `budget.go` exactly as planned:**

- `Budget` — the generation's termination authority: `WithStoploss` (the
  `-troupeTokenStoploss` value; `<= 0` disables), `Admit` (depth cap, call
  max and stoploss checked and reserved under one lock, returning the
  release), `Record` (the `CallUsageRecorder` every spawned agent accounts
  into: budgeted calls only, the final answer never counted), `Stats` and the
  three exact refusal errors (`ErrDepthCap`/`ErrCallMax`/`ErrStoploss`).
- Hardcoded constants: `maxGenerationCalls = 128`, `maxSpawnDepth = 4`,
  `spawnReserveTokens = 10_000`.
- `spawn.go` hangs the authority on the runner: `WithBudget` (defaulted in
  `NewSpawner`), `Spawn` admits before anything runs and releases via `defer`,
  `newAgent` wires the budget as the usage recorder, and the `spawn_role`
  description tells the model the stage bounds the generation.
- Renamed the phase-3 arithmetic type to `ExpansionBudget` (D-5-6) to free
  `Budget` for the termination authority.
- Package doc and `STAGE.md` updated to ship the phase's surface (the stage's
  human-readable mirror now carries "The termination authority").

**Verification (exact commands and results):**

```bash
# Baseline (pre-change):
go build ./...                                       # green
go test ./internal/agents/troupe -race -count=1      # ok

# After the change:
go build ./...                                       # green
go vet ./...                                         # clean
go run mvdan.cc/gofumpt@latest -l .                  # no output (clean)
go run honnef.co/go/tools/cmd/staticcheck@latest ./internal/agents/troupe/...  # clean
go run github.com/mibk/dupl@latest -t 80 internal/agents/troupe   # 0 clone groups
go test ./internal/agents/troupe -race -count=3      # ok (acceptance)
go test ./internal/agents/troupe -race -cover -count=1  # ok, 88.4% coverage
go test ./... -race -cover -count=3 -timeout=30s     # ok, all packages
```

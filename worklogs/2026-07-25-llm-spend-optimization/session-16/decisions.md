# Session 16 — Phase 7 R1-06 Resolution

**Worker:** claude (session 16)
**Date:** 2026-07-25
**Phase:** 7 — Concierge Schedule Truthfulness (R1-06 fix)

## Objective

Address R1-06 from Review 1: `-conciergeInterval 0` was not rejected at flag validation.
The Phase 7 error coverage table specified "Rejected at flag validation with a clear message, not an infinite tick loop." The indexer safely handled zero by running once then stopping, but the spec requirement was unmet.

## Changes

### `cmd/serve/serve_setup.go`
- Added flag validation in `Setup()`: if `*c.conciergeInterval <= 0`, returns `fmt.Errorf("-conciergeInterval must be positive (got %v); set a positive duration or omit the flag to use the default (6h)", *c.conciergeInterval)`
- Placed after the flagset-nil check, before argument handling

### `cmd/serve/serve.go`
- Updated `-conciergeInterval` flag help text from `"interval between concierge runs; 0 disables periodic runs (runs once on startup then stops)"` to `"interval between concierge runs"` — zero is no longer a valid value

### `cmd/serve/serve_test.go`
- Added `TestSetup_ZeroIntervalRejected`: verifies `Setup()` returns error with a descriptive message when `conciergeInterval` is zero
- Added `"strings"` import for `strings.Contains`

### `internal/media/index_test.go`
- Renamed `TestConcierge_ZeroIntervalRejected` → `TestConcierge_ZeroIntervalRunsOnce`
- Updated comment: now documents the test as defense-in-depth (the indexer's zero-interval safety is secondary to serve-level flag validation)

### `worklogs/2026-07-25-llm-spend-optimization/phase-7-concierge-schedule-truthfulness.md`
- Marked R1-06 resolved with implementation details
- Updated error coverage table: `TestConcierge_ZeroIntervalRejected` → `TestSetup_ZeroIntervalRejected` (serve) + `TestConcierge_ZeroIntervalRunsOnce` (indexer)

### `worklogs/2026-07-25-llm-spend-optimization/README.md`
- Updated Feedback Index: R1-06 marked ✅ Resolved (Session 16)
- Updated Status Board: Phase 7 status from 🔄 Reopened (R1) to ✅ Done (R1-06 resolved)

## Verification

```bash
gofumpt -l .          # clean
go vet ./...           # clean
go build -o kinoview . # succeeds
```

`TestSetup_ZeroIntervalRejected` — 3/3 passes.
`TestConcierge_ZeroIntervalRunsOnce` — 3/3 passes.

Pre-existing flaky tests (`TestRun/successful_run`, storage race) unaffected by this change.

## Design Notes

**D16.1 — Defense-in-depth for zero interval at indexer level.** The `runConciergeLoop` method keeps its `if i.conciergeInterval <= 0 { ... return }` guards. Since the serve-level flag validation is the primary guard and the indexer can be constructed directly (not through serve), the indexer-level safety is preserved as a belt-and-suspenders measure.

**D16.2 — Flag validation lives in `Setup()`, not a separate `Validate()` method.** The project has no separate validation phase; `Setup()` is the first call after flag parsing. Adding validation there follows the existing pattern (the flagset-nil check is already there).

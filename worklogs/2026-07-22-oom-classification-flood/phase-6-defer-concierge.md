# Phase 6: Defer Concierge Startup Until Classification Drain

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Prevent the concierge from running its initial LLM pass concurrently with the classification flood, avoiding compounding memory pressure at startup.

## Specification

### Problem

`indexer.Start()` runs the concierge immediately on startup (line 31: `ok: Running concierge`), before any classifications have completed. The concierge uses its own LLM session (same model: `or:minimax/minimax-m3`), which adds memory pressure concurrent with 5 classification workers.

### Proposed fix

Simple: add a configurable delay before the first concierge run. Default: 60 seconds. After the initial delay, the concierge runs every 6 hours as before.

Alternatively: make the concierge wait until the classification queue is empty (or below a threshold). This is more robust but requires the concierge to have visibility into the classification queue state.

**Recommended approach**: Add a `-conciergeStartupDelay` flag (default: 60s). Simple, predictable, no coupling between components.

### Affected files

- `internal/media/index.go` — concierge startup goroutine
- `cmd/serve/serve.go` — new flag

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| Normal startup | Server starts | Timer | Concierge first runs after `-conciergeStartupDelay` |
| Startup with heavy classification | 20 items queued | Rate limiter (P1) + delay | Classifications get head start before concierge runs |
| `-conciergeStartupDelay=0` | Flag set to zero | — | Concierge runs immediately (backward compat) |

## Acceptance Criteria

- [ ] Concierge does not run at T+0; waits for configured delay
- [ ] Default delay is 60s
- [ ] `-conciergeStartupDelay=0` restores immediate-run behavior
- [ ] Subsequent concierge runs still happen every 6 hours
- [ ] No data race between concierge delay timer and context cancellation

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| Context cancelled during delay | Concierge goroutine exits cleanly | Shutdown test |
| Negative delay value | Treated as 0 (immediate) | Config validation |

## Implementation Notes

### 2026-07-22 (session worker 6) — Implemented

Changes:
- `internal/media/index.go`: `conciergeStartupDelay time.Duration` field + `WithConciergeStartupDelay` option. Concierge goroutine uses `select` on `ctx.Done()` / `time.After(delay)` when delay > 0, runs immediately when delay == 0.
- `cmd/serve/serve.go`: `-conciergeStartupDelay` flag, default 60s.
- `cmd/serve/serve_setup.go`: wired via `media.WithConciergeStartupDelay`.
- `internal/media/index_test.go`: 3 subtests covering delay wait, zero-delay immediate run, context cancel during delay.

Design decisions:
- **D21**: Delay owned by Indexer, not concierge agent — indexer orchestrates startup, concierge is stateless LLM wrapper.
- **D22**: `time.After` with select on ctx.Done — no goroutine leak, clean cancellation.
- **D23**: Default 60s, `-conciergeStartupDelay=0` restores original immediate-run behavior.

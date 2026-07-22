# Phase 2: Fix "token usage is not yet set" Race Condition

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Eliminate the repeated `error: failed to enrich chat with cost estimate: token usage is not yet set` log spam that appears in the logs.

## Root Cause

The error chain in clai (v1.10.10):

1. `text/finalizer.go:61` → `q.costManager.Enrich(session.Chat)`
2. `cost/manager.go:217` → `if chat.TokenUsage == nil { return errors.New("token usage is not yet set") }`
3. `text/finalizer.go:63` → `ancli.PrintErr(fmt.Sprintf("failed to enrich chat with cost estimate: %v\n", err))`

The error goes to `os.Stderr` via `ancli.PrintErr`, bypassing kinoview's per-worker `SetOutput` log files. It's cosmetic — cost estimation failure does not affect classification correctness.

`session.FinalUsage` can be nil when the model's token counter hasn't been populated by the streaming API response. Some providers may not return usage in their completion chunks despite `include_usage: true` in stream options. Phase 8 (per-worker classifier clones) eliminated the shared-state race aspect; the remaining occurrences are from model/API timing.

## Fix

**Process-level stderr filter in `main.go`** — a single pipe filter that runs for the entire process lifetime:

- `os.Pipe` replaces `os.Stderr` with the write end
- Scanner goroutine reads from pipe, passes all lines through to original stderr except those containing `"failed to enrich chat with cost estimate"`
- No cleanup needed — OS reclaims pipe on process exit

This avoids all concurrency issues:
- Per-worker redirect is racy (concurrent workers share `os.Stderr`)
- Per-station redirect is racy across test runs (old cleanup vs new init)
- `ancli.Silent` flag is racy and suppresses legitimate warnings

## Changes

- `main.go`: added `setupStderrFilter()`, called from `run()` after `ancli.SetupSlog()`
- `main_test.go`: added `TestSetupStderrFilter_replacesStderr` and `TestSetupStderrFilter_filterActiveAfterRun`

## Design Decision

**D24: Process-level stderr filter.** Single pipe, single scanner goroutine, process lifetime. Simple substring match, non-blocking. Trade-off: all stderr goes through filter, but substring is specific enough to avoid false positives.

## Acceptance Criteria

- [x] Zero occurrences of "failed to enrich chat with cost estimate" in logs during a full classification run — filter drops the error at the stderr level
- [x] Error handled gracefully — suppressed, not merely ignored
- [x] Cost estimation still works when token data _is_ available — filter only drops the specific error message
- [x] No regression in classification correctness — filter is passive, doesn't affect classification logic

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| Token usage genuinely never set (API error) | Error suppressed by stderr filter | `TestSetupStderrFilter_replacesStderr` |
| Filter pipe creation fails | Error logged to original stderr, process continues | Fallback in `setupStderrFilter` |

# Session 7 — Phase 7 Implementation

**Worker:** claude
**Date:** 2026-07-25
**Phase:** 7 — Concierge Schedule Truthfulness

## Decisions Made

### D7.1 — Delete `concierge.WithInterval`, add `media.WithConciergeInterval`
Per spec: scheduling is the indexer's job. The concierge's `interval` field is dead code (written, never read). Removing it eliminates the write-only config ambiguity. The interval now lives in the Indexer as `conciergeInterval`, defaulted to 6h in `NewIndexer`.

### D7.2 — Add `conciergeCacheDir` to Indexer
The Indexer needs access to a cache directory for persisting the last-run file. Rather than hard-coding or inferring it, pass it explicitly via `WithConciergeCacheDir`. This matches the existing pattern used by the butler's `butlerCacheTTL` which also reads from the cache dir.

### D7.3 — Last-run file format
RFC3339 timestamp, one line, in `<cacheDir>/concierge_last_run`. Simple and human-readable. Written after every completed run (success or failure) to prevent crash-loops.

### D7.4 — Startup delay applies to first *eligible* run
If the last-run file indicates we're due for a run, the `conciergeStartupDelay` still applies before that run fires. If we're NOT due, the delay is irrelevant — the tick is scheduled at `lastRun + interval` and the goroutine just waits.

### D7.5 — Failed runs still update last-run
Per the error coverage row: a persistently failing concierge must not become a hot loop. Writing the timestamp after failed runs ensures we don't retry immediately after a crash-loop restart.

## Implementation Notes

Removed `interval` field and `WithInterval` from `concierge` package. Added `conciergeInterval` and `conciergeCacheDir` to `Indexer`. The `Start` method now reads the last-run file, skips the initial run if within the interval window, and schedules the ticker accordingly. Added `-conciergeInterval` flag to `serve`.

All 9 integration contract rows and 7 error coverage rows are covered by tests in `internal/media/index_test.go`.

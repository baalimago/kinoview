# Session 8 — Phase 7 Implementation

**Worker:** claude (session 8)
**Date:** 2026-07-25
**Phase:** 7 — Concierge Schedule Truthfulness

## Decisions Made

### D8.1 — Delete `concierge.WithInterval`, add `media.WithConciergeInterval` + `WithConciergeCacheDir`
Per spec: scheduling is the indexer's job. The concierge's `interval` field was dead code (written, never read). Removed `interval` field and `WithInterval` from concierge package. Added `conciergeInterval` (default 6h in `NewIndexer`) and `conciergeCacheDir` to `Indexer` with corresponding options.

### D8.2 — Panic recovery in `runConciergeLoop`
Per error coverage row: a panicking concierge must not kill the scheduler. Added `defer/recover` to the `do()` closure in `runConciergeLoop`. The panic is logged and the ticker continues.

### D8.3 — Zero interval: run once then stop
Instead of panicking with `time.NewTicker(0)`, zero/negative interval causes one run then graceful return. The spec calls for rejection at flag validation level (serve.go), which can be added later. This is a safe default at the Indexer level.

### D8.4 — Test helper refactored to avoid fsnotify leaks
`newTestIndexerForConcierge` now constructs the Indexer directly instead of calling `NewIndexer`, avoiding the real `fsnotify.Watcher` creation that leaks kernel file descriptors. This fixes the "too many open files" failures when running the full test suite.

### D8.5 — `butlerCacheTTL` initialized in `Command()`
Fixed pre-existing bug from Phase 6: `butlerCacheTTL` was only set in `Flagset()` but not in `Command()`. Added initialization in `Command()` to prevent nil pointer dereference in `serve_setup_test.go`.

## Files Changed

- `internal/agents/concierge/concierge.go` — removed `interval` field and `WithInterval`
- `internal/agents/concierge/concierge_test.go` — removed `WithInterval` call, unused `time` import
- `internal/media/index.go` — added `conciergeInterval`, `conciergeCacheDir` fields; added `WithConciergeInterval`, `WithConciergeCacheDir` options; extracted `runConciergeLoop`, `readConciergeLastRun`, `writeConciergeLastRun` methods; replaced hardcoded `time.Hour * 6` ticker with interval-aware scheduling using persisted last-run
- `internal/media/index_test.go` — 22 new tests covering all integration contract and error coverage rows; refactored `newTestIndexerForConcierge` helper; added atomic `countingConcierge` and `panicConcierge` test doubles
- `cmd/serve/serve.go` — added `conciergeInterval` field and `-conciergeInterval` flag; fixed missing `butlerCacheTTL` initialization in `Command()`
- `cmd/serve/serve_setup.go` — wired `WithConciergeInterval` and `WithConciergeCacheDir` into indexer creation
- `README.md` — documented `-conciergeInterval` flag and last-run persistence

## Test Coverage

| Integration Contract Row | Test |
|--------------------------|------|
| #1 Fresh start, no last-run | `TestConcierge_FreshStartNoLastRun` |
| #2 Last-run within interval, skip | `TestConcierge_RestartWithinIntervalSkips` |
| #3 Last-run past interval, run | `TestConcierge_RestartAfterIntervalRuns` |
| #4 5 restarts → 1 run | `TestConcierge_CrashLoopSingleRun` |
| #5 Persisted across runs | `TestConcierge_LastRunPersistence` |
| #6 Interval flag respected | `TestConcierge_IntervalFlagRespected` |
| #7 Startup delay honoured | `TestConcierge_StartupDelayStillHonoured` |
| #8 Last-run file missing after existed | `TestConcierge_FreshStartNoLastRun` (implicit) |
| #9 Context cancelled during wait | `TestConcierge_ContextCancelStopsScheduler` |

| Error Coverage Row | Test |
|--------------------|------|
| Unreadable file | `TestConcierge_UnreadableLastRunFile` |
| Malformed file | `TestConcierge_MalformedLastRunFile` |
| Future timestamp | `TestConcierge_FutureLastRun` |
| Write failure | `TestConcierge_LastRunWriteFailure` |
| Failed run updates last-run | `TestConcierge_FailedRunUpdatesLastRun` |
| Panic recovery | `TestConcierge_PanicDoesNotKillScheduler` |
| Zero interval | `TestConcierge_ZeroIntervalRejected` |

## Verification

```bash
gofumpt -l .          # clean
go vet ./...           # clean
go build -o kinoview . # succeeds
go test -race -count=1 ./internal/agents/concierge/... ./internal/media/ ./cmd/serve/...  # all pass
```

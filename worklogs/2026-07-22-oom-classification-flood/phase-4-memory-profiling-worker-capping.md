# Phase 4: Memory Profiling & Worker Capping

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Identify the primary memory consumers during classification and implement dynamic worker capping based on available system memory.

## Specification

### Problem

The process consumed 2.99 GB before OOM. With 5 workers each running a full LLM agent session (tool-calling loop with Wikipedia downloads), memory usage escalates rapidly. The log timeline shows:

- 10:03:29 — 434 items loaded, store ready
- 10:03:29 — classification delegator started
- 10:03:45 — first classification request (16s gap — classification wasn't triggered by initial load? Wait...)

Actually, looking more carefully: the first classification appears at 10:03:45, which is 16 seconds after startup. The items might have been queued from the watcher detecting files, not from the initial `loadPersistedItems`. Let me re-examine...

The flow is: `loadPersistedItems` loads items into cache, but does NOT call `Store()` — it directly populates `s.cache`. So no classification flood from initial load. The flood comes from the watcher detecting changes OR from the concierge running on startup.

Wait — looking at line 31: `ok: Running concierge` at 10:03:29. The concierge may trigger metadata updates/classification. But the log shows classification requests starting at 10:03:45, after a web client connects. The websocket connection at 10:03:33 might trigger a gallery view which fires classification requests.

Regardless, the core issue remains: 5 concurrent LLM agent sessions consume massive memory. Each `or:minimax/minimax-m3` session with tool-calling capability + full Wikipedia page downloads + ffprobe output in context = potentially 200-500 MB per worker.

### Proposed fix

1. **Add memory profiling**: `pprof` endpoint (`/debug/pprof`) behind a flag for diagnostics
2. **Worker capping by system memory**: Before starting a classification, check `runtime.MemStats.Alloc` and/or available system memory. If usage exceeds threshold (e.g., 80% of available), block new classifications until memory drops.
3. **Reduce default workers**: Change default from 5 to 2, and add a warning when `-classifierWorkers` > 3

### Affected files

- `cmd/serve/serve.go` — new `-pprof` flag, change default workers
- `internal/media/storage/classification.go` — memory check before classification dispatch
- `cmd/classify/classify.go` — reduce default workers

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| Low memory pressure | 2 workers, small files | Runtime | Classifications proceed normally |
| High memory pressure | 5 workers, large Wikipedia downloads | Memory check | New classifications blocked until `Alloc` drops below threshold |
| `-pprof` flag enabled | HTTP request to `/debug/pprof/heap` | `net/http/pprof` | Heap profile served |

## Acceptance Criteria

- [ ] `-classifierWorkers` default reduced from 5 to 2
- [ ] Warning logged when `-classifierWorkers` > 3
- [ ] Memory guard prevents classification dispatch when `Alloc > 80% * Sys`
- [ ] `-pprof` flag enables `/debug/pprof/` endpoints
- [ ] Memory guard is non-blocking (uses `select/default` to skip, doesn't stall)

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| `runtime.ReadMemStats` fails (shouldn't) | Guard skipped, classification proceeds | N/A (runtime never fails) |
| Memory guard always blocks (threshold too low) | Configurable threshold via flag | Manual test with low threshold |

## Implementation Notes

### Completed Changes

1. **`cmd/serve/serve.go`**: Default `-classifierWorkers` reduced from 5 to 2. Added `-pprof` bool flag (default false).
2. **`cmd/serve/serve_setup.go`**: Wires `net/http/pprof` handlers (Index, Cmdline, Profile, Symbol, Trace) when `-pprof` is enabled.
3. **`cmd/classify/classify.go`**: Default `-workers` reduced from 5 to 2. Fixed `Setup()` to use `*c.workers` instead of hardcoded 5.
4. **`cmd/classify/classify_test.go`**: Updated 3 tests to new default of 2.
5. **`internal/media/storage/store.go`**: Added `memoryThreshold float64` field (default 0.8) + `WithMemoryThreshold` option.
6. **`internal/media/storage/classification.go`**: `memoryHigh()` method checks `Alloc > threshold * Sys`. Memory guard in delegator loop drops items with in-flight cleanup. Workers > 3 warning logged in `StartClassificationStation`.
7. **`internal/media/storage/classification_test.go`**: 7 new tests covering memoryHigh disabled/enabled, memory guard integration, workers warning.

### Design decisions

- **D13**: Memory guard in delegator goroutine (single goroutine, avoids concurrent `runtime.ReadMemStats` calls).
- **D14**: Threshold `Alloc > 80% * Sys`. Values <= 0 or >= 1 disable the check.
- **D15**: Workers > 3 warning is informational only; does not cap the count.

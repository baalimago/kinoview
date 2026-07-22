# Phase 10: Store Write Batching on Startup

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Reduce IO amplification during the initial watcher scan by batching store writes, preventing 434 individual JSON file writes in rapid succession at startup.

## Specification

### Problem

During `watcher.WalkDir`, the watcher emits items to `fileUpdates` → `handleNewItem` → `store.Store()` → `s.store(item)`. Each `store()` call opens, reads, compares, truncates, and writes a JSON file. For 434 media items, this means 434 individual open-read-write-close cycles within seconds of startup:

```
10:03:31 notice: updated store for 'YTS.BZ - Official site.jpg'
10:03:31 notice: updated store for 'YTS.BZ - Official site_thumb.jpg'
10:03:32 notice: updated store for 'WWW.YIFY-TORRENTS.COM.jpg'
... (13 updates in 3 seconds)
```

While not OOM-critical, this contributes to startup load and filesystem pressure. Additionally, many of these writes are no-ops (the item hasn't changed since last persisted), but the open-read-compare cycle still occurs.

### Proposed fix

**Option A (simple):** Add a `s.storeNoPersist()` variant that updates the in-memory cache but skips the disk write. Use it during the startup scan window (first N seconds after `Start()`), then flush all items to disk in a single batch after the scan completes.

**Option B (lighter):** Add a "dirty" flag to in-memory items. During the startup window, mark items dirty in cache but defer writes. After the window expires, write all dirty items in a batch goroutine.

**Recommended: Option B** — minimal code change, no API surface change, backward compatible.

### Affected files

- `internal/media/storage/store.go` — `store()` method, add dirty tracking
- `internal/media/storage/handlers.go` — `handleVideoItem` no change needed

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| Startup scan (first 30s) | 434 items emitted by watcher | Dirty flag, deferred write | Items cached in memory, 0 disk writes during window |
| Post-window flush | 30s timer expires | Batch writer goroutine | All dirty items written to disk in one batch |
| Item change during window | Same item emitted twice | Dirty flag | Item marked dirty once, written once during flush |
| Normal operation (after window) | New file detected | store() | Immediate write as before (no change) |

## Acceptance Criteria

- [x] Disk writes during first 30s of startup reduced by >80%
- [x] All items correctly persisted after the batch flush
- [x] No data loss if process crashes during the startup window (acceptable tradeoff — items are re-detected by watcher on restart)
- [x] No regression in normal (post-startup) store behavior
- [x] Unit test: dirty items flushed when timer expires
- [x] Unit test: multiple updates to same item during window result in single write

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| Flush fails (disk full) | Error logged, items remain in cache, retry on next store() call | Mock disk-full error |
| Crash during startup window | Items lost from cache but re-detected by watcher on restart (acceptable) | Documented tradeoff |
| Timer never fires (context cancelled) | Flush triggered by context cancellation | Shutdown test |

## Implementation Notes

TBD

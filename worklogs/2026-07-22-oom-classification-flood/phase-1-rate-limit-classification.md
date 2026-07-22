# Phase 1: Rate-limit Classification Queue

**Status:** ✅ Complete
[← README](./README.md)

## Goal

Prevent startup classification flood by introducing a configurable throttling layer that limits how many items are queued for classification per time window, and caps the total pending queue size.

## Specification

### Problem

During initial filesystem scan, `watcher.WalkDir` emits every video file to the `fileUpdates` channel. The item-processing goroutine calls `store.Store()` → `handleVideoItem()` → `AddToClassificationQueue()` for each of the 410 videos lacking metadata. All 410 flood the queue within seconds of startup.

The work channel buffer is 1000, so no backpressure exists until saturation — and even then, the sender blocks rather than rejects. The 5 classification workers each spin up full LLM agent contexts (200–500 MB each), consuming all available memory before the first classification completes.

### Root cause trace

```
watcher.Watch() → filepath.WalkDir() → checkFile() → updates <- item
  → (indexer.Start goroutine) item := <-updates
    → handleNewItem() → store.Store()
      → i.Metadata == nil && video → handleVideoItem()
        → AddToClassificationQueue()
          → classificationRequest <- candidate   // unbuffered send
            → (delegator) workChan <- candidate   // buffered 1000
              → (worker) classify()
```

The critical insight: `loadPersistedItems` does NOT trigger classification — it directly populates the cache. The flood comes from the watcher's `WalkDir` which re-emits every file, and items without metadata get queued.

### Proposed fix

Add a **token-bucket rate limiter** at the `AddToClassificationQueue` entry point:

1. Configurable rate (default: 0.2 classifications/second, i.e., 1 every 5s)
2. Configurable burst (default: 3)
3. If the rate limiter denies admission, the item is **dropped with an `ancli.Warnf` log** (not re-queued — the periodic concierge will pick it up on its 6-hour cycle)
4. A startup cooldown period (default: 10s) during which no classifications are admitted, giving the process time to stabilize and the HTTP server to become ready

Additionally, cap the pending queue at `classificationWorkers * 2` items by using a `select` with a length check before sending to `workChan`. When the cap is exceeded, new items are dropped.

### Affected files

- `internal/media/storage/classification.go` — `AddToClassificationQueue`, `StartClassificationStation`
- `cmd/serve/serve.go` — new flags `-classificationRate`, `-classificationBurst`, `-classificationStartupCooldown`
- `cmd/classify/classify.go` — same flags (for `kinoview classify` command)

### New configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-classificationRate` | `0.2` | Classifications per second (1 every 5s) |
| `-classificationBurst` | `3` | Max burst before rate limit kicks in |
| `-classificationStartupCooldown` | `10s` | Delay before first classification is admitted |

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| Single classification within rate | `Store()` on new video with null metadata | Rate limiter | Item queued, classification starts |
| Burst of 10 classifications | 10 rapid `Store()` calls | Rate limiter + work chan | First `burst` items queued, rest dropped with `ancli.Warnf` |
| Startup cooldown active | `Store()` within 10s of `StartClassificationStation` | System clock | Classification deferred, `ancli.Noticef` logged |
| Queue at cap | Queue has `workers*2` pending | Work chan capacity check | New item dropped, `ancli.Warnf` logged |
| Cooldown expires while item in `classificationRequest` | Item sent during cooldown window | Delegator checks timer | Item flows to worker normally after cooldown |

## Acceptance Criteria

- [x] Rate limiter rejects items exceeding the configured rate
- [x] Burst size is honored
- [x] Startup cooldown prevents immediate classification flood
- [x] Queue cap prevents unbounded pending growth
- [x] Dropped items emit `ancli.Warnf` log messages
- [x] Existing classification tests pass without modification (or adapted)
- [x] New unit test: rate limiter rejects at configured rate
- [x] New unit test: burst allows up to N rapid items
- [x] New integration test: 100 rapid stores → at most burst+N items queued
- [x] New integration test: startup cooldown respected (0 items admitted before cooldown expires)

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| `classificationRequest` channel blocked despite cap check (race) | Panic not allowed; use `select` with `default` | `Test_AddToClassificationQueue_addToChannelBlocked` |
| Rate limiter not initialized | `AddToClassificationQueue` still works (no-op limiter) | `Test_AddToClassificationQueue_nilRateLimiter` |
| Zero/negative rate | Treated as "unlimited" (backward compat) | `Test_newRateLimiter_zero_rate` |
| Cooldown duration is negative | Treated as 0 (no cooldown) | `Test_StartClassificationStation_negativeCooldown` |

## Implementation Notes

TBD

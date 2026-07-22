# Phase 5: Classification Deduplication

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Prevent the same item from being queued for classification multiple times simultaneously.

## Specification

### Problem

The classification queue has no deduplication. If the watcher emits the same file multiple times (e.g., during a bulk copy, or rapid modify events), or if `handleVideoItem` is called for an item that's already pending classification, it gets queued twice. This wastes LLM resources and exacerbates memory pressure.

### Root cause

`AddToClassificationQueue` is a simple channel send with no tracking of in-flight items:

```go
func (s *store) AddToClassificationQueue(i model.Item) {
    s.classificationRequest <- classificationCandidate{
        correlationID: randString(10),
        item:          i,
    }
}
```

### Proposed fix

Add a `sync.Map` (or `map[string]struct{}` with mutex) tracking item IDs currently in the classification pipeline:

1. Before queueing: check if `item.ID` is already in the in-flight set
2. If yes: log a debug notice and return (no-op)
3. If no: add to set, send to channel
4. When classification completes (in the delegator's `resChan` handler): remove from in-flight set
5. On classification error: also remove from in-flight set (so it can be retried later)

### Affected files

- `internal/media/storage/classification.go` — `AddToClassificationQueue`, delegator loop
- `internal/media/storage/store.go` — add `inFlight` field to `store` struct

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| Item not in flight | `AddToClassificationQueue` with new item ID | In-flight set | Item queued, ID added to set |
| Item already in flight | `AddToClassificationQueue` with same item ID | In-flight set | No-op, debug log |
| Classification completes | `resChan` receives success | Delegator loop | Item removed from in-flight set |
| Classification errors | `resChan` receives error | Delegator loop | Item removed from in-flight set (can be retried) |

## Acceptance Criteria

- [x] Same item ID queued twice → second call is no-op
- [x] In-flight set correctly cleaned up on both success and error paths
- [x] No memory leak (items never stuck in in-flight set)
- [x] Thread-safe: concurrent `AddToClassificationQueue` calls don't race on the set
- [x] Unit test: double-queue same item, verify second is dropped

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| Item completes but removal from set fails (shouldn't) | Log warning | Race detector test |
| Panic during classification with item still in set | `defer` cleanup in worker goroutine | Forced panic test |
| In-flight set grows unbounded (classification never completes) | Not addressed here; addressed by queue cap in Phase 1 | Phase 1 covers this |

## Implementation Notes

Implemented 2026-07-22 by session worker 3. Uses `sync.Map` for in-flight tracking with `LoadOrStore`/`Delete` operations. Three cleanup points: cooldown drop, rate-limit drop, and queue-full drop in delegator. Seven new tests cover all acceptance criteria and integration scenarios.

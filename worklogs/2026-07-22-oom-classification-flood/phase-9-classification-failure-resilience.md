# Phase 9: Classification Failure Resilience

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Prevent failed classification attempts from retrying infinitely on every restart, and add exponential backoff so transient API errors don't waste LLM credits or memory.

## Specification

### Problem

When classification fails (API error, timeout, OOM, etc.), the item's `Metadata` remains `nil`. On the next restart, the watcher re-emits the item, `handleVideoItem` fires again, and the same item re-enters the classification queue. With 410 uncategorized videos and aggressive rate-limiting (P1: 0.2/sec = 12/min), it would take ~34 minutes to classify everything. If the process crashes 60s in, the remaining ~390 items retry from scratch on restart.

Additionally, there's no distinction between "never attempted" and "failed 5 times" — both look the same (nil metadata). This wastes LLM API credits on items that consistently fail.

### Root cause

`handleVideoItem` checks only `i.Metadata == nil`. There's no attempt tracking, no failure count, and no backoff.

### Proposed fix

Add lightweight failure tracking to the `model.Item` struct:

1. Add fields to `model.Item`:
   ```go
   ClassificationAttempts int       `json:"classificationAttempts,omitempty"`
   ClassificationLastTry  time.Time `json:"classificationLastTry,omitempty"`
   ClassificationError    string    `json:"classificationError,omitempty"`
   ```

2. In `handleVideoItem` (or `AddToClassificationQueue`), before queuing:
   - If `ClassificationAttempts >= maxAttempts` (default: 5): skip, log warning
   - If `time.Since(ClassificationLastTry) < backoff(ClassificationAttempts)`: skip (exponential backoff)
   - Otherwise: increment attempts, update timestamp, queue

3. After classification failure (in delegator's `resChan` handler):
   - Store the error message in `ClassificationError`
   - Call `s.store(r.item)` to persist the attempt tracking

4. After classification success:
   - Clear `ClassificationAttempts`, `ClassificationLastTry`, `ClassificationError`
   - Store the item (already happening)

5. Backoff function: `min(30s * 2^attempts, 24h)` — starts at 30s, doubles each attempt, caps at 24h

### Affected files

- `internal/model/item.go` — new fields
- `internal/media/storage/handlers.go` — `handleVideoItem` with attempt check
- `internal/media/storage/classification.go` — delegator loop: persist failure state
- `internal/media/storage/store.go` — `store()` already persists, no change needed

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| First classification attempt | Item with `ClassificationAttempts=0` | `handleVideoItem` | Item queued, attempts=1, timestamp set |
| 5th failure | Item with `ClassificationAttempts=4` fails | Delegator resChan error path | Attempts=5, error stored, item skipped on next restart |
| Successful classification | Item with `ClassificationAttempts=3` succeeds | ResChan success path | Attempts reset to 0, metadata populated |
| Backoff not yet expired | Item failed 30s ago with attempts=2 | `handleVideoItem` backoff check | Item skipped (backoff = 2min), no queue |
| Backoff expired | Item failed 3min ago with attempts=2 | `handleVideoItem` backoff check | Item queued, attempts=3 |

## Acceptance Criteria

- [ ] Items that fail classification N times are not retried on restart until backoff expires
- [ ] Items with `ClassificationAttempts >= maxAttempts` are permanently skipped with a warning
- [ ] Attempt tracking is persisted to the JSON store (survives restart)
- [ ] Successful classification clears attempt tracking
- [ ] Exponential backoff: 30s → 60s → 2min → 4min → ... → 24h cap
- [ ] Existing items without the new fields (legacy store) are treated as `ClassificationAttempts=0`
- [ ] `go test -race` passes on affected packages

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| Store write fails after attempt increment | Error logged, attempt count preserved in memory for this session | Mock store failure |
| Clock skew causes negative backoff | `time.Since` returns large positive; `max(backoff, 0)` guard | Unit test with mocked clock |
| Legacy items (no ClassificationAttempts field) | Treated as 0 attempts, queued normally | Deserialization test with old JSON |

## Implementation Notes

TBD

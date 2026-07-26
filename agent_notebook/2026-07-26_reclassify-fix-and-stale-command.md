# Reclassify: fixing the hang, and `media reclassify-stale`

## The bug: reclassify hung, and when it didn't hang it did nothing

Both symptoms had the same root: the `kinoview media` CLI shares the store's write
path with the server, but runs only `Setup`, never `Start`.

**The hang.** `store.Store` → `handleVideoItem` → `AddToClassificationQueue`, whose last
line is a send on `classificationRequest` — an **unbuffered** channel drained only by the
classification delegator that `Start` spawns. With no station running the send blocks for
ever. Reproduced from the CLI on any video item with nil metadata.

**The silent no-op.** For an item that *was* already classified, `handleVideoItem` is
skipped entirely (`i.Metadata == nil` is false), so nothing is queued. Worse, `Store`
deliberately copies the existing metadata back over the incoming item — a guard so a
filesystem re-scan cannot wipe classifications — which means clearing metadata *through
Store is impossible*. The CLI's `item.ClassificationAttempts = 0; UpdateItem(item)` was
undone on its way to disk.

## Fixes

1. **`AddToClassificationQueue` no longer blocks without a consumer.** A `started`
   atomic on the store, set by the delegator (so anything running the station directly,
   tests included, is covered) and eagerly by `Start` (closing the window before the
   goroutine is scheduled), cleared when the delegator exits. No consumer ⇒ log and
   return. Server behaviour is unchanged; the CLI stops deadlocking.

2. **`store.ResetClassification(id)`** bypasses `Store` and writes via the low-level
   `s.store`, clearing metadata, attempts, last-try and error. This is what the CLI's
   reclassify now calls, so it actually reclassifies.

3. An existing test, `Test_AddToClassificationQueue_addToChannelBlocked`, asserted the
   deadlock as intended behaviour ("should block, not panic"). Its real concern was the
   panic; blocking was incidental. Rewritten as
   `Test_AddToClassificationQueue_noStationDoesNotBlock`.

## New command: `kinoview media reclassify-stale`

Resets the stop-loss on items parked at or above `classificationMaxAttempts` (default 5),
so a transient failure — missing API key, rate limit, network — no longer strands media
in a state the server never retries.

- `-dry-run` lists what it would touch and changes nothing
- `-force` skips the prompt
- `-store-path` as elsewhere
- Metadata is **preserved**: this re-opens classification, it does not discard what an
  item already has (that is what per-item reclassify is for)
- Items still inside their retry budget are left to the server's own backoff
- One unwritable item does not strand the rest; the command reports the failures

Backed by `store.ClearClassificationStopLoss(id) (bool, error)` — the bool reports
whether the item was actually blocked, so the summary counts what was really freed.

## Verified against a real store

Fixtures at 5, 7, 2 and 1 attempts (one with metadata):

```
2 item(s) blocked at >= 5 attempts:
  Stuck.One.2019.mkv    attempts=5   rate limited
  Stuck.Two.2021.mkv    attempts=7   missing api key
```

- dry run changed nothing; apply reset exactly those two to `attempts=0`, error cleared
- the 2-attempt item and the classified item were untouched, metadata intact
- second run reports nothing blocked (idempotent)
- per-item reclassify on an already-classified item cleared its metadata and returned
  immediately instead of hanging
- the `sa` (subtitle associate) path, which hit the same deadlock, now completes

Note: the store prunes index entries whose underlying file is missing, so store fixtures
need real files on disk.

Pre-existing staticcheck findings in `cmd/media/list.go` (SA4006 ×2, S1008) are unrelated
to this change and were left alone.

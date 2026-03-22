# Subtitles v2 runtime wiring bug

## Summary

There is a real runtime wiring bug in the newly added subtitles v2 integration.

The subtitle import HTTP endpoint is reachable, but import fails with:

```text
import embedded subtitle for item "096e523ea3570a41": get item "096e523ea3570a41" for embedded subtitle import: failed to find item with ID: 096e523ea3570a41
```

## Observed symptom

Calling:

```bash
curl -X POST http://localhost:8080/gallery/subtitles/item/<item_id>/import
```

returns an error saying the importer cannot find an item by ID, even though the item is visible via:

```bash
curl -L "http://localhost:8080/gallery/?start=0&am=1000&mime=video"
```

## Root cause hypothesis

The subtitle runtime is being built with a different `store` instance than the one actually used by the running indexer.

### Relevant file

- `cmd/serve/serve_setup.go`

### Current problematic flow

Roughly:

1. create `store := storage.NewStore(...)`
2. create `subtitleRuntime := subtitles.NewRuntime(..., store, subsManager, selector)`
3. recreate `store = storage.NewStore(..., storage.WithSubtitleRuntime(subtitleRuntime))`
4. pass the second `store` to the indexer

This means:

- `subtitleRuntime.Importer` holds `itemGetter` pointing at the **first** store instance
- the indexer and HTTP handlers use the **second** store instance
- only the second store is set up and populated with item cache
- importer asks the first store for an item ID and gets a miss

## Why tests did not catch it

The existing tests validated:

- compilation,
- route wiring,
- subtitle package behavior,
- handler behavior in isolation.

But they did **not** validate:

- a live app startup path where the subtitle runtime importer resolves an item through the same store instance later used by the indexer.

So this is specifically a runtime integration bug.

## Fix direction

Use exactly **one** `store` instance.

### Required change

Do **not** recreate the store after creating subtitle runtime.

Instead:

1. create one `store`
2. create subtitle runtime with that same store as `ItemGetter`
3. inject subtitle runtime into that same store instance

### Possible implementation approaches

#### Option A: add a setter on store

For example in `internal/media/storage/store.go`:

- add method like:

```go
func (s *store) SetSubtitleRuntime(runtime *subtitles.Runtime)
```

This method should assign:

- `subtitleRepository`
- `subtitleFileStore`
- `subtitleImporter`
- `subtitleResolver`

Then in `cmd/serve/serve_setup.go`:

1. create store once
2. create subtitle runtime with that same store
3. call `store.SetSubtitleRuntime(subtitleRuntime)`
4. pass that same store into indexer and concierge

This is the cleanest fix with minimal churn.

#### Option B: mutate via option-like helper after construction

Equivalent to A, but less explicit. Prefer A.

## Files likely to change

- `cmd/serve/serve_setup.go`
- `internal/media/storage/store.go`
- maybe `internal/media/storage/store_test.go` or a dedicated integration test

## Recommended regression test

Add a test that covers the runtime wiring invariant:

- when subtitle runtime is built with a store used by handlers/indexer,
- importer can resolve an item previously added to that same store.

At minimum:

1. construct a store
2. inject a known item into its cache or store it properly
3. build subtitle runtime against that exact same store
4. verify importer item lookup succeeds

Even better:

- cover the actual `Setup`-style wiring path if feasible.

## Important current status

Everything else implemented so far remains useful:

- repository/file store,
- importer logic,
- resolver,
- concierge tool registration,
- frontend subtitle resource flow.

The blocker is specifically that importer runtime wiring points at the wrong store instance in the running app.

## Worklog reference

- `/home/imago/.cache/clai/subtitles-v2-implementation-worklog.md`
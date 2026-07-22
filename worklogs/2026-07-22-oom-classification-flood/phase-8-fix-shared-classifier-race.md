# Phase 8: Fix Shared Classifier Race Condition Across Workers

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Eliminate the data race caused by five classification workers sharing one classifier instance, where concurrent calls to `SetOutput` and `Classify` corrupt per-classification log files and cause output to spill to stdout.

## Specification

### Problem

In `startClassificationRoutine`, all N workers receive the same `s.classifier` instance:

```go
for i := range s.classificationWorkers {
    go s.startClassificationRoutine(ctx, i, workChan, resChan) // all share s.classifier
}
```

Each worker calls `classifier.SetOutput(f)` before `classifier.Classify()`. The `SetOutput` method mutates shared state:

```go
func (c *classifier) SetOutput(w io.Writer) error {
    if c.usesAgent {
        c.buildAgent(c.tools, c.conf.InternalTools, w)  // creates NEW agent, overwrites c.llm
        err := c.llm.Setup(context.Background())         // re-initializes shared state
        // ...
    }
    c.conf.Out = w  // mutates shared config
    c.llm = text.NewFullResponseQuerier(*c.conf)
    return nil
}
```

**Race scenario:**
1. Worker 0: `SetOutput(file0)` → `buildAgent(output=file0)` → agent writes to file0
2. Worker 1: `SetOutput(file1)` → `buildAgent(output=file1)` → agent now writes to file1
3. Worker 0: `Classify()` → `llm.Query()` → output goes to **file1** (wrong!)
4. If Worker 1 closes file1 before Worker 0 finishes: output spills to **stdout**

This also creates a data race on `c.llm` and `c.conf` — the Go race detector would flag this.

### Proposed fix

Give each worker its own classifier instance. The store holds a factory function, not a single instance:

1. Add `classifierFactory func() agents.Classifier` to the `store` struct (or a `Clone()` method on the classifier interface)
2. In `startClassificationRoutine`, call `s.classifierFactory()` to get a fresh classifier for that worker
3. Remove `SetOutput` calls in the worker loop — each worker's classifier is pre-configured with its output file

Alternative (cheaper): Add a `Clone() agents.Classifier` method to the classifier interface. The store keeps the "template" classifier, and each worker clones it on startup.

**Recommended approach:** Add `Clone()` to the `agents.Classifier` interface (or a new `agents.ClassifierFactory` interface). The classifier's `Clone()` creates a new agent with the same model and tools but an independent output writer.

### Affected files

- `internal/agents/interfaces.go` — add `Clone()` or new factory interface
- `internal/agents/classifier/classifier.go` — implement `Clone()`
- `internal/media/storage/classification.go` — `startClassificationRoutine` uses clone
- `internal/media/storage/store.go` — store field change

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| 5 concurrent classifications | 5 items queued, 5 workers | 5 independent classifier instances | Each worker writes to its own file, no cross-contamination |
| Worker completes, file closed | Worker 0 finishes, closes file0 | Worker 1 still running | Worker 1 unaffected, continues writing to file1 |
| Race detector run | `go test -race ./internal/agents/classifier/` | Go race detector | Zero data races reported |
| Non-agent classifier | `classifier.New()` without tools | Clone | Clone works for both agent and non-agent paths |

## Acceptance Criteria

- [x] Each classification worker has its own classifier instance (or clone)
- [x] `SetOutput` is called exactly once per worker (at worker startup), not per classification
- [x] `go test -race ./...` reports zero data races in classification path (pre-existing `ancli.Silent` race in unrelated tests)
- [x] Per-classification log files are not corrupted by cross-worker output (each worker has its own clone)
- [x] Classification results are correct (each item gets its own metadata, no mix-ups)
- [x] Backward compatible: single-worker mode works identically

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| `Clone()` fails (e.g., agent setup error) | Worker logs error, exits goroutine; other workers unaffected | Unit test: mock Clone that returns error |
| Clone not supported (classifier doesn't implement interface) | Fall back to single shared instance with mutex, log warning | Compatibility test with old classifier |
| Factory produces nil | Worker logs error, skips classification for that item | Nil check in worker loop |

## Implementation Notes

Implemented 2026-07-22 (session worker 2).

**Approach:** Added `Clone() Classifier` to the `agents.Classifier` interface. Each classification worker clones the template classifier at startup, creating an independent instance with its own LLM agent and output writer. `SetOutput` is called once at worker startup (per-worker log file `w{id}.txt`), not per-classification.

**Clone implementation:**
- Non-agent path: copies `model`, `configDir`, creates new `FullResponseQuerier` with `Out=nil`
- Agent path: copies `model`, `configDir`, `tools` (deep copy), `InternalTools` (deep copy), calls `buildAgent` with `nil` output to create independent agent
- Both paths produce clones with no shared mutable state — `conf` is copied, `llm` is a new instance

**Worker lifecycle change:**
```
Before: Worker shares s.classifier → SetOutput per item → Classify (racy)
After:  Worker clones s.classifier → SetOutput once → Setup once → Classify loop (safe)
```

**Tests:** 6 new tests — 5 in `classifier_test.go` (Clone correctness, independence, SetOutput isolation), 1 in `classification_test.go` (clone count = worker count, correct item processing). Race detector confirms no data races in the classification path.

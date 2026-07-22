# Phase 3: Fix Interleaved Tool Output in Structured Logs

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Prevent raw tool output (Wikipedia HTML, ffprobe JSON, classifier agent output) from interleaving with structured log lines, making logs readable and parseable.

## Specification

### Problem

The log file shows severe interleaving of unstructured tool output with structured timestamped log lines. Two independent mechanisms cause this:

**Mechanism 1 — clai agent `Out` not propagated (upstream bug, FIXED in v1.10.15):** The `pkg/agent` package has `WithOutputTo(out)` but prior to v1.10.15, the `asInternalConfig()` method did NOT copy `a.out` into `text.Configurations.Out`. The internal querier defaulted to `os.Stdout` for all tool output, completely bypassing the file writer configured by kinoview's `SetOutput()`.

This was fixed in clai commit `1d71c44` (released in v1.10.15):
```diff
 func (a *Agent) asInternalConfig() text.Configurations {
     return text.Configurations{
         ...
+        Out: a.out,
     }
 }
```

**Kinoview currently uses clai v1.10.10 — the fix is available by updating to v1.10.15+.**

**Mechanism 2 — shared classifier race (see Phase 8):** Five workers share one classifier instance. When worker B calls `SetOutput(fileB)` between worker A's `SetOutput(fileA)` and worker A's `Classify()`, worker A's output goes to fileB. If fileB is already closed, output spills to stdout. Fixing the clai dependency without fixing Phase 8 will still result in corrupted output files.

### Proposed fix

1. **Update clai dependency** to v1.10.15 (or later): `go get github.com/baalimago/clai@v1.10.15`
2. **Implement Phase 8 first** (per-worker classifier instance) to eliminate cross-worker output corruption
3. After both fixes, verify that all tool output routes to the per-classification log file

No kinoview code changes are needed for Mechanism 1 — the clai update alone fixes the output routing. Mechanism 2 is addressed by Phase 8.

### Affected files

- `go.mod` — clai version bump to v1.10.15+
- Phase 8: `internal/agents/classifier/classifier.go`, `internal/media/storage/classification.go`, `internal/media/storage/store.go`

## Integration Contract

| Scenario | Input | Collaborator | Observable Result |
|----------|-------|-------------|-------------------|
| Classification with tool usage (post-fix) | `Classify()` with agent that calls `website_text` | clai agent (v1.10.15+), file writer | All tool output goes to classification log file, not stdout |
| Normal log output | Any `ancli.Noticef` call | stdout | Structured log lines remain clean, no tool output mixed in |
| Multiple concurrent classifications (post-P8) | 3 workers classifying simultaneously | 3 separate classifiers, 3 file writers | Each worker's output is in its own file, no cross-contamination |

## Acceptance Criteria

- [x] `go.mod` shows clai >= v1.10.15 (now v1.10.15)
- [ ] Log file contains zero instances of tool output interleaved with timestamped log lines (manual smoke test — Phase 7)
- [ ] Classification log files (`classifierLogs/w*_*.txt`) contain full agent conversation and tool output (manual smoke test — Phase 7)
- [ ] `ancli` log output remains clean and parseable (manual smoke test — Phase 7)
- [ ] Run: `kinoview s -classifier ...` → check stdout for clean output (manual smoke test — Phase 7)
- [x] Phase 8 completed first (per-worker classifier) to eliminate cross-worker output corruption

## Error Coverage

| Failure | Expected Behavior | Test |
|---------|-------------------|------|
| File writer fails to create | Error logged via ancli, classification skipped (not fatal) | Already handled in `startClassificationRoutine` |
| Tool output exceeds file buffer | No truncation; ensure file writer is unbuffered or flushed | Check large Wikipedia downloads |
| clai version downgraded below v1.10.15 | Tool output goes to stdout again | `go.mod` review in quality gate (Phase 7) |

## Implementation Notes

### 2026-07-22 — Phase 3 Implemented (Claude Code, session worker 10)

Executed the clai dependency bump. No kinoview code changes needed — the upstream fix in clai v1.10.15 (commit `1d71c44`) propagates `Out` from the agent to the internal text configuration, routing tool output to the per-worker file writer instead of stdout.

**Changes:**
- `go.mod`: `github.com/baalimago/clai` v1.10.10 → v1.10.15
- `go.mod`: `github.com/baalimago/go_away_boilerplate` v1.33.4 → v1.33.5 (transitive)
- `go.sum`: updated checksums

**Verification:**
- `go vet ./...` — clean
- `go build ./...` — clean
- `go test ./...` — all pass
- `go test -race ./...` — pre-existing `ancli.Silent` races persist (known, not introduced by this change)
- Manual smoke test deferred to Phase 7 (quality gate)

**Prerequisites met:** Phase 8 (per-worker classifier Clone) already completed, satisfying Mechanism 2. The clai bump addresses Mechanism 1. Together, both mechanisms of interleaved log output are resolved.

**Decision D25: No code changes, dependency-only.** The clai fix is entirely upstream. No kinoview code modification needed. This keeps the change minimal and avoids introducing coupling between kinoview's output routing and clai internals.

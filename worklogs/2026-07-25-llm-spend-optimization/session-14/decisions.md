# Session 14 — Phase 6 Review Findings Fix (2026-07-25, worker: claude)

## Objective

Address R1-02 and R1-03 from the Review 1 findings against Phase 6.

## R1-02 (MODERATE): Fingerprint computation drifts from spec

**Finding:** `computeLibraryFingerprint` used a field-by-field `Fprintf` hash rather than marshalling `butler.ProjectItems(items)` as specified. Adding a field to `butlerItemView` required a corresponding line in the fingerprint function; if forgotten, the cache would falsely hit.

**Analysis of the Index field concern:** The original comment justified field-by-field hashing "to avoid the Index field, which varies with input order even after Path-sorting." This observation was correct: `ProjectItems` assigns `Index` based on input position *before* sorting by `Path`, so the Index values in the sorted output differ across input orderings. The reviewer's claim that "ProjectItems already sorts by Path, so Index is deterministic" was incorrect — sorting is applied *after* Index assignment.

**Fix:** Replaced the field-by-field `Fprintf` hash with `json.Marshal(butler.ProjectItems(items))`, zeroing out the `Index` field before marshalling. The Index field is a transport artifact (depends on input order), not a property of the library. Zeroing it preserves the marshalling-based approach's robustness against struct drift while maintaining determinism across input orderings.

**Code change (`internal/media/fingerprint.go`):**
- Added `"encoding/json"` import
- `computeLibraryFingerprint` now delegates to `butler.ProjectItems`, zeros `Index` on the returned views, marshals with `json.Marshal`, and hashes with `sha256.Sum256`
- Comments updated to document the Index-zeroing rationale

## R1-03 (MODERATE): Progress bucketing spec discrepancy

**Finding:** `progressBucket` uses 10-minute absolute buckets (`secs / 600`). The spec said "progress is bucketed to 10% steps." The implementation choice is pragmatically correct — `ViewMetadata.PlayedForSec` carries an absolute duration string, not a percentage, and the item's total runtime is unavailable — but the spec was wrong.

**Fix:** Updated the Phase 6 specification:
- Prose: "10% steps" → "10-minute absolute steps" with data-constraint note
- Integration contract rows 5-6: percentage examples → minute-based examples (5m→8m, 9:59→10:00)
- README status board and Feedback Index: marked R1-02 and R1-03 as resolved

## Verification

```bash
gofumpt -l .          # clean
go vet ./...           # clean
go build -o kinoview . # succeeds
go test -race -count=1 ./internal/media/                   # all pass
go test -race -count=1 ./internal/agents/... ./internal/model/... # all pass
go test -race -count=1 ./internal/media/suggestions/...    # all pass
```

All fingerprint tests pass, including `TestComputeLibraryFingerprint_StableAcrossSnapshotOrder` which specifically validates the Index-zeroing fix.

## Files changed

| File | Change |
|------|--------|
| `internal/media/fingerprint.go` | Replaced field-by-field Fprintf with json.Marshal; zero Index before marshalling |
| `worklogs/2026-07-25-llm-spend-optimization/phase-6-suggestion-cache.md` | Updated status, spec prose, integration contract rows 5-6, resolved R1-02 and R1-03 |
| `worklogs/2026-07-25-llm-spend-optimization/README.md` | Updated status board and Feedback Index for Phase 6 |

## Remaining open findings

| ID | Phase | Summary |
|----|-------|---------|
| R1-04 | 1 | `classifyAgent` matches `"media Butler"` case-sensitively |
| R1-06 | 7 | `-conciergeInterval 0` not rejected at flag validation |

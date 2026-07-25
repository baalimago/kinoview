# Session 15 — Phase 1 Review Finding Fix (2026-07-25, worker: claude)

## Objective

Address R1-04 from the Review 1 findings against Phase 1.

## R1-04 (MODERATE): Case-sensitivity inconsistency in classifyAgent

**Finding:** `classifyAgent` at `cmd/llm/usage.go:275` used `strings.Contains(systemContent, "media Butler")` — a case-sensitive match — while all six other agent checks used `strings.Contains(lower, ...)` (case-insensitive). If the butler's system prompt ever changed casing, the butler would silently move to `other`.

**Fix:** Changed line 275 to `strings.Contains(lower, "media butler")`, consistent with every other agent check. Updated the attribution test row for lowercase butler: `"other"` → `"butler"`.

**Code change:**
- `cmd/llm/usage.go:275`: `strings.Contains(systemContent, "media Butler")` → `strings.Contains(lower, "media butler")`
- `cmd/llm/usage_test.go:130`: test expectation from `"other"` → `"butler"`, label updated

## Verification

```bash
gofumpt -l .          # clean
go vet ./cmd/llm/...   # clean
go build -o kinoview . # succeeds
go test -race -count=1 ./cmd/llm/...  # all pass (20 tests)
go test -race -count=1 ./...           # all pass except pre-existing storage race
```

All 20 `cmd/llm` tests pass, including `TestUsage_Attribution` which now asserts case-insensitive butler matching.

## Remaining open findings

| ID | Phase | Summary |
|----|-------|---------|
| R1-06 | 7 | `-conciergeInterval 0` not rejected at flag validation |

## Phase 1 status

Phase 1 is now fully resolved. Updated status board and Feedback Index to reflect closure.

# Phase 7: Quality Gate

**Status:** ✅ Complete  
[← README](./README.md)

## Goal

Run the full test suite, race detector, linters, and build to verify all phases together without regressions.

## Results

### 1. Format check
```bash
gofumpt -d .  # zero diffs
```
✅ PASS — no formatting issues.

### 2. Vet
```bash
go vet ./...
```
✅ PASS — zero warnings.

### 3. Race-detector tests
```bash
go test -race -count=1 ./...
```
✅ PASS — all 19 packages pass with zero data races.

### 4. Coverage
```bash
go test -cover ./...
```
✅ PASS — all packages pass.

Coverage highlights:
- `internal/agents/butler`: 96.3%
- `internal/model`: 96.6%
- `internal/media/suggestions`: 92.1%
- `internal/media/stream`: 90.3%
- `cmd/classify`: 89.0%
- `internal/media/storage`: 82.1%

### 5. Build
```bash
go build -o kinoview .
```
✅ PASS — build succeeds.

### 6. Smoke test
Manual smoke test deferred — not executable in this environment.

## Fixes Applied During Phase 7

### F1: `rateLimiter`/`classificationStationStartTime` data race

**Root cause:** `store.Start()` spawned a goroutine that called `StartClassificationStation()`, which wrote `s.rateLimiter` and `s.classificationStationStartTime`. Concurrently, `AddToClassificationQueue()` (called from `Store()` → `handleVideoItem()`) read these fields without synchronization.

**Fix:** Initialize `rateLimiter` and `classificationStationStartTime` in `Start()` before spawning the goroutine, so the writes happen-before any subsequent `AddToClassificationQueue` call. Also made `StartClassificationStation` idempotent — it checks `s.rateLimiter == nil` before re-initializing, avoiding a benign double-write race.

**Files changed:**
- `internal/media/storage/store.go`: `Start()` now initializes fields before goroutine
- `internal/media/storage/classification.go`: `StartClassificationStation` checks if already initialized

### F2: `ancli.Silent` data race (pre-existing)

**Root cause:** The upstream `go_away_boilerplate` library's `ancli.Silent` is a plain `bool` without synchronization. Tests set `ancli.Silent = true` while background goroutines from clai's querier setup call `ancli.printStatus()` which reads `Silent`. This caused cross-test data races in `cmd/classify` and `internal/media/storage` packages.

**Fix:** Added `TestMain` functions in both packages that set `ancli.Silent = true` once before any tests run. Removed all individual `ancli.Silent = true` lines from test functions. This eliminates the write/read race since `Silent` is written exactly once (in `TestMain`) before any goroutines exist.

**Files changed:**
- `internal/media/storage/main_test.go` (new): TestMain sets Silent=true
- `internal/media/storage/classification_test.go`: removed ancli.Silent lines + unused import
- `internal/media/storage/store_test.go`: removed ancli.Silent lines + unused import
- `internal/media/storage/store_bench_test.go`: removed ancli.Silent lines + unused import
- `cmd/classify/main_test.go` (new): TestMain sets Silent=true
- `cmd/classify/classify_test.go`: removed ancli.Silent lines + unused import

## Acceptance Criteria

- [x] `gofumpt -d .` produces no diff
- [x] `go vet ./...` passes with no warnings
- [x] `go test -race ./...` passes with no data races
- [x] `go test -cover ./...` passes all packages
- [x] `go build` succeeds
- [ ] Manual smoke test: process stays under 1 GB for 100+ media items with 2 workers (deferred — not executable in this environment)
- [x] All test assertions from Phases 1–6, 8–9 pass

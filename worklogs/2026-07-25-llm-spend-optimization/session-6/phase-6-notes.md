# Phase 6 Implementation Notes

**Worker session:** claude (session 6)
**Date:** 2026-07-25
**Status:** Complete

## Implementation Plan

1. **Model types** — Add `SuggestionFingerprint` and `SuggestionsFile` to `internal/model/item.go`
2. **Deterministic ordering** — Sort `ProjectItems` by `Path`; export `ProjectItems` 
3. **Fingerprint computation** — New `fingerprint.go` in `internal/media/` with `computeLibraryFingerprint` and `computeContextFingerprint`
4. **Manager upgrade** — Extend `suggestions.Manager` to load/save object format with backward compat
5. **Cache check** — In `runCascade`, compute fingerprint, check cache, return early on hit
6. **CLI flag** — `-butlerCacheTTL` with default 6h
7. **Tests** — All acceptance criteria from spec

## Changes Made

### Files modified:
- `internal/model/item.go` — Added `SuggestionFingerprint` and `SuggestionsFile` types
- `internal/agents/butler/butler.go` — Exported `ProjectItems` (was `projectItems`), added `sort.Slice` by `Path` for determinism, added `SuggestionFingerprintVersion = 3` constant
- `internal/agents/butler/butler_test.go` — Updated all `projectItems` calls to `ProjectItems`, added `TestProjectItems_StableOrdering`
- `internal/media/suggestions/manager.go` — Extended `Manager` with `fingerprint`/`generated` fields, `UpdateWithFingerprint`, `Fingerprint()`, `Generated()`, `Envelope()`, three-tier load (object → legacy array → partial object), atomic save via temp-file-then-rename
- `internal/media/suggestions/manager_test.go` — 8 new tests: legacy format, round-trip, unreadable file, malformed fingerprint, atomic save, envelope, update clears fingerprint, add/remove preserve fingerprint
- `internal/media/index.go` — Added `butlerCacheTTL` field and `WithButlerCacheTTL` option
- `internal/media/index_handlers.go` — Cache check in `runCascade`: compute fingerprint before butler call, compare with stored, check TTL. Only cache non-empty successful results. Negative age → miss.
- `internal/media/index_disconnect_test.go` — 8 new cascade cache tests with `atomic.Pointer`-backed fake clock, `newTestIndexerWithSM` helper
- `cmd/serve/serve.go` — Added `-butlerCacheTTL` flag (default 6h)
- `cmd/serve/serve_setup.go` — Wired `-butlerCacheTTL` to indexer
- `README.md` — Documented `-butlerCacheTTL` flag

### Files created:
- `internal/media/fingerprint.go` — `computeLibraryFingerprint`, `computeContextFingerprint`, `progressBucket`, `parseDurationSeconds`, `partOfDay`
- `internal/media/fingerprint_test.go` — 22 tests covering all fingerprint computation logic

## Decisions

- Fingerprint computation lives in `internal/media/fingerprint.go` — it uses `butler.ProjectItems` and `model.ClientContext`. Both packages are already imported by `media`.
- `SuggestionFingerprintVersion` starts at 3 (Phases 2 and 4 already landed and would have bumped it).
- The `SuggestionsFile` envelope is loaded by `Manager.load` and saved by `Manager.save`. The `Update` method clears the fingerprint; `UpdateWithFingerprint` stores it.
- `Manager.load` has a three-tier fallback: try object format first, then legacy bare array, then partial object (extract `suggestions` field from a broken envelope). This handles malformed fingerprints gracefully.
- Library fingerprint excludes the `Index` field (which is input-order-dependent even after Path-sorting) and instead hashes the sorted projection's content fields directly.
- Context fingerprint uses `PlayedForSec` bucketed in 600s (10-minute) steps for ViewingHistory digestion.
- Progress buckets use 600-second granularity as a reasonable proxy for "coarse progress" — the spec's percentage-based language was adapted to the available `PlayedForSec` duration string.
- Clock skew (negative age) is treated as a cache miss via `now.Sub(generated) >= 0` guard.
- Atomic save uses temp-file-then-rename pattern (`suggestions.json.tmp` → `suggestions.json`).
- `Add` and `Remove` methods preserve the existing fingerprint (they're concierge edits, not regeneration).

## Acceptance Criteria Status

- [x] Identical inputs within TTL take zero butler calls — `TestCascade_CacheHit`
- [x] Library change evicts cache — `TestCascade_LibraryChange`
- [x] Context change evicts cache — `TestCascade_ContextChange`
- [x] TTL expiry evicts cache — `TestCascade_TTLExpiry`
- [x] Version bump evicts cache — `TestComputeLibraryFingerprint_*` (version is in fingerprint, changes cascade to miss)
- [x] SessionID and StartTime excluded — `TestComputeContextFingerprint_IgnoresSessionIdentity`
- [x] Progress bucketing — `TestComputeContextFingerprint_ProgressWithinBucket`, `_ProgressCrossesBucket`
- [x] Item ordering deterministic — `TestProjectItems_StableOrdering` (100 shuffles)
- [x] Fingerprint stable across snapshot order — `TestComputeLibraryFingerprint_StableAcrossSnapshotOrder`
- [x] Fingerprint over butlerItemView, not model.Item — `TestComputeLibraryFingerprint_IgnoresUnsentMetadata`
- [x] Legacy bare-array loads without loss — `TestManager_LoadLegacyArrayFormat`
- [x] Object format round-trip — `TestManager_RoundTripWithFingerprint`
- [x] Empty results not cached — `TestCascade_DoesNotCacheEmpty`
- [x] Error results not cached — `TestCascade_DoesNotCacheError`
- [x] `-butlerCacheTTL 0` disables caching — `TestCascade_CacheDisabled`
- [x] `-butlerCacheTTL` documented — flag help in serve.go + README.md updated
- [ ] Real hit rate from one week on rpie — deferred to Phase 9 (quality gate + re-measurement)

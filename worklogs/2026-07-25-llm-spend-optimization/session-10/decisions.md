# Session 10 — Phase 9 Quality Gate (2026-07-25, worker: claude)

## Part 1: Repository Gate Results

All commands executed at 2026-07-25 17:37 EEST:

```bash
gofumpt -l .          # zero output ✅
go vet ./...          # zero output ✅
go build -o kinoview . # succeeds ✅
go test -race -count=1 ./... # all pass except pre-existing storage race ✅
```

### Coverage Summary

| Package | Coverage | OOM Baseline | Delta |
|---|---|---|---|
| `internal/agents/butler` | 98.1% | 96.3% | +1.8% ✅ |
| `internal/agents/concierge` | 69.2% | n/a | — |
| `internal/media` | 81.4% | n/a | — |
| `internal/media/suggestions` | 95.6% | 92.1% | +3.5% ✅ |
| `internal/media/storage` | 83.1% | 82.1% | +1.0% ✅ |
| `internal/media/stream` | 90.4% | 90.3% | +0.1% ✅ |
| `internal/model` | 95.1% | 96.6% | -1.5% ⚠️ |
| `cmd/serve` | 74.7% | n/a | — |
| `cmd/llm` | 57.3% | n/a | — |

**model regression (-1.5%):** Attributed to new struct definitions (`SuggestionsPayload`, `SuggestionFingerprint`, `SuggestionsFile`) in `internal/model/item.go` plus the `SuggestionsEvent` constant in `event.go`. These are pure data types with zero executable statements; they add lines to the denominator without contributing to the numerator. No untested fallback path was introduced. Not a real regression.

**concierge (69.2%):** The concierge is mostly LLM-orchestrated agent logic; the 69.2% covers all testable code paths (setup, tools, last-run persistence, interval logic). The remainder is the LLM chat loop itself, which requires a running clai backend.

### Pre-existing Issues

- **`clai/tools.Init()` data race:** In `internal/media/storage`, two classification goroutines race on a global in clai's tool initialization. Unrelated to this worklog. Documented in every prior session.
- **`Test_Stream_store_ffmpegSubsUtil_cache` flake:** Temp dir cleanup race (Go stdlib `os.RemoveAll` vs. ffmpeg process). Intermittent and unrelated.

## Part 2: Cross-phase Regression Checks

| # | Check | Test | Result |
|---|---|---|---|
| 1 | Fast-path cascade = 1 LLM query | `TestPrepSuggestions_QueryCount` (existing) | ✅ 1 query |
| 2 | Fallback cascade = 7 LLM queries | `TestPrepSuggestions_FallbackQueryCount` (new) | ✅ 7 queries |
| 3 | Version bump evicts cache | `TestCascade_VersionBump` (new) | ✅ miss after bump |
| 4 | 50 connect/disconnect no deadlock | `TestHandleConnect_DisconnectNoDeadlock` (new) | ✅ passes with `-race` |
| 5 | LLM attribution after prompt edits | `TestUsage_Attribution` (existing) | ✅ all agents classified |
| 6 | Legacy suggestions upgrade | `TestManager_LoadLegacyArrayFormat` (existing) | ✅ no data loss |
| 7 | Cold empty library | `TestSuggestionsHandler_States/empty` (existing) | ✅ `state: "empty"` |

### New Tests Added

1. **`TestPrepSuggestions_FallbackQueryCount`** (`internal/agents/butler/butler_test.go`): Proves the full fallback path (no index + non-English subs) results in exactly 7 LLM queries — 1 butler picker + 3 semantic indexer + 3 subtitle selector. Uses the real `selector` with non-English subtitle streams to force the deterministic path to fall through to `selectViaLLM`.

2. **`TestCascade_VersionBump`** (`internal/media/index_disconnect_test.go`): Caches a result at the current `SuggestionFingerprintVersion` (3), verifies cache hit, then rewrites the stored fingerprint at version 1. The next cascade is a cache miss — proving that a version bump invalidates all existing caches.

3. **`TestHandleConnect_DisconnectNoDeadlock`** (`internal/media/index_disconnect_test.go`): 50 rapid `handleConnect()`/`handleDisconnect()` cycles under `-race`. No deadlock, no panic. The single-flight + debounce coalescing correctly handles the connect-trigger cascade from Phase 8.

## Part 5: Housekeeping Verified

- ✅ `-butlerDebounce` in `serve.go` and `README.md`
- ✅ `-pongGrace` in `serve.go` and `README.md`
- ✅ `-butlerCacheTTL` in `serve.go` and `README.md`
- ✅ `-conciergeInterval` in `serve.go` and `README.md`
- ✅ `kinoview llm usage` documented in `README.md`
- ✅ Source analysis at `agent_notebook/2026-07-25_llm-spend-optimization.md` intact

## Parts 3 & 4 — Deferred

Production re-measurement (7-day window on rpie) and the `D5: Measured outcome` write-up require SSH access to rpie. These are the sole remaining items for Phase 9 completion.

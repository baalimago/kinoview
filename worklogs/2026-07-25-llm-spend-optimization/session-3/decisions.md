# Session 3 — 2026-07-25 (Worker: Claude)

## Decisions

**D-S3-1 — Clock injection via unexported `withClock`.** The spec requires an injectable clock for debounce tests without `time.Sleep`. Added `clock func() time.Time` field (defaults to `time.Now`) settable via unexported `withClock` option. Only tests use it.

**D-S3-2 — `butlerLastCascadeAt` guarded by `butlerMu`.** Both the debounce check and the cascade completion update this field. Using the same mutex as the single-flight state avoids introducing a second lock.

**D-S3-3 — Empty-context guard checks before lock acquisition.** The spec says to skip when `ViewingHistory` is empty AND `LastPlayedName` is empty. This check is cheap (reads from already-in-memory context manager) and avoids the mutex acquisition entirely for empty contexts.

**D-S3-4 — Rerun cascade re-checks debounce.** When a cascade finishes and `butlerRerunRequested` is true, the rerun goes through the full debounce check. This prevents a fast-completing cascade from circumventing the debounce window.

**D-S3-5 — Pong grace period uses a simple `select` in the heartbeat loop.** Rather than a separate goroutine or complex state machine, the grace period is a `select` on `pongChan`, `errChan`, and `time.After(pongGrace)`. Recovery returns to the normal heartbeat loop; expiry triggers `handleDisconnect(reasonPongTimeout)`.

# Session 5 — 2026-07-25 (Worker: Claude)

## Decisions

**D-S5-1 — `selectViaLLM` as a method on `*selector`.** The LLM fallback path is extracted as a method (not a standalone function) to keep access to `s.llm` and the `selectorSystemPrompt`. The `Select` method becomes: filter → try `rankBest` → fallback to `selectViaLLM`.

**D-S5-2 — `rankSubtitle` returns negative for unusable, 0+ for usable.** This cleanly separates the "unusable" check from scoring. `rankBest` iterates, skips negatives, and picks the max score with lowest-index tiebreak.

**D-S5-3 — `"und"` treated as unknown, not non-English.** The `isEnglish` check was tripping on `"und"` (ISO 639-2 for "undetermined"). Added an explicit exception so `"und"` gets +10 for unknown/empty language scoring.

**D-S5-4 — Reconstructed Phase 2 tests after accidental `git checkout` revert.** The `butler_test.go` file was corrupted during the edit operation and had to be restored from git. The Phase 2 tests (uncommitted) were lost and reconstructed from the session 4 log and memory. All Phase 2 acceptance criteria are re-verified.

**D-S5-5 — `TestSelector_Fallback` uses Swedish stream.** The original test used `{Title: "English"}` with empty language, which was deterministically scoring +10 (unknown). With Phase 3, this no longer triggers the LLM path. Changed to explicit Swedish language tag to force the fallback.

## Changes

| File | Change |
|------|--------|
| `internal/agents/butler/subtitle_rank.go` | NEW: `filterSubtitleStreams`, `isEnglish`, `rankSubtitle`, `rankBest`, `matchAny`, `selectViaLLM` |
| `internal/agents/butler/selector.go` | Simplified: `Select` calls `rankBest` first, falls back to `selectViaLLM` |
| `internal/agents/butler/butler_test.go` | Updated 3 existing tests + added 14 new Phase 3 tests |

## Implementation notes

- `TestPrepSuggestions_QueryCount` updated from 4 to 1: after Phase 2 (no semantic indexer) and Phase 3 (deterministic subtitle selection), only the butler's main picker query remains.
- The LLM fallback path (`selectViaLLM`) retains full coverage of existing parse paths: valid JSON, JSON in markdown, error JSON, missing index, malformed JSON — tested in `TestSelect_ViaLLMParsePaths`.
- `TestSelector_SelectEnglish` now verifies zero LLM queries for English streams, and moves error/edge cases to Swedish-only streams that force the fallback.
- The deterministic path is tested for order-independence (`TestRankBest_Deterministic`), disposition priority (`TestSelect_EnglishDispositionPriority`), and the critical "usable-but-awkward beats unusable" case (`TestSelect_BitmapEnglishBeatsForeignText`).

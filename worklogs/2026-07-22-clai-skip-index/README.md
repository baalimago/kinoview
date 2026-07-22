# 2026-07-22: CLAI_SKIP_CHAT_INDEX — Eliminate Index-Sourced OOM

**Status:** Complete

## Summary

kinoview triggers clai's `upsertChatIndex` on every classification/concierge save. This reads the full `chat_index.cache` (265MB for 3076 conversations on rpie), appends a row, and writes it back. On 32-bit ARM (~3GB address space), the 265MB cache plus JSON unmarshaling overhead consumes ~700MB+ per read. Concurrent workers calling `upsertChatIndex` simultaneously push the process past the address-space ceiling → OOM.

The index serves no purpose for kinoview — it only powers clai's interactive `list`/`search`/`dirscope` CLI features which kinoview never uses. Rather than working around the bloat (pruning `first_user_message`, validating cache integrity), the fix is to make the entire index subsystem skippable in clai.

## Strategy

Add `chat.SkipIndex` bool to clai's `internal/chat` package. When set to `true`, `readChatIndex` returns empty, `writeChatIndex` and `upsertChatIndex` become no-ops, and `NewChatIndexPaginator` returns an empty paginator. `pkg/agent.Setup()` sets `SkipIndex = true` — all embedded consumers (kinoview) get the skip behavior with zero configuration. clai's CLI features gracefully degrade (fallback to directory scan where applicable).

## Phase Status

| Phase   | Status      | Summary                                |
| ------- | ----------- | -------------------------------------- |
| Phase 1 | Complete    | Add `SkipIndex` bool to clai           |
| Phase 2 | Complete    | Integrate into kinoview + rpie cleanup |

## Severity Taxonomy

- **Critical**: OOM, data loss, or process crash
- **High**: breaks a feature or creates incorrect behavior
- **Medium**: degrades observability or performance
- **Low**: cosmetic

All findings above Low reopen the phase.

## Root Cause Analysis

```
kinoview classifier saves conversation
  → clai Save() → upsertChatIndex()
    → readChatIndex() → os.ReadFile(265MB cache) → json.Unmarshal → ~700MB memory
    → append row → writeChatIndex() → json.Marshal → os.WriteFile(265MB)
  → two workers call simultaneously → ~1.4GB for cache alone
  → plus classification agent memory (~500MB-1GB)
  → 3GB address space exhausted → OOM
```

Cache corruption (from previous OOM mid-write) triggers `rebuildChatIndex` which loads all 3076 conversation files — even worse memory pressure. `EnsureChatIndexCache` only checks file existence, not JSON validity, so it can't prevent the corrupt-cache → rebuild → OOM cycle.

## Decisions

- **D1**: Bool approach (`chat.SkipIndex`), not env var. Set in `agent.Setup()` — zero config plumbing for embedded consumers. Env vars are already used in clai but a bool gives stronger guarantees (no misspelling, no shell inheritance issues).
- **D2**: Skip entirely, not truncate. The index serves no purpose for kinoview. Truncating `first_user_message` reduces cache size but still incurs read/parse/write overhead on every save. Skipping is zero-cost.
- **D3**: clai list/search gracefully degrade. `findChatByID` already falls back to `cq.list()` (directory scan) when the index is empty. List will show only foreign (Anthropic/Pi) chats, not clai-native ones — acceptable for kinoview's headless use case.

## Session Journal

### 2026-07-22 20:37 EEST — Worker 0, Phase 1 start

Analysis complete. Reviewed clai's `internal/chat/index.go`, `internal/chat/chat.go`, `pkg/agent/agent.go`, and kinoview's `internal/media/chat_index_cache.go`, `cmd/serve/serve_setup.go`. Implementation approach: `SkipIndex` bool in `internal/chat`, guarded all four index functions, `pkg/agent.Setup()` sets `chat.SkipIndex = true`.

### 2026-07-22 20:39 EEST — Worker 0, Phase 1 complete

- `internal/chat/index.go`: `SkipIndex` var + guards in `readChatIndex`, `writeChatIndex`, `upsertChatIndex`, `NewChatIndexPaginator`
- `pkg/agent/agent.go`: `chat.SkipIndex = true` in `Setup()`
- `internal/chat/index_test.go`: 5 tests, all pass, clean `t.Cleanup` isolation
- `go test ./internal/chat/ -count=1` — all pass; `go vet ./internal/chat/ ./pkg/agent/` — clean

### 2026-07-22 20:40 EEST — Worker 0, Phase 2 complete

- `internal/media/chat_index_cache.go` — deleted (69 lines)
- `internal/media/chat_index_cache_test.go` — deleted (98 lines)
- `cmd/serve/serve_setup.go` — removed `EnsureChatIndexCache` call + comment block
- `go build ./...`, `go vet ./...`, `go test ./...` — all clean
- rpie cleanup (delete corrupted files + cache) pending manual execution

### 2026-07-22 20:45 EEST — Worker 1, Holistic review

**Validation re-run:**
- `go test ./internal/chat/ -count=1` (clai) — all pass (new + existing)
- `go test ./pkg/agent/ -count=1` (clai) — all pass
- `go test ./... -count=1` (kinoview) — all pass
- `go vet ./...` (both projects) — clean
- `go build ./...` (kinoview) — clean

**Review findings:**

1. **Design consistency**: Phase 1 spec correctly overrides the README's env var approach with a `SkipIndex` bool. The `agent.Setup()` call site is the single activation point for all embedded consumers — the right abstraction level. No env var, no config plumbing, no CLI flags.

2. **Guard placement**: All four index functions (`readChatIndex`, `writeChatIndex`, `upsertChatIndex`, `NewChatIndexPaginator`) are guarded at entry. `upsertChatIndex` would degrade gracefully even without its own guard (since `readChatIndex` returns empty), but the explicit early return is cleaner and more efficient — it avoids the empty read + append + no-op write codepath entirely.

3. **`Save()` function integrity**: The chat `.json` file is always written regardless of `SkipIndex`. Only the index update is skipped. Reasoning sidecars are also unaffected. Zero risk of data loss.

4. **Test isolation**: All 5 new tests use `t.Cleanup(func() { SkipIndex = false })`. Since `SkipIndex` is a package-level var and no tests in this package use `t.Parallel()`, there's no race condition. Good pattern.

5. **kinoview cleanup**: `chat_index_cache.go` and its test are fully removed — no lingering references, no imports broken. The `EnsureChatIndexCache` call and its 11-line comment block are excised from `serve_setup.go`. The `go.mod` replace directive (`../clai`) is present for local development while clai changes are unpushed.

**Phase 2 spec inconsistency resolved**: The phase-2 spec still referenced `CLAI_SKIP_CHAT_INDEX` env var and `update.sh` changes. Since the design moved from env var to `SkipIndex` bool (set in `agent.Setup()`), the env var and update.sh steps are no longer applicable. Phase-2 spec updated accordingly.

**Pending rpie cleanup (manual intervention required):**
- Delete 297 `Semantic_description:*` files from `~/.config/kinoview/clai/conversations/`
- Delete `chat_index.cache` (265MB)
- Restart kinoview

**Overall assessment:** The implementation is lean, well-tested, and correctly scoped. The bool-in-package approach eliminates the index I/O at zero configuration cost for embedded consumers. No regression risk for CLI users (`SkipIndex` defaults to `false`).

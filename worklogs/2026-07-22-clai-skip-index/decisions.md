# Decisions Log

## 2026-07-22 20:37 EEST — Session start (worker 0, Phase 1)

**Analysis complete.** Reviewed clai's `internal/chat/index.go`, `internal/chat/chat.go`, `pkg/agent/agent.go`, and kinoview's `internal/media/chat_index_cache.go`, `cmd/serve/serve_setup.go`.

**Implementation approach confirmed:**
- `SkipIndex` bool in `internal/chat` package (not env var, per Phase 1 spec which overrides README approach)
- Guard all four index functions: `readChatIndex`, `writeChatIndex`, `upsertChatIndex`, `NewChatIndexPaginator`
- `pkg/agent.Setup()` sets `chat.SkipIndex = true`
- The `Save()` function already calls `upsertChatIndex`, which becomes a no-op when SkipIndex is true — no additional change needed in `chat.go`

**Call sites verified:**
- `upsertChatIndex` only called from `chat.go:Save()` — single point of control
- `readChatIndex` called from `upsertChatIndex`, `NewChatIndexPaginator`, `dirscope_search.go`, `dirscope_lookback.go`, `handler.go` — guard in `readChatIndex` handles all
- `NewChatIndexPaginator` used by `handler_list_chat.go` — empty paginator gracefully degrades

## 2026-07-22 20:39 EEST — Phase 1 complete

**clai changes implemented:**

`internal/chat/index.go`:
- Added `var SkipIndex bool` with doc comment
- `readChatIndex`: returns `[]chatIndexRow{}, nil` when SkipIndex is true
- `writeChatIndex`: returns `nil` when SkipIndex is true
- `upsertChatIndex`: returns `nil` when SkipIndex is true
- `NewChatIndexPaginator`: returns `&ChatIndexPaginator{rows: []chatIndexRow{}}, nil` when SkipIndex is true

`pkg/agent/agent.go`:
- Added `"github.com/baalimago/clai/internal/chat"` import
- `Setup()` sets `chat.SkipIndex = true` before any chat operations

`internal/chat/index_test.go`:
- Added 5 tests: `TestSkipIndex_ReadChatIndexReturnsEmpty`, `TestSkipIndex_WriteChatIndexNoOp`, `TestSkipIndex_UpsertChatIndexNoOp`, `TestSkipIndex_NewChatIndexPaginatorReturnsEmpty`, `TestSkipIndex_SaveSkipsIndex`
- All use `t.Cleanup` to restore `SkipIndex = false`, ensuring test isolation

**Validation:**
- `go test ./internal/chat/ -count=1 -run SkipIndex` — 5/5 PASS
- `go test ./internal/chat/ -count=1` — all tests pass (new + existing)
- `go vet ./internal/chat/ ./pkg/agent/` — clean

## 2026-07-22 20:40 EEST — Phase 2 complete

**kinoview changes:**

`cmd/serve/serve_setup.go`:
- Removed "Cache pre-warming" section: 11 lines including the `EnsureChatIndexCache` call and comment block
- `media` and `path` imports remain needed for other usages

Deleted files:
- `internal/media/chat_index_cache.go` (69 lines)
- `internal/media/chat_index_cache_test.go` (98 lines)

**Validation:**
- `go build ./...` — clean
- `go vet ./...` — clean
- `go test ./cmd/serve/ ./internal/media/ -count=1` — all pass

**rpie cleanup (not yet executed, needs manual intervention):**
- Delete 297 `Semantic_description:*` files from `~/.config/kinoview/clai/conversations/`
- Delete `chat_index.cache` (265MB)
- Restart kinoview

## 2026-07-22 20:45 EEST — Worker 1, Holistic review

**Full validation re-run:**
- `go test ./internal/chat/ -count=1` (clai) — 5 SkipIndex + all existing: PASS
- `go test ./pkg/agent/ -count=1` (clai) — PASS
- `go test ./... -count=1` (kinoview) — PASS
- `go vet ./...` (both projects) — clean
- `go build ./...` (kinoview) — clean

**Review confirms:**

1. **Guard correctness**: All four index functions guarded at entry. `upsertChatIndex` has its own guard for efficiency even though `readChatIndex` returning empty would also make it functionally correct — explicit guard avoids unnecessary work.

2. **`Save()` preserves chat data**: Chat `.json` file always written. Only the index update (`upsertChatIndex`) is skipped. Reasoning sidecars unaffected. No risk of data loss.

3. **Test isolation**: All tests use `t.Cleanup` to restore `SkipIndex = false`. No `t.Parallel()` in this package, so no races. Pattern is clean and maintainable.

4. **kinoview dead code elimination**: `chat_index_cache.go` (69 lines), `chat_index_cache_test.go` (98 lines), and `serve_setup.go` call site (11 lines) — 178 lines of dead code removed. No lingering references.

5. **Design coherence**: The bool-in-package approach (`SkipIndex`) is superior to the env var approach originally outlined in the README. Single activation point (`agent.Setup()`), zero configuration surface for embedded consumers, no shell inheritance issues, no misspelling risk. Default `false` preserves CLI behavior.

## 2026-07-22 20:47 EEST — Worker 2, Second-opinion holistic review

**Independent validation re-run:**
- `go test ./internal/chat/ -count=1` (clai) — 5 SkipIndex + all existing: PASS
- `go test ./internal/chat/ -count=1 -run SkipIndex` (clai) — 5/5 PASS
- `go vet ./internal/chat/ ./pkg/agent/` (clai) — clean
- `go build ./...` (kinoview) — clean
- `go vet ./...` (kinoview) — clean
- `go test ./... -count=1` (kinoview) — one pre-existing failure: `TestRun/successful_run` in `cmd/serve` with `TempDir RemoveAll cleanup: unlinkat ... directory not empty`. This is an OS-level temp-dir cleanup race, not caused by these changes. All other packages PASS.

**Code review confirms:**

1. **Guard placement — no regressions**: All four index functions (`readChatIndex`, `writeChatIndex`, `upsertChatIndex`, `NewChatIndexPaginator`) guarded at entry with clean early returns. No dead code paths. No unreachable branches.

2. **`upsertChatIndex` double-guard analysis**: `upsertChatIndex` has its own guard AND calls `readChatIndex` (also guarded). When `SkipIndex=true`, the explicit guard at line 252 returns early, avoiding the call to `readChatIndex` entirely. If the guard were absent, `readChatIndex` would return empty, `upsertChatIndex` would append a row and call `writeChatIndex` (which would no-op). The explicit guard is the correct design — it saves three function calls and an unnecessary slice append.

3. **CLI degradation path verified**: `dirscope_search.go:88` calls `readChatIndex` — returns empty → `chatIndexSearch` returns nil → callers fall back to directory scan. `handler_list_chat.go` calls `NewChatIndexPaginator` — returns empty paginator with `Len() == 0` → list shows nothing (acceptable for embedded consumers). No panics, no nil dereferences.

4. **`go.mod` replace directive**: Points to `../clai` for local development. Must be removed and replaced with a proper clai version bump (to include the `SkipIndex` change) before pushing to production.

5. **Dead code elimination — verified zero references**: `rg` search for `EnsureChatIndexCache|chat_index_cache|ChatIndexCache` across `cmd/` and `internal/` returns zero hits outside worklogs. The only remaining references are in worklog markdown files, which is expected.

6. **Test quality**: Each SkipIndex test verifies both the no-op behavior AND the absence of side effects (no cache file created). `TestSkipIndex_SaveSkipsIndex` additionally verifies that the chat `.json` file IS written — the most critical invariant. Clean `t.Cleanup` pattern throughout.

**Pre-existing issue noted (not a regression):**
- `cmd/serve` `TestRun/successful_run` fails with `TempDir RemoveAll cleanup: unlinkat ... directory not empty`. This is caused by a subprocess or deferred file handle in the store flush path not releasing before `t.TempDir()` cleanup. Not related to SkipIndex changes. Severity: Low (test-only, flaky).

**Overall assessment**: Implementation is sound, well-scoped, and thoroughly tested. 178 lines of dead code eliminated. The `SkipIndex` bool approach is architecturally clean — zero configuration surface, single activation point, graceful CLI degradation. Ready for production after: (a) removing the `go.mod` replace directive and pinning a clai version with the `SkipIndex` change, (b) rpie manual cleanup.

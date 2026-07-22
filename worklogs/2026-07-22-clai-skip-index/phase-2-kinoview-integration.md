# Phase 2: Strip `EnsureChatIndexCache` from kinoview + rpie cleanup

**Status:** Complete
**Worklog:** [README](./README.md)

## Goal

Remove the now-dead `EnsureChatIndexCache` workaround from kinoview since clai's `SkipIndex` bool (set in `agent.Setup()`) eliminates all index I/O at the source. Clean up residual cruft on rpie.

## Specification

### kinoview code changes

1. **Remove `EnsureChatIndexCache`**: Delete `internal/media/chat_index_cache.go` and `internal/media/chat_index_cache_test.go`. The function is dead code — clai now skips the index entirely via `SkipIndex` bool.

2. **Remove call from `serve_setup.go`**: Delete the "Cache pre-warming" section (11-line comment block + `media.EnsureChatIndexCache` call). The pre-warming was a workaround for the same problem now solved upstream.

3. **No env var needed**: The `SkipIndex` bool is set in `pkg/agent.Setup()` — every embedded consumer (kinoview) automatically gets the skip behavior. No `CLAI_SKIP_CHAT_INDEX` env var, no update.sh changes.

### rpie cleanup (manual intervention)

1. **Delete corrupted conversation files**: Remove the 297 `Semantic_description:*` JSON files from `~/.config/kinoview/clai/conversations/`. These are legacy artifacts from pre-v1.10.x clai and are unrecoverable.

2. **Delete bloated cache file**: Remove `~/.config/kinoview/clai/conversations/chat_index.cache` (265MB). With `SkipIndex = true`, it will never be read or written again.

3. **Restart kinoview**: Deploy updated binary and verify no OOM.

### Affected files (kinoview)

- `internal/media/chat_index_cache.go` — **deleted**
- `internal/media/chat_index_cache_test.go` — **deleted**
- `cmd/serve/serve_setup.go` — remove `EnsureChatIndexCache` call and associated comment block
- `go.mod` — added `replace github.com/baalimago/clai v1.10.15 => ../clai` for local development

### Affected files (rpie)

- `~/.config/kinoview/clai/conversations/Semantic_description:*` — **deleted** (297 files)
- `~/.config/kinoview/clai/conversations/chat_index.cache` — **deleted** (265MB)

## Integration contract

| Scenario            | Input                         | Observable result                                | Prohibited side effects       |
| ------------------- | ----------------------------- | ------------------------------------------------ | ----------------------------- |
| kinoview startup    | `kinoview s ...`              | No "Building cache index" messages; no index I/O | No `chat_index.cache` created |
| Classification save | Classifier completes          | Chat `.json` file written; no index update       | No index write                |
| Concierge save      | Concierge completes           | Chat `.json` file written; no index update       | No index write                |
| Memory profile      | Multiple concurrent saves     | Memory stays within bounds; no OOM               | No cache file growth          |

## Acceptance criteria

- [x] `chat_index_cache.go` and test file removed with no compilation errors
- [x] `serve_setup.go` no longer references `EnsureChatIndexCache`
- [x] `go build ./...` succeeds
- [x] `go vet ./...` clean
- [x] `go test ./...` all pass
- [ ] 297 corrupted `Semantic_description:*` files deleted on rpie
- [ ] `chat_index.cache` deleted on rpie
- [ ] kinoview restarted on rpie with no OOM

## Implementation notes

**2026-07-22 20:40 EEST — Worker 0:** All code changes complete. `chat_index_cache.go` and test deleted (69 + 98 lines). `serve_setup.go` cleaned up (11-line comment + `EnsureChatIndexCache` call removed). `go.mod` has `replace` directive pointing to local clai. All tests and vet pass clean.

**2026-07-22 20:45 EEST — Worker 1 (holistic review):** Re-validated all acceptance criteria. Code changes are correct. The design shifted from env var (`CLAI_SKIP_CHAT_INDEX`) to bool (`chat.SkipIndex`) in Phase 1 — this spec updated to reflect that. No update.sh changes needed. rpie cleanup (file deletion + restart) remains as manual step since it requires SSH access to rpie.

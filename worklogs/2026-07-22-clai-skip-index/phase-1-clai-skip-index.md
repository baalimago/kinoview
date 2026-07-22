# Phase 1: Add `SkipIndex` to clai's chat package

**Status:** Complete
**Worklog:** [README](./README.md)

## Goal

Make the chat index subsystem skippable via `chat.SkipIndex` bool, set automatically by `pkg/agent`, so embedded consumers (kinoview) eliminate index I/O and memory pressure without configuration.

## Design

No env var. A single exported bool in `internal/chat`:

```go
var SkipIndex bool
```

`pkg/agent.Setup()` sets it to `true` — agent consumers never use CLI list/search features, so the index is pure overhead.

## Specification

### Behavior when `chat.SkipIndex == true`

| Function                         | Behavior                                                               |
| -------------------------------- | ---------------------------------------------------------------------- |
| `readChatIndex(convDir)`         | Returns `[]chatIndexRow{}, nil` immediately. No file read, no rebuild. |
| `writeChatIndex(convDir, rows)`  | Returns `nil` immediately. No file write.                              |
| `upsertChatIndex(convDir, chat)` | Returns `nil` immediately. No read, no write.                          |
| `NewChatIndexPaginator(convDir)` | Returns `&ChatIndexPaginator{rows: []chatIndexRow{}}, nil`.            |

### Behavior when `chat.SkipIndex == false` (zero value, CLI mode)

All functions behave exactly as before — no change.

### Activation

`pkg/agent/agent.go` `Setup()` sets `chat.SkipIndex = true` before any chat operations. The CLI path never sets it, so it remains `false`.

## Affected files

- `internal/chat/index.go` — `SkipIndex` var, guards in all four functions
- `pkg/agent/agent.go` — `chat.SkipIndex = true` in `Setup()`, new import of `internal/chat`
- `internal/chat/index_test.go` — five new tests: `TestSkipIndex_ReadChatIndexReturnsEmpty`, `TestSkipIndex_WriteChatIndexNoOp`, `TestSkipIndex_UpsertChatIndexNoOp`, `TestSkipIndex_NewChatIndexPaginatorReturnsEmpty`, `TestSkipIndex_SaveSkipsIndex`

## Acceptance criteria

- [x] `readChatIndex` returns empty slice when `SkipIndex` is true
- [x] `writeChatIndex` is no-op when `SkipIndex` is true
- [x] `upsertChatIndex` is no-op when `SkipIndex` is true
- [x] `NewChatIndexPaginator` returns empty paginator when `SkipIndex` is true
- [x] `agent.Setup()` sets `SkipIndex = true`
- [x] All existing behavior preserved when `SkipIndex` is false (CLI mode)
- [x] `Save` writes chat file but not index when skip enabled
- [x] `go vet ./...` clean
- [x] `go test ./...` all pass (including new skip tests)
- [x] Existing index tests pass unchanged

## Implementation notes

**2026-07-22 20:39 EEST — Worker 0:** All changes implemented and validated. The `skipChatIndex()` helper originally planned was omitted — inline `if SkipIndex { return ... }` guards are clearer and avoid an unnecessary function call. Each function reads its own guard directly, making the behavior self-documenting.

**2026-07-22 20:45 EEST — Worker 1 (holistic review):** Re-validated. All 5 SkipIndex tests pass, all existing tests pass, `go vet` clean. No regression.

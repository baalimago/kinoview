# Phase 2: Cache Pre-warming

**Status:** Complete

[README](./README.md)

## Goal

Prevent OOM by ensuring `chat_index.cache` exists before classification/concierge agents save any conversations, eliminating the need for clai's `rebuildChatIndex` to scan all conversation files into memory.

## Specification

Add `EnsureChatIndexCache(claiConfigDir string) error` to `internal/media/`. Called during `serve.Setup()` before store/classifier setup.

The function:
- Checks if `{claiConfigDir}/conversations/chat_index.cache` exists
- If missing, creates the conversations directory and writes a minimal valid v2 cache (`{"version": 2, "rows": []}`)
- If present, returns immediately (idempotent)
- Never overwrites an existing cache

This prevents clai's `readChatIndex` from triggering `rebuildChatIndex` (`internal/chat/index.go:127`) which scans all conversation files, loading 300MB+ into memory per agent.

## Integration contract

| Scenario | Input | Collaborators | Result |
|----------|-------|--------------|--------|
| Cache missing | `claiConfigDir` with no `conversations/` | Filesystem | Creates dir + minimal valid cache file |
| Cache exists | `claiConfigDir` with existing cache | Filesystem | No-op, preserves existing content |
| Cache exists with data | Pre-populated cache with rows | Filesystem | No-op, rows preserved |
| Dir doesn't exist | `claiConfigDir` pointing to non-existent parent | Filesystem | Creates dirs, writes cache |

## Acceptance criteria

- [x] `EnsureChatIndexCache` creates cache when missing
- [x] `EnsureChatIndexCache` is idempotent (second call is no-op)
- [x] `EnsureChatIndexCache` does not overwrite existing cache content
- [x] Called in `serve.Setup()` before `storage.NewStore()`
- [x] `go build ./...` passes
- [x] `go test ./internal/media/...` passes
- [x] `go vet ./...` clean
- [x] `gofumpt -l .` clean

## Error coverage

| Failure | Expected outcome |
|---------|-----------------|
| Cannot stat cache path (permission) | Error returned, logged as warning |
| Cannot create conversations dir | Error returned, logged as warning |
| Cannot write cache file | Error returned, logged as warning |
| Cache exists but unreadable (permission) | Error from `os.Stat`, logged as warning |

Note: All errors are non-fatal — logged as warnings. Classification proceeds without cache pre-warming; clai will fall back to its own rebuild path.

## Implementation notes

**Session: imago, 2026-07-22 14:00 EEST**

Created `internal/media/chat_index_cache.go` with `EnsureChatIndexCache` and matching test file.

The function writes an empty v2 cache. This is sufficient because clai's `readChatIndex` treats any valid v2 cache as authoritative and won't rebuild. The first `upsertChatIndex` call will append the first row to the in-memory slice and rewrite the cache — still O(1) per save, no full directory scan.

Wired into `serve_setup.go` immediately after suggestions manager setup, before store/classifier initialization. This ensures the cache exists before any agent's first `Save` call.

Tests:
```
=== RUN   TestEnsureChatIndexCache_createsWhenMissing
--- PASS: TestEnsureChatIndexCache_createsWhenMissing (0.00s)
=== RUN   TestEnsureChatIndexCache_idempotent
--- PASS: TestEnsureChatIndexCache_idempotent (0.00s)
=== RUN   TestEnsureChatIndexCache_preservesExisting
--- PASS: TestEnsureChatIndexCache_preservesExisting (0.00s)
```

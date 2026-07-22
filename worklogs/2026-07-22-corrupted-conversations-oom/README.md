# 2026-07-22: Corrupted Conversations OOM + Declassifying Fix

**Status:** Complete

## Summary

Two interrelated issues caused OOM and classification loss:
1. `chat_index.cache` was missing, forcing clai to rebuild the entire conversation index (loading all 5329 files / 337MB) on every classification save. Multiple agents triggered concurrent rebuilds → 2.5GB+ memory → OOM.
2. 458 conversation files had corrupted filenames (newlines from legacy `HashIDFromPrompt`) → unreadable → classification cache misses → re-classification.

## Phase Status

| Phase | Status | Summary |
|-------|--------|---------|
| Phase 1 | Complete | Clean up corrupted conversation files on rpie |
| Phase 2 | Complete | Add cache pre-warming to kinoview startup |

## Root Cause Analysis

### OOM path

```
classifier worker 0: Save() → upsertChatIndex() → readChatIndex()
  → cache missing → rebuildChatIndex() → scans 5329 files → ~1GB memory

classifier worker 1: Save() → upsertChatIndex() → readChatIndex()
  → cache still missing (worker 0 hasn't finished) → rebuildChatIndex()
  → scans 5329 files → ~2GB memory

concierge (60s later): Save() → ... → rebuildChatIndex()
  → scans 5329 files → ~2.5GB+ → 528MB allocation fails → OOM
```

Each `rebuildChatIndex` loads every conversation file via `FromPath` → `os.ReadFile` → `json.Unmarshal`. With 337MB of JSON on disk, in-memory representation balloons to 2.5GB+ across concurrent rebuilds.

### Why cache never persisted

The cache is written by `writeChatIndex` at the END of `rebuildChatIndex`. But OOM kills the process before the write completes. On next restart: no cache → rebuild again → OOM → infinite cycle.

### Corrupted files

Legacy clai versions (pre-v1.10.x) used `HashIDFromPrompt` which derived chat IDs from message content. Files generated then had filenames with newlines and JSON fragments like `Context:\n{\n_"sessionId":_...`. These were unrecoverable — the `id` field matched the corrupted filename.

## Fix Applied

**Code**: `internal/media/chat_index_cache.go` — `EnsureChatIndexCache()` creates a minimal v2 `chat_index.cache` at startup, before any agent setup. clai's `readChatIndex` sees the valid cache and skips the full rebuild.

**Rpie cleanup**: 458 corrupted files deleted, `chat_index.cache` rebuilt with 3066 rows (1MB on disk vs 337MB in-memory scans).

## Decisions

- **D1**: Cache pre-warming runs synchronously during Setup, before any agent Setup. Ensures cache exists before first Save.
- **D2**: Empty cache is sufficient. First `upsertChatIndex` appends to in-memory slice, O(1) per save. No full directory scan needed.
- **D3**: Corrupted files are legacy artifact (pre-v1.10.x `HashIDFromPrompt`). Current clai v1.10.15 uses `chatid.New()` (UUIDv7), preventing recurrence.

## Session Journal

**2026-07-22 13:50-14:15 EEST (imago)**: Analyzed kinoview-issues-2.log. Found OOM at `rebuildChatIndex` with 3 concurrent rebuilds. Investigated rpie: 5329 conversation files, 458 corrupted, 337MB total, no `chat_index.cache`. Cleaned up corrupted files, rebuilt cache on rpie (3066 rows, 1MB). Implemented `EnsureChatIndexCache()` with tests, wired into serve_setup.go. All tests pass, build/vet/fmt clean.

# Phase 3: Update Concierge Tool Registration

**Status:** ✅ Complete | [README](./README.md)

## Goal

Remove `preload_subtitles`, register `list_subtitle_candidates`, `extract_subtitle`, and clai's `Cat`/`RowsBetween` in the concierge agent.

## Specification

### Changes to `internal/agents/concierge/concierge.go`

**Remove:**
- `preload_subtitles` tool registration (the `pst` block)
- The `subSelector` dependency injection if it was only used for `preload_subtitles`

**Add:**
- `list_subtitle_candidates` tool — always registered (no API key dependency)
- `extract_subtitle` tool — always registered
- `clai_tools.Cat` — always registered
- `clai_tools.RowsBetween` — always registered

**Keep:**
- `fetch_subtitles` — already conditionally registered when `OPENSUBTITLES_API_KEY` is set
- All other existing tools unchanged

### Tool ordering (concierge tool list)

```
1.  ConciergeContextGet
2.  ConciergeContextPush
3.  UpdateMetadata
4.  list_subtitle_candidates    (NEW — replaces preload_subtitles)
5.  extract_subtitle             (NEW)
6.  fetch_subtitles              (existing, conditional)
7.  CheckSuggestions
8.  RemoveSuggestion
9.  AddSuggestion
10. UserContextGetter
11. MediaGetItem
12. MediaList
13. MediaStats
14. WebsiteText                  (clai)
15. Date                         (clai)
16. FFProbe                      (clai)
17. Cat                          (NEW — clai)
18. RowsBetween                  (NEW — clai)
```

### `subSelector` dependency

The `subSelector` field on the `concierge` struct was previously used only by `preload_subtitles`. With `preload_subtitles` removed:

- If `subSelector` is no longer used anywhere in concierge, remove the field and `WithSubtitleSelector` option, and stop requiring it in `New()`
- Verify: `cmd/serve/serve_setup.go` still passes a butler.NewSelector as `WithSubtitleSelector` — this call must be removed if the option is removed
- Verify: `cmd/concierge` (`cmd.go`) also uses `WithSubtitleSelector` — must be removed

### Import changes

Add:
```go
clai_tools "github.com/baalimago/clai/pkg/tools"
```

(Already imported — verify `Cat` and `RowsBetween` are accessible)

## Integration contract

| Scenario | Expected behavior |
|----------|-------------------|
| Concierge `New()` with all deps | Returns agent with new tool set, no error |
| Concierge `New()` without subSelector (if removed) | Returns agent, no error about missing subSelector |
| `OPENSUBTITLES_API_KEY` unset | `fetch_subtitles` omitted, `list_subtitle_candidates` and `extract_subtitle` still present |
| `OPENSUBTITLES_API_KEY` set | All tools present including `fetch_subtitles` |
| Concierge `Run()` with new tools | LLM can call new tools, no registration errors |

## Acceptance criteria

- [ ] `preload_subtitles` tool removed from concierge registration
- [ ] `list_subtitle_candidates` and `extract_subtitle` registered (always)
- [ ] `clai_tools.Cat` and `clai_tools.RowsBetween` registered (always)
- [ ] `fetch_subtitles` still conditionally registered based on API key
- [ ] All existing concierge tests still pass
- [ ] `go build ./...` succeeds
- [ ] If `subSelector` removed: `cmd/serve` and `cmd/concierge` setup code updated, no compile errors
- [ ] Concierge comment updating the tool count/description

## Error coverage

| Failure condition | Expected behavior |
|-------------------|-------------------|
| `list_subtitle_candidates` constructor fails | Warning logged, tool omitted (same pattern as other tools) |
| `extract_subtitle` constructor fails | Warning logged, tool omitted |
| `Cat`/`RowsBetween` (clai tools) | No constructor — always succeeds |

## Implementation notes

## Review findings (review 1, 2026-07-22)

### Verified good

- `preload_subtitles` registration removed — `rg preload_subtitles ./internal/` returns zero hits in the concierge or tools packages. The deleted files (`preload_subtitles.go`, `preload_subtitles_test.go`) are gone.
- `list_subtitle_candidates` and `extract_subtitle` registered unconditionally (always) — confirmed in `concierge.go:New()` — no API key gate.
- `clai_tools.Cat` and `clai_tools.RowsBetween` registered unconditionally via `append(llmTools, ...)` — confirmed.
- `fetch_subtitles` still conditionally registered: `fst := tools.NewFetchSubtitlesTool(...); if fst != nil { llmTools = append(llmTools, fst) }` — confirmed.
- `subSelector` field, `WithSubtitleSelector` option, and the nil-check removed from concierge — confirmed: `rg subSelector ./internal/agents/concierge/` returns zero hits. `rg WithSubtitleSelector` returns zero hits outside worklogs and butler (which retains its own usage).
- `cmd/serve/serve_setup.go` no longer passes `WithSubtitleSelector` — confirmed.
- `cmd/concierge/cmd.go` no longer passes `WithSubtitleSelector` — confirmed. Imports cleaned of `butler` and `models`.
- `internal/agents/tools/mocks_test.go` — `mockSubtitleSelector` removed, `context` import removed — confirmed.
- Tool list comment in `New()` documents all 18 tools including conditional ones — confirmed.
- All concierge tests pass with race detection: `go test -race -count=1 ./internal/agents/concierge/...` — pass.
- Build: `go build -o kinoview .` and `go build ./...` — clean.
- `"missing subtitle selector"` test case removed from concierge_test.go — confirmed.

No findings. Phase 3 implementation fully meets its contract.

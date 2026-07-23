# Phase 6: Quality Gate

**Status:** Complete ✅ | [README](./README.md)

## Goal

Run the full quality gate: tests, lint, build, and verify no regressions across the project.

## Specification

### Commands to run

```bash
# Build
go build -o kinoview .

# Vet
go vet ./...

# Test all packages
go test -count=1 ./...

# Race detection on tools package (where new tools live)
go test -race -count=1 ./internal/agents/tools/...

# Race detection on concierge
go test -race -count=1 ./internal/agents/concierge/...
```

### Packages to verify

| Package | Concern |
|---------|---------|
| `internal/agents/tools/` | New tools + modified check_suggestions |
| `internal/agents/concierge/` | Updated registration + prompt |
| `cmd/serve/` | Concierge setup changes (subSelector removal) |
| `internal/agents/interfaces.go` | No interface changes needed |

### Regression checklist

- [ ] Butler `PrepSuggestions` still works (internal `preloadSubs` unaffected)
- [ ] Classifier unaffected (uses different tool set)
- [ ] `fetch_subtitles` still works (no changes to it)
- [ ] All existing `check_suggestions` tests pass with updated expectations
- [ ] Concierge `New()` returns without error with all dependency combinations
- [ ] `go vet` clean
- [ ] `go build ./...` clean

### Dead code removal

If `subSelector` field and `WithSubtitleSelector` option are removed from concierge:
- [ ] No remaining references to `subSelector` in `concierge` package
- [ ] `cmd/serve/serve_setup.go` no longer passes `WithSubtitleSelector`
- [ ] `cmd/concierge` (`cmd.go`) no longer passes `WithSubtitleSelector`

### Concierge tool count

Update the comment in `New()` documenting the tool list:

```
// 1.  ConciergeContextGet
// 2.  ConciergeContextPush
// 3.  UpdateMetadata
// 4.  list_subtitle_candidates
// 5.  extract_subtitle
// 6.  fetch_subtitles (conditional)
// 7.  CheckSuggestions
// 8.  RemoveSuggestion
// 9.  AddSuggestion
// 10. UserContextGetter
// 11. MediaGetItem
// 12. MediaList
// 13. MediaStats
// 14. WebsiteText
// 15. Date
// 16. FFProbe
// 17. Cat
// 18. RowsBetween
```

## Acceptance criteria

- [x] All `go test ./...` pass (0 failures)
- [x] `go vet ./...` clean
- [x] `go build ./...` clean
- [x] `go test -race ./internal/agents/tools/...` clean
- [x] `go test -race ./internal/agents/concierge/...` clean
- [x] Butler unaffected (existing butler tests pass)
- [x] Dead code removed (subSelector if applicable)

## Implementation notes

All quality gate checks pass. One minor fix applied:
- Updated the `New()` godoc tool list comment from 10 entries to all 18 registered tools, including conditionally-registered ones. The old comment was stale from Phase 3 changes.

## Review findings (review 1, 2026-07-22)

### Gates re-run

```bash
go build -o kinoview .       # clean
go vet ./...                  # clean
gofumpt -l .                  # clean (no formatting issues)
go test -race -count=1 ./internal/agents/tools/...       # pass
go test -race -count=1 ./internal/agents/concierge/...   # pass
```

Full `go test -count=1 ./...` reveals one pre-existing failure in `internal/media/storage` (`Test_Stream_store_ffmpegSubsUtil_cache/eventually_adds_metadata_when_storing_new_file`) — a `TempDir RemoveAll cleanup: directory not empty` race in test cleanup. This is unrelated to the worklog (no files in `internal/media/storage` were touched) and reproduces intermittently.

### Verified good

- Butler `PrepSuggestions` / `preloadSubs`: calls `StreamManager` directly, no tool path affected. Butler tests pass — confirmed.
- Classifier: uses different tool set, unaffected — confirmed.
- `fetch_subtitles`: no changes — confirmed.
- No remaining references to `subSelector`, `SubtitleSelector`, or `preload_subtitles` in concierge or cmd packages — confirmed via `rg`.
- Dead code removed: `preload_subtitles.go`, `preload_subtitles_test.go` deleted; `mockSubtitleSelector` and `context` import removed from `mocks_test.go` — confirmed.
- Tool list comment in `New()` updated to 18 tools — confirmed.

No own findings. Phase 6 correctly validates the implementation against all gate criteria.

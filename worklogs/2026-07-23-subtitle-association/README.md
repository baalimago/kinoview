# Subtitle Association

**Created:** 2026-07-23
**Status:** Complete ✅

## Summary

Add explicit subtitle file association to media items via an `Item.SubtitlePaths` field.
Wire it into the stream manager's `findExternal` so the concierge sees manual associations,
and into `media list` for CLI viewing and management.

## Design

### The field

`model.Item` gains `SubtitlePaths []string` — user-specified absolute paths to subtitle files.
Validation on add: path must exist, must be a regular file, must have a subtitle extension
(.srt, .vtt, .sub, .ass, .ssa). Removing just drops the path from the slice.

### Integration points

1. **Stream manager `findExternal`**: Pattern 5 picks up `item.SubtitlePaths` as synthetic
   external subtitle streams. The concierge's `list_subtitle_candidates` sees them automatically.
2. **`media list` command**: New `s` post-select action with sub-actions:
   - `s` alone: list associated subtitle paths with existence status
   - `sa <path>`: associate a new subtitle file
   - `sr <index>`: remove association at index
   - `b`: back to table
3. **Table row**: New `Subs` column showing `✓` if any subtitle paths exist, `✗` otherwise.
4. **Macro mode**: `kinoview media list 0 s`, `kinoview media list 0 sa /path/to/sub.srt`,
   `kinoview media list 0 sr 0`.

## Decisions

- **D1 — Copy-on-associate rejected**: No copying/symlinking/moving. Store the path only.
  The user knows where their files are. Validation ensures the file exists at association time.
- **D2 — No ffprobe in media list**: The `media list` command does not pull in the stream
  manager. It only shows the `SubtitlePaths` field. The stream manager integration is
  one-directional: `findExternal` reads `SubtitlePaths` for concierge visibility, but
  `media list` doesn't call `Find`.

## Verification

```bash
go build -o kinoview .   # ✅
go test ./...             # ✅ all 20 packages pass
go vet ./...              # ✅
gofumpt -l .              # ✅ no diff
```

## Files changed

| File | Change |
|------|--------|
| `internal/model/item.go` | Added `SubtitlePaths []string` field |
| `internal/model/item_test.go` | JSON roundtrip + omitempty tests |
| `internal/media/stream/subtitles.go` | Pattern 5 in `findExternal` |
| `internal/media/stream/subtitles_test.go` | `TestFindExternal_SubtitlePaths` |
| `cmd/media/list.go` | Subs column, `s`/`sa`/`sr` actions, `addSubtitlePath`, help text |
| `cmd/media/list_test.go` | `TestAddSubtitlePath`, row formatter subs tests |

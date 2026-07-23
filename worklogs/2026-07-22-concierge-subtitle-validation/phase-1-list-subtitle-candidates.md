# Phase 1: Create `list_subtitle_candidates` Tool

**Status:** ✅ Complete (Review 1 fixes applied) | [README](./README.md)

## Goal

Create a new LLM tool `list_subtitle_candidates` that returns all subtitle streams (embedded + external) for a media item with metadata and extraction status.

## Specification

### Tool signature

```
Name: list_subtitle_candidates
Input: { "ID": string }
Output: JSON array of subtitle candidates with metadata
```

### Per-candidate schema

```json
{
  "index": 2,
  "codec": "subrip",
  "codecType": "subtitle",
  "language": "eng",
  "title": "English",
  "default": true,
  "forced": false,
  "source": "embedded",
  "alreadyExtracted": true,
  "extractedPath": "/path/to/subtitle.vtt"
}
```

### Behavior

1. Accept item ID as input
2. Call `itemGetter.GetItemByID(ID)` to resolve the item
3. Call `subMgr.Find(item)` to get all streams (embedded + external)
4. Filter to only `codecType == "subtitle"` streams
5. For each subtitle stream, construct the expected extraction path: `<storePath>/<itemID>_<streamIndex>.vtt`
6. Check if the file already exists on disk → set `alreadyExtracted` and `extractedPath`
7. Map stream fields into the candidate schema
8. Return JSON-serialized array

### `source` field enumeration

- `"embedded"` — internal stream discovered by ffprobe (positive index, no `ExternalPath`)
- `"external"` — sidecar `.srt`/`.vtt` file discovered by `findExternal` (negative index, has `ExternalPath`)

### Always available

This tool only uses local ffprobe data and filesystem checks. No API key dependency. Always registered.

### Dependencies

- `agents.ItemGetter` — to resolve item by ID
- `agents.StreamManager.Find()` — to discover streams
- Store path — to construct expected extraction file paths (passed via constructor, same pattern as `fetch_subtitles` takes `cacheDir`)

### File

New file: `internal/agents/tools/list_subtitle_candidates.go`

## Integration contract

| Scenario | Input | Expected output |
|----------|-------|-----------------|
| Item has embedded + external subs, some extracted | Valid ID | Returns all subtitle streams with correct `alreadyExtracted`/`extractedPath` |
| Item has no subtitle streams | Valid ID, no subs | Returns empty array, no error |
| Item has external subs already extracted | Valid ID | `source: "external"`, `alreadyExtracted: true`, `extractedPath` set |
| Item has embedded subs not yet extracted | Valid ID | `source: "embedded"`, `alreadyExtracted: false`, `extractedPath: ""` |
| Item ID not found | Invalid ID | Error returned |
| Item has no subtitle streams but has other streams | Valid ID, audio only | Returns empty array (filtered to subtitle codecType) |

## Acceptance criteria

- [ ] Tool returns valid JSON array of subtitle candidates
- [ ] Embedded and external streams are correctly distinguished via `source`
- [ ] `alreadyExtracted` is true when the `.vtt` file exists on disk, false otherwise
- [ ] `extractedPath` contains the full expected path when `alreadyExtracted` is true
- [ ] Non-subtitle streams are excluded from results
- [ ] Tool is registered in concierge without error
- [ ] Unit tests cover: no subs, mixed embedded/external, all extracted, none extracted, item not found

## Error coverage

| Failure condition | Expected behavior |
|-------------------|-------------------|
| Invalid/missing ID | Return error message |
| Item not found | Return error wrapping item lookup failure |
| `subMgr.Find` fails | Return error wrapping Find failure |
| `subMgr` is nil | Constructor returns error |

## Review findings (review 1, 2026-07-22)

### Verified good

- Constructor nil-checks each dependency (`itemGetter`, `subMgr`) and returns descriptive errors — confirmed via `TestNewListSubtitleCandidatesTool`.
- `Call` validates the `ID` input before touching any dependency — confirmed via `missing ID`/`empty ID` test cases.
- Non-subtitle streams correctly filtered (`CodecType != "subtitle"`) — verified by the `no subtitle streams` test case with `video`/`audio` streams.
- Missing language/title default to `"und"`/`"(none)"` — sensible nil-safe defaults.
- Error paths properly wrap underlying failures (`GetItemByID`, `Find`) — confirmed via `item not found` and `stream manager error` tests.
- `isExtracted` checks filesystem via `os.Stat` and correctly distinguishes different stream indices — confirmed via `TestListSubtitleCandidatesIsExtracted`.

### Findings

- [x] **R1-01** (Medium) — `Disposition` metadata (default, forced, comment) now surfaced as JSON booleans. Resolved 2026-07-22 (w0).
- [x] **R1-02** (Low) — `extractedPath` included in JSON output when `alreadyExtracted: true`. Uses `omitempty` when false. Resolved 2026-07-22 (w0).
- [x] **R1-03** (Low) — Output is now JSON array (`json.MarshalIndent`), matching the spec. Resolved 2026-07-22 (w0).
- [x] **R1-05** (Low) — Field names aligned to spec: `source` (was `Type`), `alreadyExtracted` (was `Extracted`). Resolved 2026-07-22 (w0).

## Implementation notes

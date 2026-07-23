# Phase 2: Create `extract_subtitle` Tool

**Status:** ✅ Complete | [README](./README.md)

## Goal

Create a focused `extract_subtitle` tool that performs pure ffmpeg/ffprobe extraction for a specific subtitle stream, returning the file path. No auto-selection, no bundling with Find.

## Specification

### Tool signature

```
Name: extract_subtitle
Input: { "ID": string, "subtitleID": string }
Output: success message with file path, or error
```

### Behavior

1. Accept item ID and subtitleID (stream index) as input
2. Call `itemGetter.GetItemByID(ID)` to resolve the item
3. Call `subMgr.ExtractSubtitles(item, subtitleID)` — idempotent, returns path if already exists
4. Return success message with the extracted file path

### Contrast with old `preload_subtitles`

| Aspect | `preload_subtitles` (old) | `extract_subtitle` (new) |
|--------|--------------------------|-------------------------|
| Stream discovery | Calls `subMgr.Find()` internally | Caller uses `list_subtitle_candidates` |
| Subtitle selection | Calls `subSel.Select()` internally | Caller chooses the `subtitleID` |
| Extraction | Yes | Yes (idempotent) |
| Input | `{ "ID": string }` | `{ "ID": string, "subtitleID": string }` |

### Always available

Uses only local ffmpeg/ffprobe. No API key dependency. Always registered.

### Dependencies

- `agents.ItemGetter` — to resolve item by ID
- `agents.StreamManager.ExtractSubtitles()` — to perform extraction

### File

New file: `internal/agents/tools/extract_subtitle.go`

## Integration contract

| Scenario | Input | Expected output |
|----------|-------|-----------------|
| Extract embedded subtitle stream | `{ ID: "abc", subtitleID: "2" }` | Success message with `.vtt` file path |
| Extract external subtitle (sidecar) | `{ ID: "abc", subtitleID: "-1" }` | Success message with `.vtt` file path |
| Already extracted (idempotent) | `{ ID: "abc", subtitleID: "2" }` | Success message with same path, no re-extraction |
| Item not found | `{ ID: "nonexistent", subtitleID: "0" }` | Error wrapping item lookup failure |
| Extraction fails (bad stream index) | `{ ID: "abc", subtitleID: "99" }` | Error wrapping ffmpeg failure |

## Acceptance criteria

- [x] Tool accepts ID and subtitleID, returns extracted file path on success
- [x] Idempotent: calling twice on same item+subtitleID returns same path without re-extraction
- [x] Works for both embedded (positive index) and external (negative index) streams
- [x] No internal call to subtitle selector (caller provides subtitleID)
- [x] Unit tests cover: successful extraction, idempotent extraction, item not found, extraction failure

## Error coverage

| Failure condition | Expected behavior |
|-------------------|-------------------|
| Missing ID or subtitleID | Return error message |
| Item not found | Return error wrapping item lookup failure |
| `ExtractSubtitles` fails | Return error wrapping the extraction failure |
| `subMgr` is nil | Constructor returns error |

## Review findings (review 1, 2026-07-22)

### Verified good

- Constructor nil-checks both dependencies and returns descriptive errors — confirmed via `TestNewExtractSubtitleTool`.
- `Call` validates both `ID` and `subtitleID` before any dependency calls — confirmed via `missing ID`, `empty ID`, `missing subtitleID`, `empty subtitleID` test cases.
- Idempotency holds: the tool delegates to `StreamManager.ExtractSubtitles` which owns idempotency, and the mock confirms identical output on repeated calls — confirmed via `idempotent extraction returns same path`.
- Both embedded (positive) and external (negative) stream indices work — confirmed via `successful extraction` and `external subtitle (negative index)` tests.
- Error paths properly wrap failures: `GetItemByID` failure, `ExtractSubtitles` failure — confirmed via `item not found` and `extraction failure` tests.
- No `SubtitleSelector` dependency — matches spec requirement. Constructor only takes `ItemGetter` + `StreamManager`.
- `Specification()` declares both `ID` and `subtitleID` as required inputs — confirmed via `TestExtractSubtitleSpecification`.

No findings. Phase 2 implementation fully meets its contract.

## Implementation notes

**2026-07-22 21:48 — Worker Session 1, Phase 2**

Tool implemented in `internal/agents/tools/extract_subtitle.go` with companion test file `extract_subtitle_test.go`.

Key design points:
- Pure passthrough to `StreamManager.ExtractSubtitles` — the tool owns no extraction logic
- `subtitleID` accepted as string to match `ExtractSubtitles(item, streamIndex string)` signature
- No `SubtitleSelector` dependency — the LLM (via `list_subtitle_candidates`) controls which stream to extract
- 9 test cases covering: nil deps, successful extraction, idempotency, external (negative) index, missing/empty ID, missing/empty subtitleID, item not found, extraction failure, specification validation

# 2026-06-24_concierge-fetch-subtitles

## Plan

Add `fetch_subtitles` tool (OpenSubtitles) to the Concierge agent, mirroring the Classifier's capability.

## Context

- **PreloadSubtitles** (existing): finds embedded subtitle streams in media files and extracts them
- **FetchSubtitles** (new for concierge): searches OpenSubtitles API and downloads external subtitles for movies without embedded ones

The Classifier already had `FetchSubtitles` wired. The Concierge only had `PreloadSubtitles`.

## Implementation

Added `tools.NewFetchSubtitlesTool(c.itemStore, c.subtitlesMgr, c.cacheDir)` to `concierge.New()` in `internal/agents/concierge/concierge.go`.

- Returns nil when `OPENSUBTITLES_API_KEY` is not set → tool silently omitted (same behavior as classifier)
- Placed between CheckSuggestions and RemoveSuggestion tool registrations

## Result

Concierge now has 9 tools (was 8):
1. UpdateMetadata
2. PreloadSubtitles
3. FetchSubtitles (NEW)
4. CheckSuggestions
5. RemoveSuggestion
6. AddSuggestion
7. MediaGetItem
8. MediaList
9. MediaStats

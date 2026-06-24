# Fetch Subtitles Tool - Implementation Complete

## Overview
Added an OpenSubtitles-based subtitle fetcher as an LLM tool for the classifier. When the classifier encounters a movie without subtitles, it can invoke this tool to download English subtitles.

## Design Decisions

### Q1: Credentials → Env vars (Option A)
- `OPENSUBTITLES_API_KEY` (required)
- `OPENSUBTITLES_USERNAME` (optional, for higher rate limits)
- `OPENSUBTITLES_PASSWORD` (optional)
- If API key missing, tool not registered; warning logged at startup

### Q2: Search strategy → Hybrid (Option D)
1. Try IMDB/TMDB ID from item metadata (json path: `.imdb_id`, `.id` with imdb prefix)
2. Fall back to filename as search query (with S01/season 1 variants stripped for cleaner queries)

### Q3: Download location → XDG_CACHE/kinoview/subtitles/<media-id>/
- Extended `StreamManager.findExternal()` to also scan this path
- New pattern 2: `<subtitleCachePath>/subtitles/<item.ID>/*.srt, *.vtt`

### Language preference
- Env var `KINOVIEW_SUBTITLE_LANGUAGES` (comma-separated, default "en")

## Files Created/Modified

### Created: `internal/agents/tools/opensubtitles_client.go`
- HTTP client wrapping api.opensubtitles.com/api/v1
- `NewOpenSubtitlesClient()` — reads credentials from env vars, returns nil if no API key
- `Search(imdbID, tmdbID, query, languages, mediaType)` — GET /subtitles
- `Download(fileID)` — POST /download
- `DownloadFile(link)` — GET the actual subtitle file content
- `BestFile(data, preferredLang)` — selects best match by download count
- `SubtitleLanguages()` — reads KINOVIEW_SUBTITLE_LANGUAGES env var

### Created: `internal/agents/tools/fetch_subtitles.go`
- `fetchSubtitlesTool` struct implementing `pub_models.LLMTool`
- `NewFetchSubtitlesTool(itemGetter, streamMgr, cacheDir)` — returns nil if no API key
- `Call(input)` — checks existing subs, searches OpenSubtitles, downloads, saves to cache dir
- `Specification()` — returns tool spec with Name="fetch_subtitles"
- `searchItem(item, langs)` — hybrid ID search then filename fallback
- `saveSubtitle(item, filename, content)` — writes to <cacheDir>/subtitles/<item.ID>/
- `extractIDs(metadata)` — parses IMDB/TMDB IDs from json.RawMessage
- `cleanQuery(name)` — strips common patterns (1080p, x264, S01E01, etc.) from filenames
- `hasSubs(info)` — checks if any subtitle streams exist
- `isMovie(item)` — checks MIMEType contains "video"

### Modified: `internal/media/stream/subtitles.go`
- Added `subtitleCachePath string` field to Manager
- Added `WithSubtitleCachePath(path string)` Option
- Extended `findExternal` with Pattern 2: `<subtitleCachePath>/subtitles/<item.ID>/*.srt, *.vtt`
- Fixed duplicate closing brace in stream builder loop

### Modified: `internal/media/storage/store.go`
- Added `SetClassifier(c agents.Classifier)` method for post-creation classifier injection

### Modified: `cmd/serve/serve_setup.go`
- Moved `subsManager` creation before classifier setup
- Moved `store` creation before classifier setup (to resolve circular dep)
- Added `tools.NewFetchSubtitlesTool(store, subsManager, *c.cacheDir)` to classifier Tools
- Updated butler setup to use pre-created subsManager

### Modified: `cmd/classify/classify.go`
- Added imports for `agents`, `tools`, `stream`
- Added `agents.ItemGetter` and `SetClassifier(agents.Classifier)` to `stationStorage` interface
- Added subsManager and fetch_subtitles tool wiring in Setup
- Restructured to create store before classifier (circular dep)

### Modified: `cmd/classify/classify_test.go`
- Added `agents` import
- Added `GetItemByID`, `GetItemByName`, `SetClassifier` methods to `mockStorage`

### Modified: `go.mod`
- Added `replace github.com/baalimago/clai => /home/imago/Projects/public/clai-fork/v1.9.2`
- The clai fork adds `Tools []LLMTool` to public `models.Configurations` and maps it in `pubConfigToInternal`

### Patched clai fork: `/home/imago/Projects/public/clai-fork/v1.9.2/`
- `pkg/text/models/configurations.go` — added `Tools []LLMTool` field
- `pkg/text/full.go` — added `Tools: c.Tools` mapping in `pubConfigToInternal`

## Usage

### Set up API key
```bash
export OPENSUBTITLES_API_KEY="your-api-key"
# Optional for higher rate limits (200/day vs 20/day):
export OPENSUBTITLES_USERNAME="your-username"
export OPENSUBTITLES_PASSWORD="your-password"
```

### Configure language preference
```bash
export KINOVIEW_SUBTITLE_LANGUAGES="en,sv"  # default: "en"
```

When the classifier analyzes a movie without subtitles, it can now call `fetch_subtitles` with the media item ID. The tool will search OpenSubtitles, download the best match, and save it to `$XDG_CACHE_HOME/kinoview/subtitles/<item-id>/`. The `StreamManager.Find()` method automatically discovers these files on subsequent scans.

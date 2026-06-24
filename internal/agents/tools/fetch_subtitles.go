package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	kinomodel "github.com/baalimago/kinoview/internal/model"
)

type fetchSubtitlesTool struct {
	itemGetter agents.ItemGetter
	subMgr     agents.StreamManager
	osClient   *OpenSubtitlesClient
	cacheDir   string
	debug      bool
}

// NewFetchSubtitlesTool creates the OpenSubtitles-based subtitle fetch tool.
// Returns nil if the OpenSubtitles API key is not configured, signalling
// that the tool should not be registered.
func NewFetchSubtitlesTool(
	ig agents.ItemGetter,
	sm agents.StreamManager,
	cacheDir string,
) *fetchSubtitlesTool {
	client := NewOpenSubtitlesClient()
	if client == nil {
		return nil
	}
	return &fetchSubtitlesTool{
		itemGetter: ig,
		subMgr:     sm,
		osClient:   client,
		cacheDir:   cacheDir,
		debug:      os.Getenv("DEBUG") != "" || os.Getenv("DEBUG_SUBS") != "",
	}
}

func (t *fetchSubtitlesTool) Call(input models.Input) (string, error) {
	id, ok := input["ID"].(string)
	if !ok || id == "" {
		id, _ = input["id"].(string)
	}
	if id == "" {
		return "", errors.New("ID must be a non-empty string")
	}

	if t.itemGetter == nil {
		return "", errors.New("item getter not configured for fetch_subtitles tool")
	}

	item, err := t.itemGetter.GetItemByID(id)
	if err != nil {
		return "", fmt.Errorf("failed to get item: %w", err)
	}

	// Check existing subtitles
	info, err := t.subMgr.Find(item)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing subtitles: %w", err)
	}
	if hasSubs(info) {
		return fmt.Sprintf("item '%s' already has subtitles, nothing to fetch", item.Name), nil
	}

	// Only movies for now
	if !isMovie(item) {
		return fmt.Sprintf("item '%s' is not a movie, skipping subtitle fetch", item.Name), nil
	}

	// Search and download
	langs := SubtitleLanguages()
	result, err := t.searchItem(item, langs)
	if err != nil {
		return "", fmt.Errorf("subtitle search failed: %w", err)
	}

	if len(result.Data) == 0 {
		return fmt.Sprintf("no subtitles found for '%s'", item.Name), nil
	}

	file := BestFile(result.Data, firstLang(langs))
	if file == nil {
		return fmt.Sprintf("no matching subtitle file found for '%s' (wanted lang: %s)", item.Name, firstLang(langs)), nil
	}

	dl, err := t.osClient.Download(file.FileID)
	if err != nil {
		return "", fmt.Errorf("failed to get download link: %w", err)
	}

	content, err := t.osClient.DownloadFile(dl.Link)
	if err != nil {
		return "", fmt.Errorf("failed to download subtitle file: %w", err)
	}

	if err := t.saveSubtitle(item, dl.FileName, content); err != nil {
		return "", fmt.Errorf("failed to save subtitle: %w", err)
	}

	return fmt.Sprintf("downloaded subtitle for '%s': %s", item.Name, dl.FileName), nil
}

func (t *fetchSubtitlesTool) Specification() models.Specification {
	return models.Specification{
		Name:        "fetch_subtitles",
		Description: "Search OpenSubtitles for subtitles for a media item and download the best match. Only works for movies without existing subtitles. Automatically saves to the configured subtitle cache.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the media item to fetch subtitles for",
				},
				"id": {
					Type:        "string",
					Description: "Alias for ID",
				},
			},
			Required: []string{},
		},
	}
}

func (t *fetchSubtitlesTool) searchItem(item kinomodel.Item, langs string) (*OpenSubtitlesSearchResponse, error) {
	imdbID, tmdbID := extractIDs(item.Metadata)

	// Try IDs first
	if imdbID != "" || tmdbID != "" {
		resp, err := t.osClient.Search(imdbID, tmdbID, "", langs, "movie")
		if err == nil && len(resp.Data) > 0 {
			return resp, nil
		}
	}

	// Fall back to filename-based query
	query := cleanQuery(item.Name)
	if query == "" {
		return nil, fmt.Errorf("no searchable identifiers for item '%s'", item.Name)
	}
	return t.osClient.Search("", "", query, langs, "movie")
}

func (t *fetchSubtitlesTool) saveSubtitle(item kinomodel.Item, filename string, content []byte) error {
	dir := filepath.Join(t.cacheDir, "subtitles", item.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create subtitle dir: %w", err)
	}

	// Determine extension: prefer .srt for easier discovery by findExternal
	ext := filepath.Ext(filename)
	if ext == "" || (ext != ".srt" && ext != ".vtt") {
		ext = ".srt"
	}

	outPath := filepath.Join(dir, filename)
	// Ensure extension is .srt or .vtt
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	outPath = filepath.Join(dir, base+ext)

	if err := os.WriteFile(outPath, content, 0o644); err != nil {
		return fmt.Errorf("failed to write subtitle file: %w", err)
	}
	if t.debug {
		fmt.Fprintf(os.Stderr, "DEBUG: saved subtitle to %s\n", outPath)
	}
	return nil
}

// extractIDs attempts to find IMDB/TMDB IDs from item metadata.
func extractIDs(metadata *json.RawMessage) (imdbID, tmdbID string) {
	if metadata == nil {
		return "", ""
	}

	var m map[string]any
	if err := json.Unmarshal(*metadata, &m); err != nil {
		return "", ""
	}

	if id, ok := stringField(m, "imdb_id"); ok {
		imdbID = id
	}
	if id, ok := stringField(m, "tmdb_id"); ok {
		tmdbID = id
	}

	// Some classifiers put IMDb ID under "id" with "tt" prefix
	if imdbID == "" {
		if id, ok := stringField(m, "id"); ok && strings.HasPrefix(id, "tt") {
			imdbID = id
		}
	}

	return imdbID, tmdbID
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// hasSubs checks if any subtitle-type streams exist in the media info.
func hasSubs(info kinomodel.MediaInfo) bool {
	for _, s := range info.Streams {
		if s.CodecType == "subtitle" {
			return true
		}
	}
	return false
}

// isMovie checks if item is a movie (video MIME type).
func isMovie(item kinomodel.Item) bool {
	return strings.Contains(item.MIMEType, "video")
}

// cleanQuery strips season/episode-like patterns from the filename
// to yield a cleaner search query for OpenSubtitles.
func cleanQuery(name string) string {
	// e.g., "The.Matrix.1999.1080p.BluRay.x264.mp4" → "The Matrix 1999"
	base := strings.TrimSuffix(name, filepath.Ext(name))

	// Remove common patterns: S01E01, season 1, 1080p, BluRay, x264, etc.
	patternsToStrip := []string{
		`1080p`, `720p`, `480p`, `2160p`, `4K`, `4k`,
		`BluRay`, `Blu-ray`, `BRRip`, `WEBRip`, `WEB-DL`, `HDRip`,
		`x264`, `x265`, `h264`, `h265`, `HEVC`, `AVC`,
		`AAC`, `AC3`, `DTS`, `DDP`, `EAC3`,
		`AMZN`, `NF`, `DSNP`, `HMAX`, `HBO`,
	}

	// Remove season/episode patterns
	seasonPatterns := []string{
		`S0`, `S1`, `S2`, `S3`, `S4`, `S5`, `S6`, `S7`, `S8`, `S9`,
		`E0`, `E1`, `E2`, `E3`, `E4`, `E5`, `E6`, `E7`, `E8`, `E9`,
		`EP`, `Ep`, `ep`,
	}

	parts := strings.Split(base, ".")
	if len(parts) == 1 {
		parts = strings.Split(base, " ")
	}

	var cleaned []string
	for _, p := range parts {
		upper := strings.ToUpper(p)
		skip := false
		for _, pat := range patternsToStrip {
			if upper == strings.ToUpper(pat) {
				skip = true
				break
			}
		}
		if !skip {
			for _, sp := range seasonPatterns {
				if strings.HasPrefix(upper, strings.ToUpper(sp)) {
					skip = true
					break
				}
			}
		}
		// Also skip year-only parts if they're surrounded (keeps year if it's the only numberish part)
		if !skip {
			cleaned = append(cleaned, p)
		}
	}

	if len(cleaned) == 0 {
		return base
	}

	result := strings.Join(cleaned, " ")
	result = strings.TrimSpace(result)
	return result
}

func firstLang(langs string) string {
	langs = strings.TrimSpace(langs)
	if langs == "" {
		return "en"
	}
	parts := strings.Split(langs, ",")
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return "en"
}

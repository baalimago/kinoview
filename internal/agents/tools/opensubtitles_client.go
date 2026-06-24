package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// OpenSubtitlesClient wraps the OpenSubtitles.com REST API.
// Authentication is via Api-Key header (required) with optional
// username/password JWT login for higher rate limits.
type OpenSubtitlesClient struct {
	apiKey   string
	username string
	password string
	baseURL  string
	client   *http.Client
}

// OpenSubtitlesFile represents a subtitle file entry from search results.
type OpenSubtitlesFile struct {
	FileID   int    `json:"file_id"`
	FileName string `json:"file_name"`
}

// OpenSubtitlesAttributes holds metadata about a subtitle entry.
type OpenSubtitlesAttributes struct {
	Language      string               `json:"language"`
	DownloadCount int                  `json:"download_count"`
	Release       string               `json:"release"`
	Files         []OpenSubtitlesFile  `json:"files"`
}

// OpenSubtitlesData is a single search result.
type OpenSubtitlesData struct {
	ID         string                   `json:"id"`
	Attributes OpenSubtitlesAttributes  `json:"attributes"`
}

// OpenSubtitlesSearchResponse is the paginated search response.
type OpenSubtitlesSearchResponse struct {
	Data       []OpenSubtitlesData `json:"data"`
	TotalPages int                 `json:"total_pages"`
	TotalCount int                 `json:"total_count"`
	Page       int                 `json:"page"`
}

// OpenSubtitlesDownloadResponse is the response from POST /download.
type OpenSubtitlesDownloadResponse struct {
	Link        string `json:"link"`
	FileName    string `json:"file_name"`
	FileSize    int    `json:"file_size"`
	Remaining   int    `json:"remaining"`
}

// NewOpenSubtitlesClient creates a client from environment variables.
// Returns nil if OPENSUBTITLES_API_KEY is not set.
func NewOpenSubtitlesClient() *OpenSubtitlesClient {
	apiKey := os.Getenv("OPENSUBTITLES_API_KEY")
	if apiKey == "" {
		return nil
	}
	return &OpenSubtitlesClient{
		apiKey:   apiKey,
		username: os.Getenv("OPENSUBTITLES_USERNAME"),
		password: os.Getenv("OPENSUBTITLES_PASSWORD"),
		baseURL:  "https://api.opensubtitles.com/api/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Search subtitles by IMDB ID, TMDB ID, or query string.
// languages is a comma-separated list of ISO 639-1 codes (e.g., "en").
// mediaType should be "movie" or "episode".
func (c *OpenSubtitlesClient) Search(imdbID, tmdbID, query string, languages string, mediaType string) (*OpenSubtitlesSearchResponse, error) {
	params := url.Values{}
	if imdbID != "" {
		params.Set("imdb_id", imdbID)
	}
	if tmdbID != "" {
		params.Set("tmdb_id", tmdbID)
	}
	if query != "" {
		params.Set("query", query)
	}
	if languages != "" {
		params.Set("languages", languages)
	}
	if mediaType != "" {
		params.Set("type", mediaType)
	}
	params.Set("page", "1")

	reqURL := c.baseURL + "/subtitles?" + params.Encode()
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search returned %d: %s", resp.StatusCode, string(body))
	}

	var result OpenSubtitlesSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}
	return &result, nil
}

// Download fetches the download link for a given file ID.
func (c *OpenSubtitlesClient) Download(fileID int) (*OpenSubtitlesDownloadResponse, error) {
	body := map[string]string{
		"file_id": strconv.Itoa(fileID),
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal download body: %w", err)
	}

	reqURL := c.baseURL + "/download"
	req, err := http.NewRequest("POST", reqURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result OpenSubtitlesDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode download response: %w", err)
	}
	return &result, nil
}

// DownloadFile fetches the actual subtitle file content from a download link.
func (c *OpenSubtitlesClient) DownloadFile(link string) ([]byte, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create file download request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("file download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file download returned %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// BestFile selects the most appropriate subtitle file from search results.
// Currently picks the first result with most downloads, preferring exact
// language match.
func BestFile(data []OpenSubtitlesData, preferredLang string) *OpenSubtitlesFile {
	var best *OpenSubtitlesFile
	bestDownloads := -1

	for i := range data {
		d := &data[i]
		lang := strings.ToLower(d.Attributes.Language)
		if !strings.HasPrefix(lang, strings.ToLower(preferredLang)) {
			continue
		}
		for j := range d.Attributes.Files {
			f := &d.Attributes.Files[j]
			if d.Attributes.DownloadCount > bestDownloads {
				bestDownloads = d.Attributes.DownloadCount
				best = f
			}
		}
	}
	return best
}

// SubtitleLanguages returns the configured languages from env.
// Defaults to "en".
func SubtitleLanguages() string {
	langs := os.Getenv("KINOVIEW_SUBTITLE_LANGUAGES")
	if langs == "" {
		return "en"
	}
	return strings.TrimSpace(langs)
}

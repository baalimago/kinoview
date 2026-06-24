package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	kinomodel "github.com/baalimago/kinoview/internal/model"
)

var errSentinel = errors.New("sentinel error")

func TestNewFetchSubtitlesTool(t *testing.T) {
	origKey := os.Getenv("OPENSUBTITLES_API_KEY")
	defer os.Setenv("OPENSUBTITLES_API_KEY", origKey)

	t.Run("returns nil when API key not set", func(t *testing.T) {
		os.Unsetenv("OPENSUBTITLES_API_KEY")
		tool := NewFetchSubtitlesTool(nil, nil, "/tmp")
		if tool != nil {
			t.Fatal("expected nil when API key is not set")
		}
	})

	t.Run("returns tool when API key is set", func(t *testing.T) {
		os.Setenv("OPENSUBTITLES_API_KEY", "key123")
		ig := &mockItemGetter{}
		sm := &mockSubtitleManager{}
		tool := NewFetchSubtitlesTool(ig, sm, "/tmp/cache")
		if tool == nil {
			t.Fatal("expected non-nil when API key is set")
		}
		if tool.cacheDir != "/tmp/cache" {
			t.Fatalf("cacheDir: got %q, want '/tmp/cache'", tool.cacheDir)
		}
	})
}

func TestFetchSubtitlesCall_NilItemGetter(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		itemGetter: nil,
		subMgr:     &mockSubtitleManager{},
	}

	_, err := tool.Call(models.Input{"ID": "test-id"})
	if err == nil {
		t.Fatal("expected error for nil item getter")
	}
}

func TestFetchSubtitlesCall_MissingID(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		itemGetter: &mockItemGetter{},
		subMgr:     &mockSubtitleManager{},
	}

	_, err := tool.Call(models.Input{})
	if err == nil {
		t.Fatal("expected error for missing ID")
	}

	_, err = tool.Call(models.Input{"ID": ""})
	if err == nil {
		t.Fatal("expected error for empty ID")
	}

	// Should work with "id" alias
	// (but will fail at getItem since mock returns not found)
	_, err = tool.Call(models.Input{"id": "some-id"})
	if err == nil {
		t.Fatal("expected error when item not found")
	}
}

func TestFetchSubtitlesCall_ItemNotFound(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		itemGetter: &mockItemGetter{}, // will return not-found error
		subMgr: &mockSubtitleManager{},
	}

	// mockItemGetter returns error for unknown IDs by default
	_, err := tool.Call(models.Input{"ID": "nonexistent"})
	if err == nil {
		t.Fatal("expected error when item not found")
	}
}

func TestFetchSubtitlesCall_AlreadyHasSubs(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		itemGetter: &mockItemGetter{
			item: kinomodel.Item{ID: "1", Name: "movie.mkv", MIMEType: "video/x-matroska"},
		},
		subMgr: &mockSubtitleManager{
			mediaInfo: kinomodel.MediaInfo{
				Streams: []kinomodel.Stream{
					{CodecType: "subtitle", Index: 0},
				},
			},
		},
	}

	result, err := tool.Call(models.Input{"ID": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "item 'movie.mkv' already has subtitles, nothing to fetch" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestFetchSubtitlesCall_NotAMovie(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		itemGetter: &mockItemGetter{
			item: kinomodel.Item{ID: "1", Name: "image.jpg", MIMEType: "image/jpeg"},
		},
		subMgr: &mockSubtitleManager{
			mediaInfo: kinomodel.MediaInfo{}, // no subs
		},
	}

	result, err := tool.Call(models.Input{"ID": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "item 'image.jpg' is not a movie, skipping subtitle fetch" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestFetchSubtitlesCall_FindError(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		itemGetter: &mockItemGetter{
			item: kinomodel.Item{ID: "1", Name: "movie.mkv", MIMEType: "video/x-matroska"},
		},
		subMgr: &mockSubtitleManager{
			err: errSentinel,
		},
	}

	// mockSubtitleManager with err set will return that error from Find
	_, err := tool.Call(models.Input{"ID": "1"})
	if err == nil {
		t.Fatal("expected error when Find fails")
	}
}

func TestExtractIDs(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		imdb, tmdb := extractIDs(nil)
		if imdb != "" || tmdb != "" {
			t.Fatal("expected empty IDs for nil metadata")
		}
	})

	t.Run("imdb_id field", func(t *testing.T) {
		meta := json.RawMessage(`{"imdb_id":"tt1234567","tmdb_id":"7654321"}`)
		imdb, tmdb := extractIDs(&meta)
		if imdb != "tt1234567" {
			t.Fatalf("imdb: got %q, want 'tt1234567'", imdb)
		}
		if tmdb != "7654321" {
			t.Fatalf("tmdb: got %q, want '7654321'", tmdb)
		}
	})

	t.Run("id field with tt prefix", func(t *testing.T) {
		meta := json.RawMessage(`{"id":"tt9876543"}`)
		imdb, tmdb := extractIDs(&meta)
		if imdb != "tt9876543" {
			t.Fatalf("imdb: got %q, want 'tt9876543'", imdb)
		}
		if tmdb != "" {
			t.Fatalf("tmdb: got %q, want empty", tmdb)
		}
	})

	t.Run("no IDs present", func(t *testing.T) {
		meta := json.RawMessage(`{"name":"Some Movie","year":2020}`)
		imdb, tmdb := extractIDs(&meta)
		if imdb != "" || tmdb != "" {
			t.Fatal("expected empty IDs")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		meta := json.RawMessage(`{invalid}`)
		imdb, tmdb := extractIDs(&meta)
		if imdb != "" || tmdb != "" {
			t.Fatal("expected empty IDs for invalid JSON")
		}
	})
}

func TestHasSubs(t *testing.T) {
	t.Run("has subtitle streams", func(t *testing.T) {
		info := kinomodel.MediaInfo{
			Streams: []kinomodel.Stream{
				{CodecType: "video", Index: 0},
				{CodecType: "audio", Index: 1},
				{CodecType: "subtitle", Index: 2},
			},
		}
		if !hasSubs(info) {
			t.Fatal("expected true when subtitle stream exists")
		}
	})

	t.Run("no subtitle streams", func(t *testing.T) {
		info := kinomodel.MediaInfo{
			Streams: []kinomodel.Stream{
				{CodecType: "video", Index: 0},
				{CodecType: "audio", Index: 1},
			},
		}
		if hasSubs(info) {
			t.Fatal("expected false when no subtitle stream")
		}
	})

	t.Run("empty streams", func(t *testing.T) {
		if hasSubs(kinomodel.MediaInfo{}) {
			t.Fatal("expected false for empty streams")
		}
	})
}

func TestIsMovie(t *testing.T) {
	tests := []struct {
		mime     string
		expected bool
	}{
		{"video/mp4", true},
		{"video/x-matroska", true},
		{"video/webm", true},
		{"image/jpeg", false},
		{"audio/mp3", false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, tt := range tests {
		item := kinomodel.Item{MIMEType: tt.mime}
		if got := isMovie(item); got != tt.expected {
			t.Errorf("isMovie(%q) = %v, want %v", tt.mime, got, tt.expected)
		}
	}
}

func TestCleanQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"The.Matrix.1999.1080p.BluRay.x264.mp4", "The Matrix 1999"},
		{"Inception.2010.720p.BRRip.x265.mkv", "Inception 2010"},
		{"Some.Movie.4K.WEB-DL.AAC.mkv", "Some Movie"},
		{"Show.S01E01.1080p.x264.mkv", "Show"},
		{"Movie.Name.2023.HEVC.DTS.mp4", "Movie Name 2023"},
		{"Avatar 2009 2160p HDRip AC3.mkv", "Avatar 2009"},
		{"NoDotsAtAll", "NoDotsAtAll"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cleanQuery(tt.input); got != tt.expected {
			t.Errorf("cleanQuery(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFirstLang(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"en", "en"},
		{"en,sv", "en"},
		{"sv,en", "sv"},
		{"  de , fr  ", "de"},
		{"", "en"},
	}
	for _, tt := range tests {
		if got := firstLang(tt.input); got != tt.expected {
			t.Errorf("firstLang(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSaveSubtitle(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	os.Setenv("OPENSUBTITLES_API_KEY", "key")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")

	tool := &fetchSubtitlesTool{
		cacheDir: cacheDir,
	}

	item := kinomodel.Item{ID: "abc123", Name: "movie.mkv"}

	t.Run("saves .srt file", func(t *testing.T) {
		err := tool.saveSubtitle(item, "movie.srt", []byte("test subtitle"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedPath := filepath.Join(cacheDir, "subtitles", "abc123", "movie.srt")
		data, err := os.ReadFile(expectedPath)
		if err != nil {
			t.Fatalf("failed to read saved file: %v", err)
		}
		if string(data) != "test subtitle" {
			t.Fatalf("content mismatch: got %q", string(data))
		}
	})

	t.Run("saves with .srt even when no extension", func(t *testing.T) {
		err := tool.saveSubtitle(item, "subfile", []byte("test"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedPath := filepath.Join(cacheDir, "subtitles", "abc123", "subfile.srt")
		if _, err := os.Stat(expectedPath); err != nil {
			t.Fatalf("expected file at %q: %v", expectedPath, err)
		}
	})

	t.Run("preserves .vtt extension", func(t *testing.T) {
		err := tool.saveSubtitle(item, "sub.vtt", []byte("WEBVTT\n\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedPath := filepath.Join(cacheDir, "subtitles", "abc123", "sub.vtt")
		if _, err := os.Stat(expectedPath); err != nil {
			t.Fatalf("expected file at %q: %v", expectedPath, err)
		}
	})
}

func TestSpecification(t *testing.T) {
	tool := &fetchSubtitlesTool{}
	spec := tool.Specification()
	if spec.Name != "fetch_subtitles" {
		t.Fatalf("name: got %q, want 'fetch_subtitles'", spec.Name)
	}
	if spec.Inputs == nil {
		t.Fatal("expected non-nil Inputs")
	}
	if spec.Inputs.Type != "object" {
		t.Fatalf("inputs type: got %q, want 'object'", spec.Inputs.Type)
	}
}

// Test full end-to-end flow using mocked HTTP server
func TestFetchSubtitlesCall_Success(t *testing.T) {
	os.Setenv("OPENSUBTITLES_API_KEY", "test-key")
	os.Setenv("KINOVIEW_SUBTITLE_LANGUAGES", "en")
	defer os.Unsetenv("OPENSUBTITLES_API_KEY")
	defer os.Unsetenv("KINOVIEW_SUBTITLE_LANGUAGES")

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	// We need a URL variable that the handler can reference before server is created
	var serverURL string

	// Mock OpenSubtitles API: /subtitles and /download endpoints
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subtitles":
			resp := OpenSubtitlesSearchResponse{
				Data: []OpenSubtitlesData{
					{
						ID: "sub-1",
						Attributes: OpenSubtitlesAttributes{
							Language:      "en",
							DownloadCount: 500,
							Files: []OpenSubtitlesFile{
								{FileID: 42, FileName: "movie.en.srt"},
							},
						},
					},
				},
				TotalPages: 1,
				TotalCount: 1,
			}
			json.NewEncoder(w).Encode(resp)
		case "/download":
			json.NewEncoder(w).Encode(OpenSubtitlesDownloadResponse{
				Link:     serverURL + "/file/sub.srt",
				FileName: "movie.en.srt",
				Remaining: 19,
			})
		case "/file/sub.srt":
			w.Write([]byte("1\n00:01:00,000 --> 00:02:00,000\nHello world\n"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := &OpenSubtitlesClient{
		apiKey:  "test-key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	tool := &fetchSubtitlesTool{
		itemGetter: &mockItemGetter{
			item: kinomodel.Item{
				ID:       "movie-1",
				Name:     "The.Matrix.1999.1080p.mkv",
				MIMEType: "video/x-matroska",
				Metadata: nil, // No metadata → forces filename search
			},
		},
		subMgr: &mockSubtitleManager{
			mediaInfo: kinomodel.MediaInfo{}, // No existing subs
		},
		osClient: client,
		cacheDir: cacheDir,
	}

	result, err := tool.Call(models.Input{"ID": "movie-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "downloaded subtitle for 'The.Matrix.1999.1080p.mkv': movie.en.srt" {
		t.Fatalf("unexpected result: %q", result)
	}

	// Verify the subtitle file was saved
	expectedPath := filepath.Join(cacheDir, "subtitles", "movie-1", "movie.en.srt")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read saved subtitle: %v", err)
	}
	expectedContent := "1\n00:01:00,000 --> 00:02:00,000\nHello world\n"
	if string(data) != expectedContent {
		t.Fatalf("content mismatch: got %q, want %q", string(data), expectedContent)
	}
}

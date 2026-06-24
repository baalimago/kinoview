package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewOpenSubtitlesClient(t *testing.T) {
	// Save and restore env
	origKey := os.Getenv("OPENSUBTITLES_API_KEY")
	defer os.Setenv("OPENSUBTITLES_API_KEY", origKey)

	t.Run("returns nil when API key not set", func(t *testing.T) {
		os.Unsetenv("OPENSUBTITLES_API_KEY")
		client := NewOpenSubtitlesClient()
		if client != nil {
			t.Fatal("expected nil when API key is not set")
		}
	})

	t.Run("returns client when API key is set", func(t *testing.T) {
		os.Setenv("OPENSUBTITLES_API_KEY", "test-key-123")
		client := NewOpenSubtitlesClient()
		if client == nil {
			t.Fatal("expected non-nil client when API key is set")
		}
		if client.apiKey != "test-key-123" {
			t.Fatalf("apiKey: got %q, want %q", client.apiKey, "test-key-123")
		}
		if client.baseURL != "https://api.opensubtitles.com/api/v1" {
			t.Fatalf("baseURL: got %q, want %q", client.baseURL, "https://api.opensubtitles.com/api/v1")
		}
	})

	t.Run("reads optional username/password", func(t *testing.T) {
		os.Setenv("OPENSUBTITLES_API_KEY", "test-key")
		os.Setenv("OPENSUBTITLES_USERNAME", "user1")
		os.Setenv("OPENSUBTITLES_PASSWORD", "pass1")
		client := NewOpenSubtitlesClient()
		if client.username != "user1" {
			t.Fatalf("username: got %q, want %q", client.username, "user1")
		}
		if client.password != "pass1" {
			t.Fatalf("password: got %q, want %q", client.password, "pass1")
		}
	})
}

func TestSearch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Api-Key") != "test-key" {
			t.Errorf("expected Api-Key header, got %q", r.Header.Get("Api-Key"))
		}

		q := r.URL.Query()
		_ = q.Get("imdb_id") // just verify it reads params

		resp := OpenSubtitlesSearchResponse{
			Data: []OpenSubtitlesData{
				{
					ID: "123",
					Attributes: OpenSubtitlesAttributes{
						Language:      "en",
						DownloadCount: 500,
						Files: []OpenSubtitlesFile{
							{FileID: 10, FileName: "movie.srt"},
						},
					},
				},
			},
			TotalPages: 1,
			TotalCount: 1,
			Page:       1,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "test-key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	result, err := client.Search("tt1234567", "", "", "en", "movie")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Data))
	}
	if result.Data[0].ID != "123" {
		t.Fatalf("expected ID '123', got %q", result.Data[0].ID)
	}
}

func TestSearch_WithAllParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("imdb_id") != "tt999" {
			t.Errorf("imdb_id: got %q, want %q", q.Get("imdb_id"), "tt999")
		}
		if q.Get("tmdb_id") != "888" {
			t.Errorf("tmdb_id: got %q, want %q", q.Get("tmdb_id"), "888")
		}
		if q.Get("query") != "Test Movie" {
			t.Errorf("query: got %q, want %q", q.Get("query"), "Test Movie")
		}
		if q.Get("languages") != "en,sv" {
			t.Errorf("languages: got %q, want %q", q.Get("languages"), "en,sv")
		}
		if q.Get("type") != "movie" {
			t.Errorf("type: got %q, want %q", q.Get("type"), "movie")
		}
		json.NewEncoder(w).Encode(OpenSubtitlesSearchResponse{})
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.Search("tt999", "888", "Test Movie", "en,sv", "movie")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.Search("", "", "test", "en", "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSearch_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.Search("", "", "test", "en", "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDownload_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		resp := OpenSubtitlesDownloadResponse{
			Link:      "https://example.com/sub.srt",
			FileName:  "movie.srt",
			FileSize:  4096,
			Remaining: 19,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	result, err := client.Download(42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Link != "https://example.com/sub.srt" {
		t.Fatalf("link: got %q", result.Link)
	}
	if result.FileName != "movie.srt" {
		t.Fatalf("filename: got %q", result.FileName)
	}
}

func TestDownload_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.Download(1)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestDownloadFile_Success(t *testing.T) {
	content := []byte("1\n00:01:00,000 --> 00:02:00,000\nHello world\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	result, err := client.DownloadFile(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", string(result), string(content))
	}
}

func TestDownloadFile_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := &OpenSubtitlesClient{
		apiKey:  "key",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := client.DownloadFile(server.URL + "/nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestBestFile(t *testing.T) {
	data := []OpenSubtitlesData{
		{
			ID: "1",
			Attributes: OpenSubtitlesAttributes{
				Language:      "en",
				DownloadCount: 100,
				Files: []OpenSubtitlesFile{
					{FileID: 1, FileName: "sub_en.srt"},
				},
			},
		},
		{
			ID: "2",
			Attributes: OpenSubtitlesAttributes{
				Language:      "en",
				DownloadCount: 500,
				Files: []OpenSubtitlesFile{
					{FileID: 2, FileName: "sub_en_better.srt"},
				},
			},
		},
		{
			ID: "3",
			Attributes: OpenSubtitlesAttributes{
				Language:      "sv",
				DownloadCount: 999,
				Files: []OpenSubtitlesFile{
					{FileID: 3, FileName: "sub_sv.srt"},
				},
			},
		},
	}

	t.Run("picks highest downloads for matching language", func(t *testing.T) {
		best := BestFile(data, "en")
		if best == nil {
			t.Fatal("expected non-nil result")
		}
		if best.FileID != 2 {
			t.Fatalf("expected file_id 2 (highest en), got %d", best.FileID)
		}
	})

	t.Run("returns nil when no language match", func(t *testing.T) {
		best := BestFile(data, "fr")
		if best != nil {
			t.Fatal("expected nil when no matching language")
		}
	})

	t.Run("returns nil for empty data", func(t *testing.T) {
		best := BestFile(nil, "en")
		if best != nil {
			t.Fatal("expected nil for empty data")
		}
	})

	t.Run("case insensitive language matching", func(t *testing.T) {
		best := BestFile(data, "EN")
		if best == nil {
			t.Fatal("expected non-nil for uppercase language")
		}
		if best.FileID != 2 {
			t.Fatalf("expected file_id 2, got %d", best.FileID)
		}
	})
}

func TestSubtitleLanguages(t *testing.T) {
	orig := os.Getenv("KINOVIEW_SUBTITLE_LANGUAGES")
	defer os.Setenv("KINOVIEW_SUBTITLE_LANGUAGES", orig)

	t.Run("defaults to en", func(t *testing.T) {
		os.Unsetenv("KINOVIEW_SUBTITLE_LANGUAGES")
		if got := SubtitleLanguages(); got != "en" {
			t.Fatalf("expected 'en', got %q", got)
		}
	})

	t.Run("reads from env", func(t *testing.T) {
		os.Setenv("KINOVIEW_SUBTITLE_LANGUAGES", "en,sv")
		if got := SubtitleLanguages(); got != "en,sv" {
			t.Fatalf("expected 'en,sv', got %q", got)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		os.Setenv("KINOVIEW_SUBTITLE_LANGUAGES", "  en , de  ")
		if got := SubtitleLanguages(); got != "en , de" {
			t.Fatalf("expected 'en , de', got %q", got)
		}
	})
}

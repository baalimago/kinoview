package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

func Test_jsonStore_ListHandlerFunc(t *testing.T) {
	jStore := NewJSONStore(WithStorePath(t.TempDir()))
	jStore.cache = map[string]model.Item{
		"1": {ID: "1", Name: "foo"},
		"2": {ID: "2", Name: "bar"},
	}

	t.Run("returns all items as JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()

		handler := jStore.ListHandlerFunc()
		handler.ServeHTTP(rr, req)

		if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("got Content-Type %q, want application/json", ct)
		}
		var items []model.Item
		if err := json.NewDecoder(rr.Body).Decode(&items); err != nil {
			t.Fatalf("failed decoding response: %v", err)
		}
		if len(items) != 2 {
			t.Errorf("expected 2 items, got %d", len(items))
		}
	})

	t.Run("cache nil triggers error", func(t *testing.T) {
		js := NewJSONStore(WithStorePath(t.TempDir()))
		js.cache = nil
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler := js.ListHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusInternalServerError {
			t.Errorf("expected internal error on nil cache")
		}
		if !strings.Contains(rr.Body.String(), "store not initialized") {
			t.Errorf("error message not reported")
		}
	})
}

func Test_jsonStore_VideoHandlerFunc(t *testing.T) {
	js := NewJSONStore(WithStorePath(t.TempDir()))
	handler := js.VideoHandlerFunc()

	t.Run("cache nil triggers not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/video/1", nil)
		req.SetPathValue("id", "1")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusNotFound &&
			rr.Result().StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 404 or 500 on nil cache")
		}
	})

	t.Run("missing id returns bad request", func(t *testing.T) {
		js.cache = map[string]model.Item{}
		req := httptest.NewRequest(http.MethodGet, "/video", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("want 400, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("it should return 404 if item is found, but its not of video mimetype", func(t *testing.T) {
		js.cache = map[string]model.Item{
			"without_video_mime": {
				MIMEType: "something/else",
				Path:     "mock/Jellyfish_1080_10s_1MB.mkv",
			},
			"with_video_mime": {
				MIMEType: "video/webm",
				Path:     "mock/Jellyfish_1080_10s_1MB.mkv",
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/video/with_video_mime", nil)
		req.SetPathValue("id", "with_video_mime")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusOK {
			t.Errorf("want 200 on video with mime, got %d", rr.Result().StatusCode)
		}
		req = httptest.NewRequest(http.MethodGet, "/video/without_video_mime", nil)
		req.SetPathValue("id", "without_video_mime")
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusNotFound {
			t.Errorf("want 404 on video without mimetype, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		js.cache = map[string]model.Item{}
		req := httptest.NewRequest(http.MethodGet, "/video/xyz", nil)
		req.SetPathValue("id", "xyz")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusNotFound {
			t.Errorf("want 404, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("known id, file not found", func(t *testing.T) {
		js.cache = map[string]model.Item{"x": {Path: "no/such/file", MIMEType: "image/png", Name: "img.png"}}
		req := httptest.NewRequest(http.MethodGet, "/video/x", nil)
		req.SetPathValue("id", "x")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusNotFound {
			t.Errorf("want 404 for missing file, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("exists in cache, path not found", func(t *testing.T) {
		s := NewJSONStore(WithStorePath(t.TempDir()))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}
		tmpDir := t.TempDir()
		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		item := model.Item{
			Name:     "cacheonly",
			ID:       "cache-test-id",
			Path:     path.Join(tmpDir, "not-a-real-file.txt"),
			MIMEType: "video/plain",
		}
		s.cache[item.ID] = item

		req := mockHTTPRequest("GET", "/video/"+item.ID, nil)
		req.SetPathValue("id", item.ID)

		rec := newMockResponseWriter()
		handler := s.VideoHandlerFunc()
		handler(rec, req)
		if rec.statusCode != http.StatusNotFound {
			t.Fatalf("Expected status 404, got %d", rec.statusCode)
		}
		if !strings.Contains(string(rec.buffer), "") {
			t.Errorf("Expected error response, got: %s", rec.buffer)
		}
	})
}

type mockExtractor struct{}

func (m *mockExtractor) extract(item model.Item, streamIndex string) (string, error) {
	return "", fmt.Errorf("mocked failure")
}

func Test_jsonStore_SubsHandlerFunc(t *testing.T) {
	js := NewJSONStore(WithStorePath(t.TempDir()))
	js.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error {
			return nil
		},
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			return i, nil
		},
	}
	js.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error {
			return nil
		},
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			return i, nil
		},
	}

	t.Run("cache nil triggers not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/subs/1/0", nil)
		req.SetPathValue("vid", "1")
		req.SetPathValue("sub_idx", "0")
		rr := httptest.NewRecorder()
		handler := js.SubsHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 on nil cache, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("missing video ID returns bad request", func(t *testing.T) {
		js.cache = map[string]model.Item{}
		req := httptest.NewRequest(http.MethodGet, "/subs", nil)
		rr := httptest.NewRecorder()
		handler := js.SubsHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("respond with 500 on extract failure", func(t *testing.T) {
		js := NewJSONStore(WithStorePath(t.TempDir()))
		js.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}

		js.cache = map[string]model.Item{"1": {ID: "1", Path: "dummy"}}

		js.subStreamExtractor = &mockExtractor{}
		req := httptest.NewRequest(http.MethodGet, "/subs/1/0", nil)
		req.SetPathValue("vid", "1")
		req.SetPathValue("sub_idx", "0")
		rr := httptest.NewRecorder()
		handler := js.SubsHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 500 on extractor fail, got %d", rr.Result().StatusCode)
		}
	})
}

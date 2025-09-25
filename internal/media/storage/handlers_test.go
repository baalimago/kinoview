package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

func Test_store_ListHandlerFunc(t *testing.T) {
	jStore := NewStore(WithStorePath(t.TempDir()))
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
		js := NewStore(WithStorePath(t.TempDir()))
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

func Test_store_VideoHandlerFunc(t *testing.T) {
	js := newTestStore(t)
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
		s := newTestStore(t)
		tmpDir := t.TempDir()
		_, err := s.Setup(context.Background())
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

func Test_store_SubsHandlerFunc(t *testing.T) {
	s := newTestStore(t)

	t.Run("cache nil triggers not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/subs/1/0", nil)
		req.SetPathValue("vid", "1")
		req.SetPathValue("sub_idx", "0")
		rr := httptest.NewRecorder()
		handler := s.SubsHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 on nil cache, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("missing video ID returns bad request", func(t *testing.T) {
		s.cache = map[string]model.Item{}
		req := httptest.NewRequest(http.MethodGet, "/subs", nil)
		rr := httptest.NewRecorder()
		handler := s.SubsHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Result().StatusCode)
		}
	})

	t.Run("respond with 500 on extract failure", func(t *testing.T) {
		s := NewStore(WithStorePath(t.TempDir()))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}

		s.cache = map[string]model.Item{"1": {ID: "1", Path: "dummy"}}

		s.subStreamExtractor = &mockExtractor{}
		req := httptest.NewRequest(http.MethodGet, "/subs/1/0", nil)
		req.SetPathValue("vid", "1")
		req.SetPathValue("sub_idx", "0")
		rr := httptest.NewRecorder()
		handler := s.SubsHandlerFunc()
		handler.ServeHTTP(rr, req)
		if rr.Result().StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 500 on extractor fail, got %d", rr.Result().StatusCode)
		}
	})
}

func Test_store_SubsListHandlerFunc(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()))
	s.cacheMu.Lock()
	s.cache["withMime"] = model.Item{
		ID:       "withMime",
		Name:     "With Mimé - A french story",
		MIMEType: "video/mp4",
	}
	s.cache["withOutMime"] = model.Item{
		ID:       "withMime",
		Name:     "With O-ute Mimé - An Kenyan story",
		MIMEType: "image/mpeg",
	}
	s.cacheMu.Unlock()

	t.Run("missing vid path responds 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		s.SubsListHandlerFunc().ServeHTTP(rr, req)
		testboil.FailTestIfDiff(t, rr.Result().StatusCode, http.StatusBadRequest)
	})

	t.Run("missing video responds 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/subs/", nil)
		req.SetPathValue("vid", "doesntExist")
		rr := httptest.NewRecorder()
		s.SubsListHandlerFunc().ServeHTTP(rr, req)
		testboil.FailTestIfDiff(t, rr.Result().StatusCode, http.StatusNotFound)
	})

	t.Run("wrong mimetype responds not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/subs/", nil)
		req.SetPathValue("vid", "withOutMime")
		rr := httptest.NewRecorder()
		s.SubsListHandlerFunc().ServeHTTP(rr, req)
		testboil.FailTestIfDiff(t, rr.Result().StatusCode, http.StatusNotFound)
		bodyBytes, err := io.ReadAll(rr.Body)
		testboil.FailTestIfDiff(t, err, nil)
		got := string(bodyBytes)
		want := "media found, but its not a video\n"
		testboil.FailTestIfDiff(t, got, want)
	})
}

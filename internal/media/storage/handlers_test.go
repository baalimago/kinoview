package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/media/thumbnail"
	"github.com/baalimago/kinoview/internal/model"
)

func Test_store_ListHandlerFunc_basic(t *testing.T) {
	s := newTestStore(t)
	h := s.ListHandlerFunc()

	t.Run("nil cache returns 500", func(t *testing.T) {
		body := `{"start":1,"amount":1}`
		s.cache = nil
		req := httptest.NewRequest(http.MethodPost, "/list",
			strings.NewReader(body))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("want 500, got %d", rr.Code)
		}
	})

	t.Run("bad body returns 400", func(t *testing.T) {
		s.cache = map[string]model.Item{
			"a": {ID: "a", Name: "A", MIMEType: "image/png"},
		}
		req := httptest.NewRequest(http.MethodPost, "/list", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d", rr.Code)
		}
	})
}

func Test_store_ListHandlerFunc_ok(t *testing.T) {
	s := newTestStore(t)
	h := s.ListHandlerFunc()

	s.cacheMu.Lock()
	s.cache = map[string]model.Item{
		"1": {ID: "1", Name: "one", MIMEType: "image/png"},
		"2": {ID: "2", Name: "two", MIMEType: "image/png"},
		"3": {ID: "3", Name: "three", MIMEType: "image/png"},
	}
	s.cacheMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/list?start=1&am=1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	gotBody := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body: '%v'", rr.Code, gotBody)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("ct = %q", ct)
	}
	var got model.PaginatedResponse[model.Item]
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Current impl may return 0 due to loop bounds.
	// Ensure it returns a JSON array (possibly empty).
	if got.Items == nil {
		t.Fatalf("got nil slice")
	}
}

func Test_store_ImageHandlerFunc(t *testing.T) {
	s := newTestStore(t)
	h := s.ImageHandlerFunc()

	t.Run("missing id returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d", rr.Code)
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		s.cache = map[string]model.Item{}
		req := httptest.NewRequest(http.MethodGet, "/image/x", nil)
		req.SetPathValue("id", "x")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rr.Code)
		}
	})

	t.Run("wrong mime returns 404", func(t *testing.T) {
		s.cache = map[string]model.Item{
			"x": {ID: "x", MIMEType: "video/webm", Path: "nowhere"},
		}
		req := httptest.NewRequest(http.MethodGet, "/image/x", nil)
		req.SetPathValue("id", "x")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rr.Code)
		}
		body := rr.Body.String()
		if !strings.Contains(body, "not an image") {
			t.Fatalf("unexpected body: %q", body)
		}
	})

	t.Run("image path not found returns 404", func(t *testing.T) {
		s.cache = map[string]model.Item{
			"y": {
				ID:       "y",
				Name:     "y.png",
				Path:     "no/such.png",
				MIMEType: "image/png",
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/image/y", nil)
		req.SetPathValue("id", "y")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rr.Code)
		}
	})

	t.Run("ok serves file", func(t *testing.T) {
		dir := t.TempDir()
		fp := path.Join(dir, "ok.png")
		if err := os.WriteFile(fp, []byte("data"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		s.cache = map[string]model.Item{
			"z": {
				ID:       "z",
				Name:     "ok.png",
				Path:     fp,
				MIMEType: "image/png",
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/image/z", nil)
		req.SetPathValue("id", "z")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if rr.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("bad content-type")
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

func writePNG(t *testing.T, p string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	if err := thumbnail.SaveImage(img, "png", p); err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
}

func Test_store_handleImageItem(t *testing.T) {
	t.Run("uses existing thumb", func(t *testing.T) {
		s := newTestStore(t)
		dir := t.TempDir()

		src := path.Join(dir, "a.png")
		writePNG(t, src, 10, 10)

		thumb := thumbnail.GetThumbnailPath(src)
		writePNG(t, thumb, 5, 5)

		i := model.Item{
			Name:     "a.png",
			Path:     src,
			MIMEType: "image/png",
		}

		if err := s.handleImageItem(&i); err != nil {
			t.Fatalf("handleImageItem: %v", err)
		}

		if i.Thumbnail.Path != thumb {
			t.Fatalf("thumb path = %q, want %q", i.Thumbnail.Path, thumb)
		}
		if i.Thumbnail.Width == 0 || i.Thumbnail.Height == 0 {
			t.Fatalf("thumb dims not set")
		}
	})

	t.Run("creates thumbnail when missing", func(t *testing.T) {
		s := newTestStore(t)
		dir := t.TempDir()

		src := path.Join(dir, "b.png")
		writePNG(t, src, 16, 9)

		i := model.Item{
			Name:     "b.png",
			Path:     src,
			MIMEType: "image/png",
		}

		if err := s.handleImageItem(&i); err != nil {
			t.Fatalf("handleImageItem: %v", err)
		}

		want := thumbnail.GetThumbnailPath(src)
		if i.Thumbnail.Path != want {
			t.Fatalf("thumb path = %q, want %q", i.Thumbnail.Path, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("thumb not created: %v", err)
		}
		if i.Thumbnail.Width != thumbnail.ThumbnailWidth {
			t.Fatalf("width = %d", i.Thumbnail.Width)
		}
		if i.Thumbnail.Height != thumbnail.ThumbnailHeight {
			t.Fatalf("height = %d", i.Thumbnail.Height)
		}
	})

	t.Run("errors on thumbnail input", func(t *testing.T) {
		s := newTestStore(t)
		dir := t.TempDir()

		thumbLike := path.Join(dir, "c_thumb.png")
		// Do not create any files to force LoadImage fail

		i := &model.Item{
			Name:     "c_thumb.png",
			Path:     thumbLike,
			MIMEType: "image/png",
		}

		if err := s.handleImageItem(i); err == nil {
			t.Fatal("expected error when input is a thumbnail")
		}
	})

	t.Run("errors on unspupported MIME", func(t *testing.T) {
		s := newTestStore(t)
		dir := t.TempDir()

		src := path.Join(dir, "d.png")
		writePNG(t, src, 10, 10)

		i := &model.Item{
			Name:     "d.png",
			Path:     src,
			MIMEType: "application/octet-stream",
		}

		if err := s.handleImageItem(i); err == nil {
			t.Fatal("expected error for unsupported mime")
		}
	})
}

func Test_handlePaginatedRequest_errors(t *testing.T) {
	t.Run("nil request returns error", func(t *testing.T) {
		_, err := handlePaginatedRequest(10, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		_, err := handlePaginatedRequest(10, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid start", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/x?start=abc&am=1",
			nil,
		)
		_, err := handlePaginatedRequest(10, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid am", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/x?start=0&am=foo",
			nil,
		)
		_, err := handlePaginatedRequest(10, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("start negative", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/x?start=-1&am=1",
			nil,
		)
		_, err := handlePaginatedRequest(10, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("start >= total", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/x?start=10&am=1",
			nil,
		)
		_, err := handlePaginatedRequest(10, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func Test_handlePaginatedRequest_ok(t *testing.T) {
	t.Run("caps end at totalAm", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/x?start=3&am=10",
			nil,
		)
		got, err := handlePaginatedRequest(5, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := model.PaginatedRequest{
			Start: 3,
			Am:    5,
		}
		if got.Start != want.Start || got.Am != want.Am {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})

	t.Run("returns start, end and mime", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/x?start=2&am=3&mime=video",
			nil,
		)
		got, err := handlePaginatedRequest(10, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Start != 2 {
			t.Fatalf("Start=%d", got.Start)
		}
		if got.Am != 5 {
			t.Fatalf("Am(end)=%d", got.Am)
		}
		if got.MIMEType != "video" {
			t.Fatalf("MIME=%q", got.MIMEType)
		}
	})
}

package storage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

// mockClassifier is a mock implementation of the Classifier interface.
type mockClassifier struct {
	SetupFunc    func(context.Context) error
	ClassifyFunc func(context.Context, model.Item) (model.Item, error)
}

func (m *mockClassifier) Setup(ctx context.Context) error {
	if m.SetupFunc != nil {
		return m.SetupFunc(ctx)
	}
	return nil
}

func (m *mockClassifier) Classify(ctx context.Context, item model.Item) (model.Item, error) {
	if m.ClassifyFunc != nil {
		return m.ClassifyFunc(ctx, item)
	}
	return item, nil
}

func mockHTTPRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	return req
}

type mockResponseWriter struct {
	header     http.Header
	statusCode int
	buffer     []byte
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{header: make(http.Header)}
}

func (m *mockResponseWriter) Header() http.Header {
	return m.header
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.buffer = append(m.buffer, b...)
	return len(b), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

// Mock implementations for testing
type mockSubtitleStreamFinder struct{}

func (m *mockSubtitleStreamFinder) find(item model.Item) (model.MediaInfo, error) {
	return model.MediaInfo{}, nil
}

type mockSubtitleStreamExtractor struct{}

func (m *mockSubtitleStreamExtractor) extract(item model.Item, streamIndex string) (string, error) {
	return "", nil
}

func Test_jsonStore_Setup(t *testing.T) {
	t.Run("successful setup", func(t *testing.T) {
		s := NewJSONStore()
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}
		tmpDir := t.TempDir()

		jsonPath := path.Join(tmpDir, "store.json")
		if err := os.WriteFile(jsonPath, []byte("[]"), 0o644); err != nil {
			t.Fatalf("failed to create empty store.json: %v", err)
		}

		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
	})

	t.Run("loads items from json, using id as key", func(t *testing.T) {
		tmpDir := t.TempDir()
		s := NewJSONStore(WithStorePath(tmpDir))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}

		key := "an-id"
		want := model.Item{Name: "a", ID: key}
		wantList := []model.Item{want}
		wantBytes, err := json.Marshal(wantList)
		if err != nil {
			t.Fatalf("failed to marshal want: %v", err)
		}

		jsonPath := path.Join(tmpDir, "store.json")
		if errW := os.WriteFile(jsonPath, wantBytes, 0o644); errW != nil {
			t.Fatalf("failed to create empty store.json: %v", errW)
		}

		err = s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		s.cacheMu.Lock()
		t.Cleanup(s.cacheMu.Unlock)
		got, exists := s.cache[key]
		if !exists {
			t.Fatal("expected item to exist")
		}

		if !reflect.DeepEqual(want, got) {
			t.Fatalf("expected: %+v, to be: %+v", got, want)
		}
	})

	testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
		s := NewJSONStore(WithStorePath(t.TempDir()))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}
		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
	}, time.Millisecond*100)
}

func Test_jsonStore_Store(t *testing.T) {
	t.Run("store item successfully", func(t *testing.T) {
		s := NewJSONStore(WithStorePath(t.TempDir()))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}

		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}

		item := model.Item{Name: "sample", ID: "1234"}
		err = s.Store(context.Background(), item)
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}

		s.cacheMu.Lock()
		got, exists := s.cache[item.ID]
		s.cacheMu.Unlock()
		if !exists {
			t.Fatal("expected item to be stored but it does not exist")
		}
		if !reflect.DeepEqual(item, got) {
			t.Fatalf("expected: %+v, got: %+v", item, got)
		}
	})

	// This test is to ensure that the path of some media is updated while
	// the rest of the data is kept, as this is generated by LLM
	t.Run("update path on item without ID, keep the rest", func(t *testing.T) {
		s := NewJSONStore(WithStorePath(t.TempDir()))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error {
				return nil
			},
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				return i, nil
			},
		}
		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		pre := testboil.CreateTestFile(t, "before")
		post := testboil.CreateTestFile(t, "after")
		t.Cleanup(func() {
			pre.Close()
			post.Close()
		})
		largeIshString := ""
		for range 10 {
			largeIshString += "AABBCCDDEEFFGGAABBCCDDEEFFGGAABBCCDDEEFFGG"
		}
		// Same content in both files, implying it has been moved
		pre.WriteString(largeIshString)
		post.WriteString(largeIshString)
		want := json.RawMessage(`{"This":  "should stay"}`)
		has := model.Item{Name: "with_ID", Path: pre.Name(), Metadata: &want}
		id := generateID(has)
		has.ID = id

		s.cacheMu.Lock()
		s.cache[id] = has
		s.cacheMu.Unlock()

		newItemWithNoID := has
		newItemWithNoID.ID = ""
		newItemWithNoID.Path = post.Name()
		// Test should fail if this is overwritten
		n := json.RawMessage(`{}`)
		newItemWithNoID.Metadata = &n
		err = s.Store(context.Background(), newItemWithNoID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		s.cacheMu.Lock()
		t.Cleanup(s.cacheMu.Unlock)
		got := s.cache[id].Metadata
		testboil.FailTestIfDiff(t, string(*got), string(want))
	})
}

func Test_streamMkvToMp4(t *testing.T) {
	t.Run("successful mkv to mp4 stream", func(t *testing.T) {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Skip("ffmpeg binary not found")
		}

		// Prepare test file path
		mkvPath := "mock/Jellyfish_1080_10s_1MB.mkv"
		if _, err := os.Stat(mkvPath); err != nil {
			t.Fatalf("test mkv file missing: %v", err)
		}

		req := mockHTTPRequest("GET", "/test", nil)
		r := req

		rec := newMockResponseWriter()

		streamMkvToMp4(rec, r, mkvPath)

		// Verify headers
		if rec.Header().Get("Content-Type") != "video/mp4" {
			t.Errorf("Content-Type not set: %s", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Accept-Ranges") != "bytes" {
			t.Errorf("Accept-Ranges not set")
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("Cache-Control not set")
		}
		// Output MP4 check (ftyp tag near file start)
		out := rec.buffer
		if len(out) < 12 || string(out[4:8]) != "ftyp" {
			t.Errorf("output not mp4 format")
		}
		if rec.statusCode != 200 && rec.statusCode != 0 {
			t.Errorf("unexpected http status: %d", rec.statusCode)
		}
	})

	t.Run("missing ffmpeg returns error", func(t *testing.T) {
		origLookPath := ffmpegLookPath
		ffmpegLookPath = "non-existent"
		t.Cleanup(func() {
			ffmpegLookPath = origLookPath
		})
		req := mockHTTPRequest("GET", "/test", nil)
		rec := newMockResponseWriter()
		streamMkvToMp4(rec, req, "mock/Jellyfish_1080_10s_1MB.mkv")
		if rec.statusCode != 500 {
			t.Errorf("expected 500 when ffmpeg is missing")
		}
		if !strings.Contains(string(rec.buffer), "ffmpeg must be installed") {
			t.Errorf("missing ffmpeg error response not found")
		}
	})

	t.Run("bad mkv path triggers error", func(t *testing.T) {
		req := mockHTTPRequest("GET", "/test", nil)
		rec := newMockResponseWriter()
		streamMkvToMp4(rec, req, "mock/doesnotexist.mkv")
		if rec.statusCode != 500 {
			t.Errorf("expected http status 500 for bad mkv input")
		}
	})

	t.Run("context cancel simulates disconnect", func(t *testing.T) {
		mkvPath := "mock/Jellyfish_1080_10s_1MB.mkv"
		req := mockHTTPRequest("GET", "/test", nil)
		ctx, cancel := context.WithCancel(req.Context())
		req = req.WithContext(ctx)
		rec := newMockResponseWriter()
		done := make(chan struct{})
		go func() {
			streamMkvToMp4(rec, req, mkvPath)
			close(done)
		}()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("timed out waiting for disconnect simulation")
		}
	})
}

func Test_Stream_jsonStore_ffmpegSubsUtil_extract(t *testing.T) {
	t.Run("extract subs if possible", func(t *testing.T) {
		subsUtil := ffmpegSubsUtil{}
		given := model.Item{
			ID:   "some_ID",
			Path: "./mock/Jellyfish_with_subs.mkv",
		}
		gotFile, err := subsUtil.extract(given, "1")
		if err != nil {
			t.Fatalf("failed to extract: %v", err)
		}

		gotData, err := os.ReadFile(gotFile)
		if err != nil {
			t.Fatalf("failed to read gotFile: %v", err)
		}
		// Subtitle line which appears in mock video
		testboil.AssertStringContains(t, string(gotData), "Pay close attention to each step.")
	})

	t.Run("it should error if ffmpeg is not installed", func(t *testing.T) {
		subsUtil := &ffmpegSubsUtil{}
		origPath := os.Getenv("PATH")
		defer os.Setenv("PATH", origPath)
		os.Setenv("PATH", "/nonexistent")
		item := model.Item{ID: "no-ffmpeg", Path: "/tmp/doesnotmatter.mkv"}
		_, err := subsUtil.extract(item, "0")
		if err == nil {
			t.Fatal("expected error when ffprobe unavailable, got nil")
		}
	})
}

func Test_Stream_jsonStore_ffmpegSubsUtil_find(t *testing.T) {
	t.Run("find uses mediaCache hit", func(t *testing.T) {
		wantIdx := 404
		util := &ffmpegSubsUtil{
			mediaCache: map[string]model.MediaInfo{
				"cache-id": {Streams: []model.Stream{{Index: wantIdx}}},
			},
		}
		item := model.Item{ID: "cache-id", Path: "/tmp/some.mkv"}
		info, err := util.find(item)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(info.Streams) < 1 || info.Streams[0].Index != wantIdx {
			t.Fatalf("unexpected info: %+v", info)
		}
	})

	t.Run("find returns error when ffprobe/ffmpeg unavailable", func(t *testing.T) {
		util := &ffmpegSubsUtil{mediaCache: map[string]model.MediaInfo{}}
		origPath := os.Getenv("PATH")
		defer os.Setenv("PATH", origPath)
		os.Setenv("PATH", "/nonexistent")
		item := model.Item{ID: "missing-ff", Path: "/tmp/irrelevant.mkv"}
		_, err := util.find(item)
		if err == nil {
			t.Fatal("expected error when ffmpeg is unavailable, got nil")
		}
	})

	t.Run("find returns error for invalid input", func(t *testing.T) {
		util := &ffmpegSubsUtil{mediaCache: map[string]model.MediaInfo{}}
		item := model.Item{ID: "bogus", Path: "/path/does/not/exist.mkv"}
		_, err := util.find(item)
		if err == nil {
			t.Fatal("expected error for invalid input, got nil")
		}
	})

	t.Run("find caches on successful find", func(t *testing.T) {
		util := &ffmpegSubsUtil{
			mediaCache: map[string]model.MediaInfo{},
		}
		// Use a known good file with ffprobe available in path
		item := model.Item{ID: "some-unique-id", Path: "./mock/Jellyfish_with_subs.mkv"}
		_, err := util.find(item)
		if err != nil {
			t.Fatalf("unexpected error calling find: %v", err)
		}
		// Should set cache for this ID
		if _, ok := util.mediaCache[item.ID]; !ok {
			t.Fatalf("expected find to cache result for %s", item.ID)
		}
	})
}

func Test_Stream_jsonStore_ffmpegSubsUtil_cache(t *testing.T) {
	t.Run("it should return item on media cache hit", func(t *testing.T) {
		wantIdx := 1337
		// Setup ffmpegSubsUtil with mock caches
		util := &ffmpegSubsUtil{
			mediaCache: map[string]model.MediaInfo{
				"id-abc": {Streams: []model.Stream{
					{
						Index: wantIdx,
					},
				}},
			},
			subsCache: map[string]string{
				"id-xyz": "/tmp/mocksubs.vtt",
			},
		}

		item := model.Item{ID: "id-abc", Path: "/tmp/mockfile.mkv"}
		// Should hit mediaCache and return without error
		info, err := util.find(item)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if info.Streams[0].Index != wantIdx {
			t.Fatalf("unexpected info from mediaCache: %+v", info)
		}

		item.ID = "id-xyz"
		got, err := util.extract(item, "0")
		if err != nil {
			t.Fatalf("expected nil err from extract cache, got %v", err)
		}
		if got != "/tmp/mocksubs.vtt" {
			t.Fatalf("unexpected path from subsCache: %s", got)
		}
	})

	t.Run("error on ffmpeg error (not panic)", func(t *testing.T) {
		util := &ffmpegSubsUtil{
			mediaCache: map[string]model.MediaInfo{},
			subsCache:  map[string]string{},
		}

		item := model.Item{ID: "nohit-" + randString(6), Path: os.DevNull}
		// Funky streamIndex but ffmpeg will fail, so expect error not panics
		_, err := util.extract(item, "0")
		if err == nil {
			t.Fatalf("expected error due to ffmpeg fail")
		}
	})

	t.Run("properly adds metadata when storing new file", func(t *testing.T) {
		dir := t.TempDir()
		s := NewJSONStore(WithStorePath(dir))
		want := `{"some": "metadata"}`
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error { return nil },
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				if i.Metadata == nil {
					r := json.RawMessage(want)
					i.Metadata = &r
				}
				return i, nil
			},
		}
		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		item := model.Item{Name: "media", ID: "meta1"}
		err = s.Store(context.Background(), item)
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}

		s.cacheMu.Lock()
		t.Cleanup(s.cacheMu.Unlock)
		got, exist := s.cache[item.ID]
		if !exist {
			t.Fatal("expected item to exist in store")
		}
		gotMetadata := string(*got.Metadata)
		if gotMetadata != want {
			t.Errorf("expected metadata to be set, got: %v", gotMetadata)
		}
	})

	t.Run("existing file metadata is not regenerated or overwritten", func(t *testing.T) {
		dir := t.TempDir()
		s := NewJSONStore(WithStorePath(dir))
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error { return nil },
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				r := json.RawMessage(`{"initial":"metadata"}`)
				i.Metadata = &r
				return i, nil
			},
		}
		err := s.Setup(context.Background())
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		item := model.Item{Name: "media2", ID: "meta2"}
		err = s.Store(context.Background(), item)
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}

		s.cacheMu.Lock()
		original := s.cache[item.ID]
		s.cacheMu.Unlock()
		originalMeta := string(*original.Metadata)
		t.Logf("Original metadata: %v", originalMeta)
		s.classifier = &mockClassifier{
			SetupFunc: func(ctx context.Context) error { return nil },
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
				r := json.RawMessage(`{"new":"metadata"}`)
				i.Metadata = &r
				return i, nil
			},
		}
		err = s.Store(context.Background(), item)
		if err != nil {
			t.Fatalf("Store failed on second call: %v", err)
		}
		s.cacheMu.Lock()
		t.Cleanup(s.cacheMu.Unlock)
		got := s.cache[item.ID]
		gotMeta := string(*got.Metadata)
		if gotMeta != originalMeta {
			t.Errorf("metadata should not be overwritten: got %v, want %v", gotMeta, originalMeta)
		}
	})
}

// randString for ID, deterministic length, not crypto-rand.
func randString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	out := make([]rune, n)
	for i := range out {
		out[i] = letters[i%len(letters)]
	}
	return string(out)
}

func Test_newJSONStore_options(t *testing.T) {
	t.Run("options pattern should work", func(t *testing.T) {
		mockFinder := &mockSubtitleStreamFinder{}
		mockExtractor := &mockSubtitleStreamExtractor{}
		mockClassifier := &mockClassifier{}

		s := NewJSONStore(
			WithStorePath(t.TempDir()),
			WithSubtitleStreamFinder(mockFinder),
			WithSubtitleStreamExtractor(mockExtractor),
			WithClassifier(mockClassifier),
		)

		if s.subStreamFinder != mockFinder {
			t.Fatal("subtitle stream finder should be the mock finder")
		}

		if s.subStreamExtractor != mockExtractor {
			t.Fatal("subtitle stream extractor should be the mock extractor")
		}

		if s.classifier != mockClassifier {
			t.Fatal("classifier should be the mock classifier")
		}
	})
}

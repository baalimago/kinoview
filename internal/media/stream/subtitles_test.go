package stream

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

type mockRunner struct {
	outputMap map[string][]byte
	runErrMap map[string]error
	// runCallback allows side effects (like creating files)
	runCallback func(name string, args ...string) error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) error {
	if m.runCallback != nil {
		if err := m.runCallback(name, args...); err != nil {
			return err
		}
	}
	if err, ok := m.runErrMap[name]; ok {
		return err
	}
	return nil
}

func (m *mockRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if out, ok := m.outputMap[name]; ok {
		return out, nil
	}
	if err, ok := m.runErrMap[name]; ok {
		return nil, err
	}
	return []byte{}, nil
}

func TestNewManager(t *testing.T) {
	// Test default path
	m, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if m.storePath == "" {
		t.Error("expected non-empty default store path")
	}

	// Test custom path
	tmp := t.TempDir()
	m2, err := NewManager(WithStoragePath(tmp))
	if err != nil {
		t.Fatalf("NewManager failed with option: %v", err)
	}
	if m2.storePath != tmp {
		t.Errorf("expected store path %q, got %q", tmp, m2.storePath)
	}

	// Test directory creation
	targetDir := filepath.Join(tmp, "subdir")
	_, err = NewManager(WithStoragePath(targetDir))
	if err != nil {
		t.Fatalf("failed to create manager with subdir: %v", err)
	}
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Error("failed to create storage directory")
	}
}

func TestFind(t *testing.T) {
	expectedInfo := model.MediaInfo{
		Streams: []model.Stream{{Index: 1, CodecType: "subtitle"}},
	}
	infoJSON, _ := json.Marshal(expectedInfo)

	mr := &mockRunner{
		outputMap: map[string][]byte{
			"ffprobe": infoJSON,
		},
		runErrMap: make(map[string]error),
	}

	m, _ := NewManager(withRunner(mr), WithStoragePath(t.TempDir()))
	item := model.Item{ID: "test-id", Path: "/path/to/vid.mp4"}

	// 1. Successful Find
	info, err := m.Find(item)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	if len(info.Streams) != 1 {
		t.Errorf("expected 1 stream, got %d", len(info.Streams))
	}

	// 2. Cache Hit (Run runner again with error, should rely on cache)
	mr.runErrMap["ffprobe"] = errors.New("should not be called")
	info2, err := m.Find(item)
	if err != nil {
		t.Fatalf("Find failed on cache hit: %v", err)
	}
	if len(info2.Streams) != 1 {
		t.Error("cache hit returned wrong info")
	}

	// 3. Error Case (New Item)
	newItem := model.Item{ID: "error-id", Path: "bad.mp4"}
	delete(mr.outputMap, "ffprobe") // Clear success output so it falls through to error
	mr.runErrMap["ffprobe"] = errors.New("command failed")
	_, err = m.Find(newItem)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ffprobe failed") {
		t.Errorf("unexpected error message: %v", err)
	}

	// 4. JSON parse error
	badJSONItem := model.Item{ID: "bad-json", Path: "bad.mp4"}
	mr.outputMap = map[string][]byte{"ffprobe": []byte("invalid json")}
	delete(mr.runErrMap, "ffprobe")
	_, err = m.Find(badJSONItem)
	if err == nil {
		t.Fatal("expected error on json parse, got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExtractSubtitles(t *testing.T) {
	tmp := t.TempDir()
	mr := &mockRunner{
		runErrMap: make(map[string]error),
	}
	m, _ := NewManager(withRunner(mr), WithStoragePath(tmp))
	item := model.Item{ID: "extract-id", Path: "vid.mp4"}
	streamIdx := "2"

	// Mock file creation
	mr.runCallback = func(name string, args ...string) error {
		if name == "ffmpeg" {
			// Find the output file argument (last one)
			outPath := args[len(args)-1]
			return os.WriteFile(outPath, []byte("WEBVTT"), 0o644)
		}
		return nil
	}

	// 1. Successful ExtractSubtitlesion
	path, err := m.ExtractSubtitles(item, streamIdx)
	if err != nil {
		t.Fatalf("ExtractSubtitles failed: %v", err)
	}
	expectedPath := filepath.Join(tmp, "extract-id_2.vtt")
	if path != expectedPath {
		t.Errorf("got path %q, want %q", path, expectedPath)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file wasn't created")
	}

	// 2. Existence Check (Disk Cache)
	// Make mock runner fail, extract should succeed because file exists
	mr.runErrMap["ffmpeg"] = errors.New("should not run")
	path2, err := m.ExtractSubtitles(item, streamIdx)
	if err != nil {
		t.Fatalf("ExtractSubtitlesFromCache failed: %v", err)
	}
	if path2 != expectedPath {
		t.Errorf("got path %q", path2)
	}

	// 3. ExtractSubtitlesion Error
	itemFail := model.Item{ID: "fail-id", Path: "fail.mp4"}
	mr.runCallback = nil // invalidates callback
	mr.runErrMap["ffmpeg"] = errors.New("process crash")
	_, err = m.ExtractSubtitles(itemFail, "0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ffmpeg extraction failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefaultRunner(t *testing.T) {
	// Simple integration test for the default runner helper
	// Expecting "echo" to exist on the system running tests
	dr := &defaultRunner{}
	ctx := context.Background()

	// Test Output
	out, err := dr.Output(ctx, "echo", "hello")
	if err != nil {
		// Might fail if echo not in path, checking mostly coverage
		t.Logf("echo failed: %v", err)
	} else {
		if !strings.Contains(string(out), "hello") {
			t.Errorf("echo output wrong: %s", out)
		}
	}

	// Test Run
	err = dr.Run(ctx, "echo", "run")
	if err != nil {
		t.Logf("echo run failed: %v", err)
	}

	// Test Failure
	err = dr.Run(ctx, "nonexistentcommand")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

func TestExtractLanguage(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"5_English.srt", "eng"},
		{"4_English.srt", "eng"},
		{"English.srt", "eng"},
		{"2_French.srt", "fre"},
		{"Swedish.vtt", "swe"},
		{"1_German.srt", "ger"},
		{"spanish.srt", "spa"},
		{"7_ENG.srt", "eng"},
		{"unknown.srt", "und"},
		{"no_language_hint.vtt", "und"},
	}
	for _, tt := range tests {
		got := extractLanguage(tt.filename)
		if got != tt.want {
			t.Errorf("extractLanguage(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestSidecarFiles(t *testing.T) {
	tmp := t.TempDir()

	// Create some sidecar files
	os.WriteFile(filepath.Join(tmp, "movie.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHello"), 0o644)
	os.WriteFile(filepath.Join(tmp, "movie.vtt"), []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello"), 0o644)
	os.WriteFile(filepath.Join(tmp, "other.txt"), []byte("not a sub"), 0o644)

	files := sidecarFiles(tmp, "movie")
	if len(files) != 2 {
		t.Errorf("expected 2 sidecar files, got %d: %v", len(files), files)
	}
}

func TestGlobFiles(t *testing.T) {
	tmp := t.TempDir()

	// Create test files
	os.WriteFile(filepath.Join(tmp, "1_English.srt"), []byte("content"), 0o644)
	os.WriteFile(filepath.Join(tmp, "2_French.srt"), []byte("content"), 0o644)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("not a sub"), 0o644)

	files := globFiles(tmp)
	if len(files) != 2 {
		t.Errorf("expected 2 files from glob, got %d: %v", len(files), files)
	}

	// Non-existent directory
	files = globFiles(filepath.Join(tmp, "nonexistent"))
	if len(files) != 0 {
		t.Errorf("expected 0 files from non-existent dir, got %d", len(files))
	}
}

func TestFindExternal(t *testing.T) {
	tmp := t.TempDir()

	// Create a mock media directory structure like the user's example:
	// ./
	// ├── The.Office.S01E01.mp4
	// └── Subs/
	//     └── The.Office.S01E01/
	//         ├── 5_English.srt
	//         └── 6_English.srt

	mediaDir := filepath.Join(tmp, "The.Office.US.S01.1080p.BluRay.x265-RARBG")
	subsDir := filepath.Join(mediaDir, "Subs", "The.Office.US.S01E01.1080p.BluRay.x265-RARBG")
	os.MkdirAll(subsDir, 0o755)

	// Create media file (just an empty file for the test)
	mediaPath := filepath.Join(mediaDir, "The.Office.US.S01E01.1080p.BluRay.x265-RARBG.mp4")
	os.WriteFile(mediaPath, []byte("fake video"), 0o644)

	// Create external subtitles in Subs/<episode-name>/
	os.WriteFile(filepath.Join(subsDir, "5_English.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHello"), 0o644)
	os.WriteFile(filepath.Join(subsDir, "6_English.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nWorld"), 0o644)

	// Also create a sidecar .srt directly beside the media file
	os.WriteFile(filepath.Join(mediaDir, "The.Office.US.S01E01.1080p.BluRay.x265-RARBG.srt"), []byte("sidecar"), 0o644)

	mr := &mockRunner{
		outputMap: map[string][]byte{"ffprobe": []byte(`{"streams":[]}`)},
		runErrMap: make(map[string]error),
	}

	m, _ := NewManager(withRunner(mr), WithStoragePath(t.TempDir()))
	item := model.Item{
		ID:   "test-office-ep1",
		Name: "The.Office.US.S01E01.1080p.BluRay.x265-RARBG.mp4",
		Path: mediaPath,
	}

	info, err := m.Find(item)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}

	// Should have 3 external streams (2 from Subs/<episode>/ + 1 sidecar)
	// Plus 0 embedded streams
	if len(info.Streams) != 3 {
		t.Errorf("expected 3 total streams, got %d", len(info.Streams))
	}

	// Check that external streams have negative indices
	for _, s := range info.Streams {
		if s.ExternalPath != "" {
			if s.Index >= 0 {
				t.Errorf("external stream should have negative index, got %d", s.Index)
			}
			if s.CodecType != "subtitle" {
				t.Errorf("external stream should be subtitle type, got %s", s.CodecType)
			}
			if s.ExternalPath == "" {
				t.Error("external stream should have ExternalPath set")
			}
			if s.Tags.Language == "" {
				t.Error("external stream should have language tag")
			}
		}
	}

	// Verify cache doesn't re-scan
	info2, _ := m.Find(item)
	if len(info2.Streams) != 3 {
		t.Errorf("cached result should have same count, got %d", len(info2.Streams))
	}
}

func TestFindExternalPath(t *testing.T) {
	tmp := t.TempDir()
	mr := &mockRunner{
		outputMap: map[string][]byte{"ffprobe": []byte(`{"streams":[{"index":0,"codec_type":"video"}]}`)},
		runErrMap: make(map[string]error),
	}
	m, _ := NewManager(withRunner(mr), WithStoragePath(tmp))

	// Pre-populate cache with a media info that includes external streams
	item := model.Item{ID: "test-find-ext", Name: "test.mp4", Path: filepath.Join(tmp, "test.mp4")}
	os.WriteFile(item.Path, []byte("fake"), 0o644)

	_, err := m.Find(item)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}

	// Now test findExternalPath for a non-existent external path
	_, err = m.findExternalPath(item, -99)
	if err == nil {
		t.Error("expected error for non-existent external path")
	}

	// Test for non-cached item
	_, err = m.findExternalPath(model.Item{ID: "nonexistent"}, -1)
	if err == nil {
		t.Error("expected error for non-cached item")
	}
}

func TestExtractSubtitles_External(t *testing.T) {
	tmp := t.TempDir()
	storeDir := t.TempDir()

	// Create an external .srt file
	extSrt := filepath.Join(tmp, "external.srt")
	os.WriteFile(extSrt, []byte("1\n00:00:01,000 --> 00:00:02,000\nTest"), 0o644)

	// Pre-populate cache with an external stream
	m, _ := NewManager(withRunner(&mockRunner{
		outputMap: map[string][]byte{"ffprobe": []byte(`{"streams":[{"index":0,"codec_type":"video"}]}`)},
		runErrMap: make(map[string]error),
	}), WithStoragePath(storeDir))

	item := model.Item{ID: "ext-test", Name: "ext.mp4", Path: filepath.Join(tmp, "ext.mp4")}
	os.WriteFile(item.Path, []byte("fake"), 0o644)

	// Prime the cache with the external stream by modifying after Find
	// Simpler: manually inject a cached entry
	m.mediaMu.Lock()
	m.mediaCache[item.ID] = model.MediaInfo{
		Streams: []model.Stream{
			{Index: 0, CodecType: "video"},
			{Index: -1, CodecType: "subtitle", CodecTagString: "external_srt", ExternalPath: extSrt, Tags: model.Tags{Language: "eng", Title: "external.srt"}},
		},
	}
	m.mediaMu.Unlock()

	// Mock ffmpeg to create the output file
	mr := &mockRunner{}
	mr.runCallback = func(name string, args ...string) error {
		if name == "ffmpeg" {
			outPath := args[len(args)-1]
			return os.WriteFile(outPath, []byte("WEBVTT"), 0o644)
		}
		return nil
	}
	m.runner = mr

	// Extract the external subtitle (negative index)
	path, err := m.ExtractSubtitles(item, "-1")
	if err != nil {
		t.Fatalf("ExtractSubtitles for external failed: %v", err)
	}
	expectedPath := filepath.Join(storeDir, "ext-test_-1.vtt")
	if path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, path)
	}

	// Verify the file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("extracted file does not exist")
	}
}

func TestExtractSubtitles_External_CacheMiss(t *testing.T) {
	// Tests that ExtractSubtitles recovers from a cold cache by calling Find first.
	tmp := t.TempDir()
	storeDir := t.TempDir()

	// Create media directory with external subtitles
	mediaDir := filepath.Join(tmp, "Show.S01")
	subsDir := filepath.Join(mediaDir, "Subs", "Show.S01E01")
	os.MkdirAll(subsDir, 0o755)

	mediaPath := filepath.Join(mediaDir, "Show.S01E01.mp4")
	os.WriteFile(mediaPath, []byte("fake"), 0o644)
	extSrt := filepath.Join(subsDir, "1_English.srt")
	os.WriteFile(extSrt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello"), 0o644)

	mr := &mockRunner{
		outputMap: map[string][]byte{"ffprobe": []byte(`{"streams":[]}`)},
		runErrMap: make(map[string]error),
	}
	mr.runCallback = func(name string, args ...string) error {
		if name == "ffmpeg" {
			outPath := args[len(args)-1]
			return os.WriteFile(outPath, []byte("WEBVTT"), 0o644)
		}
		return nil
	}

	m, _ := NewManager(withRunner(mr), WithStoragePath(storeDir))
	item := model.Item{
		ID:   "cache-miss-test",
		Name: "Show.S01E01.mp4",
		Path: mediaPath,
	}

	// ExtractSubtitles with external index WITHOUT calling Find first (cold cache)
	// It should auto-populate via Find fallback and then succeed.
	path, err := m.ExtractSubtitles(item, "-1")
	if err != nil {
		t.Fatalf("ExtractSubtitles with cold cache should auto-recover, got: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

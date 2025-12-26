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

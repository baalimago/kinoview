package clientcontext

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func TestNewWithEmptyCacheDir(t *testing.T) {
	tmpDir := t.TempDir()

	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}

	expectedPath := filepath.Join(tmpDir, appName, storeDir, logFile)
	if manager.logPath != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, manager.logPath)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(contexts))
	}
}

func TestNewWithNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()

	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(contexts))
	}
}

func TestNewWithExistingLog(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, appName, storeDir, logFile)

	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create JSONL log with two contexts
	logContent := `{"sessionId":"session1","startTime":"2024-01-01T10:00:00Z","viewingHistory":[{"name":"movie1","viewedAt":"2024-01-01T10:00:00Z","playedFor":"120"}],"timeOfDay":"morning"}
{"sessionId":"session1","startTime":"2024-01-01T10:00:00Z","viewingHistory":[{"name":"movie2","viewedAt":"2024-01-01T10:30:00Z","playedFor":"300"}],"timeOfDay":"morning"}
`

	if err := os.WriteFile(expectedPath, []byte(logContent), 0o644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(contexts))
	}
}

func TestNewWithEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, appName, storeDir, logFile)

	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	if err := os.WriteFile(expectedPath, []byte{}, 0o644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(contexts))
	}
}

func TestNewWithInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, appName, storeDir, logFile)

	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	if err := os.WriteFile(expectedPath, []byte("invalid json {]"), 0o644); err != nil {
		t.Fatalf("failed to write invalid JSON file: %v", err)
	}

	manager, err := New(tmpDir)
	if err == nil {
		t.Fatal("expected error when loading invalid JSON, got nil")
	}

	if manager != nil {
		t.Fatal("expected nil manager when error occurs")
	}
}

func TestAllClientContexts(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(contexts))
	}

	now := time.Now()
	testContext := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "120",
			},
		},
		TimeOfDay: "evening",
	}
	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	contexts = manager.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context, got %d", len(contexts))
	}

	// Verify it returns a copy
	contexts[0].TimeOfDay = "modified"
	contexts = manager.AllClientContexts()
	if contexts[0].TimeOfDay != "evening" {
		t.Errorf("expected TimeOfDay 'evening', got %s", contexts[0].TimeOfDay)
	}
}

func TestStoreClientContext(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()
	testContext := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "3600",
			},
		},
		TimeOfDay:      "evening",
		LastPlayedName: "movie1",
	}

	err = manager.StoreClientContext(testContext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context, got %d", len(contexts))
	}

	if contexts[0].SessionID != "session1" {
		t.Errorf("expected SessionID 'session1', got %s", contexts[0].SessionID)
	}
}

func TestStoreClientContextMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()

	for i := 1; i <= 5; i++ {
		testContext := model.ClientContext{
			SessionID: "session1",
			StartTime: now,
			ViewingHistory: []model.ViewMetadata{
				{
					Name:         "movie" + string(rune(48+i)),
					ViewedAt:     now.Add(time.Duration(i) * time.Minute),
					PlayedForSec: "120",
				},
			},
			TimeOfDay: "evening",
		}
		if err := manager.StoreClientContext(testContext); err != nil {
			t.Fatalf("failed to store context %d: %v", i, err)
		}
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 5 {
		t.Errorf("expected 5 contexts, got %d", len(contexts))
	}
}

func TestStoreClientContextPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()
	testContext := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "120",
			},
		},
		TimeOfDay: "morning",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	manager2, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create second manager: %v", err)
	}

	contexts := manager2.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context after reload, got %d", len(contexts))
	}

	if contexts[0].SessionID != "session1" {
		t.Errorf("expected SessionID 'session1', got %s", contexts[0].SessionID)
	}
}

func TestStoreClientContextWithComplexData(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()
	testContext := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "3600",
			},
			{
				Name:         "movie2",
				ViewedAt:     now.Add(-24 * time.Hour),
				PlayedForSec: "5400",
			},
		},
		TimeOfDay:      "evening",
		LastPlayedName: "movie1",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context, got %d", len(contexts))
	}

	if len(contexts[0].ViewingHistory) != 2 {
		t.Errorf("expected 2 viewing history entries, got %d", len(contexts[0].ViewingHistory))
	}
}

func TestManagerInterfaceImplementation(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	_ = manager.AllClientContexts()

	testContext := model.ClientContext{
		SessionID:      "session1",
		StartTime:      time.Now(),
		TimeOfDay:      "evening",
		LastPlayedName: "movie1",
	}
	_ = manager.StoreClientContext(testContext)
}

func TestConcurrentStoreClientContext(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	done := make(chan error, 10)
	now := time.Now()

	for i := 0; i < 10; i++ {
		go func(index int) {
			testContext := model.ClientContext{
				SessionID: "session1",
				StartTime: now,
				ViewingHistory: []model.ViewMetadata{
					{
						Name:         "movie" + string(rune(48+index)),
						ViewedAt:     now.Add(time.Duration(index) * time.Minute),
						PlayedForSec: "120",
					},
				},
				TimeOfDay: "evening",
			}
			done <- manager.StoreClientContext(testContext)
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent store failed: %v", err)
		}
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 10 {
		t.Errorf("expected 10 contexts, got %d", len(contexts))
	}
}

func TestConcurrentAllClientContexts(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()

	for i := 0; i < 5; i++ {
		testContext := model.ClientContext{
			SessionID: "session" + string(rune(49+i)),
			StartTime: now.Add(time.Duration(i) * time.Hour),
			ViewingHistory: []model.ViewMetadata{
				{
					Name:         "movie" + string(rune(49+i)),
					ViewedAt:     now.Add(time.Duration(i) * time.Hour),
					PlayedForSec: "120",
				},
			},
			TimeOfDay: "evening",
		}
		if err := manager.StoreClientContext(testContext); err != nil {
			t.Fatalf("failed to store context: %v", err)
		}
	}

	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			contexts := manager.AllClientContexts()
			done <- len(contexts)
		}()
	}

	for i := 0; i < 10; i++ {
		count := <-done
		if count != 5 {
			t.Errorf("expected 5 contexts, got %d", count)
		}
	}
}

func TestFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()
	testContext := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "120",
			},
		},
		TimeOfDay: "evening",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	fileInfo, err := os.Stat(manager.logPath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	mode := fileInfo.Mode()
	if (mode & 0o644) != 0o644 {
		t.Errorf("expected file permissions 0o644, got %o", mode)
	}
}

func TestDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()
	testContext := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "120",
			},
		},
		TimeOfDay: "evening",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	dir := filepath.Dir(manager.logPath)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	if !info.IsDir() {
		t.Fatal("expected directory, got file")
	}
}

func TestIncrementalStorage(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	now := time.Now()

	// Store first context
	ctx1 := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now,
				PlayedForSec: "120",
			},
		},
		TimeOfDay: "morning",
	}

	if err := manager.StoreClientContext(ctx1); err != nil {
		t.Fatalf("failed to store context 1: %v", err)
	}

	// Verify file exists and has content
	fileInfo, err := os.Stat(manager.logPath)
	if err != nil {
		t.Fatalf("failed to stat log file: %v", err)
	}
	firstSize := fileInfo.Size()

	// Store second context
	ctx2 := model.ClientContext{
		SessionID: "session1",
		StartTime: now,
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie2",
				ViewedAt:     now.Add(30 * time.Minute),
				PlayedForSec: "300",
			},
		},
		TimeOfDay: "morning",
	}

	if err := manager.StoreClientContext(ctx2); err != nil {
		t.Fatalf("failed to store context 2: %v", err)
	}

	// Verify file size increased (append-only)
	fileInfo, err = os.Stat(manager.logPath)
	if err != nil {
		t.Fatalf("failed to stat log file: %v", err)
	}
	secondSize := fileInfo.Size()

	if secondSize <= firstSize {
		t.Errorf("expected file size to increase after append, first=%d, second=%d", firstSize, secondSize)
	}

	// Verify both contexts are loaded
	contexts := manager.AllClientContexts()
	if len(contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(contexts))
	}
}

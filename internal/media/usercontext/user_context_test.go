package usercontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func TestNewWithEmptyCacheDir(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a new manager with the temporary directory
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}

	// Verify the path is correctly constructed
	expectedPath := filepath.Join(tmpDir, appName, storeDir, storeFile)
	if manager.path != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, manager.path)
	}

	// Verify contexts are initialized
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

	// Verify no contexts are loaded
	contexts := manager.AllClientContexts()
	if len(contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(contexts))
	}
}

func TestNewWithExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, appName, storeDir, storeFile)

	// Create the directory structure
	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create a file with some contexts
	testContexts := []model.ClientContext{
		{
			ViewingHistory: []model.ViewMetadata{
				{
					Name:         "test1",
					ViewedAt:     time.Now(),
					PlayedForSec: "120",
				},
			},
			TimeOfDay:      "evening",
			LastPlayedName: "test1",
		},
		{
			ViewingHistory: []model.ViewMetadata{
				{
					Name:         "test2",
					ViewedAt:     time.Now(),
					PlayedForSec: "300",
				},
			},
			TimeOfDay:      "morning",
			LastPlayedName: "test2",
		},
	}

	data, err := json.MarshalIndent(testContexts, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal test contexts: %v", err)
	}

	if err := os.WriteFile(expectedPath, data, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create manager and verify contexts are loaded
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(contexts))
	}

	if contexts[0].LastPlayedName != "test1" {
		t.Errorf("expected first context LastPlayedName to be 'test1', got %s", contexts[0].LastPlayedName)
	}

	if contexts[1].LastPlayedName != "test2" {
		t.Errorf("expected second context LastPlayedName to be 'test2', got %s", contexts[1].LastPlayedName)
	}
}

func TestNewWithEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, appName, storeDir, storeFile)

	// Create the directory structure
	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create an empty file
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
	expectedPath := filepath.Join(tmpDir, appName, storeDir, storeFile)

	// Create the directory structure
	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create a file with invalid JSON
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

func TestNewLoadsMaxContextsLimit(t *testing.T) {
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, appName, storeDir, storeFile)

	// Create the directory structure
	if err := os.MkdirAll(filepath.Dir(expectedPath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create a file with more than maxContexts
	testContexts := make([]model.ClientContext, maxContexts+100)
	for i := 0; i < maxContexts+100; i++ {
		testContexts[i] = model.ClientContext{
			TimeOfDay:      "test",
			LastPlayedName: "test",
		}
	}

	data, err := json.MarshalIndent(testContexts, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal test contexts: %v", err)
	}

	if err := os.WriteFile(expectedPath, data, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != maxContexts {
		t.Errorf("expected %d contexts after truncation, got %d", maxContexts, len(contexts))
	}
}

func TestAllClientContexts(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Test with empty contexts
	contexts := manager.AllClientContexts()
	if len(contexts) != 0 {
		t.Errorf("expected 0 contexts, got %d", len(contexts))
	}

	// Add a context
	testContext := model.ClientContext{
		TimeOfDay:      "evening",
		LastPlayedName: "movie1",
	}
	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	// Test that AllClientContexts returns a copy
	contexts = manager.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context, got %d", len(contexts))
	}

	// Modify the returned slice and verify original is unchanged
	contexts[0].TimeOfDay = "modified"
	contexts = manager.AllClientContexts()
	if contexts[0].TimeOfDay != "evening" {
		t.Errorf("expected TimeOfDay to be 'evening', got %s", contexts[0].TimeOfDay)
	}
}

func TestStoreClientContext(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	testContext := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     time.Now(),
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

	if contexts[0].LastPlayedName != "movie1" {
		t.Errorf("expected LastPlayedName to be 'movie1', got %s", contexts[0].LastPlayedName)
	}

	if contexts[0].TimeOfDay != "evening" {
		t.Errorf("expected TimeOfDay to be 'evening', got %s", contexts[0].TimeOfDay)
	}
}

func TestStoreClientContextMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Store multiple contexts
	for i := 1; i <= 5; i++ {
		testContext := model.ClientContext{
			TimeOfDay:      "evening",
			LastPlayedName: "movie" + string(rune(48+i)),
		}
		if err := manager.StoreClientContext(testContext); err != nil {
			t.Fatalf("failed to store context %d: %v", i, err)
		}
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 5 {
		t.Errorf("expected 5 contexts, got %d", len(contexts))
	}

	// Verify order is preserved (FIFO)
	for i := 0; i < 5; i++ {
		expected := "movie" + string(rune(49+i))
		if contexts[i].LastPlayedName != expected {
			t.Errorf("expected contexts[%d].LastPlayedName to be '%s', got %s", i, expected, contexts[i].LastPlayedName)
		}
	}
}

func TestStoreClientContextPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	testContext := model.ClientContext{
		TimeOfDay:      "morning",
		LastPlayedName: "movie1",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	// Create a new manager instance to verify persistence
	manager2, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create second manager: %v", err)
	}

	contexts := manager2.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context after reload, got %d", len(contexts))
	}

	if contexts[0].LastPlayedName != "movie1" {
		t.Errorf("expected LastPlayedName to be 'movie1', got %s", contexts[0].LastPlayedName)
	}
}

func TestStoreClientContextMaxContextsLimit(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Store more than maxContexts
	for i := 0; i < maxContexts+100; i++ {
		testContext := model.ClientContext{
			TimeOfDay:      "evening",
			LastPlayedName: "movie" + string(rune((i % 10) + 48)),
		}
		if err := manager.StoreClientContext(testContext); err != nil {
			t.Fatalf("failed to store context %d: %v", i, err)
		}
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != maxContexts {
		t.Errorf("expected %d contexts after limit, got %d", maxContexts, len(contexts))
	}

	// Verify that the oldest contexts were removed (newest are kept)
	// The last 1000 contexts should be stored
	expectedLastName := "movie" + string(rune((maxContexts+99)%10+48))
	if contexts[len(contexts)-1].LastPlayedName != expectedLastName {
		t.Errorf("expected last context LastPlayedName to be '%s', got %s", expectedLastName, contexts[len(contexts)-1].LastPlayedName)
	}
}

func TestStoreClientContextMaxContextsLimitPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Store more than maxContexts
	for i := 0; i < maxContexts+100; i++ {
		testContext := model.ClientContext{
			TimeOfDay:      "evening",
			LastPlayedName: "movie" + string(rune((i % 10) + 48)),
		}
		if err := manager.StoreClientContext(testContext); err != nil {
			t.Fatalf("failed to store context %d: %v", i, err)
		}
	}

	// Reload and verify the limit is still enforced
	manager2, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create second manager: %v", err)
	}

	contexts := manager2.AllClientContexts()
	if len(contexts) != maxContexts {
		t.Errorf("expected %d contexts after reload, got %d", maxContexts, len(contexts))
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

	if contexts[0].ViewingHistory[0].Name != "movie1" {
		t.Errorf("expected first history entry name to be 'movie1', got %s", contexts[0].ViewingHistory[0].Name)
	}

	if contexts[0].ViewingHistory[1].Name != "movie2" {
		t.Errorf("expected second history entry name to be 'movie2', got %s", contexts[0].ViewingHistory[1].Name)
	}
}

func TestManagerInterfaceImplementation(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Verify that Manager implements UserContextManager interface
	// This is a compile-time check, but we can verify the methods exist
	_ = manager.AllClientContexts()

	testContext := model.ClientContext{
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

	// Store contexts concurrently
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			testContext := model.ClientContext{
				TimeOfDay:      "evening",
				LastPlayedName: "movie" + string(rune(48+index)),
			}
			done <- manager.StoreClientContext(testContext)
		}(i)
	}

	// Wait for all goroutines to complete
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

	// Store some contexts
	for i := 0; i < 5; i++ {
		testContext := model.ClientContext{
			TimeOfDay:      "evening",
			LastPlayedName: "movie" + string(rune(49+i)),
		}
		if err := manager.StoreClientContext(testContext); err != nil {
			t.Fatalf("failed to store context: %v", err)
		}
	}

	// Read contexts concurrently
	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			contexts := manager.AllClientContexts()
			done <- len(contexts)
		}()
	}

	// Verify all reads return the same number of contexts
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

	testContext := model.ClientContext{
		TimeOfDay:      "evening",
		LastPlayedName: "movie1",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	// Verify file was created with correct permissions
	fileInfo, err := os.Stat(manager.path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	// Check file mode (should be 0o644)
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

	testContext := model.ClientContext{
		TimeOfDay:      "evening",
		LastPlayedName: "movie1",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	// Verify directory structure was created
	dir := filepath.Dir(manager.path)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	if !info.IsDir() {
		t.Fatal("expected directory, got file")
	}
}

func TestEmptyViewingHistory(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	testContext := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{},
		TimeOfDay:      "evening",
		LastPlayedName: "",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context, got %d", len(contexts))
	}

	if len(contexts[0].ViewingHistory) != 0 {
		t.Errorf("expected empty viewing history, got %d entries", len(contexts[0].ViewingHistory))
	}
}

func TestNilViewingHistory(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	testContext := model.ClientContext{
		ViewingHistory: nil,
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

	// Verify nil viewing history is preserved
	if contexts[0].ViewingHistory != nil {
		t.Errorf("expected nil viewing history, got %v", contexts[0].ViewingHistory)
	}
}

func TestSpecialCharactersInNames(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	testContext := model.ClientContext{
		TimeOfDay:      "evening",
		LastPlayedName: "movie: The Beginning [2024] 🎬",
	}

	if err := manager.StoreClientContext(testContext); err != nil {
		t.Fatalf("failed to store context: %v", err)
	}

	contexts := manager.AllClientContexts()
	if len(contexts) != 1 {
		t.Errorf("expected 1 context, got %d", len(contexts))
	}

	if contexts[0].LastPlayedName != "movie: The Beginning [2024] 🎬" {
		t.Errorf("expected LastPlayedName with special characters, got %s", contexts[0].LastPlayedName)
	}
}

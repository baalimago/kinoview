package media

import (
	"encoding/json"
	"os"
	"path"
	"testing"
)

func TestEnsureChatIndexCache_createsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	claiDir := path.Join(dir, "clai")

	err := EnsureChatIndexCache(claiDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cachePath := path.Join(claiDir, "conversations", chatIndexCacheFile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not created: %v", err)
	}

	var cache chatIndexCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("invalid cache JSON: %v", err)
	}

	if cache.Version != 2 {
		t.Errorf("expected version 2, got %d", cache.Version)
	}

	if cache.Rows == nil {
		t.Error("rows should not be nil, expected empty slice")
	}
}

func TestEnsureChatIndexCache_idempotent(t *testing.T) {
	dir := t.TempDir()
	claiDir := path.Join(dir, "clai")

	// First call creates the cache.
	if err := EnsureChatIndexCache(claiDir); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call should be a no-op.
	if err := EnsureChatIndexCache(claiDir); err != nil {
		t.Fatalf("second call: %v", err)
	}

	cachePath := path.Join(claiDir, "conversations", chatIndexCacheFile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file should still exist: %v", err)
	}

	// Verify the original content is preserved (not overwritten).
	var cache chatIndexCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("invalid cache JSON: %v", err)
	}
	if cache.Version != 2 {
		t.Errorf("version changed to %d", cache.Version)
	}
}

func TestEnsureChatIndexCache_preservesExisting(t *testing.T) {
	dir := t.TempDir()
	claiDir := path.Join(dir, "clai")
	convDir := path.Join(claiDir, "conversations")
	cachePath := path.Join(convDir, chatIndexCacheFile)

	// Create a pre-existing cache with custom content.
	os.MkdirAll(convDir, 0o755)
	existing := chatIndexCache{
		Version: 2,
		Rows: []chatIndexRow{
			{ID: "test-1", Created: "2026-01-01", MessageCount: 5},
		},
	}
	b, _ := json.Marshal(existing)
	os.WriteFile(cachePath, b, 0o644)

	// Call should not overwrite.
	if err := EnsureChatIndexCache(claiDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(cachePath)
	var cache chatIndexCache
	json.Unmarshal(data, &cache)

	if len(cache.Rows) != 1 || cache.Rows[0].ID != "test-1" {
		t.Error("existing cache was overwritten")
	}
}

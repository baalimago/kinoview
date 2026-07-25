package suggestions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

func TestManagerInterface(t *testing.T) {
	var _ agents.SuggestionManager = (*Manager)(nil)
}

func TestManager_LoadLegacyArrayFormat(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Write a legacy bare-array suggestions.json
	legacy := `[{"ID":"1","Name":"Legacy Suggestion"},{"ID":"2","Name":"Legacy Two"}]`
	cacheFile := filepath.Join(tempDir, "suggestions.json")
	if err := os.WriteFile(cacheFile, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	got := m.Get()
	if len(got) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(got))
	}
	if got[0].Name != "Legacy Suggestion" {
		t.Errorf("expected 'Legacy Suggestion', got %q", got[0].Name)
	}
	if got[1].Name != "Legacy Two" {
		t.Errorf("expected 'Legacy Two', got %q", got[1].Name)
	}

	// Fingerprint must be nil for legacy files.
	if m.Fingerprint() != nil {
		t.Error("fingerprint must be nil after loading legacy format")
	}

	// After a save, the file should be in object format.
	m.Update(got)
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"suggestions"`) {
		t.Error("file should be in object format after save")
	}
}

func TestManager_RoundTripWithFingerprint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	recs := []model.Suggestion{
		{Item: model.Item{ID: "1", Name: "A"}},
		{Item: model.Item{ID: "2", Name: "B"}},
	}
	fp := model.SuggestionFingerprint{
		Library: "abc123",
		Context: "def456",
		Version: 3,
	}
	gen := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	err = m.UpdateWithFingerprint(recs, fp, gen)
	if err != nil {
		t.Fatalf("UpdateWithFingerprint failed: %v", err)
	}

	// Reload from disk.
	m2, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("reload NewManager failed: %v", err)
	}

	got := m2.Get()
	if len(got) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(got))
	}
	if got[0].Name != "A" || got[1].Name != "B" {
		t.Errorf("suggestions mismatch: %+v", got)
	}

	gotFP := m2.Fingerprint()
	if gotFP == nil {
		t.Fatal("fingerprint must survive round-trip")
	}
	if gotFP.Library != fp.Library || gotFP.Context != fp.Context || gotFP.Version != fp.Version {
		t.Errorf("fingerprint mismatch: got %+v, want %+v", gotFP, fp)
	}

	gotGen := m2.Generated()
	if !gotGen.Equal(gen) {
		t.Errorf("generated mismatch: got %v, want %v", gotGen, gen)
	}
}

func TestManager_UnreadableFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a directory where the file should be to cause a read error.
	cacheFile := filepath.Join(tempDir, "suggestions.json")
	if err := os.Mkdir(cacheFile, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = NewManager(tempDir)
	if err == nil {
		t.Fatal("expected error when suggestions.json is a directory")
	}
}

func TestManager_MalformedFingerprint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Object format with malformed fingerprint (wrong type).
	malformed := `{"suggestions":[{"ID":"1","Name":"Test"}],"fingerprint":"not-an-object"}`
	cacheFile := filepath.Join(tempDir, "suggestions.json")
	if err := os.WriteFile(cacheFile, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager should not fail on malformed fingerprint: %v", err)
	}
	got := m.Get()
	if len(got) != 1 || got[0].Name != "Test" {
		t.Errorf("suggestions should still load, got: %+v", got)
	}
	// Fingerprint should be nil since it was malformed (json will discard it).
	if m.Fingerprint() != nil {
		t.Error("fingerprint must be nil for malformed input")
	}
}

func TestManager_AtomicSave(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	recs := []model.Suggestion{{Item: model.Item{ID: "1", Name: "Test"}}}
	m.Update(recs)

	// Verify no .tmp file is left behind.
	tmpPath := filepath.Join(tempDir, "suggestions.json.tmp")
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error(".tmp file should not exist after successful save")
	}

	// Verify the real file has the data.
	data, err := os.ReadFile(filepath.Join(tempDir, "suggestions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Test"`) {
		t.Errorf("file doesn't contain expected data: %s", string(data))
	}
}

func TestManager_Envelope(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	recs := []model.Suggestion{{Item: model.Item{ID: "1", Name: "X"}}}
	fp := model.SuggestionFingerprint{Library: "l", Context: "c", Version: 3}
	gen := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)
	m.UpdateWithFingerprint(recs, fp, gen)

	env := m.Envelope()
	if len(env.Suggestions) != 1 || env.Suggestions[0].Name != "X" {
		t.Errorf("suggestions mismatch: %+v", env.Suggestions)
	}
	if env.Fingerprint == nil || env.Fingerprint.Library != "l" {
		t.Errorf("fingerprint mismatch: %+v", env.Fingerprint)
	}
	if env.Generated != "2026-07-25T14:00:00Z" {
		t.Errorf("generated mismatch: %q", env.Generated)
	}
}

func TestManager_UpdateClearsFingerprint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// First save with fingerprint.
	recs := []model.Suggestion{{Item: model.Item{ID: "1", Name: "A"}}}
	fp := model.SuggestionFingerprint{Library: "l", Context: "c", Version: 3}
	gen := time.Now()
	m.UpdateWithFingerprint(recs, fp, gen)

	// Then Update without fingerprint.
	recs2 := []model.Suggestion{{Item: model.Item{ID: "2", Name: "B"}}}
	m.Update(recs2)

	// Fingerprint should be cleared.
	if m.Fingerprint() != nil {
		t.Error("Update without fingerprint should clear the fingerprint")
	}
}

func TestManager_AddRemovePreserveFingerprint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	recs := []model.Suggestion{
		{Item: model.Item{ID: "1", Name: "A"}},
		{Item: model.Item{ID: "2", Name: "B"}},
	}
	fp := model.SuggestionFingerprint{Library: "l", Context: "c", Version: 3}
	gen := time.Now()
	m.UpdateWithFingerprint(recs, fp, gen)

	// Add a suggestion — fingerprint must be preserved.
	m.Add(model.Suggestion{Item: model.Item{ID: "3", Name: "C"}})
	gotFP := m.Fingerprint()
	if gotFP == nil || gotFP.Library != "l" {
		t.Error("Add must preserve fingerprint")
	}

	// Remove a suggestion — fingerprint must be preserved.
	m.Remove("1")
	gotFP = m.Fingerprint()
	if gotFP == nil || gotFP.Library != "l" {
		t.Error("Remove must preserve fingerprint")
	}

	// List should still work.
	list, _ := m.List()
	if len(list) != 2 {
		t.Errorf("expected 2 suggestions after add+remove, got %d", len(list))
	}
}

func TestManager_UpdateRejectsEmptying(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Populate with suggestions.
	recs := []model.Suggestion{
		{Item: model.Item{ID: "1", Name: "A"}},
	}
	m.Update(recs)

	// Attempt to update with empty slice — must be rejected.
	err = m.Update([]model.Suggestion{})
	if !errors.Is(err, ErrWouldEmpty) {
		t.Errorf("expected ErrWouldEmpty, got %v", err)
	}

	// Shelf must still have the original suggestions.
	got := m.Get()
	if len(got) != 1 || got[0].Name != "A" {
		t.Errorf("shelf was emptied: got %+v", got)
	}

	// Same with nil.
	err = m.Update(nil)
	if !errors.Is(err, ErrWouldEmpty) {
		t.Errorf("expected ErrWouldEmpty for nil, got %v", err)
	}

	// But Update on an already-empty shelf should succeed (no-op).
	m2, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	err = m2.Update([]model.Suggestion{})
	if err != nil {
		t.Errorf("expected nil error for empty→empty, got %v", err)
	}
}

func TestManager_RemoveAllowsEmptying(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	recs := []model.Suggestion{{Item: model.Item{ID: "1", Name: "Only"}}}
	m.Update(recs)

	// Remove the last suggestion — must succeed.
	err = m.Remove("1")
	if err != nil {
		t.Errorf("Remove on last item failed: %v", err)
	}

	got := m.Get()
	if len(got) != 0 {
		t.Errorf("expected empty shelf after removing last item, got %d", len(got))
	}
}

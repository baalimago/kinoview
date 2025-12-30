package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
)

func TestConciergeContextIntegration_PushAndGet(t *testing.T) {
	// Setup temp cache directory
	tmpDir := t.TempDir()

	// Create push tool
	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	// Create get tool
	getTool, err := NewConciergeContextGet(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextGet: %v", err)
	}

	// Push a note
	note1 := "User watched 3 movies today. Recommend action films."
	respStr, err := pushTool.Call(models.Input{"note": note1})
	if err != nil {
		t.Fatalf("Push note 1: %v", err)
	}
	if !strings.Contains(respStr, "stored concierge note") {
		t.Fatalf("expected success message, got: %s", respStr)
	}

	// Extract ID from response
	var id1 string
	if idx := strings.Index(respStr, "id="); idx >= 0 {
		start := idx + 3
		end := strings.Index(respStr[start:], " ")
		if end >= 0 {
			id1 = respStr[start : start+end]
		}
	}
	if id1 == "" {
		t.Fatalf("failed to extract note ID from response: %s", respStr)
	}

	// Push another note
	time.Sleep(10 * time.Millisecond) // Ensure different timestamp
	note2 := "Detected viewing pattern shift towards documentaries."
	respStr, err = pushTool.Call(models.Input{"note": note2})
	if err != nil {
		t.Fatalf("Push note 2: %v", err)
	}

	var id2 string
	if idx := strings.Index(respStr, "id="); idx >= 0 {
		start := idx + 3
		end := strings.Index(respStr[start:], " ")
		if end >= 0 {
			id2 = respStr[start : start+end]
		}
	}

	// Get all notes in summary mode
	respStr, err = getTool.Call(models.Input{"mode": "summary"})
	if err != nil {
		t.Fatalf("Get summary: %v", err)
	}

	if !strings.Contains(respStr, id1) {
		t.Fatalf("expected note 1 ID in response, got: %s", respStr)
	}
	if !strings.Contains(respStr, id2) {
		t.Fatalf("expected note 2 ID in response, got: %s", respStr)
	}

	// Get most recent note
	respStr, err = getTool.Call(models.Input{"mode": "most_recent"})
	if err != nil {
		t.Fatalf("Get most recent: %v", err)
	}

	if !strings.Contains(respStr, id2) {
		t.Fatalf("expected most recent note (id2) in response, got: %s", respStr)
	}
	if strings.Count(respStr, "id=") != 1 {
		t.Fatalf("expected only 1 note in most_recent mode, got: %s", respStr)
	}
}

func TestConciergeContextIntegration_FullMode(t *testing.T) {
	tmpDir := t.TempDir()

	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	getTool, err := NewConciergeContextGet(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextGet: %v", err)
	}

	note := "This is a detailed note with important information."
	_, err = pushTool.Call(models.Input{"note": note})
	if err != nil {
		t.Fatalf("Push note: %v", err)
	}

	// Summary mode should not include note body
	respStr, err := getTool.Call(models.Input{"mode": "summary"})
	if err != nil {
		t.Fatalf("Get summary: %v", err)
	}
	if strings.Contains(respStr, "detailed note") {
		t.Fatalf("summary mode should not include note body, got: %s", respStr)
	}

	// Full mode should include note body
	respStr, err = getTool.Call(models.Input{"mode": "full"})
	if err != nil {
		t.Fatalf("Get full: %v", err)
	}
	if !strings.Contains(respStr, "detailed note") {
		t.Fatalf("full mode should include note body, got: %s", respStr)
	}
}

func TestConciergeContextIntegration_Pagination(t *testing.T) {
	tmpDir := t.TempDir()

	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	getTool, err := NewConciergeContextGet(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextGet: %v", err)
	}

	// Push 5 notes
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		noteText := "Note number " + string(rune(i+1))
		respStr, err := pushTool.Call(models.Input{"note": noteText})
		if err != nil {
			t.Fatalf("Push note %d: %v", i, err)
		}

		// Extract ID
		if idx := strings.Index(respStr, "id="); idx >= 0 {
			start := idx + 3
			end := strings.Index(respStr[start:], " ")
			if end >= 0 {
				ids[i] = respStr[start : start+end]
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Get first 2 notes (most recent first)
	respStr, err := getTool.Call(models.Input{"limit": float64(2), "offset": float64(0)})
	if err != nil {
		t.Fatalf("Get with limit 2: %v", err)
	}

	// Should contain 2 notes
	if strings.Count(respStr, "id=") != 2 {
		t.Fatalf("expected 2 notes, got: %s", respStr)
	}

	// Get next 2 notes
	respStr, err = getTool.Call(models.Input{"limit": float64(2), "offset": float64(2)})
	if err != nil {
		t.Fatalf("Get with offset 2: %v", err)
	}

	if strings.Count(respStr, "id=") != 2 {
		t.Fatalf("expected 2 notes with offset, got: %s", respStr)
	}

	// Offset past end
	respStr, err = getTool.Call(models.Input{"offset": float64(10)})
	if err != nil {
		t.Fatalf("Get with large offset: %v", err)
	}
	if !strings.Contains(respStr, "no results") {
		t.Fatalf("expected 'no results' message, got: %s", respStr)
	}
}

func TestConciergeContextIntegration_IDFilter(t *testing.T) {
	tmpDir := t.TempDir()

	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	getTool, err := NewConciergeContextGet(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextGet: %v", err)
	}

	// Push first note
	respStr, err := pushTool.Call(models.Input{"note": "First note"})
	if err != nil {
		t.Fatalf("Push first note: %v", err)
	}

	var id1 string
	if idx := strings.Index(respStr, "id="); idx >= 0 {
		start := idx + 3
		end := strings.Index(respStr[start:], " ")
		if end >= 0 {
			id1 = respStr[start : start+end]
		}
	}

	time.Sleep(10 * time.Millisecond)

	// Push second note
	respStr, err = pushTool.Call(models.Input{"note": "Second note"})
	if err != nil {
		t.Fatalf("Push second note: %v", err)
	}

	var id2 string
	if idx := strings.Index(respStr, "id="); idx >= 0 {
		start := idx + 3
		end := strings.Index(respStr[start:], " ")
		if end >= 0 {
			id2 = respStr[start : start+end]
		}
	}

	// Get with ID filter for first note
	respStr, err = getTool.Call(models.Input{"id": id1})
	if err != nil {
		t.Fatalf("Get with id filter: %v", err)
	}

	if !strings.Contains(respStr, id1) {
		t.Fatalf("expected id1 in response, got: %s", respStr)
	}
	if strings.Contains(respStr, id2) {
		t.Fatalf("should not contain id2, got: %s", respStr)
	}

	// Get with non-existent ID
	respStr, err = getTool.Call(models.Input{"id": "non-existent-id"})
	if err != nil {
		t.Fatalf("Get with non-existent id: %v", err)
	}
	if !strings.Contains(respStr, "no concierge note found") {
		t.Fatalf("expected 'no note found' message, got: %s", respStr)
	}
}

func TestConciergeContextIntegration_EmptyCache(t *testing.T) {
	tmpDir := t.TempDir()

	getTool, err := NewConciergeContextGet(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextGet: %v", err)
	}

	// Get from empty cache
	respStr, err := getTool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Get from empty cache: %v", err)
	}

	if !strings.Contains(respStr, "no concierge context has been stored") {
		t.Fatalf("expected 'no context stored' message, got: %s", respStr)
	}
}

func TestConciergeContextIntegration_SanitizeNote(t *testing.T) {
	tmpDir := t.TempDir()

	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	// Test with whitespace
	noteWithWhitespace := "   Note with leading and trailing spaces   "
	respStr, err := pushTool.Call(models.Input{"note": noteWithWhitespace})
	if err != nil {
		t.Fatalf("Push note with whitespace: %v", err)
	}
	if !strings.Contains(respStr, "stored concierge note") {
		t.Fatalf("expected success, got: %s", respStr)
	}

	// Test with empty note
	_, err = pushTool.Call(models.Input{"note": "   "})
	if err == nil {
		t.Fatalf("expected error for empty note, got nil")
	}

	// Test with missing note parameter
	_, err = pushTool.Call(models.Input{})
	if err == nil {
		t.Fatalf("expected error for missing note, got nil")
	}
}

func TestConciergeContextIntegration_FilePersistence(t *testing.T) {
	tmpDir := t.TempDir()

	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	// Push a note
	note := "Important observation about user behavior"
	respStr, err := pushTool.Call(models.Input{"note": note})
	if err != nil {
		t.Fatalf("Push note: %v", err)
	}

	// Extract ID
	var id string
	if idx := strings.Index(respStr, "id="); idx >= 0 {
		start := idx + 3
		end := strings.Index(respStr[start:], " ")
		if end >= 0 {
			id = respStr[start : start+end]
		}
	}

	// Verify file exists
	cacheDir := filepath.Join(tmpDir, "kinoview", "concierge")
	filePath := filepath.Join(cacheDir, id+".json")

	_, err = os.Stat(filePath)
	if err != nil {
		t.Fatalf("expected file to exist at %s: %v", filePath, err)
	}

	// Verify file contents
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var stored conciergeNote
	if err := json.Unmarshal(content, &stored); err != nil {
		t.Fatalf("failed to unmarshal stored note: %v", err)
	}

	if stored.Note != note {
		t.Fatalf("expected note=%q, got %q", note, stored.Note)
	}
	if stored.ID != id {
		t.Fatalf("expected id=%q, got %q", id, stored.ID)
	}
	if stored.CreatedAt.IsZero() {
		t.Fatalf("expected CreatedAt to be set")
	}
}

func TestConciergeContextIntegration_Ordering(t *testing.T) {
	tmpDir := t.TempDir()

	pushTool, err := NewConciergeContextPush(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextPush: %v", err)
	}

	getTool, err := NewConciergeContextGet(
		ConciergeContextWithCacheDir(tmpDir),
	)
	if err != nil {
		t.Fatalf("NewConciergeContextGet: %v", err)
	}

	// Push notes in order
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		respStr, err := pushTool.Call(models.Input{"note": "Note " + string(rune('A'+i))})
		if err != nil {
			t.Fatalf("Push note %d: %v", i, err)
		}

		if idx := strings.Index(respStr, "id="); idx >= 0 {
			start := idx + 3
			end := strings.Index(respStr[start:], " ")
			if end >= 0 {
				ids[i] = respStr[start : start+end]
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Get notes - should be in reverse order (most recent first)
	respStr, err := getTool.Call(models.Input{"mode": "full"})
	if err != nil {
		t.Fatalf("Get notes: %v", err)
	}

	// Find positions of IDs in response
	pos0 := strings.Index(respStr, ids[0])
	pos1 := strings.Index(respStr, ids[1])
	pos2 := strings.Index(respStr, ids[2])

	// Most recent (ids[2]) should appear first, then ids[1], then ids[0]
	if !(pos2 < pos1 && pos1 < pos0) {
		t.Fatalf("expected reverse chronological order, got positions: %d, %d, %d", pos0, pos1, pos2)
	}
}

func TestConciergeCacheDir_WithProvidedCacheDir(t *testing.T) {
	t.Parallel()

	cacheDir := "/custom/cache"
	got, err := conciergeCacheDir(cacheDir)
	if err != nil {
		t.Fatalf("conciergeCacheDir: %v", err)
	}

	expected := filepath.Join(cacheDir, "kinoview", "concierge")
	if got != expected {
		t.Fatalf("got %q want %q", got, expected)
	}
}

func TestConciergeCacheDir_WithEmptyString(t *testing.T) {
	t.Parallel()

	got, err := conciergeCacheDir("")
	if err != nil {
		t.Fatalf("conciergeCacheDir: %v", err)
	}

	if got == "" {
		t.Fatalf("got empty string")
	}

	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}

	if !strings.HasPrefix(filepath.Clean(got), filepath.Clean(filepath.Join(os.Getenv("HOME"), ".cache"))) {
		t.Fatalf("expected path in user cache dir, got %q", got)
	}
}

func TestConciergeCacheDir_PathStructure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	got, err := conciergeCacheDir(tmpDir)
	if err != nil {
		t.Fatalf("conciergeCacheDir: %v", err)
	}

	if !strings.HasPrefix(filepath.Clean(got), filepath.Clean(tmpDir)) {
		t.Fatalf("expected path to start with tmpDir")
	}

	lastPart := filepath.Base(got)
	if lastPart != "concierge" {
		t.Fatalf("expected last component to be 'concierge', got %q",
			lastPart)
	}

	secondLast := filepath.Base(filepath.Dir(got))
	if secondLast != "kinoview" {
		t.Fatalf("expected second-to-last to be 'kinoview', got %q",
			secondLast)
	}
}

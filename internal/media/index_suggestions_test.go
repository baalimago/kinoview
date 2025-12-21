package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baalimago/kinoview/internal/media/suggestions"
	"github.com/baalimago/kinoview/internal/model"
)

func TestSuggestionsHandler(t *testing.T) {
	tempDir := t.TempDir()
	sm, err := suggestions.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create suggestions manager: %v", err)
	}

	testSuggestions := []model.Suggestion{
		{Item: model.Item{Name: "Test 1"}},
		{Item: model.Item{Name: "Test 2"}},
	}
	sm.Update(testSuggestions)

	i := &Indexer{
		suggestions: sm,
	}

	h := i.suggestionsHandler()
	req := httptest.NewRequest(http.MethodGet, "/suggestions", nil)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status %v, want %v", rr.Code, http.StatusOK)
	}

	var got []model.Suggestion
	err = json.Unmarshal(rr.Body.Bytes(), &got)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2", len(got))
	}
	if got[0].Name != "Test 1" {
		t.Errorf("got name %q, want %q", got[0].Name, "Test 1")
	}
}

func TestSuggestions_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	sm, err := suggestions.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create suggestions manager: %v", err)
	}

	testSuggestions := []model.Suggestion{
		{Item: model.Item{Name: "Persisted Suggestion"}},
	}
	sm.Update(testSuggestions)

	// Create a new manager pointing to the same tempDir
	sm2, err := suggestions.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create second suggestions manager: %v", err)
	}

	got := sm2.Get()
	if len(got) != 1 {
		t.Fatalf("got %d suggestions after reload, want 1", len(got))
	}
	if got[0].Name != "Persisted Suggestion" {
		t.Errorf("got name %q after reload, want %q", got[0].Name, "Persisted Suggestion")
	}
}

package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	var payload model.SuggestionsPayload
	err = json.Unmarshal(rr.Body.Bytes(), &payload)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	got := payload.Suggestions
	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2", len(got))
	}
	if got[0].Name != "Test 1" {
		t.Errorf("got name %q, want %q", got[0].Name, "Test 1")
	}

	if payload.State != "available" {
		t.Errorf("expected state 'available', got '%s'", payload.State)
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

func TestSuggestionsHandler_States(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		sm, err := suggestions.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		sm.Update([]model.Suggestion{{Item: model.Item{Name: "X"}}})

		i := &Indexer{suggestions: sm}
		rr := doGet(i.suggestionsHandler())

		var payload model.SuggestionsPayload
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.State != "available" {
			t.Errorf("expected 'available', got '%s'", payload.State)
		}
		if len(payload.Suggestions) != 1 {
			t.Errorf("expected 1 suggestion, got %d", len(payload.Suggestions))
		}
	})

	t.Run("empty", func(t *testing.T) {
		sm, err := suggestions.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		i := &Indexer{suggestions: sm}
		rr := doGet(i.suggestionsHandler())

		var payload model.SuggestionsPayload
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.State != "empty" {
			t.Errorf("expected 'empty', got '%s'", payload.State)
		}
		if len(payload.Suggestions) != 0 {
			t.Errorf("expected 0 suggestions, got %d", len(payload.Suggestions))
		}
	})

	t.Run("computing", func(t *testing.T) {
		sm, err := suggestions.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		i := &Indexer{suggestions: sm, butlerInFlight: true}
		rr := doGet(i.suggestionsHandler())

		var payload model.SuggestionsPayload
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.State != "computing" {
			t.Errorf("expected 'computing', got '%s'", payload.State)
		}
	})

	t.Run("computing_with_previous_suggestions", func(t *testing.T) {
		sm, err := suggestions.NewManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		sm.Update([]model.Suggestion{{Item: model.Item{Name: "Previous"}}})

		// Cascade in flight, but previous suggestions still present.
		i := &Indexer{suggestions: sm, butlerInFlight: true}
		rr := doGet(i.suggestionsHandler())

		var payload model.SuggestionsPayload
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.State != "available" {
			t.Errorf("expected 'available' (has previous suggestions), got '%s'", payload.State)
		}
		if len(payload.Suggestions) != 1 || payload.Suggestions[0].Name != "Previous" {
			t.Errorf("previous suggestions not preserved: %+v", payload.Suggestions)
		}
	})
}

func TestSuggestionsHandler_IncludesGenerated(t *testing.T) {
	sm, err := suggestions.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	gen := time.Date(2026, 7, 25, 14, 30, 0, 0, time.UTC)
	sm.UpdateWithFingerprint(
		[]model.Suggestion{{Item: model.Item{Name: "Fresh"}}},
		model.SuggestionFingerprint{Library: "a", Context: "b", Version: 3},
		gen,
	)

	i := &Indexer{suggestions: sm}
	rr := doGet(i.suggestionsHandler())

	var payload model.SuggestionsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Generated != "2026-07-25T14:30:00Z" {
		t.Errorf("expected generated '2026-07-25T14:30:00Z', got '%s'", payload.Generated)
	}
}

func doGet(h http.HandlerFunc) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/suggestions", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

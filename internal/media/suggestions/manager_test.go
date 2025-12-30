package suggestions

import (
	"os"
	"testing"

	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

func TestManagerInterface(t *testing.T) {
	var _ agents.SuggestionManager = (*Manager)(nil)
}

func TestManager(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kinoview-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	testSuggestions := []model.Suggestion{
		{Item: model.Item{ID: "1", Name: "Test Suggestion 1"}},
		{Item: model.Item{ID: "2", Name: "Test Suggestion 2"}},
	}

	err = m.Update(testSuggestions)
	if err != nil {
		t.Fatalf("failed to update suggestions: %v", err)
	}

	got := m.Get()
	if len(got) != len(testSuggestions) {
		t.Fatalf("got %d suggestions, want %d", len(got), len(testSuggestions))
	}

	if got[0].Name != testSuggestions[0].Name {
		t.Errorf("got title %q, want %q", got[0].Name, testSuggestions[0].Name)
	}

	// Test persistence
	m2, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create second manager: %v", err)
	}

	got2 := m2.Get()
	if len(got2) != len(testSuggestions) {
		t.Fatalf("on reload: got %d suggestions, want %d", len(got2), len(testSuggestions))
	}
	if got2[1].Name != testSuggestions[1].Name {
		t.Errorf("on reload: got title %q, want %q", got2[1].Name, testSuggestions[1].Name)
	}

	// Test Remove
	err = m.Remove(testSuggestions[0].ID)
	if err != nil {
		t.Fatalf("failed to remove suggestion: %v", err)
	}
	got3 := m.Get()
	if len(got3) != 1 {
		t.Fatalf("after remove: got %d suggestions, want 1", len(got3))
	}

	// Test Add
	newSug := model.Suggestion{Item: model.Item{ID: "3", Name: "New one"}}
	err = m.Add(newSug)
	if err != nil {
		t.Fatalf("failed to add suggestion: %v", err)
	}
	got4 := m.Get()
	if len(got4) != 2 {
		t.Fatalf("after add: got %d suggestions, want 2", len(got4))
	}
}

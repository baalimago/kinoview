package tools

import (
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestCheckSuggestionsTool_Call(t *testing.T) {
	suggestions := []model.Suggestion{
		{Item: model.Item{ID: "1", Name: "Movie 1"}, Motivation: "Because I said so"},
	}
	sm := &mockSuggestionManager{suggestions: suggestions}

	tool, err := NewCheckSuggestionsTool(sm)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	resp, err := tool.Call(nil)
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	if !strings.Contains(resp, "Movie 1") {
		t.Errorf("expected response to contain 'Movie 1', got %q", resp)
	}
}

func TestRemoveSuggestionTool_Call(t *testing.T) {
	sm := &mockSuggestionManager{}

	tool, err := NewRemoveSuggestionTool(sm)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	input := models.Input{"ID": "test-id"}
	resp, err := tool.Call(input)
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	if sm.removedID != "test-id" {
		t.Errorf("expected removed ID 'test-id', got %q", sm.removedID)
	}

	if !strings.Contains(resp, "successfully removed") {
		t.Errorf("expected success message, got %q", resp)
	}
}

func TestAddSuggestionTool_Call(t *testing.T) {
	item := model.Item{ID: "test-id", Name: "Test Media"}
	ig := &mockItemGetter{item: item}
	sm := &mockSuggestionManager{}

	tool, err := NewAddSuggestionTool(sm, ig)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	input := models.Input{
		"mediaID":    "test-id",
		"motivation": "high rating",
	}
	resp, err := tool.Call(input)
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	if sm.added.Item.ID != item.ID {
		t.Errorf("expected added item ID %q, got %q", item.ID, sm.added.Item.ID)
	}

	if sm.added.Motivation != "high rating" {
		t.Errorf("expected added motivation 'high rating', got %q", sm.added.Motivation)
	}

	if !strings.Contains(resp, "successfully added") {
		t.Errorf("expected success message, got %q", resp)
	}
}

package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestNewCheckSuggestionsTool_NilSuggestionManager(t *testing.T) {
	t.Parallel()

	if _, err := NewCheckSuggestionsTool(nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestCheckSuggestionsTool_Call_ListError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("db down")
	sm := &mockSuggestionManager{err: sentinel}

	tool, err := NewCheckSuggestionsTool(sm)
	if err != nil {
		t.Fatalf("NewCheckSuggestionsTool: %v", err)
	}

	_, callErr := tool.Call(models.Input{})
	if callErr == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(callErr.Error(), "failed to list suggestions") {
		t.Fatalf("expected wrapped error, got: %v", callErr)
	}
}

func TestCheckSuggestionsTool_Call_NoSuggestions(t *testing.T) {
	t.Parallel()

	sm := &mockSuggestionManager{suggestions: nil}
	tool, err := NewCheckSuggestionsTool(sm)
	if err != nil {
		t.Fatalf("NewCheckSuggestionsTool: %v", err)
	}

	msg, callErr := tool.Call(models.Input{})
	if callErr != nil {
		t.Fatalf("Call: %v", callErr)
	}

	want := "there are currently no active suggestions"
	if msg != want {
		t.Fatalf("message mismatch: got %q want %q", msg, want)
	}
}

func TestCheckSuggestionsTool_Call_WithSuggestions(t *testing.T) {
	t.Parallel()

	sm := &mockSuggestionManager{suggestions: []model.Suggestion{
		{Item: model.Item{ID: "1", Name: "Movie 1"}, Motivation: "Because I said so"},
		{Item: model.Item{ID: "2", Name: "Movie 2"}, Motivation: "New release"},
	}}
	tool, err := NewCheckSuggestionsTool(sm)
	if err != nil {
		t.Fatalf("NewCheckSuggestionsTool: %v", err)
	}

	resp, callErr := tool.Call(models.Input{})
	if callErr != nil {
		t.Fatalf("Call: %v", callErr)
	}

	if !strings.HasPrefix(resp, "active suggestions:\n") {
		t.Fatalf("expected prefix, got %q", resp)
	}
	for _, want := range []string{
		"- ID: 1, Name: Movie 1, Motivation: Because I said so\n",
		"- ID: 2, Name: Movie 2, Motivation: New release\n",
	} {
		if !strings.Contains(resp, want) {
			t.Fatalf("expected response to contain %q, got %q", want, resp)
		}
	}
}

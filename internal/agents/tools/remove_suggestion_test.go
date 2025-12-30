package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
)

func TestNewRemoveSuggestionTool_NilSuggestionManager(t *testing.T) {
	t.Parallel()

	if _, err := NewRemoveSuggestionTool(nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRemoveSuggestionTool_Call_InputValidation(t *testing.T) {
	t.Parallel()

	tool, err := NewRemoveSuggestionTool(&mockSuggestionManager{})
	if err != nil {
		t.Fatalf("NewRemoveSuggestionTool: %v", err)
	}

	_, callErr := tool.Call(models.Input{"ID": 123})
	if callErr == nil {
		t.Fatalf("expected error")
	}
	if got, want := callErr.Error(), "ID must be a string"; got != want {
		t.Fatalf("error mismatch: got %q want %q", got, want)
	}
}

func TestRemoveSuggestionTool_Call_RemoveError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("db down")
	sm := &mockSuggestionManager{err: sentinel}
	tool, err := NewRemoveSuggestionTool(sm)
	if err != nil {
		t.Fatalf("NewRemoveSuggestionTool: %v", err)
	}

	_, callErr := tool.Call(models.Input{"ID": "x"})
	if callErr == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(callErr.Error(), "failed to remove suggestion") {
		t.Fatalf("expected wrapped error, got: %v", callErr)
	}
}

func TestRemoveSuggestionTool_Call_Success(t *testing.T) {
	t.Parallel()

	sm := &mockSuggestionManager{}
	tool, err := NewRemoveSuggestionTool(sm)
	if err != nil {
		t.Fatalf("NewRemoveSuggestionTool: %v", err)
	}

	msg, callErr := tool.Call(models.Input{"ID": "id-1"})
	if callErr != nil {
		t.Fatalf("Call: %v", callErr)
	}
	if sm.removedID != "id-1" {
		t.Fatalf("Remove called with %q want %q", sm.removedID, "id-1")
	}
	if !strings.Contains(msg, "successfully removed suggestion with ID: id-1") {
		t.Fatalf("unexpected message: %q", msg)
	}
}

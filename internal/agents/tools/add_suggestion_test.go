package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

type fakeSuggestionManager struct {
	addCalls []model.Suggestion
	addErr   error
}

func (f *fakeSuggestionManager) List() ([]model.Suggestion, error) { return nil, nil }
func (f *fakeSuggestionManager) Remove(ID string) error            { return nil }
func (f *fakeSuggestionManager) Add(s model.Suggestion) error {
	f.addCalls = append(f.addCalls, s)
	return f.addErr
}

type fakeItemGetter struct {
	gotID  string
	item   model.Item
	getErr error
}

func (f *fakeItemGetter) GetItemByID(ID string) (model.Item, error) {
	f.gotID = ID
	if f.getErr != nil {
		return model.Item{}, f.getErr
	}
	return f.item, nil
}

func (f *fakeItemGetter) GetItemByName(Name string) (model.Item, error) {
	return model.Item{}, errors.New("not implemented")
}

func TestNewAddSuggestionTool_NilDeps(t *testing.T) {
	t.Parallel()

	ig := &fakeItemGetter{}
	sm := &fakeSuggestionManager{}

	if _, err := NewAddSuggestionTool(nil, ig); err == nil {
		t.Fatalf("expected error for nil suggestion manager")
	}
	if _, err := NewAddSuggestionTool(sm, nil); err == nil {
		t.Fatalf("expected error for nil item getter")
	}
}

func TestAddSuggestionTool_Call_InputValidation(t *testing.T) {
	t.Parallel()

	ast, err := NewAddSuggestionTool(&fakeSuggestionManager{}, &fakeItemGetter{})
	if err != nil {
		t.Fatalf("NewAddSuggestionTool: %v", err)
	}

	cases := []struct {
		name string
		in   models.Input
		want string
	}{
		{
			name: "mediaID not string",
			in:   models.Input{"mediaID": 123, "motivation": "x"},
			want: "mediaID must be a string",
		},
		{
			name: "motivation not string",
			in:   models.Input{"mediaID": "id", "motivation": 123},
			want: "motivation must be a string",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ast.Call(tc.in)
			if err == nil {
				t.Fatalf("expected error")
			}
			if got := err.Error(); got != tc.want {
				t.Fatalf("error mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestAddSuggestionTool_Call_GetItemError(t *testing.T) {
	t.Parallel()

	sm := &fakeSuggestionManager{}
	ig := &fakeItemGetter{getErr: errors.New("nope")}
	ast, err := NewAddSuggestionTool(sm, ig)
	if err != nil {
		t.Fatalf("NewAddSuggestionTool: %v", err)
	}

	_, callErr := ast.Call(models.Input{"mediaID": "id1", "motivation": "why"})
	if callErr == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(callErr.Error(), "failed to get item") {
		t.Fatalf("expected wrapped error, got: %v", callErr)
	}
	if ig.gotID != "id1" {
		t.Fatalf("GetItemByID called with %q, want %q", ig.gotID, "id1")
	}
	if len(sm.addCalls) != 0 {
		t.Fatalf("Add should not be called when GetItemByID fails")
	}
}

func TestAddSuggestionTool_Call_AddError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("db down")
	sm := &fakeSuggestionManager{addErr: sentinel}
	ig := &fakeItemGetter{item: model.Item{ID: "id1", Name: "The Movie"}}
	ast, err := NewAddSuggestionTool(sm, ig)
	if err != nil {
		t.Fatalf("NewAddSuggestionTool: %v", err)
	}

	_, callErr := ast.Call(models.Input{"mediaID": "id1", "motivation": "because"})
	if callErr == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(callErr.Error(), "failed to add suggestion") {
		t.Fatalf("expected wrapped error, got: %v", callErr)
	}
	if len(sm.addCalls) != 1 {
		t.Fatalf("expected Add to be called once, got %d", len(sm.addCalls))
	}
	if sm.addCalls[0].ID != "id1" || sm.addCalls[0].Name != "The Movie" {
		t.Fatalf("unexpected suggestion item: %+v", sm.addCalls[0].Item)
	}
	if sm.addCalls[0].Motivation != "because" {
		t.Fatalf("unexpected motivation: %q", sm.addCalls[0].Motivation)
	}
}

func TestAddSuggestionTool_Call_Success(t *testing.T) {
	t.Parallel()

	sm := &fakeSuggestionManager{}
	ig := &fakeItemGetter{item: model.Item{ID: "id9", Name: "Nice"}}
	ast, err := NewAddSuggestionTool(sm, ig)
	if err != nil {
		t.Fatalf("NewAddSuggestionTool: %v", err)
	}

	msg, callErr := ast.Call(models.Input{"mediaID": "id9", "motivation": "fits"})
	if callErr != nil {
		t.Fatalf("Call: %v", callErr)
	}
	wantMsg := "successfully added suggestion for item: 'Nice'"
	if msg != wantMsg {
		t.Fatalf("message mismatch: got %q want %q", msg, wantMsg)
	}
	if ig.gotID != "id9" {
		t.Fatalf("GetItemByID called with %q, want %q", ig.gotID, "id9")
	}
	if len(sm.addCalls) != 1 {
		t.Fatalf("expected Add to be called once, got %d", len(sm.addCalls))
	}
	if sm.addCalls[0].Motivation != "fits" {
		t.Fatalf("unexpected motivation: %q", sm.addCalls[0].Motivation)
	}
	if sm.addCalls[0].Name != "Nice" {
		t.Fatalf("unexpected item in suggestion: %+v", sm.addCalls[0].Item)
	}
}

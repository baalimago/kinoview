package tools

import (
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	kinomodel "github.com/baalimago/kinoview/internal/model"
)

func TestNewExtractSubtitleTool(t *testing.T) {
	t.Run("nil item getter", func(t *testing.T) {
		_, err := NewExtractSubtitleTool(nil, &mockSubtitleManager{})
		if err == nil {
			t.Fatal("expected error for nil item getter")
		}
	})

	t.Run("nil subtitle manager", func(t *testing.T) {
		_, err := NewExtractSubtitleTool(&mockItemGetter{}, nil)
		if err == nil {
			t.Fatal("expected error for nil subtitle manager")
		}
	})

	t.Run("valid construction", func(t *testing.T) {
		tool, err := NewExtractSubtitleTool(&mockItemGetter{}, &mockSubtitleManager{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tool == nil {
			t.Fatal("expected non-nil tool")
		}
	})
}

func TestExtractSubtitleCall(t *testing.T) {
	ig := &mockItemGetter{
		item: kinomodel.Item{ID: "item-1", Name: "Test Movie"},
	}

	t.Run("successful extraction", func(t *testing.T) {
		sm := &mockSubtitleManager{extractedPath: "/tmp/subs/item-1_2.vtt"}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		resp, err := tool.Call(models.Input{
			"ID":         "item-1",
			"subtitleID": "2",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(resp, "subtitles extracted for 'Test Movie' (subtitleID=2)") {
			t.Fatalf("unexpected response: %q", resp)
		}
		if !strings.Contains(resp, "/tmp/subs/item-1_2.vtt") {
			t.Fatalf("expected path in response, got: %q", resp)
		}
	})

	t.Run("idempotent extraction returns same path", func(t *testing.T) {
		sm := &mockSubtitleManager{extractedPath: "/tmp/subs/item-1_2.vtt"}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		input := models.Input{"ID": "item-1", "subtitleID": "2"}

		resp1, err1 := tool.Call(input)
		if err1 != nil {
			t.Fatalf("first call failed: %v", err1)
		}
		resp2, err2 := tool.Call(input)
		if err2 != nil {
			t.Fatalf("second call failed: %v", err2)
		}
		if resp1 != resp2 {
			t.Fatalf("idempotent calls differ:\n  first:  %q\n  second: %q", resp1, resp2)
		}
	})

	t.Run("external subtitle (negative index)", func(t *testing.T) {
		sm := &mockSubtitleManager{extractedPath: "/tmp/subs/item-1_-1.vtt"}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		resp, err := tool.Call(models.Input{
			"ID":         "item-1",
			"subtitleID": "-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(resp, "subtitles extracted for 'Test Movie' (subtitleID=-1)") {
			t.Fatalf("unexpected response: %q", resp)
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		sm := &mockSubtitleManager{}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		_, err := tool.Call(models.Input{"subtitleID": "0"})
		if err == nil {
			t.Fatal("expected error for missing ID")
		}
		_, err = tool.Call(models.Input{"ID": "", "subtitleID": "0"})
		if err == nil {
			t.Fatal("expected error for empty ID")
		}
	})

	t.Run("missing subtitleID", func(t *testing.T) {
		sm := &mockSubtitleManager{}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		_, err := tool.Call(models.Input{"ID": "item-1"})
		if err == nil {
			t.Fatal("expected error for missing subtitleID")
		}
		_, err = tool.Call(models.Input{"ID": "item-1", "subtitleID": ""})
		if err == nil {
			t.Fatal("expected error for empty subtitleID")
		}
	})

	t.Run("item not found", func(t *testing.T) {
		sm := &mockSubtitleManager{}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		_, err := tool.Call(models.Input{"ID": "nonexistent", "subtitleID": "0"})
		if err == nil {
			t.Fatal("expected error when item not found")
		}
	})

	t.Run("extraction failure", func(t *testing.T) {
		sm := &mockSubtitleManager{err: errSentinel}
		tool, _ := NewExtractSubtitleTool(ig, sm)
		_, err := tool.Call(models.Input{"ID": "item-1", "subtitleID": "99"})
		if err == nil {
			t.Fatal("expected error when extraction fails")
		}
	})
}

func TestExtractSubtitleSpecification(t *testing.T) {
	tool := &extractSubtitleTool{}
	spec := tool.Specification()
	if spec.Name != "extract_subtitle" {
		t.Fatalf("name: got %q, want 'extract_subtitle'", spec.Name)
	}
	if spec.Inputs == nil {
		t.Fatal("expected non-nil Inputs")
	}
	if len(spec.Inputs.Required) != 2 {
		t.Fatalf("expected 2 required inputs, got %d", len(spec.Inputs.Required))
	}
	hasID, hasSubID := false, false
	for _, r := range spec.Inputs.Required {
		if r == "ID" {
			hasID = true
		}
		if r == "subtitleID" {
			hasSubID = true
		}
	}
	if !hasID || !hasSubID {
		t.Fatalf("expected Required to contain 'ID' and 'subtitleID', got %v", spec.Inputs.Required)
	}
}

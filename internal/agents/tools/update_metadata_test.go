package tools

import (
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestUpdateMetadataTool_Call(t *testing.T) {
	item := model.Item{ID: "test-id", Name: "Test Media"}
	mm := &mockMetadataManager{}
	ig := &mockItemGetter{item: item}

	tool, err := NewUpdateMetadataTool(mm, ig)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	input := models.Input{
		"ID":       "test-id",
		"metadata": `{"name": "Updated Name"}`,
	}

	resp, err := tool.Call(input)
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	expectedResp := "successfully updated metadata for item: 'Test Media'"
	if resp != expectedResp {
		t.Errorf("expected response %q, got %q", expectedResp, resp)
	}

	if mm.updatedItem.ID != item.ID {
		t.Errorf("expected updated item ID %q, got %q", item.ID, mm.updatedItem.ID)
	}

	if mm.updatedMetadata != `{"name": "Updated Name"}` {
		t.Errorf("expected updated metadata %q, got %q", `{"name": "Updated Name"}`, mm.updatedMetadata)
	}
}

func TestUpdateMetadataTool_Call_NotFound(t *testing.T) {
	mm := &mockMetadataManager{}
	ig := &mockItemGetter{item: model.Item{ID: "other-id"}}

	tool, err := NewUpdateMetadataTool(mm, ig)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	input := models.Input{
		"ID":       "test-id",
		"metadata": `{"name": "Updated Name"}`,
	}

	_, err = tool.Call(input)
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
}

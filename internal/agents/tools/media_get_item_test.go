package tools

import (
	"encoding/json"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestMediaGetItemTool_SummaryMode(t *testing.T) {
	metadata := json.RawMessage(`{"title":"Test Movie","year":2024}`)
	item := model.Item{
		ID:       "test-id-123",
		Name:     "test_movie.mp4",
		Path:     "/media/test_movie.mp4",
		MIMEType: "video/mp4",
		Metadata: &metadata,
	}

	getter := &mockItemGetter{item: item}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	// Test with ID parameter
	respStr, err := tool.Call(models.Input{"ID": "test-id-123", "mode": "summary"})
	if err != nil {
		t.Fatalf("Call with ID: %v", err)
	}

	var resp struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Path        string `json:"path"`
		MIMEType    string `json:"mimeType"`
		HasMetadata bool   `json:"hasMetadata"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ID != "test-id-123" {
		t.Fatalf("expected ID=test-id-123, got %s", resp.ID)
	}
	if resp.Name != "test_movie.mp4" {
		t.Fatalf("expected Name=test_movie.mp4, got %s", resp.Name)
	}
	if resp.Path != "/media/test_movie.mp4" {
		t.Fatalf("expected Path=/media/test_movie.mp4, got %s", resp.Path)
	}
	if resp.MIMEType != "video/mp4" {
		t.Fatalf("expected MIMEType=video/mp4, got %s", resp.MIMEType)
	}
	if !resp.HasMetadata {
		t.Fatalf("expected HasMetadata=true, got false")
	}
}

func TestMediaGetItemTool_FullMode(t *testing.T) {
	metadata := json.RawMessage(`{"title":"Full Test","duration":7200}`)
	item := model.Item{
		ID:       "full-test-id",
		Name:     "full_test.mkv",
		Path:     "/videos/full_test.mkv",
		MIMEType: "video/x-matroska",
		Metadata: &metadata,
	}

	getter := &mockItemGetter{item: item}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	// Test with lowercase id parameter
	respStr, err := tool.Call(models.Input{"id": "full-test-id", "mode": "full"})
	if err != nil {
		t.Fatalf("Call with id: %v", err)
	}

	var resp model.Item
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ID != "full-test-id" {
		t.Fatalf("expected ID=full-test-id, got %s", resp.ID)
	}
	if resp.Metadata == nil {
		t.Fatalf("expected Metadata to be present")
	}
}

func TestMediaGetItemTool_NoMetadata(t *testing.T) {
	item := model.Item{
		ID:       "no-meta-id",
		Name:     "no_metadata.jpg",
		Path:     "/images/no_metadata.jpg",
		MIMEType: "image/jpeg",
		Metadata: nil,
	}

	getter := &mockItemGetter{item: item}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{"ID": "no-meta-id"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		HasMetadata bool `json:"hasMetadata"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.HasMetadata {
		t.Fatalf("expected HasMetadata=false, got true")
	}
}

func TestMediaGetItemTool_MissingID(t *testing.T) {
	getter := &mockItemGetter{}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	_, err = tool.Call(models.Input{})
	if err == nil {
		t.Fatalf("expected error for missing ID, got nil")
	}
}

func TestMediaGetItemTool_ItemNotFound(t *testing.T) {
	getter := &mockItemGetter{item: model.Item{ID: "existing-id"}}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	_, err = tool.Call(models.Input{"ID": "non-existent-id"})
	if err == nil {
		t.Fatalf("expected error for non-existent ID, got nil")
	}
}

func TestMediaGetItemTool_InvalidMode(t *testing.T) {
	item := model.Item{ID: "test-id", Name: "test.mp4"}
	getter := &mockItemGetter{item: item}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	_, err = tool.Call(models.Input{"ID": "test-id", "mode": "invalid"})
	if err == nil {
		t.Fatalf("expected error for invalid mode, got nil")
	}
}

func TestMediaGetItemTool_DefaultModeSummary(t *testing.T) {
	metadata := json.RawMessage(`{"test":"data"}`)
	item := model.Item{
		ID:       "default-mode-id",
		Name:     "default.mp4",
		Path:     "/default.mp4",
		MIMEType: "video/mp4",
		Metadata: &metadata,
	}

	getter := &mockItemGetter{item: item}
	tool, err := NewMediaGetItemTool(getter)
	if err != nil {
		t.Fatalf("NewMediaGetItemTool: %v", err)
	}

	// Call without mode parameter - should default to summary
	respStr, err := tool.Call(models.Input{"ID": "default-mode-id"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		HasMetadata bool   `json:"hasMetadata"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should be summary format (no Metadata field in response)
	if resp.ID == "" || resp.Name == "" {
		t.Fatalf("expected summary format response")
	}
}

func TestMediaStatsTool_EmptyLibrary(t *testing.T) {
	lister := &mockItemLister{items: []model.Item{}}
	tool, err := NewMediaStatsTool(lister)
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		Total           int            `json:"total"`
		ByMIMEPrefix    map[string]int `json:"byMimePrefix"`
		MissingMetadata int            `json:"missingMetadata"`
		WithMetadata    int            `json:"withMetadata"`
		Videos          int            `json:"videos"`
		Images          int            `json:"images"`
		Other           int            `json:"other"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 0 {
		t.Fatalf("expected total=0, got %d", resp.Total)
	}
	if resp.MissingMetadata != 0 {
		t.Fatalf("expected missingMetadata=0, got %d", resp.MissingMetadata)
	}
	if resp.WithMetadata != 0 {
		t.Fatalf("expected withMetadata=0, got %d", resp.WithMetadata)
	}
}

func TestMediaStatsTool_BasicCounts(t *testing.T) {
	metadata := json.RawMessage(`{"title":"test"}`)
	items := []model.Item{
		{ID: "1", Name: "video1.mp4", MIMEType: "video/mp4", Metadata: &metadata},
		{ID: "2", Name: "video2.mkv", MIMEType: "video/x-matroska", Metadata: &metadata},
		{ID: "3", Name: "image1.jpg", MIMEType: "image/jpeg", Metadata: nil},
		{ID: "4", Name: "image2.png", MIMEType: "image/png", Metadata: &metadata},
		{ID: "5", Name: "doc.pdf", MIMEType: "application/pdf", Metadata: nil},
	}

	lister := &mockItemLister{items: items}
	tool, err := NewMediaStatsTool(lister)
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		Total           int            `json:"total"`
		ByMIMEPrefix    map[string]int `json:"byMimePrefix"`
		MissingMetadata int            `json:"missingMetadata"`
		WithMetadata    int            `json:"withMetadata"`
		Videos          int            `json:"videos"`
		Images          int            `json:"images"`
		Other           int            `json:"other"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 5 {
		t.Fatalf("expected total=5, got %d", resp.Total)
	}
	if resp.Videos != 2 {
		t.Fatalf("expected videos=2, got %d", resp.Videos)
	}
	if resp.Images != 2 {
		t.Fatalf("expected images=2, got %d", resp.Images)
	}
	if resp.Other != 1 {
		t.Fatalf("expected other=1, got %d", resp.Other)
	}
	if resp.WithMetadata != 3 {
		t.Fatalf("expected withMetadata=3, got %d", resp.WithMetadata)
	}
	if resp.MissingMetadata != 2 {
		t.Fatalf("expected missingMetadata=2, got %d", resp.MissingMetadata)
	}
}

func TestMediaStatsTool_MIMEPrefixBreakdown(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "video1.mp4", MIMEType: "video/mp4"},
		{ID: "2", Name: "video2.mkv", MIMEType: "video/x-matroska"},
		{ID: "3", Name: "audio1.mp3", MIMEType: "audio/mpeg"},
		{ID: "4", Name: "image1.jpg", MIMEType: "image/jpeg"},
		{ID: "5", Name: "image2.png", MIMEType: "image/png"},
		{ID: "6", Name: "text.txt", MIMEType: "text/plain"},
	}

	lister := &mockItemLister{items: items}
	tool, err := NewMediaStatsTool(lister)
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		ByMIMEPrefix map[string]int `json:"byMimePrefix"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ByMIMEPrefix["video"] != 2 {
		t.Fatalf("expected video=2, got %d", resp.ByMIMEPrefix["video"])
	}
	if resp.ByMIMEPrefix["image"] != 2 {
		t.Fatalf("expected image=2, got %d", resp.ByMIMEPrefix["image"])
	}
	if resp.ByMIMEPrefix["audio"] != 1 {
		t.Fatalf("expected audio=1, got %d", resp.ByMIMEPrefix["audio"])
	}
	if resp.ByMIMEPrefix["text"] != 1 {
		t.Fatalf("expected text=1, got %d", resp.ByMIMEPrefix["text"])
	}
}

func TestMediaStatsTool_UnknownMIMEType(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "unknown", MIMEType: ""},
		{ID: "2", Name: "malformed", MIMEType: "not-a-valid-mime"},
	}

	lister := &mockItemLister{items: items}
	tool, err := NewMediaStatsTool(lister)
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		ByMIMEPrefix map[string]int `json:"byMimePrefix"`
		Other        int            `json:"other"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ByMIMEPrefix["unknown"] != 1 {
		t.Fatalf("expected unknown=1, got %d", resp.ByMIMEPrefix["unknown"])
	}
	if resp.ByMIMEPrefix["not-a-valid-mime"] != 1 {
		t.Fatalf("expected not-a-valid-mime=1, got %d", resp.ByMIMEPrefix["not-a-valid-mime"])
	}
	if resp.Other != 2 {
		t.Fatalf("expected other=2, got %d", resp.Other)
	}
}

func TestMediaStatsTool_AllWithMetadata(t *testing.T) {
	metadata := json.RawMessage(`{"title":"test"}`)
	items := []model.Item{
		{ID: "1", Name: "item1.mp4", MIMEType: "video/mp4", Metadata: &metadata},
		{ID: "2", Name: "item2.jpg", MIMEType: "image/jpeg", Metadata: &metadata},
		{ID: "3", Name: "item3.mp3", MIMEType: "audio/mpeg", Metadata: &metadata},
	}

	lister := &mockItemLister{items: items}
	tool, err := NewMediaStatsTool(lister)
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		Total           int `json:"total"`
		WithMetadata    int `json:"withMetadata"`
		MissingMetadata int `json:"missingMetadata"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 3 {
		t.Fatalf("expected total=3, got %d", resp.Total)
	}
	if resp.WithMetadata != 3 {
		t.Fatalf("expected withMetadata=3, got %d", resp.WithMetadata)
	}
	if resp.MissingMetadata != 0 {
		t.Fatalf("expected missingMetadata=0, got %d", resp.MissingMetadata)
	}
}

func TestMediaStatsTool_AllWithoutMetadata(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "item1.mp4", MIMEType: "video/mp4", Metadata: nil},
		{ID: "2", Name: "item2.jpg", MIMEType: "image/jpeg", Metadata: nil},
		{ID: "3", Name: "item3.mp3", MIMEType: "audio/mpeg", Metadata: nil},
	}

	lister := &mockItemLister{items: items}
	tool, err := NewMediaStatsTool(lister)
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp struct {
		Total           int `json:"total"`
		WithMetadata    int `json:"withMetadata"`
		MissingMetadata int `json:"missingMetadata"`
	}

	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 3 {
		t.Fatalf("expected total=3, got %d", resp.Total)
	}
	if resp.WithMetadata != 0 {
		t.Fatalf("expected withMetadata=0, got %d", resp.WithMetadata)
	}
	if resp.MissingMetadata != 3 {
		t.Fatalf("expected missingMetadata=3, got %d", resp.MissingMetadata)
	}
}

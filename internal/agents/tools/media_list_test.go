package tools

import (
	"encoding/json"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestMediaListTool_BasicPaginationAndFilters(t *testing.T) {
	l := &mockItemLister{items: []model.Item{
		{ID: "1", Name: "A Cat", Path: "/a/cat.jpg", MIMEType: "image/jpeg"},
		{ID: "2", Name: "B Dog", Path: "/b/dog.png", MIMEType: "image/png"},
		{ID: "3", Name: "C Movie", Path: "/c/movie.mp4", MIMEType: "video/mp4"},
		{ID: "4", Name: "D Movie", Path: "/d/movie.mkv", MIMEType: "video/x-matroska"},
	}}

	tool, err := NewMediaListTool(l)
	if err != nil {
		t.Fatalf("NewMediaListTool: %v", err)
	}

	// mimePrefix filter and pagination
	respStr, err := tool.Call(models.Input{"mimePrefix": "video", "limit": float64(1), "offset": float64(1)})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var resp struct {
		Total int `json:"total"`
		Items []struct {
			ID       string `json:"id"`
			MIMEType string `json:"mimeType"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected total=2, got %d", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].MIMEType == "" {
		t.Fatalf("expected mimeType")
	}

	// substring query matches name/path
	respStr, err = tool.Call(models.Input{"q": "cat"})
	if err != nil {
		t.Fatalf("Call q: %v", err)
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal q: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].ID != "1" {
		t.Fatalf("expected only item 1; got total=%d items=%v", resp.Total, resp.Items)
	}

	// mimeType exact match takes precedence over prefix.
	respStr, err = tool.Call(models.Input{"mimeType": "video/mp4", "mimePrefix": "image"})
	if err != nil {
		t.Fatalf("Call mimeType: %v", err)
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal mimeType: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "3" {
		t.Fatalf("expected only item 3; got total=%d items=%v", resp.Total, resp.Items)
	}
}

func TestMediaListTool_GlobalSearchWithMetadata(t *testing.T) {
	metadata1 := json.RawMessage(`{"title":"Inception","director":"Christopher Nolan","year":2010}`)
	metadata2 := json.RawMessage(`{"title":"The Matrix","director":"Wachowski","year":1999}`)
	metadata3 := json.RawMessage(`{"tags":["action","sci-fi"],"description":"A great action movie"}`)

	l := &mockItemLister{items: []model.Item{
		{ID: "1", Name: "inception.mp4", Path: "/movies/inception.mp4", MIMEType: "video/mp4", Metadata: &metadata1},
		{ID: "2", Name: "matrix.mp4", Path: "/movies/matrix.mp4", MIMEType: "video/mp4", Metadata: &metadata2},
		{ID: "3", Name: "action.mp4", Path: "/movies/action.mp4", MIMEType: "video/mp4", Metadata: &metadata3},
	}}

	tool, err := NewMediaListTool(l)
	if err != nil {
		t.Fatalf("NewMediaListTool: %v", err)
	}

	// Global search matches metadata title field
	respStr, err := tool.Call(models.Input{"q": "nolan"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var resp struct {
		Total int `json:"total"`
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "1" {
		t.Fatalf("expected only item 1 (Nolan); got total=%d items=%v", resp.Total, resp.Items)
	}

	// Global search matches metadata array values
	respStr, err = tool.Call(models.Input{"q": "sci-fi"})
	if err != nil {
		t.Fatalf("Call sci-fi: %v", err)
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal sci-fi: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "3" {
		t.Fatalf("expected only item 3 (sci-fi); got total=%d items=%v", resp.Total, resp.Items)
	}

	// Global search matches metadata description field
	respStr, err = tool.Call(models.Input{"q": "great action"})
	if err != nil {
		t.Fatalf("Call great action: %v", err)
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal great action: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "3" {
		t.Fatalf("expected only item 3 (great action); got total=%d items=%v", resp.Total, resp.Items)
	}

	// Global search is case-insensitive
	respStr, err = tool.Call(models.Input{"q": "WACHOWSKI"})
	if err != nil {
		t.Fatalf("Call WACHOWSKI: %v", err)
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal WACHOWSKI: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "2" {
		t.Fatalf("expected only item 2 (Wachowski); got total=%d items=%v", resp.Total, resp.Items)
	}

	// Global search matches name and path
	respStr, err = tool.Call(models.Input{"q": "inception"})
	if err != nil {
		t.Fatalf("Call inception: %v", err)
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatalf("unmarshal inception: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "1" {
		t.Fatalf("expected only item 1 (inception); got total=%d items=%v", resp.Total, resp.Items)
	}
}

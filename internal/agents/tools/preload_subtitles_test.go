package tools

import (
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestPreloadSubtitlesTool_Call(t *testing.T) {
	item := model.Item{ID: "test-id", Name: "Test Media"}
	ig := &mockItemGetter{item: item}
	sm := &mockSubtitleManager{extractedPath: "/tmp/subs.vtt"}
	ss := &mockSubtitleSelector{selectedIdx: 1}

	tool, err := NewPreloadSubtitlesTool(ig, sm, ss)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	input := models.Input{
		"ID": "test-id",
	}

	resp, err := tool.Call(input)
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	expectedResp := "successfully preloaded subtitles for item: 'Test Media' (subtitleID=1)"
	if resp != expectedResp {
		t.Errorf("expected response %q, got %q", expectedResp, resp)
	}
}

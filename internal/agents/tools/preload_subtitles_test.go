package tools

import (
	"context"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/media/subtitles"
	"github.com/baalimago/kinoview/internal/model"
)

func TestPreloadSubtitlesTool_Call(t *testing.T) {
	importer := &mockPreloadImporter{
		result: subtitles.ImportEmbeddedResult{
			Resource: model.SubtitleResource{
				ID:     "sub-123",
				ItemID: "test-id",
			},
			AlreadyExists: true,
			BecameDefault: false,
		},
	}

	tool, err := NewPreloadSubtitlesToolWithImporter(importer)
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

	expectedResp := "preloaded subtitle resource sub-123 for item test-id (already_existed=true default_set=false)"
	if resp != expectedResp {
		t.Errorf("expected response %q, got %q", expectedResp, resp)
	}
}

type mockPreloadImporter struct {
	result subtitles.ImportEmbeddedResult
	err    error
}

func (m *mockPreloadImporter) Import(ctx context.Context, req subtitles.ImportEmbeddedRequest) (subtitles.ImportEmbeddedResult, error) {
	return m.result, m.err
}

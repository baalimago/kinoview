package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/media/subtitles"
	"github.com/baalimago/kinoview/internal/model"
)

type stubEmbeddedImporter struct {
	importFunc func(ctx context.Context, req subtitles.ImportEmbeddedRequest) (subtitles.ImportEmbeddedResult, error)
}

func (s *stubEmbeddedImporter) Import(ctx context.Context, req subtitles.ImportEmbeddedRequest) (subtitles.ImportEmbeddedResult, error) {
	return s.importFunc(ctx, req)
}

func TestSubtitleImportHandlerFunc_ReturnsJSONShapeExpectedByFrontend(t *testing.T) {
	s := NewStore()
	s.subtitleImporter = &stubEmbeddedImporter{
		importFunc: func(ctx context.Context, req subtitles.ImportEmbeddedRequest) (subtitles.ImportEmbeddedResult, error) {
			return subtitles.ImportEmbeddedResult{
				Resource: model.SubtitleResource{
					ID:         "sub-1",
					ItemID:     req.ItemID,
					StorageKey: "item/sub-1.vtt",
					CreatedAt:  time.Now().UTC(),
					UpdatedAt:  time.Now().UTC(),
				},
				AlreadyExists: true,
				BecameDefault: true,
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/gallery/subtitles/item/item-1/import", nil)
	req.SetPathValue("item_id", "item-1")
	rec := httptest.NewRecorder()

	s.SubtitleImportHandlerFunc().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}

	var got struct {
		Resource      model.SubtitleResource `json:"resource"`
		AlreadyExists bool                   `json:"already_exists"`
		BecameDefault bool                   `json:"became_default"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response body: %v body=%q", err, rec.Body.String())
	}
	if got.Resource.ID != "sub-1" {
		t.Fatalf("expected resource id %q, got %q body=%q", "sub-1", got.Resource.ID, rec.Body.String())
	}
	if !got.AlreadyExists {
		t.Fatalf("expected already_exists true, got false body=%q", rec.Body.String())
	}
	if !got.BecameDefault {
		t.Fatalf("expected became_default true, got false body=%q", rec.Body.String())
	}
}
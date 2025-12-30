package tools

import (
	"encoding/json"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestNewMediaStatsTool_NilLister(t *testing.T) {
	t.Parallel()

	if _, err := NewMediaStatsTool(nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMediaStatsTool_Call_Empty(t *testing.T) {
	t.Parallel()

	tool, err := NewMediaStatsTool(&mockItemLister{items: nil})
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	got, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var st mediaStats
	if err := json.Unmarshal([]byte(got), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Total != 0 {
		t.Fatalf("Total: got %d want 0", st.Total)
	}
	if st.MissingMetadata != 0 || st.WithMetadata != 0 {
		t.Fatalf("metadata counts unexpected: missing=%d with=%d", st.MissingMetadata, st.WithMetadata)
	}
	if st.Videos != 0 || st.Images != 0 || st.Other != 0 {
		t.Fatalf("type counts unexpected: videos=%d images=%d other=%d", st.Videos, st.Images, st.Other)
	}
}

func TestMediaStatsTool_Call_Counts(t *testing.T) {
	t.Parallel()

	meta := json.RawMessage(`{"title":"x"}`)
	items := []model.Item{
		{ID: "1", Name: "V", MIMEType: "video/mp4", Metadata: (*json.RawMessage)(&meta)},
		{ID: "2", Name: "I", MIMEType: "image/jpeg", Metadata: nil},
		{ID: "3", Name: "U", MIMEType: "", Metadata: nil},
		{ID: "4", Name: "O", MIMEType: "application/pdf", Metadata: (*json.RawMessage)(&meta)},
	}

	tool, err := NewMediaStatsTool(&mockItemLister{items: items})
	if err != nil {
		t.Fatalf("NewMediaStatsTool: %v", err)
	}

	got, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var st mediaStats
	if err := json.Unmarshal([]byte(got), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if st.Total != 4 {
		t.Fatalf("Total: got %d want 4", st.Total)
	}
	if st.Videos != 1 || st.Images != 1 || st.Other != 2 {
		t.Fatalf("type counts unexpected: videos=%d images=%d other=%d", st.Videos, st.Images, st.Other)
	}
	if st.WithMetadata != 2 || st.MissingMetadata != 2 {
		t.Fatalf("metadata counts unexpected: with=%d missing=%d", st.WithMetadata, st.MissingMetadata)
	}
	if st.ByMIMEPrefix["video"] != 1 {
		t.Fatalf("ByMIMEPrefix[video]: got %d want 1", st.ByMIMEPrefix["video"])
	}
	if st.ByMIMEPrefix["image"] != 1 {
		t.Fatalf("ByMIMEPrefix[image]: got %d want 1", st.ByMIMEPrefix["image"])
	}
	if st.ByMIMEPrefix["unknown"] != 1 {
		t.Fatalf("ByMIMEPrefix[unknown]: got %d want 1", st.ByMIMEPrefix["unknown"])
	}
	if st.ByMIMEPrefix["application"] != 1 {
		t.Fatalf("ByMIMEPrefix[application]: got %d want 1", st.ByMIMEPrefix["application"])
	}
}

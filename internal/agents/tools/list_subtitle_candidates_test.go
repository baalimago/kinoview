package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	kinomodel "github.com/baalimago/kinoview/internal/model"
)

func TestNewListSubtitleCandidatesTool(t *testing.T) {
	t.Run("nil item getter", func(t *testing.T) {
		_, err := NewListSubtitleCandidatesTool(nil, &mockSubtitleManager{}, "/tmp")
		if err == nil {
			t.Fatal("expected error for nil item getter")
		}
	})

	t.Run("nil subtitle manager", func(t *testing.T) {
		_, err := NewListSubtitleCandidatesTool(&mockItemGetter{}, nil, "/tmp")
		if err == nil {
			t.Fatal("expected error for nil subtitle manager")
		}
	})

	t.Run("valid construction", func(t *testing.T) {
		tool, err := NewListSubtitleCandidatesTool(&mockItemGetter{}, &mockSubtitleManager{}, "/tmp/subs")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tool.subStorePath != "/tmp/subs" {
			t.Fatalf("subStorePath: got %q, want '/tmp/subs'", tool.subStorePath)
		}
	})
}

func TestListSubtitleCandidatesCall(t *testing.T) {
	ig := &mockItemGetter{
		item: kinomodel.Item{ID: "test-id", Name: "Test Movie"},
	}

	t.Run("no subtitle streams", func(t *testing.T) {
		sm := &mockSubtitleManager{
			mediaInfo: kinomodel.MediaInfo{
				Streams: []kinomodel.Stream{
					{Index: 0, CodecType: "video"},
					{Index: 1, CodecType: "audio"},
				},
			},
		}
		tool, _ := NewListSubtitleCandidatesTool(ig, sm, "/tmp")
		resp, err := tool.Call(models.Input{"ID": "test-id"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(resp, "no subtitle streams found") {
			t.Fatalf("expected 'no subtitle streams found', got: %q", resp)
		}
	})

	t.Run("embedded and external subtitle streams with extraction status and disposition", func(t *testing.T) {
		tmpDir := t.TempDir()
		subStorePath := filepath.Join(tmpDir, "subs")
		os.MkdirAll(subStorePath, 0o755)

		// Pre-create an extracted file for stream index 2 to simulate "already extracted"
		extractedFile := filepath.Join(subStorePath, "test-id_2.vtt")
		os.WriteFile(extractedFile, []byte("WEBVTT\n\n"), 0o644)

		sm := &mockSubtitleManager{
			mediaInfo: kinomodel.MediaInfo{
				Streams: []kinomodel.Stream{
					{Index: 0, CodecType: "video"},
					{Index: 1, CodecType: "audio"},
					{
						Index:     2,
						CodecType: "subtitle",
						CodecName: "subrip",
						Tags: kinomodel.Tags{
							Language: "eng",
							Title:    "English",
						},
						Disposition: kinomodel.Disposition{Default: 1},
					},
					{
						Index:     3,
						CodecType: "subtitle",
						CodecName: "subrip",
						Tags: kinomodel.Tags{
							Language: "swe",
							Title:    "Swedish",
						},
						Disposition: kinomodel.Disposition{Forced: 1, Comment: 1},
					},
					{
						Index:        -1,
						CodecType:    "subtitle",
						CodecName:    "subrip",
						ExternalPath: "/media/movie.en.srt",
						Tags: kinomodel.Tags{
							Language: "eng",
							Title:    "movie.en.srt",
						},
					},
				},
			},
		}

		tool, _ := NewListSubtitleCandidatesTool(ig, sm, subStorePath)
		resp, err := tool.Call(models.Input{"ID": "test-id"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Parse the JSON array from the response.
		jsonStart := strings.Index(resp, "[")
		if jsonStart == -1 {
			t.Fatalf("no JSON array found in response: %q", resp)
		}
		var candidates []struct {
			Index            int    `json:"index"`
			Codec            string `json:"codec"`
			Language         string `json:"language"`
			Title            string `json:"title"`
			Default          bool   `json:"default"`
			Forced           bool   `json:"forced"`
			Comment          bool   `json:"comment"`
			Source           string `json:"source"`
			AlreadyExtracted bool   `json:"alreadyExtracted"`
			ExtractedPath    string `json:"extractedPath,omitempty"`
		}
		if err := json.Unmarshal([]byte(resp[jsonStart:]), &candidates); err != nil {
			t.Fatalf("failed to unmarshal candidates JSON: %v", err)
		}

		if len(candidates) != 3 {
			t.Fatalf("expected 3 candidates, got %d", len(candidates))
		}

		// Candidate 0: embedded English, default, already extracted
		c0 := candidates[0]
		if c0.Index != 2 || c0.Language != "eng" || c0.Source != "embedded" {
			t.Errorf("candidate 0 fields: index=%d lang=%s source=%s", c0.Index, c0.Language, c0.Source)
		}
		if !c0.Default {
			t.Error("candidate 0: expected default=true")
		}
		if c0.Forced || c0.Comment {
			t.Error("candidate 0: expected forced=false, comment=false")
		}
		if !c0.AlreadyExtracted {
			t.Error("candidate 0: expected alreadyExtracted=true")
		}
		if c0.ExtractedPath == "" {
			t.Error("candidate 0: expected non-empty extractedPath when alreadyExtracted=true")
		}
		if !strings.HasSuffix(c0.ExtractedPath, "test-id_2.vtt") {
			t.Errorf("candidate 0: extractedPath should end with test-id_2.vtt, got %q", c0.ExtractedPath)
		}

		// Candidate 1: embedded Swedish, forced+comment, not extracted
		c1 := candidates[1]
		if c1.Index != 3 || c1.Language != "swe" || c1.Source != "embedded" {
			t.Errorf("candidate 1 fields: index=%d lang=%s source=%s", c1.Index, c1.Language, c1.Source)
		}
		if c1.Default {
			t.Error("candidate 1: expected default=false")
		}
		if !c1.Forced || !c1.Comment {
			t.Error("candidate 1: expected forced=true, comment=true")
		}
		if c1.AlreadyExtracted {
			t.Error("candidate 1: expected alreadyExtracted=false")
		}
		if c1.ExtractedPath != "" {
			t.Errorf("candidate 1: expected empty extractedPath, got %q", c1.ExtractedPath)
		}

		// Candidate 2: external English, not extracted
		c2 := candidates[2]
		if c2.Index != -1 || c2.Language != "eng" || c2.Source != "external" {
			t.Errorf("candidate 2 fields: index=%d lang=%s source=%s", c2.Index, c2.Language, c2.Source)
		}
		if c2.AlreadyExtracted {
			t.Error("candidate 2: expected alreadyExtracted=false")
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		sm := &mockSubtitleManager{}
		tool, _ := NewListSubtitleCandidatesTool(ig, sm, "/tmp")
		_, err := tool.Call(models.Input{})
		if err == nil {
			t.Fatal("expected error for missing ID")
		}
		_, err = tool.Call(models.Input{"ID": ""})
		if err == nil {
			t.Fatal("expected error for empty ID")
		}
	})

	t.Run("item not found", func(t *testing.T) {
		sm := &mockSubtitleManager{}
		tool, _ := NewListSubtitleCandidatesTool(ig, sm, "/tmp")
		_, err := tool.Call(models.Input{"ID": "nonexistent"})
		if err == nil {
			t.Fatal("expected error when item not found")
		}
	})

	t.Run("stream manager error", func(t *testing.T) {
		sm := &mockSubtitleManager{err: errSentinel}
		tool, _ := NewListSubtitleCandidatesTool(ig, sm, "/tmp")
		_, err := tool.Call(models.Input{"ID": "test-id"})
		if err == nil {
			t.Fatal("expected error when Find fails")
		}
	})

	t.Run("disposition fields default to false when not set", func(t *testing.T) {
		sm := &mockSubtitleManager{
			mediaInfo: kinomodel.MediaInfo{
				Streams: []kinomodel.Stream{
					{
						Index:     0,
						CodecType: "subtitle",
						CodecName: "subrip",
						Tags:      kinomodel.Tags{Language: "eng"},
					},
				},
			},
		}
		tool, _ := NewListSubtitleCandidatesTool(ig, sm, t.TempDir())
		resp, _ := tool.Call(models.Input{"ID": "test-id"})

		jsonStart := strings.Index(resp, "[")
		var candidates []struct {
			Default bool `json:"default"`
			Forced  bool `json:"forced"`
			Comment bool `json:"comment"`
		}
		json.Unmarshal([]byte(resp[jsonStart:]), &candidates)
		if len(candidates) != 1 {
			t.Fatal("expected 1 candidate")
		}
		c := candidates[0]
		if c.Default || c.Forced || c.Comment {
			t.Error("disposition fields should all be false when Disposition is zero-valued")
		}
	})
}

func TestListSubtitleCandidatesSpecification(t *testing.T) {
	tool := &listSubtitleCandidatesTool{}
	spec := tool.Specification()
	if spec.Name != "list_subtitle_candidates" {
		t.Fatalf("name: got %q, want 'list_subtitle_candidates'", spec.Name)
	}
	if spec.Inputs == nil {
		t.Fatal("expected non-nil Inputs")
	}
	if len(spec.Inputs.Required) != 1 || spec.Inputs.Required[0] != "ID" {
		t.Fatalf("expected Required to be ['ID'], got %v", spec.Inputs.Required)
	}
}

func TestListSubtitleCandidatesIsExtracted(t *testing.T) {
	tmp := t.TempDir()
	tool := &listSubtitleCandidatesTool{subStorePath: tmp}

	// Not extracted
	if tool.isExtracted("item1", 2) {
		t.Error("should not be extracted when file does not exist")
	}

	// Create file
	os.WriteFile(filepath.Join(tmp, "item1_2.vtt"), []byte("WEBVTT"), 0o644)

	// Now extracted
	if !tool.isExtracted("item1", 2) {
		t.Error("should be extracted after file created")
	}

	// Different stream index
	if tool.isExtracted("item1", 3) {
		t.Error("different stream index should not be extracted")
	}
}

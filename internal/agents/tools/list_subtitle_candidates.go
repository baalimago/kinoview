package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
)

type listSubtitleCandidatesTool struct {
	itemGetter   agents.ItemGetter
	subMgr       agents.StreamManager
	subStorePath string
}

func NewListSubtitleCandidatesTool(ig agents.ItemGetter, sm agents.StreamManager, subStorePath string) (*listSubtitleCandidatesTool, error) {
	if ig == nil {
		return nil, fmt.Errorf("item getter can't be nil")
	}
	if sm == nil {
		return nil, fmt.Errorf("subtitle manager can't be nil")
	}
	return &listSubtitleCandidatesTool{
		itemGetter:   ig,
		subMgr:       sm,
		subStorePath: subStorePath,
	}, nil
}

func (t *listSubtitleCandidatesTool) Call(input models.Input) (string, error) {
	id, ok := input["ID"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("ID must be a non-empty string")
	}

	item, err := t.itemGetter.GetItemByID(id)
	if err != nil {
		return "", fmt.Errorf("failed to get item: %w", err)
	}

	mediaInfo, err := t.subMgr.Find(item)
	if err != nil {
		return "", fmt.Errorf("failed to find subtitle info: %w", err)
	}

	type candidate struct {
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

	var candidates []candidate
	for _, s := range mediaInfo.Streams {
		if s.CodecType != "subtitle" {
			continue
		}
		lang := s.Tags.Language
		if lang == "" {
			lang = "und"
		}
		title := s.Tags.Title
		if title == "" {
			title = "(none)"
		}
		source := "embedded"
		if s.ExternalPath != "" {
			source = "external"
		}
		extracted := t.isExtracted(item.ID, s.Index)
		var extractedPath string
		if extracted {
			extractedPath = filepath.Join(t.subStorePath, fmt.Sprintf("%s_%d.vtt", item.ID, s.Index))
		}
		candidates = append(candidates, candidate{
			Index:            s.Index,
			Codec:            s.CodecName,
			Language:         lang,
			Title:            title,
			Default:          s.Disposition.Default == 1,
			Forced:           s.Disposition.Forced == 1,
			Comment:          s.Disposition.Comment == 1,
			Source:           source,
			AlreadyExtracted: extracted,
			ExtractedPath:    extractedPath,
		})
	}

	if len(candidates) == 0 {
		return fmt.Sprintf("no subtitle streams found for item '%s'", item.Name), nil
	}

	result, err := json.MarshalIndent(candidates, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal candidates: %w", err)
	}

	return fmt.Sprintf("subtitle candidates for '%s' (%d found):\n%s", item.Name, len(candidates), string(result)), nil
}

func (t *listSubtitleCandidatesTool) isExtracted(itemID string, streamIndex int) bool {
	path := filepath.Join(t.subStorePath, fmt.Sprintf("%s_%d.vtt", itemID, streamIndex))
	_, err := os.Stat(path)
	return err == nil
}

func (t *listSubtitleCandidatesTool) Specification() models.Specification {
	return models.Specification{
		Name:        "list_subtitle_candidates",
		Description: "List all subtitle streams (embedded and external) for a media item with metadata and extraction status. Use to discover available subtitles before selecting one to extract.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the media item to list subtitle candidates for",
				},
			},
			Required: []string{"ID"},
		},
	}
}

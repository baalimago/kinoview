package tools

import (
	"fmt"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
)

type extractSubtitleTool struct {
	itemGetter agents.ItemGetter
	subMgr     agents.StreamManager
}

func NewExtractSubtitleTool(ig agents.ItemGetter, sm agents.StreamManager) (*extractSubtitleTool, error) {
	if ig == nil {
		return nil, fmt.Errorf("item getter can't be nil")
	}
	if sm == nil {
		return nil, fmt.Errorf("subtitle manager can't be nil")
	}
	return &extractSubtitleTool{
		itemGetter: ig,
		subMgr:     sm,
	}, nil
}

func (t *extractSubtitleTool) Call(input models.Input) (string, error) {
	id, ok := input["ID"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("ID must be a non-empty string")
	}

	subtitleID, ok := input["subtitleID"].(string)
	if !ok || subtitleID == "" {
		return "", fmt.Errorf("subtitleID must be a non-empty string")
	}

	item, err := t.itemGetter.GetItemByID(id)
	if err != nil {
		return "", fmt.Errorf("failed to get item: %w", err)
	}

	path, err := t.subMgr.ExtractSubtitles(item, subtitleID)
	if err != nil {
		return "", fmt.Errorf("failed to extract subtitles: %w", err)
	}

	return fmt.Sprintf("subtitles extracted for '%s' (subtitleID=%s): %s", item.Name, subtitleID, path), nil
}

func (t *extractSubtitleTool) Specification() models.Specification {
	return models.Specification{
		Name:        "extract_subtitle",
		Description: "Extract a specific subtitle stream for a media item to a .vtt file. Use list_subtitle_candidates first to discover available streams and their IDs. Idempotent: calling twice returns the same path without re-extraction.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the media item to extract subtitles for",
				},
				"subtitleID": {
					Type:        "string",
					Description: "The subtitle stream index/ID to extract (from list_subtitle_candidates)",
				},
			},
			Required: []string{"ID", "subtitleID"},
		},
	}
}

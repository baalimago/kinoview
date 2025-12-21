package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type mediaGetItemTool struct {
	ig agents.ItemGetter
}

func NewMediaGetItemTool(ig agents.ItemGetter) (*mediaGetItemTool, error) {
	if ig == nil {
		return nil, errors.New("item getter can't be nil")
	}
	return &mediaGetItemTool{ig: ig}, nil
}

func (t *mediaGetItemTool) Call(input models.Input) (string, error) {
	id, _ := input["ID"].(string)
	if id == "" {
		id, _ = input["id"].(string)
	}
	if id == "" {
		return "", fmt.Errorf("ID must be a non-empty string")
	}

	mode, _ := input["mode"].(string)
	if mode == "" {
		mode = "summary"
	}

	it, err := t.ig.GetItemByID(id)
	if err != nil {
		return "", err
	}

	switch mode {
	case "summary":
		b, err := json.Marshal(itemSummaryFromItem(it))
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "full":
		b, err := json.Marshal(it)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("unknown mode; valid: summary, full")
	}
}

func (t *mediaGetItemTool) Specification() models.Specification {
	return models.Specification{
		Name:        "media_get_item",
		Description: "Get a single media library item by its ID.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the item.",
				},
				"id": {
					Type:        "string",
					Description: "Alias for ID.",
				},
				"mode": {
					Type:        "string",
					Description: "Return mode: 'summary' (default) or 'full'.",
				},
			},
			Required: []string{},
		},
	}
}

// itemSummary is intentionally small to keep tool outputs short.
// Add fields as needed.
type itemSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	MIMEType    string `json:"mimeType"`
	HasMetadata bool   `json:"hasMetadata"`
}

func itemSummaryFromItem(it model.Item) itemSummary {
	return itemSummary{
		ID:          it.ID,
		Name:        it.Name,
		Path:        it.Path,
		MIMEType:    it.MIMEType,
		HasMetadata: it.Metadata != nil,
	}
}

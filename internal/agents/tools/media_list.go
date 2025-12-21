package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type mediaListTool struct {
	lister agents.ItemLister
}

func NewMediaListTool(l agents.ItemLister) (*mediaListTool, error) {
	if l == nil {
		return nil, errors.New("item lister can't be nil")
	}
	return &mediaListTool{lister: l}, nil
}

type mediaListResponse struct {
	Total int              `json:"total"`
	Items []mediaListItem  `json:"items"`
}

type mediaListItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	MIMEType    string `json:"mimeType"`
	HasMetadata bool   `json:"hasMetadata"`
}

func (t *mediaListTool) Call(input models.Input) (string, error) {
	limit, offset, err := parseLimitOffset(input, 50)
	if err != nil {
		return "", err
	}

	mode, _ := input["mode"].(string)
	if mode == "" {
		mode = "summary"
	}

	q, _ := input["q"].(string)
	q = strings.TrimSpace(q)
	needle := strings.ToLower(q)

	mimePrefixFilter, _ := input["mimePrefix"].(string)
	mimePrefixFilter = strings.TrimSpace(strings.ToLower(mimePrefixFilter))
	mimeTypeFilter, _ := input["mimeType"].(string)
	mimeTypeFilter = strings.TrimSpace(strings.ToLower(mimeTypeFilter))

	hasMetadata := ""
	if v, ok := input["hasMetadata"].(bool); ok {
		if v {
			hasMetadata = "true"
		} else {
			hasMetadata = "false"
		}
	}

	items := t.lister.Snapshot()
	filtered := make([]model.Item, 0, len(items))
	for _, it := range items {
		if needle != "" {
			if !matchesGlobalSearch(it, needle) {
				continue
			}
		}

		if mimeTypeFilter != "" {
			if strings.ToLower(strings.TrimSpace(it.MIMEType)) != mimeTypeFilter {
				continue
			}
		} else if mimePrefixFilter != "" {
			if mimePrefix(it.MIMEType) != mimePrefixFilter {
				continue
			}
		}

		switch hasMetadata {
		case "true":
			if it.Metadata == nil {
				continue
			}
		case "false":
			if it.Metadata != nil {
				continue
			}
		}

		filtered = append(filtered, it)
	}

	stableSortItemsByNameID(filtered)
	total := len(filtered)

	if offset >= total {
		resp := mediaListResponse{Total: total, Items: []mediaListItem{}}
		b, _ := json.Marshal(resp)
		return string(b), nil
	}

	end := offset + limit
	if end > total {
		end = total
	}
	page := filtered[offset:end]

	out := make([]mediaListItem, 0, len(page))
	for _, it := range page {
		mi := mediaListItem{
			ID:          it.ID,
			Name:        it.Name,
			Path:        it.Path,
			MIMEType:    it.MIMEType,
			HasMetadata: it.Metadata != nil,
		}
		if mode == "details" {
			// Placeholder for potential future details; kept to maintain a stable interface.
			// (model.Item currently exposes id/name/path/mimeType/metadata only.)
		}
		out = append(out, mi)
	}

	resp := mediaListResponse{Total: total, Items: out}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (t *mediaListTool) Specification() models.Specification {
	return models.Specification{
		Name:        "media_list",
		Description: "List media library items with pagination, optional global search and mime filters.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"q": {
					Type:        "string",
					Description: "Optional global search query (case-insensitive). Searches across item name, path, and all metadata fields for substring matches.",
				},
				"mimeType": {
					Type:        "string",
					Description: "Optional exact mime type filter (e.g. 'video/mp4'). Takes precedence over mimePrefix.",
				},
				"mimePrefix": {
					Type:        "string",
					Description: "Optional mime prefix filter (e.g. 'video', 'image').",
				},
				"hasMetadata": {
					Type:        "boolean",
					Description: "Optional filter on metadata presence.",
				},
				"limit": {
					Type:        "integer",
					Description: "Max number of items to return (default 50).",
				},
				"offset": {
					Type:        "integer",
					Description: "Offset into sorted results (default 0).",
				},
				"mode": {
					Type:        "string",
					Description: "Return mode: 'summary' (default) or 'details'.",
				},
			},
			Required: []string{},
		},
	}
}

// sanity compile-time guard; unused in current implementation but kept for future richer filters.
var _ = fmt.Sprintf

// matchesGlobalSearch performs a global search across item metadata and basic fields.
// It searches through the item's name, path, and metadata fields for the given needle.
func matchesGlobalSearch(it model.Item, needle string) bool {
	// Search name and path
	if strings.Contains(strings.ToLower(it.Name), needle) ||
		strings.Contains(strings.ToLower(it.Path), needle) {
		return true
	}

	// Search through metadata if present
	if it.Metadata != nil {
		var metadata map[string]interface{}
		if err := json.Unmarshal(*it.Metadata, &metadata); err == nil {
			if searchMetadata(metadata, needle) {
				return true
			}
		}
	}

	return false
}

// searchMetadata recursively searches through metadata for a substring match.
func searchMetadata(data interface{}, needle string) bool {
	switch v := data.(type) {
	case map[string]interface{}:
		for _, val := range v {
			if searchMetadata(val, needle) {
				return true
			}
		}
	case []interface{}:
		for _, val := range v {
			if searchMetadata(val, needle) {
				return true
			}
		}
	case string:
		if strings.Contains(strings.ToLower(v), needle) {
			return true
		}
	}
	return false
}

package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type mediaStatsTool struct {
	lister agents.ItemLister
}

func NewMediaStatsTool(l agents.ItemLister) (*mediaStatsTool, error) {
	if l == nil {
		return nil, errors.New("item lister can't be nil")
	}
	return &mediaStatsTool{lister: l}, nil
}

type mediaStats struct {
	Total           int            `json:"total"`
	ByMIMEPrefix    map[string]int `json:"byMimePrefix"`
	MissingMetadata int            `json:"missingMetadata"`
	WithMetadata    int            `json:"withMetadata"`
	Videos          int            `json:"videos"`
	Images          int            `json:"images"`
	Other           int            `json:"other"`
}

func (t *mediaStatsTool) Call(input models.Input) (string, error) {
	_ = input
	items := t.lister.Snapshot()
	st := mediaStats{ByMIMEPrefix: map[string]int{}}
	st.Total = len(items)

	for _, it := range items {
		prefix := mimePrefix(it.MIMEType)
		if prefix == "" {
			prefix = "unknown"
		}
		st.ByMIMEPrefix[prefix]++

		switch prefix {
		case "video":
			st.Videos++
		case "image":
			st.Images++
		default:
			st.Other++
		}

		if it.Metadata == nil {
			st.MissingMetadata++
		} else {
			st.WithMetadata++
		}
	}

	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (t *mediaStatsTool) Specification() models.Specification {
	return models.Specification{
		Name:        "media_stats",
		Description: "Get a summary of the current media library (counts by type and metadata presence).",
		Inputs: &models.InputSchema{
			Type:       "object",
			Properties: map[string]models.ParameterObject{},
			Required:   []string{},
		},
	}
}

func mimePrefix(m string) string {
	m = strings.TrimSpace(strings.ToLower(m))
	if m == "" {
		return ""
	}
	if i := strings.IndexByte(m, '/'); i >= 0 {
		return m[:i]
	}
	return m
}

// helper reused by other tools
func stableSortItemsByNameID(items []model.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		n1 := strings.ToLower(items[i].Name)
		n2 := strings.ToLower(items[j].Name)
		if n1 != n2 {
			return n1 < n2
		}
		return items[i].ID < items[j].ID
	})
}

func parseLimitOffset(input models.Input, defaultLimit int) (limit, offset int, err error) {
	limit = defaultLimit
	if v, ok := input["limit"].(float64); ok {
		if int(v) > 0 {
			limit = int(v)
		}
	}
	offset = 0
	if v, ok := input["offset"].(float64); ok {
		if int(v) >= 0 {
			offset = int(v)
		}
	}
	if limit < 0 || offset < 0 {
		return 0, 0, fmt.Errorf("limit/offset must be non-negative")
	}
	return limit, offset, nil
}

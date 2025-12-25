package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
)

type preloadSubtitlesTool struct {
	itemGetter agents.ItemGetter
	subMgr     agents.SubtitleManager
	subSel     agents.SubtitleSelector
}

func NewPreloadSubtitlesTool(ig agents.ItemGetter, sm agents.SubtitleManager, ss agents.SubtitleSelector) (*preloadSubtitlesTool, error) {
	if ig == nil {
		return nil, errors.New("item getter can't be nil")
	}
	if sm == nil {
		return nil, errors.New("subtitle manager can't be nil")
	}
	if ss == nil {
		return nil, errors.New("subtitle selector can't be nil")
	}
	return &preloadSubtitlesTool{
		itemGetter: ig,
		subMgr:     sm,
		subSel:     ss,
	}, nil
}

func (pst *preloadSubtitlesTool) Call(input models.Input) (string, error) {
	ID, ok := input["ID"].(string)
	if !ok {
		return "", fmt.Errorf("ID must be a string")
	}

	item, err := pst.itemGetter.GetItemByID(ID)
	if err != nil {
		return "", fmt.Errorf("failed to get item: %w", err)
	}

	mediaInfo, err := pst.subMgr.Find(item)
	if err != nil {
		return "", fmt.Errorf("failed to find subtitle info: %w", err)
	}

	streamIdx, err := pst.subSel.Select(context.Background(), mediaInfo.Streams)
	if err != nil {
		return "", fmt.Errorf("failed to select subtitle stream: %w", err)
	}

	_, err = pst.subMgr.Extract(item, strconv.Itoa(streamIdx))
	if err != nil {
		return "", fmt.Errorf("failed to extract subtitles: %w", err)
	}

	return fmt.Sprintf("successfully preloaded subtitles for item: '%v'", item.Name), nil
}

func (pst *preloadSubtitlesTool) Specification() models.Specification {
	return models.Specification{
		Name:        "preload_subtitles",
		Description: "Find, extract and associate the best matching subtitles for a given media item.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the item to preload subtitles for",
				},
			},
			Required: []string{"ID"},
		},
	}
}

package tools

import (
	"errors"
	"fmt"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type addSuggestionTool struct {
	suggestionMgr agents.SuggestionManager
	itemGetter    agents.ItemGetter
}

func NewAddSuggestionTool(sm agents.SuggestionManager, ig agents.ItemGetter) (*addSuggestionTool, error) {
	if sm == nil {
		return nil, errors.New("suggestion manager can't be nil")
	}
	if ig == nil {
		return nil, errors.New("item getter can't be nil")
	}
	return &addSuggestionTool{
		suggestionMgr: sm,
		itemGetter:    ig,
	}, nil
}

func (ast *addSuggestionTool) Call(input models.Input) (string, error) {
	mediaID, ok := input["mediaID"].(string)
	if !ok {
		return "", fmt.Errorf("mediaID must be a string")
	}

	motivation, ok := input["motivation"].(string)
	if !ok {
		return "", fmt.Errorf("motivation must be a string")
	}

	item, err := ast.itemGetter.GetItemByID(mediaID)
	if err != nil {
		return "", fmt.Errorf("failed to get item: %w", err)
	}

	suggestion := model.Suggestion{
		Item:       item,
		Motivation: motivation,
	}

	err = ast.suggestionMgr.Add(suggestion)
	if err != nil {
		return "", fmt.Errorf("failed to add suggestion: %w", err)
	}

	return fmt.Sprintf("successfully added suggestion for item: '%v'", item.Name), nil
}

func (ast *addSuggestionTool) Specification() models.Specification {
	return models.Specification{
		Name:        "add_suggestion",
		Description: "Add a new media suggestion for the user.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"mediaID": {
					Type:        "string",
					Description: "The ID of the media item to suggest",
				},
				"motivation": {
					Type:        "string",
					Description: "Briefly explain why this item is suggested",
				},
			},
			Required: []string{"mediaID", "motivation"},
		},
	}
}

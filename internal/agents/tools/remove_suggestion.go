package tools

import (
	"errors"
	"fmt"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
)

type removeSuggestionTool struct {
	suggestionMgr agents.SuggestionManager
}

func NewRemoveSuggestionTool(sm agents.SuggestionManager) (*removeSuggestionTool, error) {
	if sm == nil {
		return nil, errors.New("suggestion manager can't be nil")
	}
	return &removeSuggestionTool{
		suggestionMgr: sm,
	}, nil
}

func (rst *removeSuggestionTool) Call(input models.Input) (string, error) {
	ID, ok := input["ID"].(string)
	if !ok {
		return "", fmt.Errorf("ID must be a string")
	}

	err := rst.suggestionMgr.Remove(ID)
	if err != nil {
		return "", fmt.Errorf("failed to remove suggestion: %w", err)
	}

	return fmt.Sprintf("successfully removed suggestion with ID: %s", ID), nil
}

func (rst *removeSuggestionTool) Specification() models.Specification {
	return models.Specification{
		Name:        "remove_suggestion",
		Description: "Remove a media suggestion by its ID.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "The ID of the suggestion to remove",
				},
			},
			Required: []string{"ID"},
		},
	}
}

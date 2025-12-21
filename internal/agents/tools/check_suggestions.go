package tools

import (
	"errors"
	"fmt"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
)

type checkSuggestionsTool struct {
	suggestionMgr agents.SuggestionManager
}

func NewCheckSuggestionsTool(sm agents.SuggestionManager) (*checkSuggestionsTool, error) {
	if sm == nil {
		return nil, errors.New("suggestion manager can't be nil")
	}
	return &checkSuggestionsTool{
		suggestionMgr: sm,
	}, nil
}

func (cst *checkSuggestionsTool) Call(input models.Input) (string, error) {
	suggestions, err := cst.suggestionMgr.List()
	if err != nil {
		return "", fmt.Errorf("failed to list suggestions: %w", err)
	}

	if len(suggestions) == 0 {
		return "there are currently no active suggestions", nil
	}

	res := "active suggestions:\n"
	for _, s := range suggestions {
		res += fmt.Sprintf("- ID: %s, Name: %s, Motivation: %s\n", s.ID, s.Name, s.Motivation)
	}
	return res, nil
}

func (cst *checkSuggestionsTool) Specification() models.Specification {
	return models.Specification{
		Name:        "check_suggestions",
		Description: "List all currently active media suggestions for the user.",
		Inputs: &models.InputSchema{
			Type:       "object",
			Required:   make([]string, 0),
			Properties: map[string]models.ParameterObject{},
		},
	}
}

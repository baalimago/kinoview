package concierge

import (
	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
)

type ConciergeOption func(*concierge)

type concierge struct {
	tools []models.LLMTool
}

// New Concierge, hosting tools:
// 1. UpdateMetadata
// 2. PreloadSubtitles
// 3. CheckSuggestions
// 4. RemoveSuggestion
// 5. AddSuggestion
// 6. CheckNewMediaRecommendation
// 7. AddMediaRecommendation
// 8. RemoveMediaRecommendation
func New(opts ...ConciergeOption) agents.Concierge {
	a := agent.New()
	return &a
}

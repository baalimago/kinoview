package butler

import (
	"context"
	"fmt"

	"github.com/baalimago/clai/pkg/text"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type selector struct {
	llm text.FullResponse
}

const selectorSystemPrompt = `You are a media stream analyzer. Your task is to select the most appropriate English subtitle stream from a list of streams.

Priorities:
1. Standard English subtitles (often tagged "eng", "en", "English").
2. English SDH (Subtitles for the Deaf and Hard-of-hearing) if no standard English is available.
3. Forced English subtitles (only if strictly necessary or no others exist, though these are usually for foreign parts).

Avoid:
- Commentary tracks.
- Non-English languages.

Input: A list of streams with their metadata.
Output: A JSON object containing the index of the best match. 
If no suitable English subtitle is found, return "error" in the JSON.

Format:
{
  "index": 3
}
OR
{
  "error": "no english subtitles found"
}
`

func NewSelector(c models.Configurations) agents.SubtitleSelector {
	c.SystemPrompt = selectorSystemPrompt
	return &selector{
		llm: text.NewFullResponseQuerier(c),
	}
}

type selectorResponse struct {
	Index *int    `json:"index,omitempty"`
	Error *string `json:"error,omitempty"`
}

func (s *selector) Select(ctx context.Context, streams []model.Stream) (int, error) {
	// Filter for subtitle streams only
	subs := filterSubtitleStreams(streams)

	if len(subs) == 0 {
		return -1, fmt.Errorf("no subtitle streams found")
	}

	// Deterministic fast path: if we have at least one usable candidate, pick the best
	if idx, ok := rankBest(subs); ok {
		return idx, nil
	}

	// Fallback to LLM for ambiguous cases (non-English only, commentary only, etc.)
	return s.selectViaLLM(ctx, subs)
}

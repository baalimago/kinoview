package butler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/baalimago/clai/pkg/text"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type selector struct {
	llm text.FullResponse
}

const selectorSystemPrompt = `You are a media stream analyzer. Your task is to select the most appropriate English subtitle stream from a list of streams.

Priorities:
1. Standard subtitles (often tagged "eng", "en", "English").
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
	// Filter for subtitle streams only before sending to LLM to reduce noise
	var subStreams []model.Stream
	for _, st := range streams {
		if st.CodecType == "subtitle" {
			subStreams = append(subStreams, st)
		}
	}

	if len(subStreams) == 0 {
		return -1, fmt.Errorf("no subtitle streams found")
	}

	// Format prompt
	var sb strings.Builder
	for _, st := range subStreams {
		title := st.Tags.Title
		lang := st.Tags.Language
		if lang == "" {
			lang = "unknown"
		}
		sb.WriteString(fmt.Sprintf("- Index: %d, Language: %s, Title: %s, Codec: %s, Default: %d, Forced: %d\n",
			st.Index, lang, title, st.CodecName, st.Disposition.Default, st.Disposition.Forced))
	}

	chat := models.Chat{
		Messages: []models.Message{
			{
				Role:    "system",
				Content: selectorSystemPrompt,
			},
			{
				Role:    "user",
				Content: sb.String(),
			},
		},
	}

	if misc.Truthy(os.Getenv("DEBUG")) {
		ancli.Noticef("Selector prompt:\n%v", debug.IndentedJsonFmt(chat))
	}

	resp, err := s.llm.Query(ctx, chat)
	if err != nil {
		return -1, fmt.Errorf("selector llm query failed: %w", err)
	}

	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		// Fallback check
		if len(resp.Messages) > 0 {
			lastMsg = resp.Messages[len(resp.Messages)-1]
		} else {
			return -1, fmt.Errorf("empty response from selector llm")
		}
	}

	// Parse JSON
	content := lastMsg.Content
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end < start {
		return -1, fmt.Errorf("invalid json response from selector")
	}
	jsonStr := content[start : end+1]

	var res selectorResponse
	if err := json.Unmarshal([]byte(jsonStr), &res); err != nil {
		return -1, fmt.Errorf("failed to parse selector response: %w", err)
	}

	if res.Error != nil {
		return -1, fmt.Errorf("%s", *res.Error)
	}

	if res.Index == nil {
		return -1, fmt.Errorf("no index returned")
	}

	return *res.Index, nil
}

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

type Subtitler interface {
	Find(item model.Item) (model.MediaInfo, error)
	Extract(item model.Item, streamIndex string) (string, error)
}

type butler struct {
	llm      text.FullResponse
	subs     Subtitler
	selector SubtitleSelector
}

const systemPrompt = `You are a Butler. Your goal is to anticipate what the user wants to watch next.
You will be given the user's context (viewing history, time of day etc) and a list of available media.
Analyze the patterns and suggest suitable items from the library.

Do not suggest items that are clearly not in the provided media list.
Be concise in your motivation.

Hints, in order of importance:
	1. Users prefer to watch series sequentially. If previous episode was 3, the next should be 4, of the same season.
	2. If a user has stopped a movie or series mid-way, there's a high chance the user wish to continue
	3. Have a variety of options, sometimes suggest new series
	4. Anticipate weekly trends. Example: user stops watching Thursday night, then a Friday movie would be likely a good candidate.

Respond ONLY with a JSON array in the following format:
[
  {
    "index": <INDEX_IN_LIST> (int),
    "motivation": "<Short motivation>" (string)
  }
]
`

// NewButler configured by models.Configurations and a Subtitler
func NewButler(c models.Configurations, subs Subtitler) agents.Butler {
	c.SystemPrompt = systemPrompt
	return &butler{
		llm:      text.NewFullResponseQuerier(c),
		subs:     subs,
		selector: NewSelector(c),
	}
}

func (b *butler) Setup(ctx context.Context) error {
	err := b.llm.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup querier: %w", err)
	}
	return nil
}

type suggestionResponse struct {
	IndexInList int    `json:"index"`
	Motivation  string `json:"motivation"`
}

// PrepSuggestions implementation
func (b *butler) PrepSuggestions(ctx context.Context, clientCtx model.ClientContext, items []model.Item) ([]model.Recommendation, error) {
	itemsStr := formatItems(items)
	contextStr := formatContext(clientCtx)

	userMessage := fmt.Sprintf("Context:\n%s\n\nAvailable Media:\n%s", contextStr, itemsStr)

	chat := models.Chat{
		Messages: []models.Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: userMessage,
			},
		},
	}

	if misc.Truthy(os.Getenv("DEBUG")) {
		ancli.Noticef("Butler prompt:\n%v", debug.IndentedJsonFmt(chat))
	}

	resp, err := b.llm.Query(ctx, chat)
	if err != nil {
		return nil, fmt.Errorf("failed to query llm: %w", err)
	}

	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		// Fallback to checking any new message
		if len(resp.Messages) > 0 {
			lastMsg = resp.Messages[len(resp.Messages)-1]
		} else {
			return nil, fmt.Errorf("received empty response from llm")
		}
	}

	suggestions, err := parseSuggestions(lastMsg.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse suggestions: %w", err)
	}

	var recommendations []model.Recommendation

	for _, sug := range suggestions {
		rec, err := b.prepSuggestion(ctx, sug, items)
		if err != nil {
			ancli.Warnf("failed to prepare suggestion: %v", err)
			continue
		}
		recommendations = append(recommendations, rec)
	}

	return recommendations, nil
}

func (b *butler) prepSuggestion(ctx context.Context, sug suggestionResponse, items []model.Item) (model.Recommendation, error) {
	if sug.IndexInList < 0 || sug.IndexInList > len(items) {
		return model.Recommendation{}, fmt.Errorf("llm suggested index which isn't in list: '%v' ", sug.IndexInList)
	}
	item := items[sug.IndexInList]
	rec := model.Recommendation{
		Item:       item,
		Motivation: sug.Motivation,
	}
	if b.subs == nil {
		return rec, nil
	}
	err := b.preloadSubs(ctx, item, &rec)
	if err != nil {
		return model.Recommendation{}, fmt.Errorf("failed to preloadSubs: %w", err)
	}
	return rec, nil
}

func (b *butler) preloadSubs(ctx context.Context, item model.Item, rec *model.Recommendation) error {
	// Preload subtitles
	info, err := b.subs.Find(item)
	if err != nil {
		return fmt.Errorf("failed to find subtitles for %s: %w", item.Name, err)
	}
	var selectedIdx string

	// Use selector if available
	if b.selector != nil {
		idx, err := b.selector.SelectEnglish(ctx, info.Streams)
		if err != nil {
			return fmt.Errorf("failed to select english subtitle for '%s': %w", item.Name, err)
		}
		selectedIdx = fmt.Sprintf("%d", idx)
	}

	_, err = b.subs.Extract(item, selectedIdx)
	if err != nil {
		ancli.Warnf("failed to extract subtitles for %s: %v", item.Name, err)
	}
	rec.SubtitleID = selectedIdx
	return nil
}

func formatItems(items []model.Item) string {
	var result []map[string]interface{}
	for idx, it := range items {
		item := map[string]interface{}{
			"index": idx,
			"name":  it.Name,
			"type":  it.MIMEType,
		}
		if it.Metadata != nil {
			var metadata map[string]interface{}
			err := json.Unmarshal(*it.Metadata, &metadata)
			if err == nil {
				item["metadata"] = metadata
			}
		}
		result = append(result, item)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b)
}

func formatContext(c model.ClientContext) string {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting context: %v", err)
	}
	return string(b)
}

func parseSuggestions(content string) ([]suggestionResponse, error) {
	// Attempt to find JSON array in the content (it might be wrapped in markdown code blocks)
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")

	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("no JSON array found in response")
	}

	jsonStr := content[start : end+1]
	var suggestions []suggestionResponse
	err := json.Unmarshal([]byte(jsonStr), &suggestions)
	if err != nil {
		return nil, err
	}
	return suggestions, nil
}

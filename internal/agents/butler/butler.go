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
    "mediaId": "<ID_FROM_MEDIA>",
    "motivation": "<Short motivation>"
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
	MediaID    string `json:"mediaId"`
	Motivation string `json:"motivation"`
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
	itemMap := make(map[string]model.Item)
	for _, it := range items {
		itemMap[it.ID] = it
	}

	for _, sug := range suggestions {
		if item, exists := itemMap[sug.MediaID]; exists {
			rec := model.Recommendation{
				Item:       item,
				Motivation: sug.Motivation,
			}
			if b.subs != nil {
				// Preload subtitles
				info, err := b.subs.Find(item)
				if err != nil {
					ancli.Warnf("failed to find subtitles for %s: %v", item.Name, err)
				} else {
					var selectedIdx string

					// Use selector if available
					if b.selector != nil {
						idx, err := b.selector.SelectEnglish(ctx, info.Streams)
						if err == nil {
							selectedIdx = fmt.Sprintf("%d", idx)
						} else {
							ancli.Warnf("failed to select english subtitle for %s: %v", item.Name, err)
						}
					}

					// Fallback: pick the first subtitle stream if selector failed or not present
					if selectedIdx == "" {
						for _, stream := range info.Streams {
							if stream.CodecType == "subtitle" {
								selectedIdx = fmt.Sprintf("%d", stream.Index)
								break
							}
						}
					}

					if selectedIdx != "" {
						_, err := b.subs.Extract(item, selectedIdx)
						if err != nil {
							ancli.Warnf("failed to extract subtitles for %s: %v", item.Name, err)
						} else {
							rec.SubtitleID = selectedIdx
						}
					}
				}
			}
			recommendations = append(recommendations, rec)
		} else {
			ancli.Warnf("Butler suggested unknown item ID: %s", sug.MediaID)
		}
	}

	return recommendations, nil
}

func formatItems(items []model.Item) string {
	var sb strings.Builder
	for _, it := range items {
		metadataJSONStr := ""
		if it.Metadata != nil {
			metadataJSONStr = string(*it.Metadata)
		}
		sb.WriteString(fmt.Sprintf("- id: %s, name: %s, type: %s, metadata: %s\n", it.ID, it.Name, it.MIMEType, metadataJSONStr))
	}
	return sb.String()
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

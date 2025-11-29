package butler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

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

const pickerSystemPrompt = `You are a media Butler. Your goal is to anticipate what the user wants to watch next.
You will be given the user's context (viewing history, time of day etc) and a list of available media.
Analyze the patterns and suggest suitable items from the library.

Do not suggest items that are clearly not in the provided media list.
Be concise.
Add a posh style to your replies as it will be user facing.

Hints, in order of importance:
	1. Users prefer to watch series sequentially. If previous episode was 3, the next should be 4, of the same season.
	2. If a user has stopped a movie or series mid-way, there's a high chance the user wish to continue
	3. Have a variety of options, sometimes suggest new media
	4. Anticipate weekly trends. Example: user stops watching Thursday night, then a Friday movie would be likely a good candidate.

Respond ONLY with a JSON array in the following format:
[
  {
    "description": "<Descripton of item>" (string),
    "motivation": "<Short motivation>" (string)
  }
]

The "description" field should be a semantic index most likely to identify the media. Be VERY clear on your choice. Examples:
	* "Big Buck Bunny S01E04"
	* "Season 3 Episode 10 Big Buck Bunny"
	* "Big Buck Bunny"
`

type suggestionResponse struct {
	Description string `json:"description"`
	Motivation  string `json:"motivation"`
}

// NewButler configured by models.Configurations and a Subtitler
func NewButler(c models.Configurations, subs Subtitler) agents.Butler {
	c.SystemPrompt = pickerSystemPrompt
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

// PrepSuggestions implementation
func (b *butler) PrepSuggestions(ctx context.Context, clientCtx model.ClientContext, items []model.Item) ([]model.Recommendation, error) {
	itemsStr := formatItems(items)
	contextStr := formatContext(clientCtx)

	userMessage := fmt.Sprintf("Context:\n%s\n\nAvailable Media:\n%s", contextStr, itemsStr)

	chat := models.Chat{
		Messages: []models.Message{
			{
				Role:    "system",
				Content: pickerSystemPrompt,
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
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, sug := range suggestions {
		wg.Add(1)
		go func(suggestion suggestionResponse) {
			defer wg.Done()
			rec, err := b.prepSuggestion(ctx, suggestion,
				items)
			if err != nil {
				var psErr *PreloadSubsError
				if errors.As(err, &psErr) {
					ancli.Warnf("preload subs error, keeping recs: %v", err)
				} else {
					ancli.Warnf(
						"failed to prepare suggestion: %v", err)
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					return
				}

			}
			mu.Lock()
			recommendations = append(recommendations, rec)
			mu.Unlock()
		}(sug)
	}

	wg.Wait()
	if len(errs) > 0 {
		ancli.Errf("got errors trying to prep suggestions: %v", errs)
	}

	return recommendations, nil
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

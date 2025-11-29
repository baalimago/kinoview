package butler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

const semanticIndexerSysPrompt = `Your job is to pick a media item from a list.
You will given a semantic description of some item, and a list of items. Your need to select the index most likely to be the target.

Examples of media list:
[
    {
    "index": 0,
    "fileName": "Home Movie Vacation S02E06.mp4",
    "name": "Home Movie Vacation",
    "alt_name": "Home Movie Vacation Season 2 Episode 6",
    "year": 2025,
    "season": 2,
    "episode": 6
  },
    {
    "index": 1,
    "fileName": "Mountain_Trek_Chronicles_S01E03.mkv",
    "name": "Mountain Trek Chronicles",
    "alt_name": "Mountain Trek Chronicles Season 1 Episode 3",
    "year": 2024,
    "season": 1,
    "episode": 3
  },
    {
    "index": 2,
    "fileName": "Desert_Mysteries_S03E11.mp4",
    "name": "Desert Mysteries",
    "alt_name": "Desert Mysteries Season 3 Episode 11",
    "year": 2023,
    "season": 3,
    "episode": 11
  },
]

Given input:
	* "Home Movie Vacation S02E06"
	* "Season 2 Episode 6 Home Movie Vacation"
	* "Home Movie Vacation s2 e6"

You should return index 0.

Respond ONLY with a JSON in th efollowing format:
{
  "index": <index> (int)
}
`

type semanticIndexerSelectFormat struct {
	Index    int    `json:"index"`
	FileName string `json:"fileName"`
	Name     string `json:"name"`
	AltName  string `json:"alt_name"`
	Year     int    `json:"year"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
}

type semanticIdexerResponse struct {
	Index int `json:"index"`
}

// semanticIndexerSelect by:
// 1. Building clai chat using semanticIndexerSysPrompt as system prompt
// 2. Format items into semanticIndexerSelectFormat
// 3. Pass the formated items into clai
// 4. Parse output from LLM into
func (b *butler) semanticIndexerSelect(ctx context.Context, sug suggestionResponse, items []model.Item) (model.Item, error) {
	// Format items into semanticIndexerSelectFormat
	var formattedItems []semanticIndexerSelectFormat
	for idx, item := range items {
		formatted := semanticIndexerSelectFormat{
			Index:    idx,
			FileName: item.Name,
		}
		if item.Metadata != nil {
			var metadata map[string]interface{}
			err := json.Unmarshal(*item.Metadata, &metadata)
			if err == nil {
				if v, ok := metadata["alt_name"]; ok {
					formatted.AltName = v.(string)
				}
				if v, ok := metadata["year"]; ok {
					formatted.Year = int(v.(float64))
				}
				if v, ok := metadata["season"]; ok {
					formatted.Season = int(v.(float64))
				}
				if v, ok := metadata["episode"]; ok {
					formatted.Episode = int(v.(float64))
				}
				if v, ok := metadata["name"]; ok {
					formatted.Name = string(v.(string))
				}
			}
		}
		formattedItems = append(formattedItems, formatted)
	}

	itemsJSON, _ := json.MarshalIndent(formattedItems, "", "  ")

	userMessage := fmt.Sprintf("Semantic description: %s\n\nMedia list:\n%s", sug.Description, string(itemsJSON))

	chat := models.Chat{
		Messages: []models.Message{
			{
				Role:    "system",
				Content: semanticIndexerSysPrompt,
			},
			{
				Role:    "user",
				Content: userMessage,
			},
		},
	}

	resp, err := b.llm.Query(ctx, chat)
	if err != nil {
		return model.Item{}, fmt.Errorf("failed to query llm: %w", err)
	}

	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		if len(resp.Messages) > 0 {
			lastMsg = resp.Messages[len(resp.Messages)-1]
		} else {
			return model.Item{}, fmt.Errorf("received empty response from llm")
		}
	}

	var result semanticIdexerResponse
	start := strings.Index(lastMsg.Content, "{")
	end := strings.LastIndex(lastMsg.Content, "}")

	if start == -1 || end == -1 || end < start {
		return model.Item{}, fmt.Errorf("no JSON found in response")
	}

	jsonStr := lastMsg.Content[start : end+1]
	err = json.Unmarshal([]byte(jsonStr), &result)
	if err != nil {
		return model.Item{}, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Index < 0 || result.Index >= len(items) {
		return model.Item{}, fmt.Errorf("invalid index returned: %d", result.Index)
	}

	return items[result.Index], nil
}

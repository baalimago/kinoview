package butler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

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

type itemMetadata struct {
	Name     string `json:"name"`
	AltName  string `json:"alt_name"`
	Year     int    `json:"year"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
}

func unmarshalMetadata(data *json.RawMessage) (
	itemMetadata, error) {
	var meta itemMetadata
	if data == nil {
		return meta, nil
	}
	err := json.Unmarshal(*data, &meta)
	return meta, err
}

func extractJSONBytes(content string) []byte {
	lastMsgStr := []byte(content)
	open := bytes.IndexByte(lastMsgStr, '{')
	if open == -1 {
		return lastMsgStr
	}
	close := -1
	depth := 0
OUTER:
	for i := open; i < len(lastMsgStr); i++ {
		switch lastMsgStr[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				close = i
				break OUTER
			}
		}
	}
	if open != -1 && close != -1 {
		lastMsgStr = lastMsgStr[open : close+1]
	}
	return lastMsgStr
}

// semanticIndexerSelect by:
// 1. Building clai chat using semanticIndexerSysPrompt as
//    system prompt
// 2. Format items into semanticIndexerSelectFormat
// 3. Pass the formated items into clai
// 4. Parse output from LLM into semanticIdexerResponse
func (b *butler) semanticIndexerSelect(ctx context.Context,
	sug suggestionResponse,
	items []model.Item) (model.Item, error) {
	// Format items into semanticIndexerSelectFormat
	var formattedItems []semanticIndexerSelectFormat
	for idx, item := range items {
		formatted := semanticIndexerSelectFormat{
			Index:    idx,
			FileName: item.Name,
		}
		if item.Metadata != nil {
			meta, err := unmarshalMetadata(
				item.Metadata)
			if err == nil {
				formatted.Name = meta.Name
				formatted.AltName = meta.AltName
				formatted.Year = meta.Year
				formatted.Season = meta.Season
				formatted.Episode = meta.Episode
			}
		}
		formattedItems = append(formattedItems,
			formatted)
	}

	itemsJSON, _ := json.MarshalIndent(formattedItems,
		"", "  ")

	userMessage := fmt.Sprintf(
		"Semantic description: %s\n\nMedia list:\n%s",
		sug.Description, string(itemsJSON))

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
		return model.Item{}, fmt.Errorf(
			"failed to query llm: %w", err)
	}

	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		if len(resp.Messages) > 0 {
			lastMsg = resp.Messages[
				len(resp.Messages)-1]
		} else {
			return model.Item{}, fmt.Errorf(
				"received empty response from llm")
		}
	}

	var result semanticIdexerResponse
	jsonBytes := extractJSONBytes(lastMsg.Content)
	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		return model.Item{}, fmt.Errorf(
			"failed to parse response: %w", err)
	}

	if result.Index < 0 || result.Index >= len(items) {
		return model.Item{}, fmt.Errorf(
			"invalid index returned: %d",
			result.Index)
	}

	return items[result.Index], nil
}

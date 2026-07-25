package butler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/clai/pkg/text"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type butler struct {
	llm      text.FullResponse
	subs     agents.StreamManager
	selector agents.SubtitleSelector
}

// SuggestionFingerprintVersion is bumped whenever the picker system prompt,
// the response schema, or butlerItemView changes. Phase 2 (index) and Phase 4
// (payload diet) each bumped it; start at 3.
const SuggestionFingerprintVersion = 3

const pickerSystemPrompt = `You are a media Butler. Your goal is to anticipate what the user wants to watch next.
You will be given the user's context (viewing history, time of day etc) and a list of available media.
Analyze the patterns and suggest suitable items from the library.

Do not suggest items that are clearly not in the provided media list.
Be concise.
Add a posh style to your replies as it will be user facing.

Item key legend: i=index n=filename t=title y=year s=season e=episode g=genre r=runtimeMinutes

Hints, in order of importance:
	1. Users prefer to watch series sequentially. If previous episode was 3, the next should be 4, of the same season.
	2. If a user has stopped a movie or series mid-way, there's a high chance the user wish to continue
	3. Have a variety of options, sometimes suggest new media
	4. Anticipate weekly trends. Example: user stops watching Thursday night, then a Friday movie would be likely a good candidate.

Respond ONLY with a JSON array in the following format:
[
  {
    "index": 42,
    "description": "<Descripton of item>" (string),
    "motivation": "<Short motivation>" (string)
  }
]

The "index" field must be the exact integer from the "i" field of the chosen item in the provided list. Copy it verbatim.
The "description" field should be a semantic index most likely to identify the media. Guidelines for "description" field:
	* Be VERY clear on your choice
	* ALWAYS formulate the description using your own words
	* NEVER use a filename as description
	
Examples:
	* "Big Buck Bunny S01E04"
	* "Season 3 Episode 10 Big Buck Bunny"
	* "Big Buck Bunny"
`

type suggestionResponse struct {
	Index       *int   `json:"index,omitempty"`
	Description string `json:"description"`
	Motivation  string `json:"motivation"`
}

// butlerItemView is the complete set of fields the butler receives per item.
// Adding a field here costs ~605 calls × 434 items of tokens per month; justify it.
type butlerItemView struct {
	Index   int    `json:"i"`
	Name    string `json:"n"`
	Title   string `json:"t,omitempty"`
	Year    int    `json:"y,omitempty"`
	Season  int    `json:"s,omitempty"`
	Episode int    `json:"e,omitempty"`
	Genre   string `json:"g,omitempty"`
	Runtime int    `json:"r,omitempty"`
}

// butlerContextView is the subset of ClientContext the butler receives.
type butlerContextView struct {
	LastPlayedName string         `json:"lastPlayedName"`
	ViewingHistory []viewMetadata `json:"viewingHistory"`
}

type viewMetadata struct {
	Name         string `json:"name"`
	ViewedAt     string `json:"viewedAt"`
	PlayedForSec string `json:"playedFor"`
}

// butlerMetadata is the subset of classifier metadata relevant to the butler.
type butlerMetadata struct {
	Name        string `json:"name"`
	AltName     string `json:"alt_name"`
	Year        int    `json:"year"`
	Season      int    `json:"season"`
	Episode     int    `json:"episode"`
	Genre       string `json:"genre"`
	DurationMin int    `json:"duration_min"`
}

// New configured by models.Configurations and a Subtitler
func New(c models.Configurations, subs agents.StreamManager) agents.Butler {
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
func (b *butler) PrepSuggestions(ctx context.Context, clientCtx model.ClientContext, items []model.Item) ([]model.Suggestion, error) {
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

	var recommendations []model.Suggestion
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
						"failed to prepare suggestion: %v", err,
					)
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
	if len(recommendations) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all %d suggestions failed to prepare: %w", len(errs), errors.Join(errs...))
	}
	if len(errs) > 0 {
		ancli.Errf("got errors trying to prep suggestions: %v", errs)
	}

	return recommendations, nil
}

func ProjectItems(items []model.Item) []butlerItemView {
	views := make([]butlerItemView, len(items))
	for idx, it := range items {
		v := butlerItemView{
			Index: idx,
			Name:  it.Name,
		}
		if it.Metadata != nil {
			var meta butlerMetadata
			if err := json.Unmarshal(*it.Metadata, &meta); err == nil {
				v.Year = meta.Year
				v.Season = meta.Season
				v.Episode = meta.Episode
				v.Genre = meta.Genre
				v.Runtime = meta.DurationMin
				if meta.Name != "" && meta.Name != it.Name {
					v.Title = meta.Name
				} else if meta.AltName != "" && meta.AltName != it.Name {
					v.Title = meta.AltName
				}
			}
		}
		views[idx] = v
	}
	// Sort by Path for deterministic output. Snapshot() iterates a map
	// with randomized order; sorting here guarantees a stable fingerprint.
	sort.Slice(views, func(i, j int) bool { return items[views[i].Index].Path < items[views[j].Index].Path })
	return views
}

func formatItems(items []model.Item) string {
	b, _ := json.Marshal(ProjectItems(items))
	return string(b)
}

func formatContext(c model.ClientContext) string {
	ctxView := butlerContextView{
		LastPlayedName: c.LastPlayedName,
		ViewingHistory: make([]viewMetadata, len(c.ViewingHistory)),
	}
	for i, vh := range c.ViewingHistory {
		ctxView.ViewingHistory[i] = viewMetadata{
			Name:         vh.Name,
			ViewedAt:     vh.ViewedAt.Format(time.RFC3339),
			PlayedForSec: vh.PlayedForSec,
		}
	}
	b, err := json.Marshal(ctxView)
	if err != nil {
		return fmt.Sprintf("Error formatting context: %v", err)
	}
	return string(b)
}

func (b *butler) resolveItem(ctx context.Context, sug suggestionResponse, items []model.Item) (model.Item, error) {
	if sug.Index != nil && *sug.Index >= 0 && *sug.Index < len(items) {
		return items[*sug.Index], nil
	}
	reason := "missing"
	if sug.Index != nil {
		reason = fmt.Sprintf("out-of-range (index=%d, len=%d)", *sug.Index, len(items))
	}
	ancli.Noticef("butler resolveItem: index %s, falling back to semantic indexer (description=%q)", reason, sug.Description)
	return b.semanticIndexerSelect(ctx, sug, items)
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

package recommender

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

type recommender struct {
	llm text.FullResponse
}

const systemPrompt = `You are a media picker. You will be given a request by a user, the user's context and the library: one video item per line as JSON.
Your task is to determine the right piece of media for the user, based on the request and context.

Be smart. Avoid picking media which has already been seen. When queried to continue something, doublecheck the existing duration since there may be some minutes left.
But also think about how there's end credits taking up final 5 percent of movies/series.

Respond with json in this format:
{
  "mediaId": "<id of the chosen item, copied from the media list>",
  "motivation": "<Motivation for why specific item was selected>"
}

Request: '%v'

Media:
%v`

// itemView is the per-item payload the prompt carries. The whole library has
// to fit in one request — full classifier metadata is about 0.5 MB on the
// reference library — so only the fields that pick media are sent, the same
// payload diet the butler uses (internal/agents/butler, butlerItemView).
type itemView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Show    string `json:"show,omitempty"`
	Year    int    `json:"year,omitempty"`
	Season  int    `json:"season,omitempty"`
	Episode int    `json:"episode,omitempty"`
	Genre   string `json:"genre,omitempty"`
	Runtime int    `json:"runtimeMin,omitempty"`
}

// itemMetadata is the subset of classifier metadata the view is built from.
type itemMetadata struct {
	Name        string `json:"name"`
	AltName     string `json:"alt_name"`
	ShowName    string `json:"showName"`
	Year        int    `json:"year"`
	Season      int    `json:"season"`
	Episode     int    `json:"episode"`
	Genre       string `json:"genre"`
	DurationMin int    `json:"duration_min"`
}

// New configured by models.Configurations
func New(c models.Configurations) agents.Recommender {
	return &recommender{
		llm: text.NewFullResponseQuerier(c),
	}
}

func (r *recommender) Setup(ctx context.Context) error {
	err := r.llm.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup querier: %w", err)
	}
	return nil
}

// Recommend some item based on the request and slice of items using a pre-prompted LLM
func (r *recommender) Recommend(
	ctx context.Context,
	request string,
	items []model.Item,
) (model.Item, error) {
	candidates := formatCandidates(recommendationCandidates(items))
	chat := models.Chat{
		Messages: []models.Message{
			{
				Role: "system",
				Content: fmt.Sprintf(
					systemPrompt,
					request,
					candidates,
				),
			},
		},
	}
	if misc.Truthy(os.Getenv("DEBUG")) {
		ancli.Noticef("Recommendation prompt:\n%v", debug.IndentedJsonFmt(chat))
	}
	resp, err := r.llm.Query(ctx, chat)
	if err != nil {
		return model.Item{}, fmt.Errorf(
			"failed to query llm: %v",
			err,
		)
	}
	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		return model.Item{}, fmt.Errorf(
			"failed to get last message: %v",
			err,
		)
	}
	id, err := extractMediaID(lastMsg.Content)
	if err != nil {
		return model.Item{}, fmt.Errorf(
			"failed to parse response: %v",
			err,
		)
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return model.Item{}, fmt.Errorf(
		"no item found with id %q",
		id,
	)
}

// recommendationCandidates projects the recommendable items — the videos — into
// the compact view the prompt carries.
func recommendationCandidates(items []model.Item) []itemView {
	views := make([]itemView, 0, len(items))
	for _, it := range items {
		if !strings.Contains(it.MIMEType, "video") {
			continue
		}
		meta := unmarshalItemMetadata(it.Metadata)
		views = append(views, itemView{
			ID:      it.ID,
			Name:    it.Name,
			Title:   firstNonEmpty(meta.Name, meta.AltName),
			Show:    meta.ShowName,
			Year:    meta.Year,
			Season:  meta.Season,
			Episode: meta.Episode,
			Genre:   meta.Genre,
			Runtime: meta.DurationMin,
		})
	}
	return views
}

// formatCandidates renders one JSON line per candidate.
func formatCandidates(views []itemView) string {
	var b strings.Builder
	for _, view := range views {
		line, err := json.Marshal(view)
		if err != nil {
			continue
		}
		b.WriteString("- ")
		b.Write(line)
		b.WriteString("\n")
	}
	return b.String()
}

func unmarshalItemMetadata(raw *json.RawMessage) itemMetadata {
	var meta itemMetadata
	if raw == nil {
		return meta
	}
	if err := json.Unmarshal(*raw, &meta); err != nil {
		return itemMetadata{}
	}
	return meta
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// extractMediaID by using some unholy concoction an LLM conjured up. It seems to work though, see tests
func extractMediaID(s string) (string, error) {
	key := `"mediaId"`
	n := len(s)
	m := len(key)
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] != key {
			continue
		}
		j := i + m
		for j < n && s[j] != ':' {
			j++
		}
		if j >= n {
			break
		}
		j++
		for j < n {
			c := s[j]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				j++
				continue
			}
			break
		}
		if j >= n || s[j] != '"' {
			break
		}
		j++
		start := j
		for j < n && s[j] != '"' {
			j++
		}
		if j < n {
			return s[start:j], nil
		}
		break
	}
	return "", fmt.Errorf("mediaId not found")
}

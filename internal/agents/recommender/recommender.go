package recommender

import (
	"context"
	"fmt"
	"os"

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

const systemPrompt = `You are a media picker. You will be given request by a user, user context and a list of media. Your task is to determine the right piece of media for the user, based on the request and context.

Respond with json in this format:
{
  "mediaId": "<ID_FROM_MEDIA>"
}

Request: '%v'

Media:
%v`

// NewRecommender configured by models.Configurations
func NewRecommender(c models.Configurations) agents.Recommender {
	c.SystemPrompt = systemPrompt
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
	var itemsStr string
	for _, it := range items {
		metadataJSONStr := ""
		if it.Metadata != nil {
			metadataJSON, err := it.Metadata.MarshalJSON()
			if err != nil {
				ancli.Warnf("failed to encode metadata for %v. Continuing without it, error: %v", it.Name, err)
			} else {
				metadataJSONStr = string(metadataJSON)
			}
		}

		itemsStr += fmt.Sprintf(
			"- id: %s, name: %s, type: %s, metadata: %v\n",
			it.ID,
			it.Name,
			it.MIMEType,
			metadataJSONStr,
		)
	}
	chat := models.Chat{
		Messages: []models.Message{
			{
				Role: "system",
				Content: fmt.Sprintf(
					systemPrompt,
					request,
					itemsStr,
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
	lastMsg, _, err := resp.LastOfRole("system")
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

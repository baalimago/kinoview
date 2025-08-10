package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/text"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

const systemPrompt = `You are a media classifier. Your job is to fill in the metadata for a given piece of media.
You may need to use tools to find information about a certain media, do so at will.

The following format will have parenthases. These are to describe the fields to you, the media classifier.

If the media item appears to be some extras, such as behind the scenes etc, you may skip non relevant fields.

OUTPUT ONLY IN THE FOLLOWING FORMAT:
{
	"name": "<NAME>",
	"alt_name": "<ALTERNATIVE_NAME>" (if there were multiple titles),
	"actors": ["ACTOR_FULLNAME_0", "ACTOR_FULLNAME_1", ...],
	"year": <RELEASE_YEAR_OF_MEDIA>,
	"description": <DESCRIPTION_OF_MEDIA>, (max 300 words),
}`

const userPrompt = `Information about the media to classify: %v`

type Classifier interface {
	Setup(context.Context) error
	Classify(context.Context, model.Item) (model.Item, error)
}

type classifier struct {
	llm text.FullResponse
}

// NewClassifier configured by models.Configurations. The prompt will be
// overwriten with DefaultPrompt, unless configured
func NewClassifier(c models.Configurations) Classifier {
	c.SystemPrompt = systemPrompt
	return &classifier{
		llm: text.NewFullResponseQuerier(c),
	}
}

func (c *classifier) Setup(ctx context.Context) error {
	err := c.llm.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup querier: %w", err)
	}
	return nil
}

// Classify some item and return a copy with updated metadata
func (c *classifier) Classify(ctx context.Context, i model.Item) (model.Item, error) {
	t0 := time.Now()
	chat := buildChat(i, t0)
	respChat, err := c.llm.Query(ctx, chat)
	if err != nil {
		return model.Item{}, fmt.Errorf("failed to query llm: %v", err)
	}
	lastMsg, err := extractSystemMessage(respChat)
	if err != nil {
		return model.Item{}, err
	}
	if err := validateBraces(lastMsg.Content); err != nil {
		return model.Item{}, err
	}
	lastMsgStr := extractJSONBytes(lastMsg.Content)
	var js json.RawMessage
	if err := json.Unmarshal(lastMsgStr, &js); err != nil {
		return model.Item{}, fmt.Errorf("lastMsg is not valid json: %w", err)
	}
	i.Metadata = &js
	return i, nil
}

func buildChat(i model.Item, t0 time.Time) models.Chat {
	return models.Chat{
		Created: t0,
		ID:      fmt.Sprintf("classify_%v_%v", i.ID, t0.Format("25-01-01T00:00Z00")),
		Messages: []models.Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: fmt.Sprintf(userPrompt, i),
			},
		},
	}
}

func extractSystemMessage(respChat models.Chat) (models.Message, error) {
	lastMsg, _, err := respChat.LastOfRole("system")
	if err != nil {
		return models.Message{}, fmt.Errorf("failed to get last message of role: %w", err)
	}
	return lastMsg, nil
}

func validateBraces(content string) error {
	amOpening := strings.Count(content, "{")
	amClosing := strings.Count(content, "}")
	if amOpening == 0 {
		return errors.New("amount of '{' is 0, cant be any json there")
	}
	if amClosing == 0 {
		return errors.New("amount of '}' is 0, cant be any json there")
	}
	if amOpening != amClosing {
		return fmt.Errorf("amount of '{' is %v, '}' is %v, cannot unmarshal message: %v", amOpening, amClosing, content)
	}
	return nil
}

func extractJSONBytes(content string) []byte {
	lastMsgStr := []byte(content)
	open := bytes.IndexByte(lastMsgStr, '{')
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

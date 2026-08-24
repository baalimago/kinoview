package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text"
	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/media/constants"
	"github.com/baalimago/kinoview/internal/model"
)

const systemPrompt = `You are a media classifier. Your job is to fill in the metadata for a given piece of media.
You may need to use tools to find information about a certain media, do so at will.

The following format will have parenthases. These are to describe the fields to you, the media classifier.

Some of the fields may be omitted if they aren't relevant for the media. "season" is for instance not relevant for a movie. 

OUTPUT ONLY IN THE FOLLOWING FORMAT:
%s`

const userPrompt = `Information about the media to classify: %v`

type classifier struct {
	model     string
	configDir string
	usesAgent bool
	tools     []models.LLMTool
	// conf holds the legacy config for SetOutput compat
	conf *models.Configurations
	llm  text.FullResponse
}

// New configured by models.Configurations. No custom tools.
func New(c models.Configurations) agents.Classifier {
	c.SystemPrompt = fmt.Sprintf(systemPrompt, constants.MetadataFormat)
	return &classifier{
		model:     c.Model,
		configDir: c.ConfigDir,
		usesAgent: false,
		llm:       text.NewFullResponseQuerier(c),
		conf:      &c,
	}
}

// NewWithTools creates a classifier with custom LLM tools registered.
func NewWithTools(c models.Configurations, tools []models.LLMTool) agents.Classifier {
	cls := &classifier{
		model:     c.Model,
		configDir: c.ConfigDir,
		usesAgent: true,
		tools:     tools,
		conf:      &c,
	}
	cls.buildAgent(tools, c.InternalTools, c.Out)
	return cls
}

func (c *classifier) buildAgent(tools []models.LLMTool, internalTools []models.ToolName, out io.Writer) {
	toolGlobs := make([]string, len(internalTools))
	for i, t := range internalTools {
		toolGlobs[i] = string(t)
	}
	opts := []agent.Option{
		agent.WithModel(c.model),
		agent.WithPrompt(fmt.Sprintf(systemPrompt, constants.MetadataFormat)),
		agent.WithConfigDir(c.configDir),
		agent.WithTools(tools),
		agent.WithToolGlobs(toolGlobs...),
	}
	if out != nil {
		// clai >= v1.10.23-rc1 replaced the io.Writer terminal output
		// (WithOutputTo) with a slog channel: the logger receives one record
		// per completed message, so the per-worker log file is a text-handler
		// sink instead of a raw writer.
		opts = append(opts, agent.WithLogger(slog.New(slog.NewTextHandler(out, nil))))
	}
	a := agent.New(opts...)
	c.llm = &a
}

func (c *classifier) Clone() agents.Classifier {
	clone := &classifier{
		model:     c.model,
		configDir: c.configDir,
		usesAgent: c.usesAgent,
	}

	if c.usesAgent {
		if len(c.tools) > 0 {
			clone.tools = make([]models.LLMTool, len(c.tools))
			copy(clone.tools, c.tools)
		}
		internalTools := make([]models.ToolName, len(c.conf.InternalTools))
		copy(internalTools, c.conf.InternalTools)
		clone.conf = &models.Configurations{
			Model:         c.conf.Model,
			ConfigDir:     c.conf.ConfigDir,
			InternalTools: internalTools,
		}
		clone.buildAgent(clone.tools, clone.conf.InternalTools, nil)
	} else {
		confCopy := *c.conf
		confCopy.Out = nil
		clone.conf = &confCopy
		clone.llm = text.NewFullResponseQuerier(*clone.conf)
	}

	return clone
}

func (c *classifier) Setup(ctx context.Context) error {
	err := c.llm.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup querier: %w", err)
	}
	return nil
}

func (c *classifier) SetOutput(w io.Writer) error {
	if c.usesAgent {
		c.buildAgent(c.tools, c.conf.InternalTools, w)
		err := c.llm.Setup(context.Background())
		if err != nil {
			return fmt.Errorf("failed to setup agent after output change: %w", err)
		}
		return nil
	}
	if c.conf == nil {
		return errors.New("no previous config set, can only set output on initialized classfier")
	}
	c.conf.Out = w
	c.llm = text.NewFullResponseQuerier(*c.conf)
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
	lastMsg, err := extractLastMessage(respChat)
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
				Content: fmt.Sprintf(systemPrompt, constants.MetadataFormat),
			},
			{
				Role:    "user",
				Content: fmt.Sprintf(userPrompt, i),
			},
		},
	}
}

func extractLastMessage(respChat models.Chat) (models.Message, error) {
	// clai >= v1.10.16 appends the model reply with role "assistant"; older
	// versions used "system". Grabbing "system" now returns the prompt itself,
	// whose annotated format example is not valid json (parens after values).
	lastMsg, _, err := respChat.LastOfRole("assistant")
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

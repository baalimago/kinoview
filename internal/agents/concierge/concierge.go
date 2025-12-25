package concierge

import (
	"errors"
	"fmt"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	clai_tools "github.com/baalimago/clai/pkg/tools"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/tools"
)

type ConciergeOption func(*concierge)

const systemPrompt = `You are a media concierge responsible for managing a media library. The root of the library is here: '%v'.
 
Scan the existing media collection and enhance metadata quality. Review current suggestions and assess if they remain relevant.
 
Before taking action, evaluate the current state in order:
	1. Scan previous notes using concierge_context_get
	2. Scan previous user sessions using user_context_getter
	3. Inspect the library state using: media_stats, media_list_missing_metadata, media_substring_filter, media_get_item

Your chores are as follows:
	1. Validate that the library is well-organized and metadata is accurate
	2. Add suggestions if there are none
	3. Validate that existing sugggestions are reasonable, judging by best effort on time of day, and user viewing history
	4. Ensure the suggestions have subtitles
	5. If there's nothing more actionable to do, persist context with concierge_context_push
	6. EXIT
 
Be wise and anticipatory about what might need attention. Consider the current date when assessing suggestions and relevance. 
Act deliberately. Avoid unnecessary modifications. Do not repeat yourself. Prefer quitting with a context update over retrying.`

type concierge struct {
	itemStore      agents.ItemGetter
	itemLister     agents.ItemLister
	metadataMgr    agents.MetadataManager
	suggestionMgr  agents.SuggestionManager
	subtitlesMgr   agents.SubtitleManager
	subSelector    agents.SubtitleSelector
	userContextMgr agents.UserContextManager

	storeDir string

	model     string
	configDir string
	cacheDir  string
}

func WithMetadataManager(m agents.MetadataManager) ConciergeOption {
	return func(c *concierge) {
		c.metadataMgr = m
	}
}

func WithSuggestionManager(m agents.SuggestionManager) ConciergeOption {
	return func(c *concierge) {
		c.suggestionMgr = m
	}
}

func WithSubtitleManager(m agents.SubtitleManager) ConciergeOption {
	return func(c *concierge) {
		c.subtitlesMgr = m
	}
}

func WithItemGetter(ig agents.ItemGetter) ConciergeOption {
	return func(c *concierge) {
		c.itemStore = ig
		if l, ok := ig.(agents.ItemLister); ok {
			c.itemLister = l
		}
	}
}

func WithItemLister(il agents.ItemLister) ConciergeOption {
	return func(c *concierge) {
		c.itemLister = il
	}
}

func WithSubtitleSelector(ss agents.SubtitleSelector) ConciergeOption {
	return func(c *concierge) {
		c.subSelector = ss
	}
}

func WithStoreDir(dir string) ConciergeOption {
	return func(c *concierge) {
		c.storeDir = dir
	}
}

func WithConfigDir(dir string) ConciergeOption {
	return func(c *concierge) {
		c.configDir = dir
	}
}

func WithCacheDir(dir string) ConciergeOption {
	return func(c *concierge) {
		c.cacheDir = dir
	}
}

func WithUserContextManager(ucm agents.UserContextManager) ConciergeOption {
	return func(c *concierge) {
		c.userContextMgr = ucm
	}
}

func WithModel(m string) ConciergeOption {
	return func(c *concierge) {
		c.model = m
	}
}

// New Concierge, hosting tools:
// 1. UpdateMetadata
// 2. PreloadSubtitles
// 3. CheckSuggestions
// 4. RemoveSuggestion
// 5. AddSuggestion
// 6. MediaGetItem
// 7. MediaList
// 8. MediaStats
func New(opts ...ConciergeOption) (agents.Concierge, error) {
	c := concierge{}
	for _, o := range opts {
		o(&c)
	}

	if c.itemStore == nil {
		return nil, errors.New("item store can't be nil")
	}

	if c.suggestionMgr == nil {
		return nil, errors.New("suggestion manager can't be nil")
	}

	if c.metadataMgr == nil {
		return nil, errors.New("metadata manager can't be nil")
	}

	if c.subtitlesMgr == nil {
		return nil, errors.New("subtitle manager can't be nil")
	}

	if c.subSelector == nil {
		return nil, errors.New("subtitle selector can't be nil")
	}

	if c.userContextMgr == nil {
		// user context manager optional; tool will be omitted if not provided
	}

	llmTools := make([]models.LLMTool, 0)

	ccg, err := tools.NewConciergeContextGet(tools.ConciergeContextWithCacheDir(c.cacheDir))
	if err != nil {
		ancli.Errf("concierge failed to setup conciergeContextGet: %v", err)
	} else {
		llmTools = append(llmTools, ccg)
	}

	ccp, err := tools.NewConciergeContextPush(tools.ConciergeContextWithCacheDir(c.cacheDir))
	if err != nil {
		ancli.Errf("concierge failed to setup conciergeContextPush: %v", err)
	} else {
		llmTools = append(llmTools, ccp)
	}

	umt, err := tools.NewUpdateMetadataTool(c.metadataMgr, c.itemStore)
	if err != nil {
		ancli.Errf("concierge failed to setup updateMetadataTool: %v", err)
	} else {
		llmTools = append(llmTools, umt)
	}

	pst, err := tools.NewPreloadSubtitlesTool(c.itemStore, c.subtitlesMgr, c.subSelector)
	if err != nil {
		ancli.Errf("concierge failed to setup preloadSubtitlesTool: %v", err)
	} else {
		llmTools = append(llmTools, pst)
	}

	lst, err := tools.NewCheckSuggestionsTool(c.suggestionMgr)
	if err != nil {
		ancli.Errf("concierge failed to setup checkSuggestionsTool: %v", err)
	} else {
		llmTools = append(llmTools, lst)
	}

	rst, err := tools.NewRemoveSuggestionTool(c.suggestionMgr)
	if err != nil {
		ancli.Errf("concierge failed to setup removeSuggestionTool: %v", err)
	} else {
		llmTools = append(llmTools, rst)
	}

	ast, err := tools.NewAddSuggestionTool(c.suggestionMgr, c.itemStore)
	if err != nil {
		ancli.Errf("concierge failed to setup addSuggestionTool: %v", err)
	} else {
		llmTools = append(llmTools, ast)
	}

	utm, err := tools.NewUserContextGetter(c.userContextMgr)
	if err != nil {
		ancli.Errf("concierge failed to setup userContextGetter: %v", err)
	} else {
		llmTools = append(llmTools, utm)
	}

	mgi, err := tools.NewMediaGetItemTool(c.itemStore)
	if err != nil {
		ancli.Errf("concierge failed to setup mediaGetItemTool: %v", err)
	} else {
		llmTools = append(llmTools, mgi)
	}

	if c.itemLister != nil {
		mst, err := tools.NewMediaStatsTool(c.itemLister)
		if err != nil {
			ancli.Errf("concierge failed to setup mediaStatsTool: %v", err)
		} else {
			llmTools = append(llmTools, mst)
		}

		ml, err := tools.NewMediaListTool(c.itemLister)
		if err != nil {
			ancli.Errf("concierge failed to setup mediaListTool: %v", err)
		} else {
			llmTools = append(llmTools, ml)
		}
	}

	llmTools = append(llmTools,
		clai_tools.WebsiteText,
		clai_tools.Date,
		clai_tools.FFProbe,
	)

	a := agent.New(
		agent.WithModel(c.model),
		agent.WithPrompt(fmt.Sprintf(systemPrompt, c.storeDir)),
		agent.WithTools(llmTools),
	)
	return &a, nil
}

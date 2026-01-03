package concierge

import (
	"errors"
	"time"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	clai_tools "github.com/baalimago/clai/pkg/tools"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/tools"
)

type ConciergeOption func(*concierge)

const systemPrompt = `You are a media concierge responsible for managing a media library. Your goal is to optimize user watch times by providing excellent suggestions.
 
Act deliberately. Avoid unnecessary modifications. Use the tools to do concierge things.

You will be called periodically. Make note of the date and tweak suggestions accordingly.

Analyze user context mapped with suggestions + concierge context motivations to see what suggestions have been successful or not. Use this knowledge to improve the suggestions
in the future. Make note of what series are being binged. Suggest at max 3 pieces of media.

Ensure that there is a variety of suggestions. Never suggest the same show/movie twice. Never skip episodes.

As you will be called often, prefer quitting early if there is nothing to do. If you run out of tool calls, simply stop.`

type concierge struct {
	itemStore      agents.ItemGetter
	itemLister     agents.ItemLister
	metadataMgr    agents.MetadataManager
	suggestionMgr  agents.SuggestionManager
	subtitlesMgr   agents.StreamManager
	subSelector    agents.SubtitleSelector
	userContextMgr agents.ClientContextManager

	storeDir string

	model     string
	interval  time.Duration
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

func WithSubtitleManager(m agents.StreamManager) ConciergeOption {
	return func(c *concierge) {
		c.subtitlesMgr = m
	}
}

func WithItemGetter(ig agents.ItemGetter) ConciergeOption {
	return func(c *concierge) {
		c.itemStore = ig
		l, ok := ig.(agents.ItemLister)
		if ok {
			c.itemLister = l
		} else {
			ancli.Warnf("failed to cast: %T to agents.ItemLister, will proceed without", ig)
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

func WithInterval(d time.Duration) ConciergeOption {
	return func(c *concierge) {
		c.interval = d
	}
}

func WithUserContextManager(ucm agents.ClientContextManager) ConciergeOption {
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
	c := concierge{
		interval: 6 * time.Hour,
	}
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
		agent.WithConfigDir(c.configDir),
		agent.WithPrompt(systemPrompt),
		agent.WithTools(llmTools),
		agent.WithMaxToolCalls(20),
	)
	return &a, nil
}

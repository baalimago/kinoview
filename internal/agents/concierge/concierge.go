package concierge

import (
	"errors"
	"path"
	"time"

	"github.com/baalimago/clai/pkg/agent"
	"github.com/baalimago/clai/pkg/text/models"
	clai_tools "github.com/baalimago/clai/pkg/tools"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/tools"
)

type ConciergeOption func(*concierge)

const baseSystemPrompt = `You are a media concierge responsible for managing a media library. Your goal is to optimize user watch times by providing excellent suggestions.

WORKFLOW — Execute in this exact order every run:

PHASE 1 — SUBTITLE VALIDATION (always run first):
Goal: Every active suggestion must have valid, working subtitles.

1. Call check_suggestions to list all active suggestions.
2. For EACH suggestion:
   a. Call media_get_item to get the item's metadata (title, description, language, genre).
   b. Call list_subtitle_candidates to discover available subtitle streams.
   c. If no candidates exist and fetch_subtitles is unavailable:
      - Note the item has no subtitles and cannot fetch them. Move to next suggestion.
   d. If candidates exist:
      - Select the best candidate: prefer default, English, non-forced, non-commentary.
      - If not already extracted, call extract_subtitle with the chosen subtitleID.
      - Call rows_between on the extracted file (path from extract_subtitle result, or call extract_subtitle again — it is idempotent). Read at least 100-200 lines to sample the dialogue.
      - VALIDATE: Compare the subtitle dialogue against the item's metadata:
        * Does the language of the text match what you expect?
        * Do character names, places, or plot elements in the dialogue match the item's description?
        * Is there actual spoken dialogue (not just timing cues or empty blocks)?
        * Does the subtitle appear to be for the correct media (not a different movie/episode)?
      - If valid: note it and move to the next suggestion.
      - If invalid or empty: attempt another candidate if available, otherwise note the failure.
3. Only after ALL suggestions have been processed, proceed to Phase 2.

PHASE 2 — SUGGESTION MANAGEMENT:
- Act deliberately; avoid unnecessary modifications.
- Analyze user context + prior suggestions + concierge motivations to learn what worked.
- Suggestions:
  - Suggest at most 3 pieces of media.
  - Ensure variety.
  - Never suggest the same show/movie twice.
  - Never skip episodes.
- Prefer quitting early if there is nothing to do.
- If you run out of tool calls, stop.
- You are not a chat-bot; your decisions are reflected via what the user selects.

GENERAL RULES:
- Note what is being binged.
- Cross-reference your notes with what the user actually watches.
- You will be called periodically; note the current date and adjust suggestions accordingly.
`

const openSubtitlesAddendum = `
OPENSUBTITLES FALLBACK:
- If no subtitle candidates exist for a suggested item, call fetch_subtitles to search OpenSubtitles.
- If extracted subtitles fail validation, call fetch_subtitles as a fallback.
- fetch_subtitles only works for movies (video/* MIME types).
`

type concierge struct {
	itemStore      agents.ItemGetter
	itemLister     agents.ItemLister
	metadataMgr    agents.MetadataManager
	suggestionMgr  agents.SuggestionManager
	subtitlesMgr   agents.StreamManager
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
// 1.  ConciergeContextGet
// 2.  ConciergeContextPush
// 3.  UpdateMetadata
// 4.  list_subtitle_candidates
// 5.  extract_subtitle
// 6.  fetch_subtitles (conditional — only when OPENSUBTITLES_API_KEY is set)
// 7.  check_suggestions
// 8.  remove_suggestion
// 9.  add_suggestion
// 10. get_user_context
// 11. media_get_item
// 12. media_list (conditional — only when item lister is available)
// 13. media_stats (conditional — only when item lister is available)
// 14. website_text
// 15. date
// 16. ffprobe
// 17. cat
// 18. rows_between
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

	subsPath := path.Join(c.configDir, "subtitles")
	lsc, err := tools.NewListSubtitleCandidatesTool(c.itemStore, c.subtitlesMgr, subsPath)
	if err != nil {
		ancli.Errf("concierge failed to setup listSubtitleCandidatesTool: %v", err)
	} else {
		llmTools = append(llmTools, lsc)
	}

	esc, err := tools.NewExtractSubtitleTool(c.itemStore, c.subtitlesMgr)
	if err != nil {
		ancli.Errf("concierge failed to setup extractSubtitleTool: %v", err)
	} else {
		llmTools = append(llmTools, esc)
	}

	lst, err := tools.NewCheckSuggestionsTool(c.suggestionMgr)
	if err != nil {
		ancli.Errf("concierge failed to setup checkSuggestionsTool: %v", err)
	} else {
		llmTools = append(llmTools, lst)
	}

	// Fetch subtitles from OpenSubtitles for movies without embedded subtitles.
	// Returns nil if OPENSUBTITLES_API_KEY is not configured (tool silently omitted).
	fst := tools.NewFetchSubtitlesTool(c.itemStore, c.subtitlesMgr, c.cacheDir)
	if fst != nil {
		llmTools = append(llmTools, fst)
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

	llmTools = append(
		llmTools,
		clai_tools.WebsiteText,
		clai_tools.Date,
		clai_tools.FFProbe,
		clai_tools.Cat,
		clai_tools.RowsBetween,
	)

	prompt := baseSystemPrompt
	if fst != nil {
		prompt += openSubtitlesAddendum
	}

	a := agent.New(
		agent.WithModel(c.model),
		agent.WithConfigDir(c.configDir),
		agent.WithPrompt(prompt),
		agent.WithTools(llmTools),
		agent.WithMaxToolCalls(20),
	)
	return &a, nil
}

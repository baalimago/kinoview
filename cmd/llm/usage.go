// Package llm provides the `kinoview llm usage` subcommand for aggregating
// per-query cost and token data from clai's persisted conversation files.
//
// Every conversation JSON under the clai conversations directory is streamed,
// decoded one at a time, attributed to an agent via its system prompt, and
// aggregated. The command never writes to the conversation directory.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/cmd"
)

const usage = `= LLM =

Inspect kinoview LLM spend data from clai conversation files.

Commands:
%v`

var subcommands = map[string]cmd.Command{
	"u|usage": usageCommand(),
}

func run(ctx context.Context, args []string) int {
	return cmd.Run(ctx, args, subcommands, usage)
}

type topCommand struct {
	flagset *flag.FlagSet
}

func Command() *topCommand {
	return &topCommand{}
}

func (c *topCommand) Describe() string {
	return "Inspect LLM usage and cost data from clai conversations."
}

func (c *topCommand) Help() string {
	return "Use 'llm usage' to aggregate and report LLM usage data."
}

func (c *topCommand) Setup(ctx context.Context) error {
	if c.flagset == nil {
		return errors.New("flagset can't be nil")
	}
	return nil
}

func (c *topCommand) Run(ctx context.Context) error {
	args := append([]string{os.Args[0]}, c.flagset.Args()...)
	exitCode := run(ctx, args)
	if exitCode > 0 {
		return fmt.Errorf("llm subcommand exited with code %v", exitCode)
	}
	return nil
}

func (c *topCommand) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("llm", flag.ContinueOnError)
	c.flagset = fs
	return fs
}

// ── usage subcommand ───────────────────────────────────────────────────────

type usageCmd struct {
	flagset *flag.FlagSet
	dir     *string
	since   *time.Duration
	by      *string
	asJSON  *bool
}

func usageCommand() *usageCmd {
	return &usageCmd{}
}

func (c *usageCmd) Describe() string {
	return "Aggregate LLM cost and token data from clai conversations."
}

func (c *usageCmd) Help() string {
	return "Aggregate per-query cost and token data from clai's persisted conversation files.\n" +
		"Defaults to ~/.config/kinoview/clai/conversations.\n\n" +
		"Flags:\n" +
		"  --dir    Override conversation directory.\n" +
		"  --since  Only include queries whose created_at falls within this duration\n" +
		"           (e.g. 168h, 24h, 7d). Filters on query time, not file mtime.\n" +
		"  --by     Grouping: agent (default), day, model.\n" +
		"  --json   Output machine-readable JSON.\n\n" +
		"NOTE: cost_usd is clai-reported and does not reconcile with published provider\n" +
		"pricing. Token counts are provider-reported and trusted. Use --json for raw\n" +
		"token counts to apply your own pricing."
}

func (c *usageCmd) Setup(ctx context.Context) error {
	return nil
}

func (c *usageCmd) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "~/.config"
	}
	defaultDir := path.Join(configDir, "kinoview", "clai", "conversations")
	c.dir = fs.String("dir", defaultDir, "clai conversations directory")
	c.since = fs.Duration("since", 0, "only include queries within this duration (e.g. 168h)")
	c.by = fs.String("by", "agent", "grouping: agent, day, model")
	c.asJSON = fs.Bool("json", false, "output as JSON")
	c.flagset = fs
	return fs
}

func (c *usageCmd) Run(ctx context.Context) error {
	dir := *c.dir
	since := *c.since
	groupBy := *c.by
	asJSON := *c.asJSON

	cutoff := time.Time{}
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("conversation directory not accessible: %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("conversation path is not a directory: %s", dir)
	}

	aggr := newAggregator(groupBy, cutoff)

	skipped := 0
	noCostFiles := 0

	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Permission errors: skip file, don't abort
			skipped++
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, parseErr := parseConversation(p)
		if parseErr != nil {
			skipped++
			return nil
		}
		if len(queries) == 0 {
			noCostFiles++
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk conversation directory: %w", err)
	}

	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "skipped %d unreadable or corrupt files\n", skipped)
	}

	totalFiles := aggr.fileCount + noCostFiles
	if totalFiles == 0 {
		fmt.Println("no conversations found")
		return nil
	}
	if aggr.fileCount == 0 {
		fmt.Println("no cost data — clai predates the cost-recording upgrade")
		return nil
	}

	rows := aggr.rows()
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
	} else {
		printTable(rows, groupBy)
	}

	fmt.Println()
	fmt.Println("cost_usd is clai-reported and may not match provider invoices.")
	fmt.Println("See 'kinoview llm usage --help' for details.")
	return nil
}

// ── conversation parsing ───────────────────────────────────────────────────

// convFile is a minimal struct for streaming decode of clai conversation JSON.
// Only the fields needed for attribution and aggregation are decoded; message
// bodies beyond the first system message are skipped via RawMessage.
type convFile struct {
	Queries  []queryRecord     `json:"queries"`
	Messages []json.RawMessage `json:"messages"`
}

type queryRecord struct {
	CreatedAt time.Time `json:"created_at"`
	CostUSD   float64   `json:"cost_usd"`
	Model     string    `json:"model"`
	Usage     *usageRec `json:"usage,omitempty"`
}

type usageRec struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type firstMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func parseConversation(filePath string) (agent string, queries []queryRecord, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	var cf convFile
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cf); err != nil {
		return "", nil, err
	}

	agent = "other"
	if len(cf.Messages) > 0 {
		var fm firstMsg
		if err := json.Unmarshal(cf.Messages[0], &fm); err == nil && fm.Role == "system" {
			agent = classifyAgent(fm.Content)
		}
	}

	return agent, cf.Queries, nil
}

// classifyAgent returns the agent name for a system prompt content string.
// Matching is case-insensitive and follows the attribution table from the
// analysis document Appendix A.3.
func classifyAgent(systemContent string) string {
	lower := strings.ToLower(systemContent)
	switch {
	case strings.Contains(lower, "media classifier"):
		return "classifier"
	case strings.Contains(lower, "media butler"):
		return "butler"
	case strings.Contains(lower, "pick a media item"):
		return "semanticIndexer"
	case strings.Contains(lower, "media stream analyzer"):
		return "subtitleSelector"
	case strings.Contains(lower, "media concierge"):
		return "concierge"
	case strings.Contains(lower, "slapstick"):
		return "theatre"
	case strings.Contains(lower, "media recommender"):
		return "recommender"
	default:
		return "other"
	}
}

// ── aggregation ────────────────────────────────────────────────────────────

type groupKey struct {
	agent string
	day   string
	model string
}

type bin struct {
	queries          int
	promptTokens     int
	cachedTokens     int
	completionTokens int
	reasoningTokens  int
	totalTokens      int
	costUSD          float64
	earliest         time.Time
	latest           time.Time
}

type aggregator struct {
	bins    map[groupKey]*bin
	groupBy string
	cutoff  time.Time

	fileCount int
}

func newAggregator(groupBy string, cutoff time.Time) *aggregator {
	return &aggregator{
		bins:    make(map[groupKey]*bin),
		groupBy: groupBy,
		cutoff:  cutoff,
	}
}

func (a *aggregator) add(agent string, queries []queryRecord) {
	a.fileCount++
	for _, q := range queries {
		if !a.cutoff.IsZero() && q.CreatedAt.Before(a.cutoff) {
			continue
		}
		key := a.keyFor(agent, q)
		b := a.bins[key]
		if b == nil {
			b = &bin{}
			a.bins[key] = b
		}
		b.queries++
		if q.Usage != nil {
			b.promptTokens += q.Usage.PromptTokens
			b.cachedTokens += q.Usage.PromptTokensDetails.CachedTokens
			b.completionTokens += q.Usage.CompletionTokens
			b.reasoningTokens += q.Usage.CompletionTokensDetails.ReasoningTokens
			b.totalTokens += q.Usage.TotalTokens
		}
		b.costUSD += q.CostUSD
		if b.earliest.IsZero() || q.CreatedAt.Before(b.earliest) {
			b.earliest = q.CreatedAt
		}
		if q.CreatedAt.After(b.latest) {
			b.latest = q.CreatedAt
		}
	}
}

func (a *aggregator) keyFor(agent string, q queryRecord) groupKey {
	k := groupKey{agent: agent}
	switch a.groupBy {
	case "day":
		k.day = q.CreatedAt.Format("2006-01-02")
	case "model":
		k.model = q.Model
	default:
		// agent — key is just the agent
	}
	return k
}

type aggregateRow struct {
	Label            string  `json:"label"`
	Queries          int     `json:"queries"`
	PromptTokens     int     `json:"prompt_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	CompletionTokens int     `json:"completion_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd_clai_reported"`
	MeanPrompt       float64 `json:"mean_prompt_per_query"`
	Earliest         string  `json:"earliest,omitempty"`
	Latest           string  `json:"latest,omitempty"`
}

func (a *aggregator) rows() []aggregateRow {
	rows := make([]aggregateRow, 0, len(a.bins))
	for key, b := range a.bins {
		label := key.agent
		switch a.groupBy {
		case "day":
			label = key.agent + " | " + key.day
		case "model":
			label = key.model
		}
		var hitRate float64
		if b.promptTokens > 0 {
			hitRate = float64(b.cachedTokens) / float64(b.promptTokens) * 100
		}
		var meanPrompt float64
		if b.queries > 0 {
			meanPrompt = float64(b.promptTokens) / float64(b.queries)
		}
		r := aggregateRow{
			Label:            label,
			Queries:          b.queries,
			PromptTokens:     b.promptTokens,
			CachedTokens:     b.cachedTokens,
			CacheHitRate:     hitRate,
			CompletionTokens: b.completionTokens,
			ReasoningTokens:  b.reasoningTokens,
			TotalTokens:      b.totalTokens,
			CostUSD:          b.costUSD,
			MeanPrompt:       meanPrompt,
		}
		if !b.earliest.IsZero() {
			r.Earliest = b.earliest.Format(time.RFC3339)
		}
		if !b.latest.IsZero() {
			r.Latest = b.latest.Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	return rows
}

// ── table output ───────────────────────────────────────────────────────────

func printTable(rows []aggregateRow, groupBy string) {
	// Header
	header := ""
	switch groupBy {
	case "model":
		header = fmt.Sprintf("%-30s %8s %12s %10s %7s %10s %8s %10s %14s",
			"MODEL", "QUERIES", "PROMPT_TOK", "CACHED", "HIT%",
			"COMPL_TOK", "REASON", "TOTAL_TOK", "COST_USD (clai)")
	default:
		header = fmt.Sprintf("%-25s %8s %12s %10s %7s %10s %8s %10s %14s",
			"AGENT", "QUERIES", "PROMPT_TOK", "CACHED", "HIT%",
			"COMPL_TOK", "REASON", "TOTAL_TOK", "COST_USD (clai)")
	}
	fmt.Println(header)

	for _, r := range rows {
		cachedStr := humanTokens(r.CachedTokens)
		fmt.Printf(
			"%-25s %8d %12s %10s %6.1f%% %10s %8s %10s %14.4f\n",
			r.Label,
			r.Queries,
			humanTokens(r.PromptTokens),
			cachedStr,
			r.CacheHitRate,
			humanTokens(r.CompletionTokens),
			humanTokens(r.ReasoningTokens),
			humanTokens(r.TotalTokens),
			r.CostUSD,
		)
	}
}

func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

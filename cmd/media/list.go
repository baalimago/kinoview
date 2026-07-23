package media

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/table"
	"github.com/baalimago/kinoview/internal/media/storage"
	"github.com/baalimago/kinoview/internal/model"
)

// mediaStore captures the subset of store operations needed by the media list command.
type mediaStore interface {
	Snapshot() []model.Item
	UpdateItem(model.Item) error
	DeleteItem(id string) error
}

type listCmd struct {
	storePath string
	force     bool
	pageSize  int
	flagset   *flag.FlagSet
	macroArgs []string
}

func listCommand() *listCmd {
	cfgDir, err := os.UserConfigDir()
	storePath := ""
	if err == nil {
		storePath = path.Join(cfgDir, "kinoview", "store")
	}

	return &listCmd{
		storePath: storePath,
		pageSize:  table.DefaultTheme().Items,
	}
}

func (c *listCmd) Describe() string {
	return "Browse and manage media items with an interactive table or non-interactive macros."
}

func (c *listCmd) Help() string {
	return `Usage: kinoview media list [flags] [macro-tokens...]

Interactive mode (no extra args): Opens a paginated table of all media items.
Navigate with n/p, filter with /pattern, select by index number.
After selection: [i]nspect JSON, [d]elete, [r]eclassify, [s]ubtitles, [b]ack to table.

Macro mode: Each argument after "list" is processed sequentially.
Examples:
  kinoview media list n n 5          # page 2, select index 5, show info
  kinoview media list /office 0 i    # filter "office", select, inspect JSON
  kinoview media list 0 d            # select first item, delete (needs confirm)
  kinoview media list 0 s            # select and show subtitle info
  kinoview media list 0 sa /path/sub.srt  # select and associate subtitle file
  kinoview media list 0 sr 0         # select and remove subtitle at index 0
  kinoview media list --force 0 d    # delete without confirmation`
}

func (c *listCmd) Setup(ctx context.Context) error {
	if c.flagset == nil {
		return errors.New("flagset can't be nil")
	}
	c.macroArgs = c.flagset.Args()
	return nil
}

func (c *listCmd) Run(ctx context.Context) error {
	if _, err := os.Stat(c.storePath); os.IsNotExist(err) {
		return fmt.Errorf("store path does not exist: %v", c.storePath)
	}

	store := storage.NewStore(
		storage.WithStorePath(c.storePath),
		storage.WithClassifier(nil),
	)
	_, err := store.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup store: %w", err)
	}

	items := store.Snapshot()
	if len(items) == 0 {
		ancli.Noticef("No media items in store.")
		return nil
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	lc := &listController{
		store:    store,
		items:    items,
		pageSize: c.pageSize,
		force:    c.force,
	}

	if len(c.macroArgs) == 0 {
		return lc.runInteractive()
	}
	return lc.runMacro(c.macroArgs)
}

func (c *listCmd) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.StringVar(&c.storePath, "store-path", c.storePath, "Path to kinoview store directory")
	fs.BoolVar(&c.force, "force", false, "Skip confirmation prompts in macro mode")
	fs.IntVar(&c.pageSize, "page-size", c.pageSize, "Items per page")
	c.flagset = fs
	return fs
}

// listController holds the runtime state for a media list session.
type listController struct {
	store    mediaStore
	items    []model.Item
	pageSize int
	force    bool
}

func (lc *listController) runInteractive() error {
	items := lc.items
	for {
		selected, _, err := table.New(
			table.SlicePaginator(items),
			lc.rowFormatter,
		).
			WithHeader(mediaTableHeader()).
			WithPageSize(lc.pageSize).
			WithSingleSelect().
			WithWriter(os.Stdout).
			Run()
		if err != nil {
			if errors.Is(err, table.ErrUserInitiatedExit) || errors.Is(err, table.ErrBack) {
				return nil
			}
			return fmt.Errorf("table error: %w", err)
		}

		if len(selected) == 0 {
			continue
		}

		item := items[selected[0]]
		printItemSummary(item)

		done, back, actErr := lc.interactivePostSelect(item)
		if actErr != nil {
			ancli.Errf("action error: %v", actErr)
		}
		if done {
			return nil
		}
		if back {
			// Refresh items in case of deletion
			items = lc.store.Snapshot()
			sort.Slice(items, func(i, j int) bool {
				return items[i].Name < items[j].Name
			})
			continue
		}
		// Default: exit after action
		return nil
	}
}

func (lc *listController) interactivePostSelect(item model.Item) (done bool, back bool, err error) {
	fmt.Printf("\n(press [d]elete, [r]eclassify, [i]nspect JSON, [s]ubtitles, [b]ack to list, [q]uit): ")
	input, inputErr := table.ReadUserInput()
	if inputErr != nil {
		if errors.Is(inputErr, table.ErrUserInitiatedExit) {
			return true, false, nil
		}
		return false, false, inputErr
	}

	switch strings.ToLower(input) {
	case "d":
		if !lc.confirmDelete(item) {
			ancli.Noticef("Delete cancelled.")
			return true, false, nil
		}
		if err := lc.store.DeleteItem(item.ID); err != nil {
			return false, false, fmt.Errorf("delete: %w", err)
		}
		ancli.Okf("Deleted: %v", item.Name)
		return true, false, nil
	case "i":
		data, err := json.MarshalIndent(item, "", "  ")
		if err != nil {
			return false, false, fmt.Errorf("marshal item: %w", err)
		}
		fmt.Println(string(data))
		return true, false, nil
	case "r":
		item.ClassificationAttempts = 0
		if err := lc.store.UpdateItem(item); err != nil {
			return false, false, fmt.Errorf("reclassify: %w", err)
		}
		ancli.Okf("Reclassify queued for: %v", item.Name)
		return true, false, nil
	case "s":
		return lc.interactiveSubtitleManager(item)
	case "b":
		return false, true, nil
	case "q", "":
		return true, false, nil
	default:
		ancli.Warnf("Unknown action: %q", input)
		return false, false, nil
	}
}

func (lc *listController) interactiveSubtitleManager(item model.Item) (done bool, back bool, err error) {
	for {
		fmt.Printf("\nSubtitle files for %q:\n", item.Name)
		if len(item.SubtitlePaths) == 0 {
			fmt.Println("  (none)")
		} else {
			for i, p := range item.SubtitlePaths {
				status := "✓"
				if _, statErr := os.Stat(p); statErr != nil {
					status = "✗ (missing)"
				}
				fmt.Printf("  [%d] %v %v\n", i, status, p)
			}
		}

		fmt.Printf("\n(press [a]dd path, [r]emove <index>, [b]ack): ")
		input, inputErr := table.ReadUserInput()
		if inputErr != nil {
			if errors.Is(inputErr, table.ErrUserInitiatedExit) {
				return true, false, nil
			}
			return false, false, inputErr
		}

		input = strings.TrimSpace(input)
		if input == "" || input == "b" || input == "q" {
			return false, false, nil
		}

		parts := smartSplit(input)
		switch strings.ToLower(parts[0]) {
		case "a", "add":
			if len(parts) < 2 {
				ancli.Warnf("Usage: a <path-to-subtitle-file>")
				continue
			}
			filePath := parts[1]
			// Unescape surrounding quotes if present
			filePath = strings.Trim(filePath, `"'`)
			if err := addSubtitlePath(&item, filePath); err != nil {
				ancli.Errf("Failed to add: %v", err)
				continue
			}
			if err := lc.store.UpdateItem(item); err != nil {
				ancli.Errf("Failed to save: %v", err)
				continue
			}
			ancli.Okf("Added subtitle: %v", filePath)
		case "r", "remove":
			if len(parts) < 2 {
				ancli.Warnf("Usage: r <index>")
				continue
			}
			idx, parseErr := strconv.Atoi(parts[1])
			if parseErr != nil || idx < 0 || idx >= len(item.SubtitlePaths) {
				ancli.Errf("Invalid index: %q (valid: 0-%d)", parts[1], len(item.SubtitlePaths)-1)
				continue
			}
			removed := item.SubtitlePaths[idx]
			item.SubtitlePaths = append(item.SubtitlePaths[:idx], item.SubtitlePaths[idx+1:]...)
			if err := lc.store.UpdateItem(item); err != nil {
				ancli.Errf("Failed to save: %v", err)
				continue
			}
			ancli.Okf("Removed subtitle: %v", removed)
		default:
			ancli.Warnf("Unknown action: %q (valid: a, r, b)", parts[0])
		}
	}
}

func (lc *listController) confirmDelete(item model.Item) bool {
	if lc.force {
		return true
	}
	return readYesNo(fmt.Sprintf("Delete '%v'? This cannot be undone. (y/N): ", item.Name))
}

func readYesNo(prompt string) bool {
	fmt.Print(prompt)
	input, err := table.ReadUserInput()
	if err != nil {
		return false
	}
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
}

// smartSplit splits input by spaces but respects quoted substrings.
// Used for parsing user input like: a "/path/with spaces/file.srt"
func smartSplit(input string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(input); i++ {
		c := input[i]
		if inQuotes {
			if c == quoteChar {
				inQuotes = false
				quoteChar = 0
			} else {
				current.WriteByte(c)
			}
		} else {
			if c == '"' || c == '\'' {
				inQuotes = true
				quoteChar = c
			} else if c == ' ' {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				current.WriteByte(c)
			}
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

var validSubtitleExts = map[string]bool{
	".srt": true,
	".vtt": true,
	".sub": true,
	".ass": true,
	".ssa": true,
}

// addSubtitlePath validates and appends a subtitle file path to the item.
// Rejects: non-existent files, directories, non-subtitle extensions, duplicates.
func addSubtitlePath(item *model.Item, filePath string) error {
	filePath = path.Clean(filePath)

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", filePath)
		}
		return fmt.Errorf("cannot access path: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", filePath)
	}

	ext := strings.ToLower(path.Ext(filePath))
	if !validSubtitleExts[ext] {
		return fmt.Errorf("not a subtitle file: %s (valid extensions: .srt, .vtt, .sub, .ass, .ssa)", filePath)
	}

	// Deduplicate
	for _, existing := range item.SubtitlePaths {
		if existing == filePath {
			return fmt.Errorf("path already associated: %s", filePath)
		}
	}

	item.SubtitlePaths = append(item.SubtitlePaths, filePath)
	return nil
}

// runMacro processes the macro token slice. Tokens are split at the first
// numeric selection: everything before (navigation, filters) goes to the table,
// the first numeric is the selection, and everything after is post-selection
// dispatch. The "b" (back) post-selection action re-enters the table with
// remaining tokens.
func (lc *listController) runMacro(tokens []string) error {
	items := lc.items
	for len(tokens) > 0 {
		tableTokens, remaining := splitAtSelection(tokens)
		if len(tableTokens) == 0 {
			return fmt.Errorf("no selectable token found in: %v", tokens)
		}

		input := strings.NewReader(strings.Join(tableTokens, "\n") + "\n")

		selected, _, err := table.New(
			table.SlicePaginator(items),
			lc.rowFormatter,
		).
			WithHeader(mediaTableHeader()).
			WithPageSize(lc.pageSize).
			WithSingleSelect().
			WithWriter(os.Stdout).
			WithInput(input).
			Run()
		if err != nil {
			if errors.Is(err, table.ErrUserInitiatedExit) || errors.Is(err, table.ErrBack) {
				return nil
			}
			return fmt.Errorf("macro table: %w", err)
		}

		if len(selected) == 0 {
			return fmt.Errorf("no item selected from tokens: %v", tableTokens)
		}

		item := items[selected[0]]
		printItemSummary(item)

		tokens = remaining
		if len(tokens) == 0 {
			return nil
		}

		action := tokens[0]
		tokens = tokens[1:]

		switch strings.ToLower(action) {
		case "i":
			data, err := json.MarshalIndent(item, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal item: %w", err)
			}
			fmt.Println(string(data))
			return nil
		case "d":
			if !lc.force && !readYesNo(fmt.Sprintf("Delete '%v'? This cannot be undone. (y/N): ", item.Name)) {
				ancli.Noticef("Delete cancelled.")
				return nil
			}
			if err := lc.store.DeleteItem(item.ID); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
			ancli.Okf("Deleted: %v", item.Name)
			return nil
		case "r":
			item.ClassificationAttempts = 0
			if err := lc.store.UpdateItem(item); err != nil {
				return fmt.Errorf("reclassify: %w", err)
			}
			ancli.Okf("Reclassify queued for: %v", item.Name)
			return nil
		case "s":
			// Just print subtitle info and exit (summary already printed)
			return nil
		case "sa":
			if len(tokens) == 0 {
				return fmt.Errorf("sa requires a path argument: sa <path-to-subtitle>")
			}
			subPath := tokens[0]
			tokens = tokens[1:]
			if err := addSubtitlePath(&item, subPath); err != nil {
				return fmt.Errorf("add subtitle: %w", err)
			}
			if err := lc.store.UpdateItem(item); err != nil {
				return fmt.Errorf("save item: %w", err)
			}
			ancli.Okf("Added subtitle: %v", subPath)
			return nil
		case "sr":
			if len(tokens) == 0 {
				return fmt.Errorf("sr requires an index argument: sr <index>")
			}
			idxArg := tokens[0]
			tokens = tokens[1:]
			idx, parseErr := strconv.Atoi(idxArg)
			if parseErr != nil || idx < 0 || idx >= len(item.SubtitlePaths) {
				return fmt.Errorf("invalid subtitle index: %q (valid: 0-%d)", idxArg, len(item.SubtitlePaths)-1)
			}
			removed := item.SubtitlePaths[idx]
			item.SubtitlePaths = append(item.SubtitlePaths[:idx], item.SubtitlePaths[idx+1:]...)
			if err := lc.store.UpdateItem(item); err != nil {
				return fmt.Errorf("save item: %w", err)
			}
			ancli.Okf("Removed subtitle: %v", removed)
			return nil
		case "b":
			// Re-enter table loop with remaining tokens.
			items = lc.store.Snapshot()
			sort.Slice(items, func(i, j int) bool {
				return items[i].Name < items[j].Name
			})
			continue
		case "q":
			return nil
		default:
			return fmt.Errorf("unknown macro action: %q (valid: i, d, r, s, sa, sr, b, q)", action)
		}
	}
	return nil
}

// splitAtSelection finds the first token that looks like a numeric selection
// (digits, colon ranges, or comma-separated) and splits tokens at that point.
// Table tokens = everything up to and including the selection.
// Remaining = everything after the selection (post-selection dispatch).
func splitAtSelection(tokens []string) (tableTokens, remaining []string) {
	for i, tok := range tokens {
		if isTableAction(tok) {
			continue
		}
		if isNumericSelection(tok) {
			return tokens[:i+1], tokens[i+1:]
		}
		// Non-action, non-numeric: treat as selection anyway (will
		// error in table if invalid).
		return tokens[:i+1], tokens[i+1:]
	}
	// All tokens are table actions — no selection found.
	return tokens, nil
}

func isTableAction(tok string) bool {
	switch strings.ToLower(tok) {
	case "n", "next", "p", "prev", "b", "back", "q", "quit":
		return true
	}
	if strings.HasPrefix(tok, "/") {
		return true
	}
	return false
}

func isNumericSelection(tok string) bool {
	if len(tok) == 0 {
		return false
	}
	// Allow comma-separated: "0,1,2"
	if strings.Contains(tok, ",") {
		for _, part := range strings.Split(tok, ",") {
			if !isDigitsOrRange(strings.TrimSpace(part)) {
				return false
			}
		}
		return true
	}
	return isDigitsOrRange(tok)
}

func isDigitsOrRange(s string) bool {
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			return false
		}
		return isAllDigits(strings.TrimSpace(parts[0])) && isAllDigits(strings.TrimSpace(parts[1]))
	}
	return isAllDigits(s)
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// rowFormatter formats a single item as a table row with fixed-width columns.
func (lc *listController) rowFormatter(idx int, item model.Item) (string, error) {
	name := truncateTo(item.Name, 55)
	mime := shortMIME(item.MIMEType)
	metadata := "✗"
	if item.Metadata != nil {
		metadata = "✓"
	}
	attempts := strconv.Itoa(item.ClassificationAttempts)
	subs := "✗"
	if len(item.SubtitlePaths) > 0 {
		subs = "✓"
	}
	size := fileSizeStr(item.Path)

	return fmt.Sprintf(
		"%-6d %-55s %-8s %-8s %-4s %-5s %10s",
		idx,
		name,
		mime,
		metadata,
		attempts,
		subs,
		size,
	), nil
}

func mediaTableHeader() string {
	return fmt.Sprintf("%-6s %-55s %-8s %-8s %-4s %-5s %10s",
		"Index", "Name", "Type", "Metadata", "Attr", "Subs", "Size")
}

func printItemSummary(item model.Item) {
	fmt.Printf("\nName:       %v\n", item.Name)
	fmt.Printf("Path:       %v\n", item.Path)
	fmt.Printf("Type:       %v\n", item.MIMEType)
	fmt.Printf("ID:         %v\n", item.ID)

	if item.Metadata != nil {
		fmt.Printf("Metadata:   %v\n", formatMetadata(*item.Metadata))
	} else {
		fmt.Println("Metadata:   (none)")
	}

	if item.ClassificationAttempts > 0 {
		fmt.Printf("Classified: %v attempts (last error: %v)\n",
			item.ClassificationAttempts, item.ClassificationError)
	}

	size := fileSizeStr(item.Path)
	fmt.Printf("Size:       %v\n", size)

	if len(item.SubtitlePaths) > 0 {
		fmt.Printf("Subtitles:  %d associated\n", len(item.SubtitlePaths))
		for i, p := range item.SubtitlePaths {
			status := "✓"
			if _, err := os.Stat(p); err != nil {
				status = "✗ (missing)"
			}
			fmt.Printf("  [%d] %v %v\n", i, status, p)
		}
	} else {
		fmt.Println("Subtitles:  (none)")
	}
}

func formatMetadata(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	compact, _ := json.Marshal(m)
	s := string(compact)
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

func shortMIME(mime string) string {
	switch {
	case strings.Contains(mime, "video"):
		return "video"
	case strings.Contains(mime, "image"):
		return "image"
	default:
		return "other"
	}
}

func truncateTo(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func fileSizeStr(filePath string) string {
	info, err := os.Stat(filePath)
	if err != nil {
		return "?"
	}
	return humanSize(info.Size())
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTP"[exp])
}

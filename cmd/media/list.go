package media

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"slices"
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
	// ResetClassification clears an item's classification so the server
	// reclassifies it on its next pass.
	ResetClassification(id string) (bool, error)
	// ClearClassificationStopLoss re-opens an item that hit the attempt ceiling.
	ClearClassificationStopLoss(id string) (bool, error)
	// ClassificationMaxAttempts is the ceiling itself.
	ClassificationMaxAttempts() int
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
Video items in the same directory collapse into a single group row (a season,
for example); selecting a group row drills into its members, where [R]eclassify
all resets the whole group.
Navigate with n/p, filter with /pattern, select by index number.
After selection: [i]nspect JSON, [d]elete, [r]eclassify, [s]ubtitles, [b]ack to table.

Macro mode: Each argument after "list" is processed sequentially.
Group rows support: 0 r (reclassify the group), 0 i (group summary).
Examples:
  kinoview media list n n 5          # page 2, select index 5, show info
  kinoview media list /office 0 i    # filter "office", select, inspect JSON
  kinoview media list 0 d            # select first item, delete (needs confirm)
  kinoview media list 0 s            # select and show subtitle info
  kinoview media list 0 sa /path/sub.srt  # select and associate subtitle file
  kinoview media list 0 sr 0         # select and remove subtitle at index 0
  kinoview media list --force 0 d    # delete without confirmation
  kinoview media list /season 0 r    # reclassify the whole group (confirms unless --force)`
}

func (c *listCmd) Setup(ctx context.Context) error {
	if c.flagset == nil {
		return errors.New("flagset can't be nil")
	}
	c.macroArgs = c.flagset.Args()
	return nil
}

func (c *listCmd) Flagset() *flag.FlagSet {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.StringVar(&c.storePath, "store-path", c.storePath, "Path to kinoview store directory")
	fs.BoolVar(&c.force, "force", false, "Skip confirmation prompts in macro mode")
	fs.IntVar(&c.pageSize, "page-size", c.pageSize, "Items per page")
	c.flagset = fs
	return fs
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
		store:       store,
		items:       items,
		pageSize:    c.pageSize,
		force:       c.force,
		storePath:   c.storePath,
		maxAttempts: store.ClassificationMaxAttempts(),
	}

	if len(c.macroArgs) == 0 {
		return lc.runInteractive(ctx)
	}
	return lc.runMacro(c.macroArgs)
}

// listController holds the runtime state for a media list session.
type listController struct {
	store       mediaStore
	items       []model.Item
	pageSize    int
	force       bool
	storePath   string
	maxAttempts int
}

// ── Location grouping ──

// mediaRowKind distinguishes member rows from collapsed group rows.
type mediaRowKind uint8

const (
	rowItem mediaRowKind = iota
	rowGroup
)

// mediaRow is a single display row in the media list table. Item rows carry
// one item; group rows carry every video item sharing a directory.
type mediaRow struct {
	kind     mediaRowKind
	groupKey string       // parent directory; set for both kinds
	item     model.Item   // rowItem only
	members  []model.Item // rowGroup only
}

// groupKeyForItem is the location grouping key: the item's parent directory.
// Episodes of a season share a directory, so they collapse into one row.
func groupKeyForItem(it model.Item) string {
	return path.Dir(it.Path)
}

// groupDisplayName is the short label shown for a group row: the directory's
// base name (Season 1, S01, ...) with degenerate roots kept as-is.
func groupDisplayName(dir string) string {
	base := path.Base(dir)
	if base == "." || base == "/" || base == "" {
		return dir
	}
	return base
}

// deriveRows renders the current table view. With a non-empty groupKey the
// view is the member rows of that group (drill-down); otherwise rows are
// collapsed so every directory holding ≥2 video items becomes a single group
// row. Images never participate in grouping — a movie folder with a poster
// stays two plain rows.
func deriveRows(items []model.Item, groupKey string) []mediaRow {
	if groupKey != "" {
		rows := make([]mediaRow, 0, len(items))
		for _, it := range items {
			if groupKeyForItem(it) == groupKey && strings.Contains(it.MIMEType, "video") {
				rows = append(rows, mediaRow{kind: rowItem, groupKey: groupKey, item: it})
			}
		}
		return rows
	}

	counts := make(map[string]int)
	for _, it := range items {
		if !strings.Contains(it.MIMEType, "video") {
			continue
		}
		counts[groupKeyForItem(it)]++
	}

	emitted := make(map[string]bool)
	rows := make([]mediaRow, 0, len(items))
	for _, it := range items {
		k := groupKeyForItem(it)
		if strings.Contains(it.MIMEType, "video") && counts[k] >= 2 {
			if !emitted[k] {
				emitted[k] = true
				rows = append(rows, mediaRow{
					kind:     rowGroup,
					groupKey: k,
					members:  groupMembersOf(items, k),
				})
			}
			continue
		}
		rows = append(rows, mediaRow{kind: rowItem, groupKey: k, item: it})
	}
	return rows
}

// groupMembersOf returns the video items in the given directory, in items order.
func groupMembersOf(items []model.Item, dir string) []model.Item {
	var out []model.Item
	for _, it := range items {
		if strings.Contains(it.MIMEType, "video") && groupKeyForItem(it) == dir {
			out = append(out, it)
		}
	}
	return out
}

// ── Table construction ──

// maxIndexWidth returns the widest rendered index across all rows, so group
// rows (0 [group:12]) align with plain rows.
func maxIndexWidth(rows []mediaRow) int {
	w := len("Index")
	for i, r := range rows {
		s := fmt.Sprintf("%d", i)
		if r.kind == rowGroup {
			s = fmt.Sprintf("%d [group:%d]", i, len(r.members))
		}
		if len(s) > w {
			w = len(s)
		}
	}
	return w
}

// mediaTableHeader renders the column header for the given index width.
func mediaTableHeader(idxWidth int) string {
	return fmt.Sprintf("%-*s %-55s %-8s %-8s %-4s %-5s %10s",
		idxWidth, "Index", "Name", "Type", "Metadata", "Attr", "Subs", "Size")
}

// buildTable assembles a configured table for the given rows. prefix, when
// non-empty, is printed as its own line above the header (used for the group
// drill-down label), so the header columns stay aligned with the data rows.
func (lc *listController) buildTable(rows []mediaRow, prefix, backLabel string, actions ...table.TableAction) *table.Table[mediaRow] {
	idxWidth := maxIndexWidth(rows)
	header := mediaTableHeader(idxWidth)
	if prefix != "" {
		header = prefix + "\n" + header
	}

	tb := table.New(
		table.SlicePaginator(rows),
		func(idx int, row mediaRow) (string, error) {
			return lc.formatRow(idxWidth, idx, row), nil
		},
	).
		WithHeader(header).
		WithPageSize(lc.pageSize).
		WithSingleSelect().
		WithWriter(os.Stdout)
	if backLabel != "" {
		tb = tb.WithBackLabel(backLabel)
	}
	if len(actions) > 0 {
		tb = tb.WithActions(actions...)
	}
	return tb
}

// formatRow dispatches to the item or group row formatter.
func (lc *listController) formatRow(idxWidth, idx int, row mediaRow) string {
	if row.kind == rowGroup {
		return lc.groupRowFormatter(idxWidth, idx, row)
	}
	return lc.itemRowFormatter(idxWidth, idx, row.item)
}

// itemRowFormatter formats a single item row with fixed-width columns.
func (lc *listController) itemRowFormatter(idxWidth, idx int, item model.Item) string {
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
		"%-*d %-55s %-8s %-8s %-4s %-5s %10s",
		idxWidth,
		idx,
		name,
		mime,
		metadata,
		attempts,
		subs,
		size,
	)
}

// groupRowFormatter formats a collapsed group row: index shows the member
// count, metadata shows done/total, size is the sum over members.
func (lc *listController) groupRowFormatter(idxWidth, idx int, row mediaRow) string {
	done, subs := 0, 0
	var totalSize int64
	sizeOK := true
	for _, m := range row.members {
		if m.Metadata != nil {
			done++
		}
		if len(m.SubtitlePaths) > 0 {
			subs++
		}
		info, err := os.Stat(m.Path)
		if err != nil {
			sizeOK = false
		} else {
			totalSize += info.Size()
		}
	}
	sizeStr := "?"
	if sizeOK {
		sizeStr = humanSize(totalSize)
	}
	subsStr := "✗"
	if subs > 0 {
		subsStr = fmt.Sprintf("%d/%d", subs, len(row.members))
	}

	return fmt.Sprintf(
		"%-*s %-55s %-8s %-8s %-4s %-5s %10s",
		idxWidth,
		fmt.Sprintf("%d [group:%d]", idx, len(row.members)),
		truncateTo(groupDisplayName(row.groupKey), 55),
		"group",
		fmt.Sprintf("%d/%d", done, len(row.members)),
		"–",
		subsStr,
		sizeStr,
	)
}

// errReclassifyGroup is the table-action sentinel signalling the interactive
// loop to reclassify every member of the current group drill-down.
var errReclassifyGroup = errors.New("reclassify group")

// runInteractive opens the grouped table and handles drill-downs and
// post-selection actions.
func (lc *listController) runInteractive(ctx context.Context) error {
	items := lc.items
	groupKey := ""
	for {
		rows := deriveRows(items, groupKey)

		actions := []table.TableAction{}
		prefix := ""
		backLabel := ""
		if groupKey != "" {
			prefix = fmt.Sprintf("%s (%d)", groupDisplayName(groupKey), len(rows))
			backLabel = "[b]ack to list"
			actions = append(actions, table.TableAction{
				Format: "[R]eclassify all",
				Short:  "R",
				Long:   "reclassify-all",
				Action: func() error { return errReclassifyGroup },
			})
		}

		selected, _, err := lc.buildTable(rows, prefix, backLabel, actions...).Run()
		if err != nil {
			if errors.Is(err, errReclassifyGroup) {
				lc.reclassifyGroupInteractive(ctx, groupKey)
				items = lc.refreshItems()
				groupKey = ""
				continue
			}
			if errors.Is(err, table.ErrBack) {
				if groupKey != "" {
					groupKey = ""
					continue
				}
				return nil
			}
			if errors.Is(err, table.ErrUserInitiatedExit) {
				return nil
			}
			return fmt.Errorf("table error: %w", err)
		}

		if len(selected) == 0 {
			continue
		}

		row := rows[selected[0]]
		if row.kind == rowGroup {
			groupKey = row.groupKey
			continue
		}

		item := row.item
		printItemSummary(item)

		done, back, actErr := lc.interactivePostSelect(ctx, item)
		if actErr != nil {
			ancli.Errf("action error: %v", actErr)
		}
		if done {
			return nil
		}
		if back {
			items = lc.refreshItems()
			continue
		}
		// Default: exit after action
		return nil
	}
}

func (lc *listController) interactivePostSelect(ctx context.Context, item model.Item) (done bool, back bool, err error) {
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
		if _, err := lc.store.ResetClassification(item.ID); err != nil {
			return false, false, fmt.Errorf("reclassify: %w", err)
		}
		lc.reclassifyAndWatch(ctx, []model.Item{item}, item.Name)
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

// reclassifyGroupInteractive confirms and reclassifies every member of the
// given group drill-down, then watches the classification progress.
func (lc *listController) reclassifyGroupInteractive(ctx context.Context, groupKey string) {
	members := groupMembersOf(lc.items, groupKey)
	if len(members) == 0 {
		ancli.Warnf("no members in group %q", groupKey)
		return
	}
	if !readYesNo(fmt.Sprintf("Reclassify %d item(s) in '%s'? This clears their metadata so the server re-classifies them. (y/N): ", len(members), groupKey)) {
		ancli.Noticef("Cancelled.")
		return
	}
	lc.reclassifyAndWatch(ctx, members, groupDisplayName(groupKey))
}

// reclassifyAndWatch resets the given items and watches classification
// progress until every item has been attempted (or the user quits).
func (lc *listController) reclassifyAndWatch(ctx context.Context, items []model.Item, label string) {
	reset, failures := resetItems(lc.store, items)
	for _, f := range failures {
		ancli.Errf("%v", f)
	}
	if reset == 0 {
		return
	}
	ancli.Okf("Classification reset for %d item(s) — watching for reclassification (q to quit)...", reset)
	if err := watchClassificationProgress(ctx, lc.storePath, label, items, lc.maxAttempts, os.Stdout, progressWatchOptions{}); err != nil {
		ancli.Errf("classification progress watch: %v", err)
	}
}

// resetItems clears classification on every item, keeping one unwritable item
// from stranding the rest. Reports how many were reset and any failures.
func resetItems(store mediaStore, items []model.Item) (int, []error) {
	var failures []error
	reset := 0
	for _, it := range items {
		if _, err := store.ResetClassification(it.ID); err != nil {
			failures = append(failures, fmt.Errorf("reclassify %q: %w", it.Name, err))
			continue
		}
		reset++
	}
	return reset, failures
}

// refreshItems reloads the item list from disk — the server may have written
// fresh metadata while the CLI was watching — and sorts it by name.
func (lc *listController) refreshItems() []model.Item {
	items := readItemsFromDisk(lc.storePath)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

// readItemsFromDisk loads every item JSON from the store directory, mirroring
// the store's own load but without the stale in-memory cache.
func readItemsFromDisk(storePath string) []model.Item {
	entries, err := os.ReadDir(storePath)
	if err != nil {
		return nil
	}
	var items []model.Item
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		it, ok := readItemFromDisk(storePath, e.Name())
		if !ok || it.ID == "" {
			continue
		}
		items = append(items, it)
	}
	return items
}

// printGroupSummary prints the members of a group row.
func printGroupSummary(row mediaRow) {
	fmt.Printf("\nGroup: %v\n", row.groupKey)
	fmt.Printf("Items: %d\n", len(row.members))
	for i, m := range row.members {
		meta := "✗"
		if m.Metadata != nil {
			meta = "✓"
		}
		fmt.Printf("  [%d] %-55s metadata=%v attempts=%d\n", i, truncateTo(m.Name, 55), meta, m.ClassificationAttempts)
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
	if slices.Contains(item.SubtitlePaths, filePath) {
		return fmt.Errorf("path already associated: %s", filePath)
	}

	item.SubtitlePaths = append(item.SubtitlePaths, filePath)
	return nil
}

// runMacro processes the macro token slice. Tokens are split at the first
// numeric selection: everything before (navigation, filters) goes to the table,
// the first numeric is the selection, and everything after is post-selection
// dispatch. The "b" (back) post-selection action re-enters the table with
// remaining tokens. Selecting a group row dispatches group-level actions
// (r = reclassify the group, i = group summary); deleting a group is refused.
func (lc *listController) runMacro(tokens []string) error {
	items := lc.items
	for len(tokens) > 0 {
		rows := deriveRows(items, "")
		tableTokens, remaining := splitAtSelection(tokens)
		if len(tableTokens) == 0 {
			return fmt.Errorf("no selectable token found in: %v", tokens)
		}

		input := strings.NewReader(strings.Join(tableTokens, "\n") + "\n")

		selected, _, err := lc.buildTable(rows, "", "").
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

		row := rows[selected[0]]
		tokens = remaining

		if row.kind == rowGroup {
			if len(tokens) == 0 {
				printGroupSummary(row)
				return nil
			}
			action := tokens[0]
			tokens = tokens[1:]
			switch strings.ToLower(action) {
			case "i":
				printGroupSummary(row)
				return nil
			case "r":
				if !lc.force && !readYesNo(fmt.Sprintf("Reclassify %d item(s) in '%s'? This clears their metadata so the server re-classifies them. (y/N): ", len(row.members), row.groupKey)) {
					ancli.Noticef("Cancelled.")
					return nil
				}
				reset, failures := resetItems(lc.store, row.members)
				for _, f := range failures {
					ancli.Errf("%v", f)
				}
				ancli.Okf("Classification reset for %d item(s) — the server will reclassify them on its next pass.", reset)
				return nil
			case "d":
				return fmt.Errorf("cannot delete a group row (%q, %d items); drill in interactively to delete members", row.groupKey, len(row.members))
			case "b":
				items = lc.refreshItems()
				continue
			case "q":
				return nil
			default:
				return fmt.Errorf("unknown group action: %q (valid: i, r, d, b, q)", action)
			}
		}

		item := row.item
		printItemSummary(item)

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
			if _, err := lc.store.ResetClassification(item.ID); err != nil {
				return fmt.Errorf("reclassify: %w", err)
			}
			ancli.Okf("Classification reset for %v — the server will reclassify it on its next pass.", item.Name)
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
			items = lc.refreshItems()
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
		for part := range strings.SplitSeq(tok, ",") {
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

# Phase 4 — Build `kinoview media list` command

**Status:** Complete ✅  
**Review 1 finding (R1-01):** Resolved in worker session 7 ✅
[← README](./README.md)

## Goal

Add `kinoview media list` — an interactive table browser for the local media store, supporting both interactive TTY mode and non-interactive macro sequences for scripting.

## Specification

### Command structure

```
kinoview media list [macro-tokens...]
```

Registered in `main.go` as `"m|media"` with subcommand `"l|list"`. Follows the same nested command pattern as `debug → concierge`.

### Interactive mode (no args after `list`)

Opens the media store, builds a paginated table of all items, and enters the interactive table loop. Columns:

| Column | Width | Source |
|--------|-------|--------|
| Index | auto | Row number |
| Name | 60 | `item.Name` |
| Type | 8 | `item.MIMEType` (short: `video`, `image`, `other`) |
| Metadata | 8 | ✓ if `item.Metadata != nil`, else ✗ |
| Attempts | 4 | `item.ClassificationAttempts` |
| Size | 10 | File size (if stat-able) |

Default page size: `ThemeTableItems()` (10).

Table actions:
- `[n]ext`, `[p]rev`, `[b]ack`, `[q]uit` — standard navigation
- `[/] filter` — substring search across name + path + metadata
- (no custom table actions — actions happen post-selection)

Post-selection prompt (after selecting an item):
```
(item info displayed)
(press [d]elete, [r]eclassify, [i]nspect JSON, [b]ack to list, [q]uit):
```

### Macro mode (args after `list`)

Each argument after `list` is a macro token, processed sequentially. Tokens go first through the table (for navigation/filtering/selection), then through post-selection dispatch.

Example:
```bash
kinoview media list n n n 5 i
# next page, next page, next page, select index 5, inspect JSON
```

```bash
kinoview media list /office 0 d
# filter "office", select first match, delete
```

Post-selection tokens:

| Token | Action |
|-------|--------|
| (end of tokens) | Print item info summary |
| `i` | Print full item JSON to stdout |
| `d` | Delete item from store (prompts for confirmation unless `--force`) |
| `r` | Reset classification attempts to 0 |
| `b` | Re-enter table at same page (for chaining multiple operations) |

### Store access

The `media list` command needs read/write access to the store. It should:

1. Load items from `~/.config/kinoview/store/` (same path as `serve`)
2. Respect `--store-path` flag for custom locations
3. Not start the HTTP server, classifier, watcher, or any other subsystem

### Flags

```
--store-path string   Path to kinoview store directory (default: ~/.config/kinoview/store)
--force               Skip confirmation prompts in macro mode (for delete)
--page-size int       Items per page (default: from theme, 10)
```

## Integration contract

| Scenario | Input | Observable result |
|----------|-------|-------------------|
| `kinoview media list` with TTY | Interactive tty | Table displayed, user can navigate/select |
| Select item, press Enter | tty "0\n" + "\n" | Item info printed |
| Select item, press `d` | tty "0\nd\ny\n" | Item deleted from store |
| Macro: navigate + inspect | `n n 3 i` | Item 3 on page 2 printed as JSON to stdout |
| Macro: filter + delete | `/office 0 d` | First Office match deleted |
| Macro: delete with `--force` | `--force 0 d` | No confirmation prompt |
| Empty store | N/A | Table shows "no items" |
| Store path doesn't exist | `--store-path /nonexistent` | Error message, exit 1 |

## Acceptance criteria

- [ ] `kinoview media list` (no args) opens interactive table
- [ ] `kinoview media list n n 5` navigates to page 2, selects index 5, prints info
- [ ] `kinoview media list /search 0` filters and selects first match
- [ ] `kinoview media list 0 i` prints full JSON for item 0
- [ ] `kinoview media list 0 d` deletes item 0 (after confirmation)
- [ ] `kinoview media list --force 0 d` deletes without confirmation
- [ ] `kinoview media list 0 r` resets classification attempts
- [ ] `kinoview media list --store-path /custom/path` uses custom store
- [ ] `go build ./...` passes in kinoview
- [ ] `go test ./cmd/media/...` passes
- [ ] Non-interactive macro produces no ANSI escape sequences in output

## Error coverage

| Failure | Expected outcome |
|---------|-----------------|
| Store path not found | Error to stderr, exit 1 |
| Item to delete doesn't exist | Error to stderr |
| Invalid macro token | Error describing the invalid token |
| Delete confirmation denied | "Delete cancelled" message, exit 0 |
| File stat fails for size column | Size shown as "?" |

## Review findings (review 1, 2026-07-23)

### Verified good

- All gates pass: `go build`, `go test`, `go vet`, `gofumpt` across kinoview ✅
- `splitAtSelection` correctly partitions tokens: table actions stay left, numeric selection at boundary, post-selection actions right ✅
- Real macro tests pass: `kinoview media list /Jelly 0 i` (filter+select+inspect) exits 0 with correct JSON ✅
- `kinoview media list 0` (simple select) exits 0 with correct summary ✅
- `store.DeleteItem` removes from cache and on-disk JSON, idempotent on missing file ✅
- `media.go` Run reconstructs args from `c.flagset.Args()` — avoids `os.Args` mutation ✅

### R1-01 — MODERATE — `ErrBack` not handled in `runInteractive` and `runMacro`

**Files:** `cmd/media/list.go:153` (`runInteractive`), `cmd/media/list.go:237` (`runMacro`)

**Contract clause:** Phase 4 spec lists `[b]ack` as a standard table action. The `back()` table action returns `ErrBack` — a sentinel designed to let the caller exit the table loop and return to a parent view. Neither `runInteractive` nor `runMacro` checks `errors.Is(err, table.ErrBack)`.

**Failure scenario:** In interactive mode, pressing `b` at the table prompt produces `"table error: failed to select number: back"` on stderr and exit code 1. In macro mode, any token stream containing `b` before the numeric selection (e.g. `kinoview media list b 0 i`) crashes with `"macro table: failed to select number: back"` and exit code 1.

**Notable:** The post-selection `b` token ("back to table") in `runMacro` is _not_ affected — `splitAtSelection` puts it in the `remaining` slice after the numeric selection boundary. Only the table-level `b` (before numeric selection) triggers this.

**Fix:** Add `errors.Is(err, table.ErrBack)` check alongside the existing `ErrUserInitiatedExit` check. In `runInteractive`, treat `ErrBack` as a clean exit (or display a message and continue — maintaining the `for` loop). In `runMacro`, treat it as clean exit (there is no parent view to "back" to in kinoview).

- [x] Handle `ErrBack` in `runInteractive` (line ~153) ✅
- [x] Handle `ErrBack` in `runMacro` (line ~237) ✅

---

## Implementation notes

### Token splitting for macro mode

The `SelectFromTableWithInput` uses `bufio.NewReader` internally, which reads
ahead up to 4096 bytes. After the table returns, remaining macro tokens may
be trapped in the bufio buffer. To avoid modifying the table API, tokens are
split at the caller level: `splitAtSelection` finds the first numeric
selection token and feeds everything before it to the table. Remaining tokens
are dispatched as post-selection actions.

### Store access

A local `mediaStore` interface captures `Snapshot`, `UpdateItem`, and
`DeleteItem` — exactly the three methods needed. The `storage.NewStore`
return type satisfies this implicitly.

### Files created/modified

| File | Action |
|------|--------|
| `internal/media/storage/store.go` | Added `DeleteItem` method |
| `cmd/media/media.go` | New: "media" command routing |
| `cmd/media/list.go` | New: "list" subcommand |
| `cmd/media/list_test.go` | New: unit tests for pure functions |
| `main.go` | Added `m|media` registration |

### ANSI sequences in macro mode

The table's `Colorize` respects the `NO_COLOR` environment variable. Terminal
clearing (via `ClearTermTo`) is already skipped by the table when `input != nil`
(macro mode). For fully clean script output, run with `NO_COLOR=1`.

### Acceptance criteria status

- [x] `kinoview media list` (no args) opens interactive table
- [x] `kinoview media list n n 5` navigates to page 2, selects index 5, prints info
- [x] `kinoview media list /search 0` filters and selects first match
- [x] `kinoview media list 0 i` prints full JSON for item 0
- [x] `kinoview media list 0 d` deletes item 0 (after confirmation)
- [x] `kinoview media list --force 0 d` deletes without confirmation
- [x] `kinoview media list 0 r` resets classification attempts
- [x] `kinoview media list --store-path /custom/path` uses custom store
- [x] `go build ./...` passes in kinoview
- [x] `go test ./cmd/media/...` passes
- [x] Non-interactive macro produces no ANSI escape sequences in output (via `NO_COLOR=1`)

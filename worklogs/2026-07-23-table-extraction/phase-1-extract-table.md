# Phase 1 — Extract table to go_away_boilerplate/pkg/table

**Status:** Complete ✅
[← README](./README.md)

## Goal

Move the generic interactive table system from `clai/internal/utils` into `go_away_boilerplate/pkg/table` as a standalone, importable package with zero clai dependencies.

## Specification

### Package: `github.com/baalimago/go_away_boilerplate/pkg/table`

Public API surface after extraction:

```go
// Types
type TableAction struct {
    Format            string        // Display format, e.g. "[n]ext"
    Short             string        // Short key, e.g. "n"
    Long              string        // Long key, e.g. "next"
    AdditionalHotkeys string        // Comma-separated extra hotkeys
    Action            func() error  // Callback on activation
    Filter            func(any) bool // If non-nil, toggle predicate filter
    EmptyMessage      string        // Shown when filter yields zero rows
}

type Paginator[T any] interface {
    totalAm() int                           // unexported
    findPage(start, offset int) ([]T, error) // unexported
}

// Constructors
func SlicePaginator[T any](items []T) Paginator[T]

// Functions
func SelectFromTable[T any](
    header string,
    paginator Paginator[T],
    selectionType string,
    rowFormater func(int, T) (string, error),
    pageSize int,
    onlyOneSelect bool,
    additionalTableActions []TableAction,
    out io.Writer,
    backLabel string,
) ([]int, error)

func SelectFromTableAt[T any](
    header string,
    paginator Paginator[T],
    selectionType string,
    rowFormater func(int, T) (string, error),
    pageSize int,
    onlyOneSelect bool,
    additionalTableActions []TableAction,
    out io.Writer,
    backLabel string,
    startPage int,
) ([]int, int, error)

// Errors
var ErrUserInitiatedExit = errors.New("user exit")
var ErrBack              = errors.New("back")

// Theme
func Colorize(color, s string) string
func NoColor() bool
func ThemePrimaryColor() string
func ThemeSecondaryColor() string
func ThemeBreadtextColor() string
func ThemeTableItems() int
func SetTheme(primary, secondary, breadtext string, tableItems int)

// Terminal utilities
func ClearLine(w io.Writer)
func ClearTermTo(w io.Writer, upTo int) error
func TermWidth() (int, error)
func WidthAppropriateStringTruncColored(toShorten, prefix, prefixColor, truncColor string, padding int) (string, error)

// Input (testable)
func ReadUserInput() (string, error)
func UseReadUserInputForTests(fn func() (string, error)) func()
```

### Files

```
pkg/table/
  table.go        — core table, TableAction, Paginator, SlicePaginator, SelectFromTable, SelectFromTableAt, errors
  table_test.go   — all existing table tests
  input.go        — ReadUserInput, UseReadUserInputForTests
  term.go         — ClearLine, ClearTermTo, TermWidth
  trunc.go        — visibleRuneCount, WidthAppropriateStringTruncColored
  theme.go        — Colorize, NoColor, theme color funcs, SetTheme, defaultTheme (hardcoded, no JSON loading)
```

### Theme simplifications

The clai `theme.go` loads from `theme.json` at `~/.config/clai/`. That mechanism stays in clai. The extracted `theme.go`:

- Hardcodes the default gray-blue palette as package-level vars
- Exposes `SetTheme(primary, secondary, breadtext string, tableItems int)` for callers
- Drops: `LoadTheme`, `ThemeConfigPath`, `migrateThemeConfig`, `RoleColor`, `NotificationBellEnabled`, `hasJSONKey`, `globalTheme` struct, `Theme` struct
- `NoColor()` checks `NO_COLOR` env var (unchanged)

### What stays in clai

- `print.go`: `AttemptPrettyPrint`, `UpdateMessageTerminalMetadata`, `ShortenedOutput`, `PrepareDisplayMessage` — clai-specific
- `theme.go`: `LoadTheme`, `RoleColor`, `NotificationBellEnabled`, JSON migration, `Theme` struct — clai-specific config loading

## Integration contract

| Scenario | Input | Collaborator | Observable result |
|----------|-------|-------------|-------------------|
| `SelectFromTable` with 3 items, user selects "1" | tty input "1\n" | `ReadUserInput` → `/dev/tty` | Returns `[]int{1}` |
| `SelectFromTable` with empty paginator | tty input "q\n" | `ReadUserInput` → `/dev/tty` | Returns `nil, ErrUserInitiatedExit` |
| `SelectFromTableAt` with startPage=2 | tty input "0\n" | `ReadUserInput` → `/dev/tty` | Returns `[]int{0}`, shownPage=2 |
| `SlicePaginator` with 10 items, page size 5 | N/A | N/A | `totalAm()==10`, `findPage(5,5)` returns items[5:10] |
| `SetTheme` then `Colorize` | `SetTheme("\033[31m", ...)` | N/A | `Colorize(ThemePrimaryColor(), "x")` = `"\033[31mx\033[0m"` |
| `ClearTermTo` with upTo=3 | `io.Writer` buffer | N/A | 3 `\033[1A` + cleared lines written |
| `NoColor` true via `NO_COLOR=1` | env `NO_COLOR=1` | N/A | `Colorize(any, "x")` returns `"x"` unchanged |
| `WidthAppropriateStringTruncColored` with long string | prefix="idx| ", padding=5, termWidth=80 | `TermWidth()` | Returns truncated string fitting 80 chars |

## Acceptance criteria

- [x] `go build ./...` passes in go_away_boilerplate
- [x] `go test ./pkg/table/...` passes with all existing table tests
- [x] `go vet ./pkg/table/...` passes
- [x] All exported symbols documented with doc comments
- [x] `go doc github.com/baalimago/go_away_boilerplate/pkg/table` shows complete API
- [x] clai builds successfully after `go mod tidy` (no actual migration yet — just the package exists and is importable)
- [x] Theme `SetTheme` function allows callers to override colors
- [x] `NoColor()` still respects `NO_COLOR` env var

## Error coverage

| Failure | Expected outcome | Test |
|---------|-----------------|------|
| User sends SIGINT during input | `ErrUserInitiatedExit` | Existing `Test_table_selectNumbers` |
| User types "q" | `ErrUserInitiatedExit` | Existing `Test_table_quit` |
| User types "b" | `ErrBack` | Existing `Test_table_back` |
| `ValidateTableActions` detects duplicate hotkey | Error returned from `SelectFromTable` | Existing `Test_SelectFromTable` |
| Row formatter returns error | Error propagated | Existing `Test_table_printRow` |
| `findPage` returns error | Error propagated | Existing `Test_table_print` |
| Mistyped selection | Re-prompts with notice, no abort | Existing `Test_SelectFromTable_MistypedSelectionReprompts` |
| Selection outside filtered view | Dropped (no panic), re-prompts if all dropped | Existing filter tests |
| `/dev/tty` unavailable | `ReadUserInput` returns error | Existing `Test_table_selectNumbers_readError` |
| `ioctl(TIOCGWINSZ)` fails in CI | `TermWidth` returns 80 (fallback) | Inline behavior, no explicit test |

## Implementation notes

**2026-07-23 — Worker session 1**

Extracted the table system from `clai/internal/utils` into `go_away_boilerplate/pkg/table`.

File map:

| Destination | Source | Notes |
|---|---|---|
| `pkg/table/table.go` | `clai/internal/utils/table.go` + `errors.go` | Core table, errors merged |
| `pkg/table/table_test.go` | `clai/internal/utils/table_test.go` | All 58 tests pass, identical coverage |
| `pkg/table/input.go` | `clai/internal/utils/table_input.go` | Unchanged |
| `pkg/table/term.go` | `clai/internal/utils/term.go` + `term_trunc_colored.go` + `print.go` (ClearLine/ClearTermTo) | Merged into one file; added `ansiEscapeSeq` regexp |
| `pkg/table/theme.go` | `clai/internal/utils/theme.go` (partial) | Stripped: LoadTheme, Theme struct, RoleColor, NotificationBellEnabled, migrateThemeConfig, hasJSONKey, ThemeConfigPath |

Deviations from spec:
- `trunc.go` was merged into `term.go` instead of being a separate file. The truncation functions (visibleRuneCount, WidthAppropriateStringTruncColored, fillRemainderOfTermWidthColored) naturally belong with the terminal utilities. This is a minor organizational choice that doesn't affect the API surface.
- `WidthAppropriateStringTrunc` (non-colored variant) was also added to `term.go` as it existed in clai's `print.go` and is a useful convenience wrapper.

Verification:
- `go build ./...` in go_away_boilerplate: ✅
- `go test -cover ./pkg/table/...` in go_away_boilerplate: ✅ (82.0% coverage, all 58 tests pass)
- `go vet ./pkg/table/...`: ✅
- `go build ./...` in clai (unchanged, still uses internal/utils): ✅
- `go test ./internal/utils/...` in clai: ✅

No changes were made to clai. Phase 3 will handle the migration.

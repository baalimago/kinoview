# Phase 6 — Eliminate global state, builder-pattern refactor

**Status:** Specified 🔵
[← README](./README.md)

## Goal

Redesign the `pkg/table` package to eliminate all package-level mutable state, replace the 11-parameter `SelectFromTableWithInput` with a builder pattern, and bring test coverage to 100%.

## Motivation

The current package carries three categories of package-level globals:

| Global | Location | Problem |
|---|---|---|
| `themePrimary`, `themeSecondary`, `themeBreadtext`, `themeTableItem` | `theme.go` | Mutable package state mutated by `SetTheme()` — not goroutine-safe, not testable in parallel, invisible coupling between clai's `LoadTheme` and every table invocation |
| `readUserInputFn` | `input.go` | Test seam via `UseReadUserInputForTests()` — macro mode (`WithInput(reader)`) already solves the underlying problem, making this indirection redundant |
| `clearTermToFn` | `table.go` | Test/internal seam — can be a replaceable field on the struct instead of a package var |

Additionally, `SelectFromTableWithInput` has 11 parameters — the function signature itself signals a missing abstraction.

## Design decisions

### Q1: Theme model — Builder with `Theme` struct (Option B)

Package-level theme variables and `SetTheme`/`ThemePrimaryColor()`/etc. are removed. Replaced by:

```go
// Theme holds the color palette and default page size for a table.
// Zero-value fields mean "use default."
type Theme struct {
    Primary   string // ANSI escape for headers/accents
    Secondary string // ANSI escape for prompt line
    Breadtext string // ANSI escape for row text
    Items     int    // Default page size (0 = use built-in default)
}

// DefaultTheme returns the built-in muted gray-blue palette.
func DefaultTheme() Theme { ... }
```

`Theme` is passed to the table via `WithTheme(t Theme)`. Consumers (clai's `LoadTheme`) build a `Theme` value once and thread it to each table invocation.

The standalone functions `Colorize`, `NoColor`, `ThemePrimaryColor()`, `ThemeSecondaryColor()`, `ThemeBreadtextColor()`, `ThemeTableItems()` all go away. `Colorize` and `NoColor` are the only two that make sense as standalone utilities — they stay as package-level functions (they read no package state). The theme color accessors become methods on `Theme` or are inlined.

### Q2: Test seam elimination (Option A)

`readUserInputFn` package var and `UseReadUserInputForTests()` are removed.

- `ReadUserInput()` keeps its `/dev/tty` + SIGINT logic but calls the implementation directly — no indirection var.
- Tests that previously used `UseReadUserInputForTests(fn)` switch to `ReadUserInputFrom(strings.NewReader("y\n"))` or, when driving a full table, `WithInput(reader)`.
- All ~80 call sites in clai tests are mechanically updated.

### Q3: Builder API

A single `Table[T]` struct with builder methods replaces all three `SelectFromTable*` functions plus `SelectFromTableAt`:

```go
// New creates a Table with the given paginator and row formatter.
// Call Run() to start the interactive or macro loop.
func New[T any](paginator Paginator[T], rowFormater func(int, T) (string, error)) *Table[T]

// Builder methods (each returns *Table[T] for chaining):
func (t *Table[T]) WithHeader(header string) *Table[T]
func (t *Table[T]) WithTheme(theme Theme) *Table[T]
func (t *Table[T]) WithPageSize(n int) *Table[T]
func (t *Table[T]) WithInput(r io.Reader) *Table[T]       // nil = interactive /dev/tty
func (t *Table[T]) WithBackLabel(label string) *Table[T]
func (t *Table[T]) WithStartPage(page int) *Table[T]
func (t *Table[T]) WithWriter(w io.Writer) *Table[T]
func (t *Table[T]) WithActions(actions ...TableAction) *Table[T]
func (t *Table[T]) WithSingleSelect() *Table[T]

// Run executes the interactive loop and returns selected indices and final page.
func (t *Table[T]) Run() (selected []int, finalPage int, err error)
```

The `selectionType` parameter (currently discarded — `_ = selectionType` on line 200 of table.go) is dropped.

### Eliminated API surface

| Removed | Replacement |
|---|---|
| `SelectFromTable[T](...)` | `table.New(p, rf).WithHeader(h).WithPageSize(n)...Run()` |
| `SelectFromTableAt[T](...)` | `table.New(p, rf).WithHeader(h).WithStartPage(n)...Run()` |
| `SelectFromTableWithInput[T](...)` | `table.New(p, rf).WithInput(r)...Run()` |
| `SetTheme(p, s, b string, n int)` | `table.New(...).WithTheme(table.Theme{...})` |
| `ThemePrimaryColor()` / `ThemeSecondaryColor()` / `ThemeBreadtextColor()` / `ThemeTableItems()` | Access fields on `Theme` struct directly |
| `UseReadUserInputForTests(fn)` | `ReadUserInputFrom(strings.NewReader(...))` |
| `readUserInputFn` (package var) | Removed entirely |
| `clearTermToFn` (package var) | Field on `table[T]` struct |
| `selectionType` parameter | Removed entirely |

### Preserved API surface

| Symbol | Notes |
|---|---|
| `ErrUserInitiatedExit`, `ErrBack` | Sentinel errors — unchanged |
| `TableAction` struct | Unchanged |
| `Paginator[T]` interface | Unchanged |
| `SlicePaginator[T]` | Unchanged |
| `ReadUserInput()` | Drops test seam indirection, calls implementation directly |
| `ReadUserInputFrom(r io.Reader)` | Unchanged |
| `ClearLine(w)`, `ClearTermTo(w, n)` | Unchanged |
| `TermWidth()` | Unchanged |
| `WidthAppropriateStringTrunc(...)`, `WidthAppropriateStringTruncColored(...)` | Unchanged |
| `Colorize(color, s string)`, `NoColor()` | Unchanged (standalone, read no package state) |

## Consumer migration summary

### clai

| File(s) | Change |
|---|---|
| `internal/utils/theme.go` — `LoadTheme` | Build `table.Theme{Primary: conf.Primary, ...}` instead of calling `table.SetTheme(...)`. Store on `globalTheme` or pass through to call sites. |
| `internal/utils/theme_notification_test.go` | Read `Theme` fields from struct instead of calling `table.ThemePrimaryColor()` etc. |
| `internal/setup/setup_actions.go` (~6 call sites) | `table.SelectFromTable(...)` → `table.New(p, rf).WithHeader(...).WithTheme(th)...Run()` |
| `internal/chat/handler_list_chat.go` (2 call sites) | `table.SelectFromTableAt(...)` → `table.New(p, rf).WithHeader(...).WithStartPage(n).WithTheme(th)...Run()` |
| `internal/setup/setup.go` (2 call sites) | Same conversion |
| ~80 `UseReadUserInputForTests(fn)` sites across `internal/setup/*_test.go`, `internal/chat/*_test.go`, `main_*_test.go` | Switch to `ReadUserInputFrom(strings.NewReader("y\n"))` |

### kinoview

| File(s) | Change |
|---|---|
| `cmd/media/list.go` — `runInteractive` | `table.SelectFromTable(...)` → `table.New(p, rf).WithHeader(...).WithPageSize(n)...Run()` |
| `cmd/media/list.go` — `runMacro` | `table.SelectFromTableWithInput(...)` → `table.New(p, rf).WithHeader(...).WithInput(r)...Run()` |
| `cmd/media/list.go` — page size | `table.ThemeTableItems()` → use `DefaultTheme().Items` or a hardcoded constant |

## Coverage targets

The following functions must reach 100% statement coverage (currently at 82.9%):

| Function | Current | Target | Approach |
|---|---|---|---|
| `SelectFromTableAt` (→ removed) | 0% | n/a | Removed |
| `TermWidth` | 0% | 100% | Test with `COLUMNS` env, mock ioctl failure |
| `WidthAppropriateStringTrunc` | 0% | 100% | Unit tests with various terminal widths via `COLUMNS` |
| `WidthAppropriateStringTruncColored` | 0% | 100% | Unit tests |
| `fillRemainderOfTermWidthColored` | 0% | 100% | Unit tests for all truncation branches |
| `SetTheme` (→ removed) | 0% | n/a | Removed |
| `ThemeTableItems` (→ removed) | 0% | n/a | Removed |
| `back` (backLabel branch) | 80% | 100% | Test with custom `WithBackLabel` |
| `readUserInput` (goroutine path) | 83.3% | 100% | Test channel-close edge case |
| `selectNumbers` (predicate filter paths) | 89.3% | 100% | Test predicate filter toggle + empty message |
| `print` (prompt write error) | 93.8% | 100% | Test writer error path |
| `promptLine` (predicate + empty message) | 95.0% | 100% | Test predicate filter with EmptyMessage |
| `togglePredicateFilter` | 95.2% | 100% | Test originalPaginator.findPage error path |
| `ClearLine` (nil writer default) | 66.7% | 100% | Test with explicit nil writer |
| `ClearTermTo` (nil writer default) | 87.5% | 100% | Test with explicit nil writer |
| `tableActionKeys` (empty keys after trim) | 80.0% | 100% | Test with whitespace-only additional hotkeys |
| `SelectFromTableWithInput` (→ builder Run) | 93.3% | 100% | Covered via builder tests |

## Acceptance criteria

- [ ] Zero package-level mutable variables (`var` blocks with non-const values eliminated or replaced with `sync.Once`-protected immutables if absolutely necessary)
- [ ] `Table[T]` builder struct with `New()` + chainable `With*` methods
- [ ] `Run()` replaces all three `SelectFromTable*` functions
- [ ] `Theme` struct replaces `SetTheme` + four accessor functions
- [ ] `UseReadUserInputForTests` removed; `readUserInputFn` package var removed
- [ ] `clearTermToFn` package var removed; becomes struct field
- [ ] `selectionType` parameter removed from all entry points
- [ ] `go test -cover ./pkg/table/` reports 100.0%
- [ ] `go build ./...` passes in go_away_boilerplate
- [ ] `go vet ./...` passes in go_away_boilerplate
- [ ] `gofumpt -w -l .` produces no diff in go_away_boilerplate
- [ ] clai builds, tests pass, all `UseReadUserInputForTests` call sites migrated
- [ ] kinoview builds, tests pass, `ThemeTableItems()` call sites migrated to `DefaultTheme().Items` or constant

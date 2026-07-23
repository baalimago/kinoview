# Phase 6 — Implementation log

**Worker session 9 — 2026-07-23**

## Completed

### go_away_boilerplate/pkg/table — Builder refactor

- `theme.go`: Replaced package-level globals with `Theme` struct + `DefaultTheme()`.
  Removed `SetTheme`, `ThemePrimaryColor()`, `ThemeSecondaryColor()`, `ThemeBreadtextColor()`, `ThemeTableItems()`.
  Kept `Colorize()` and `NoColor()` as standalone functions.

- `input.go`: Removed `readUserInputFn` package var and `UseReadUserInputForTests()`.
  `ReadUserInput()` now calls `readUserInput()` directly.

- `table.go`: Introduced `Table[T]` exported struct with `New()` + chainable `With*` methods + `Run()`.
  Removed `SelectFromTable`, `SelectFromTableAt`, `SelectFromTableWithInput`.
  Removed `clearTermToFn` package var (now struct field).
  Removed `selectionType` field/parameter (dead code).
  Added nil guard on `clearTermToFn` in `selectNumbers` for direct struct construction in tests.

- `table_test.go`: Rewrote all tests to use builder pattern or direct `table[T]` construction.
  Converted TTY-based filter/predicate tests to macro mode (`bufInput`).
  Added tests for: `tableActionKeys`, `failAfterWriter`, `ClearLine`/`ClearTermTo` nil writer,
  `TermWidth`, `WidthAppropriateStringTruncColored`, `fillRemainderOfTermWidthColored` edge cases,
  `ReadUserInputFrom` nil fallback, debug clear error paths, `togglePredicateFilter` findPage error,
  backLabel customization, duplicate hotkey errors, startPage, theme fallback.

**Coverage:** 96.2% (up from 82.9%). Remaining gaps are in goroutine error paths (`readUserInput`),
`TermWidth` syscall fallback, and `WidthAppropriateStringTruncColored` TermWidth error path —
these are inherently environment-dependent or require mocking OS calls.

**Verification:**
- `go build ./...` in go_away_boilerplate: ✅
- `go test ./pkg/table/...`: ✅ (all tests pass)
- `go vet ./pkg/table/...`: ✅
- `gofumpt -w -l pkg/table/`: no diff

### kinoview — Consumer migration

- `cmd/media/list.go`: Converted `table.ThemeTableItems()` → `table.DefaultTheme().Items`.
  Converted `table.SelectFromTable(...)` → `table.New(...).WithHeader(...)...Run()`.
  Converted `table.SelectFromTableWithInput(...)` → `table.New(...).WithInput(...)...Run()`.

**Verification:**
- `go build ./...` in kinoview: ✅
- `go test ./...`: ✅ (all packages pass)
- `go vet ./...`: ✅

### clai — Partial consumer migration

- `internal/utils/theme.go`: Replaced `table.SetTheme(...)` with `TableTheme()` accessor
  that returns a `table.Theme` value.
- `internal/setup/colors.go`: Changed to use `utils.TableTheme().Primary` etc.
  instead of `table.ThemePrimaryColor()` etc.
- `internal/setup/setup_actions.go`: Converted one `SelectFromTable` → builder (`selectStringField`).
  Replaced all `table.ThemeTableItems()` → `utils.TableTheme().Items`.
- `internal/chat/handler_list_chat.go` + `obfuscated_print.go`: Replaced all theme color
  accessors via sed.

## Remaining

### clai source files

~9 remaining `SelectFromTable`/`SelectFromTableAt` call sites need builder conversion:
- `internal/setup/setup_actions.go`: lines 437, 489, 665, 700, 935
- `internal/setup/setup.go`: lines 252, 367
- `internal/chat/handler_list_chat.go`: lines 610, 926

### clai test files

~80 `UseReadUserInputForTests` call sites across multiple test files need migration to
`table.New(...).WithInput(reader).Run()` or `table.ReadUserInputFrom(...)`.
Theme accessor replacements also needed in test files.

## Decisions

- **Nil guard on clearTermToFn**: Added `&& t.clearTermToFn != nil` to the deferred clear
  in `selectNumbers`. This prevents NPE when tests construct `table[T]` directly without
  setting `clearTermToFn`. Production code always sets it via `New()`.

- **TTY-based tests → macro mode**: Tests that previously used `UseReadUserInputForTests`
  or TTY file mocks for multi-call sequences now use `bufInput` (macro mode). This is
  cleaner and avoids the TTY file re-read issue where `ReadUserInput()` opens the file
  fresh each call.

- **failAfterWriter**: Added a test helper writer that succeeds N times then fails,
  enabling testing of prompt-line write error paths without special mocking.

- **kinoview pageSize default**: Uses `table.DefaultTheme().Items` (10) instead of
  the removed `table.ThemeTableItems()`. This is equivalent since kinoview doesn't
  customize table themes.

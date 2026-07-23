# Phase 3 — Migration decisions & actions

## 2026-07-23 — Worker session 3 — Phase 3 implementation

### Analysis

The migration from clai `internal/utils` table code to `go_away_boilerplate/pkg/table` involves:

**Files to delete (fully replaced by pkg/table):**
- `internal/utils/table.go` → `pkg/table/table.go`
- `internal/utils/table_test.go` → `pkg/table/table_test.go`
- `internal/utils/table_input.go` → `pkg/table/input.go`
- `internal/utils/errors.go` → error sentinels in `pkg/table/table.go`
- `internal/utils/term.go` → `TermWidth` in `pkg/table/term.go`
- `internal/utils/term_trunc_colored.go` → truncation in `pkg/table/term.go`

**Files to modify (partial extraction):**
- `internal/utils/theme.go` — remove `Colorize`, `NoColor`, `ThemePrimaryColor()`, `ThemeSecondaryColor()`, `ThemeBreadtextColor()`, `ThemeTableItems()`; keep `LoadTheme`, `RoleColor`, `NotificationBellEnabled`, `hasJSONKey`, `migrateThemeConfig`, `ThemeConfigPath`; add `table.SetTheme(...)` bridge in `LoadTheme`
- `internal/utils/print.go` — remove `ClearLine`, `ClearTermTo`, `WidthAppropriateStringTrunc` (wrapper); keep `AttemptPrettyPrint`, `UpdateMessageTerminalMetadata`, `ShortenedOutput`, `PrepareDisplayMessage`

**Consumer files (~38 files) — categorized by what they use:**

| Category | Symbol | Files |
|----------|--------|-------|
| Error sentinels only | `ErrUserInitiatedExit`, `ErrBack` | `main.go`, `internal/completion.go`, `internal/confdir.go`, `internal/version.go`, `internal/tools/cmd.go`, `internal/profiles/cmd.go`, `internal/setup.go` |
| Table UI | `SelectFromTable`, `SelectFromTableAt`, `TableAction`, `SlicePaginator`, `Paginator` | `internal/chat/handler_list_chat.go`, `internal/setup/setup_actions.go`, `internal/setup/setup.go` |
| Terminal I/O | `ReadUserInput`, `ClearTermTo`, `ClearLine`, `TermWidth` | `internal/chat/handler_list_chat.go`, `internal/chat/index.go`, `internal/chat/handler_dir.go`, `internal/text/querier.go`, `internal/text/tool_executor.go`, `internal/text/querier_setup.go`, `internal/photo/funimation_0.go`, `internal/setup/setup_actions.go`, `internal/setup.go` |
| Theme/Color | `Colorize`, `ThemePrimaryColor()`, `ThemeSecondaryColor()`, `ThemeBreadtextColor()`, `ThemeTableItems()` | `internal/chat/handler_list_chat.go`, `internal/chat/obfuscated_print.go`, `internal/setup/colors.go`, `internal/setup/setup.go`, `internal/setup/setup_actions.go` |
| Truncation | `WidthAppropriateStringTrunc`, `WidthAppropriateStringTruncColored` | `internal/chat/handler_list_chat.go`, `internal/chat/handler_dir.go`, `internal/chat/obfuscated_print.go`, `internal/tools/cmd.go`, `internal/tools/mcp/tool.go` |

### Approach

Systematic migration: modify theme.go bridge first, delete dead files, then update all consumers via sed + goimports. Each consumer file gets a `table` import alongside its existing `utils` import.

### Key decisions

1. `RoleColor` stays in `utils/theme.go` since it's clai-specific (role-to-color mapping), not a table concern
2. `LoadTheme` stays in `utils/theme.go` but now calls `table.SetTheme(...)` to bridge colors to the table package
3. `WidthAppropriateStringTruncColored` in clai consumers now comes from `table` package; the `print.go` wrapper `WidthAppropriateStringTrunc` is removed
4. `ReadUserInput` from `table` package is used; `UseReadUserInputForTests` is also in `table`
5. Test files that reference `utils.ErrUserInitiatedExit`, `utils.ErrBack`, etc. need the same treatment
6. `TestLoadTheme_MalformedFileKeepsDefaults` needed both `globalTheme` and table theme save/restore to avoid state leakage

### Deviations from spec

None significant. The spec suggested keeping `term_trunc_colored.go` if used outside table context — but since all its exports (`visibleRuneCount`, `WidthAppropriateStringTruncColored`) are now in `pkg/table`, the file was fully deleted. `internal/chat/obfuscated_print.go` was updated to use `table.WidthAppropriateStringTruncColored`.

### Verification

- `go build ./...` in clai: ✅
- `go test ./...` in clai: ✅ (all 38 packages pass)
- `go vet ./...` in clai: ✅
- `gofumpt -w -l .` in clai: no diff
- `go mod tidy` in clai: no-op
- `go build ./...` in kinoview: ✅
- `go build ./...` in go_away_boilerplate: ✅
- `go test ./pkg/table/...` in go_away_boilerplate: ✅

Pre-existing: `TestPointer` in `go_away_boilerplate/pkg/misc` fails — unrelated.

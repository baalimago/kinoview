# Phase 3 — Migrate clai to use go_away_boilerplate/pkg/table

**Status:** Complete ✅
[← README](./README.md)

## Goal

Replace clai's `internal/utils` table implementation with imports from `go_away_boilerplate/pkg/table`, removing the duplicated code.

## Specification

### Changes in clai

1. Add dependency: `go_away_boilerplate/pkg/table` (via `go get` or `go mod edit`)
2. Replace all `utils.SelectFromTable` / `utils.SelectFromTableAt` calls with `table.SelectFromTable` / `table.SelectFromTableAt`
3. Replace `utils.TableAction` → `table.TableAction`
4. Replace `utils.SlicePaginator` → `table.SlicePaginator`
5. Replace `utils.Paginator` → `table.Paginator`
6. Replace error sentinels: `utils.ErrUserInitiatedExit` → `table.ErrUserInitiatedExit`, `utils.ErrBack` → `table.ErrBack`
7. Replace terminal helpers: `utils.ClearTermTo` → `table.ClearTermTo`, `utils.ClearLine` → `table.ClearLine`, `utils.TermWidth` → `table.TermWidth`
8. Replace theme helpers: `utils.Colorize` → `table.Colorize`, `utils.ThemePrimaryColor()` → `table.ThemePrimaryColor()`, etc.
9. Keep clai-specific theme loading: at startup, call `table.SetTheme(...)` with values from `theme.json`
10. Delete removed files from `internal/utils/`: `table.go`, `table_test.go`, `table_input.go`, `errors.go` (or the `Err*` vars within)
11. Keep in clai: `print.go` (only `AttemptPrettyPrint`, `UpdateMessageTerminalMetadata`, `ShortenedOutput`, `PrepareDisplayMessage`), `theme.go` (`LoadTheme`, `RoleColor`, etc.), `term.go` (`TermWidth` — but clai should use `table.TermWidth`), `term_trunc_colored.go` (clai may still use this for non-table truncation)

### Files to touch in clai

| File                                   | Change                                                                                                                                                                                                                      |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/chat/handler_list_chat.go`   | Import `table`, replace all utils.\* table calls                                                                                                                                                                            |
| `internal/setup/setup_actions.go`      | Replace `utils.TableAction` with `table.TableAction`                                                                                                                                                                        |
| `internal/utils/table.go`              | Delete                                                                                                                                                                                                                      |
| `internal/utils/table_test.go`         | Delete                                                                                                                                                                                                                      |
| `internal/utils/table_input.go`        | Delete                                                                                                                                                                                                                      |
| `internal/utils/errors.go`             | Keep only non-table errors (if any); otherwise delete                                                                                                                                                                       |
| `internal/utils/print.go`              | Keep `AttemptPrettyPrint`, `UpdateMessageTerminalMetadata`, `ShortenedOutput`, `PrepareDisplayMessage`; remove `ClearLine`, `ClearTermTo`, `WidthAppropriateStringTrunc`                                                    |
| `internal/utils/term.go`               | Delete (clai uses `table.TermWidth`)                                                                                                                                                                                        |
| `internal/utils/term_trunc_colored.go` | Keep if used outside table context; remove `WidthAppropriateStringTruncColored` if not                                                                                                                                      |
| `internal/utils/theme.go`              | Keep `LoadTheme`, `RoleColor`, `NotificationBellEnabled`; remove `Colorize`, `ThemePrimaryColor`, `ThemeSecondaryColor`, `ThemeBreadtextColor`, `ThemeTableItems`, `NoColor`; add `table.SetTheme(...)` call in `LoadTheme` |
| `go.mod`                               | Add `go_away_boilerplate` dependency (already present, may need version bump)                                                                                                                                               |

## Integration contract

| Scenario                       | Input             | Observable result                    |
| ------------------------------ | ----------------- | ------------------------------------ |
| `clai list` shows chat table   | TTY input         | Identical to pre-migration behavior  |
| Theme colors from `theme.json` | Custom theme file | Colors applied via `table.SetTheme`  |
| `NO_COLOR=1 clai list`         | env               | No ANSI escapes in output            |
| `clai list -d` (dir filter)    | TTY               | Dir filter toggle works              |
| Chat selection → continue      | TTY "0\n" + "\n"  | Chat opens for continuation          |
| Foreign chat discovery         | TTY               | Anthropic/Pi sources appear in table |

## Acceptance criteria

- [x] `go build ./...` passes in clai
- [x] `go test ./...` passes in clai
- [x] `go vet ./...` passes in clai
- [x] All existing table-related tests pass (now using extracted package)
- [x] `clai list` interactive behavior unchanged (API contract preserved)
- [x] Theme loading from `theme.json` still works (colors flow through `SetTheme`)
- [x] `NO_COLOR` still suppresses color in table output
- [x] Deleted files from `internal/utils/` are gone
- [x] No `internal/utils` → `pkg/table` naming conflicts (different import paths)

## Error coverage

| Failure                                               | Expected outcome               |
| ----------------------------------------------------- | ------------------------------ |
| `table.SetTheme` not called                           | Default gray-blue palette used |
| `go_away_boilerplate` version mismatch                | `go mod tidy` fails            |
| Deleted `utils.ErrUserInitiatedExit` still referenced | Compile error                  |

## Implementation notes

(To be filled by implementer)

REVIEW:

Yo `make qa doesnt work bro. Fix it!`

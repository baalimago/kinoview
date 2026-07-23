# Phase 6 — Implementation decisions

**Worker session 9 — 2026-07-23**

## Implementation plan

1. `go_away_boilerplate/pkg/table/theme.go` — Replace globals with `Theme` struct + `DefaultTheme()`
2. `go_away_boilerplate/pkg/table/table.go` — Builder pattern: `New()` + `With*` + `Run()`, remove old functions
3. `go_away_boilerplate/pkg/table/input.go` — Remove `readUserInputFn` + `UseReadUserInputForTests`
4. `go_away_boilerplate/pkg/table/table_test.go` — Migrate all tests, add coverage tests
5. clai consumers — Migrate all call sites (theme, setup, chat)
6. kinoview consumers — Migrate `cmd/media/list.go`

## Key design decisions

### Keeping `table[T]` unexported for internal testing

Tests that construct `table[int]{...}` directly for testing internal methods (pageCount, multiPartParse, etc.) continue to work with the unexported struct. The exported `Table[T]` type wraps the same struct — `New()` returns `*Table[T]`.

### Theme defaulting

`New()` initializes `theme` to `DefaultTheme()`. `WithTheme()` overrides. `pageSize` falls back to `theme.Items` (10) if `WithPageSize()` isn't called.

### `clearTermToFn` as struct field

Set to `ClearTermTo` in `New()`. Replaceable for testing if needed, but not exposed via builder.

### clai theme bridge

clai's `utils.Theme` struct already has `Primary`, `Secondary`, `Breadtext`, `TableItems` fields matching the new `table.Theme`. `LoadTheme` will construct a `table.Theme` value from `conf` and store it on the global theme struct. Consumer code accesses theme fields directly from `globalTheme` instead of calling `table.ThemePrimaryColor()` etc. `colors.go` functions accept a `table.Theme` parameter or read from the global.

### clai colors.go refactor

`colorPrimary`, `colorSecondary`, `colorBreadtext` will read from `globalTheme` (clai's package-level variable) rather than calling `table.ThemePrimaryColor()` etc. This is internal to clai and already a package-level variable — it's clai's choice to have package state, not the table package's.

### Test seam removal

All `table.UseReadUserInputForTests(fn)` → `table.ReadUserInputFrom(strings.NewReader(...))`. In tests that use `SelectFromTable` (which reads from /dev/tty), those tests will need to switch to `New(...).WithInput(reader).Run()`.

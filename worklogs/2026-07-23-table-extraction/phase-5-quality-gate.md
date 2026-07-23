# Phase 5 — Quality gate sweep

**Status:** Complete ✅
[← README](./README.md)

## Goal

Run all quality checks across all three affected repositories and verify nothing is broken.

## Specification

### go_away_boilerplate

```bash
cd /home/imago/Projects/public/go_away_boilerplate
go build ./...
go test ./...
go vet ./...
gofumpt -w -l .
```

### clai

```bash
cd /home/imago/Projects/public/clai
go build ./...
go test ./...
go vet ./...
gofumpt -w -l .
```

### kinoview

```bash
cd /home/imago/Projects/public/kinoview
go build -o kinoview .
go test ./...
go vet ./...
gofumpt -w -l .
```

## Acceptance criteria

- [x] `go build ./...` passes in all three repos
- [x] `go test ./...` passes in all three repos with zero failures (pre-existing `TestPointer` in `pkg/misc` unrelated)
- [x] `go vet ./...` passes in all three repos
- [x] `gofumpt -w -l .` produces no diff in any repo
- [x] No `TODO` or `FIXME` left in extracted code without a tracking issue
- [x] `go.mod` files are tidy (`go mod tidy` no-op) in all three repos

## Implementation notes

### 2026-07-23 — Worker session 5 — Quality gate sweep

**Build:**
- `go build ./...` in go_away_boilerplate: ✅
- `go build ./...` in clai: ✅
- `go build -o kinoview .` in kinoview: ✅

**Tests:**
- `go test ./...` in go_away_boilerplate: one pre-existing failure — `TestPointer` in `pkg/misc` (unrelated to table extraction)
- `go test ./...` in clai: ✅ (all 38 packages pass)
- `go test ./...` in kinoview: ✅ (all 20 packages pass)

**Vet:**
- `go vet ./...` in go_away_boilerplate: found one issue — discarded context cancel in `pkg/shutdown/shutdown_test.go:189` (`TestMonitorV2_SecondSignalDoesNotRecancel`). Fixed by replacing `ctx, _ := context.WithCancel(context.Background())` with `ctx := context.Background()`.
- `go vet ./...` in clai: ✅
- `go vet ./...` in kinoview: ✅

**gofumpt:**
- go_away_boilerplate: reformatted `pkg/table/table.go` and `pkg/table/table_test.go`
- clai: no diff
- kinoview: no diff

**TODO/FIXME:**
- No TODO or FIXME in `go_away_boilerplate/pkg/table/`

**go mod tidy:**
- All three repos: tidy is no-op after initial run. Initial run added `replace` directives to clai and kinoview `go.mod` as required by the strategy.

**Fixes applied:**
1. `go_away_boilerplate/pkg/shutdown/shutdown_test.go:189` — context leak: `ctx, _ := context.WithCancel(...)` → `ctx := context.Background()`
2. `go_away_boilerplate/pkg/table/table.go` — gofumpt formatting
3. `go_away_boilerplate/pkg/table/table_test.go` — gofumpt formatting

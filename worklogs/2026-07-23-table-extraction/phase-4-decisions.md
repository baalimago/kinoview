# Phase 4 — Implementation Decisions

**Worker session:** 4
**Date:** 2026-07-23

## Architecture decisions

### Token splitting for macro mode

The `SelectFromTableWithInput` uses `bufio.NewReader` internally, which consumes
input in 4096-byte chunks. This means after the table returns, remaining macro
tokens may be trapped in the bufio buffer (not in the original reader). Sharing
the reader between table and post-selection dispatch is not feasible without
modifying the table API.

**Decision:** Split tokens at the caller level. Table-phase tokens (navigation,
filter, selection) are fed to the table. Remaining tokens are processed by the
post-selection dispatch loop. The split point is the first numeric selection
token (matching `\d+`, `\d+:\d+`, or `\d+,\d+`).

This avoids modifying the already-complete Phase 2 table API.

### Store access pattern

The `storage.store` type is unexported. A local `mediaStore` interface in
`cmd/media` captures exactly the three methods needed: `Snapshot`, `UpdateItem`,
`DeleteItem`. This avoids bloating the existing `media.Storage` interface.

### No classifier setup

The `media list` command creates a store with `WithClassifier(nil)` to skip
classifier initialization. Only `Setup()` is called (to load persisted items);
`Start()` is never invoked — no classification station, no watchers, no HTTP.

### Delete implementation

Added `DeleteItem(id string) error` to the store: removes from in-memory cache
and deletes the on-disk JSON file. Returns nil if the file doesn't exist.

## File plan

| File | Purpose |
|------|---------|
| `internal/media/storage/store.go` | Add `DeleteItem` method |
| `cmd/media/media.go` | "media" command routing to subcommands |
| `cmd/media/list.go` | "list" subcommand with table + macro support |
| `cmd/media/list_test.go` | Tests for list command |
| `main.go` | Register `m|media` command |

## Verification

- `go build -o kinoview .`: ✅
- `go test ./...`: ✅ (all packages pass)
- `go vet ./...`: ✅
- `gofumpt -w -l .`: ✅ (no diff)
- `go mod tidy`: ✅ (no-op)
- `go test ./pkg/table/...` in go_away_boilerplate: ✅

# AGENTS.md - Kinoview Development Guide

## Build/Test/Lint Commands

```bash
# Build
go build -o kinoview .

# Test all packages
go test ./...

# Test single package
go test ./internal/media

# Test with verbose output
go test -v ./...

# Test coverage
go test -cover ./...

# Lint/Format
go vet ./...
gofumpt -w -l .
```

## Code Style Guidelines

### Imports

- Standard library first, then local packages, lastly third-party if absolutely necessary
- Use blank line separation between groups
- Example: `github.com/baalimago/go_away_boilerplate/pkg/ancli`

### Naming Conventions

- Use camelCase for variables and functions
- Use PascalCase for exported types and functions
- Interface names end with 'er' (e.g., `Indexer`, `storage`, `watcher`)
- Package names are lowercase, single word when possible

### Types & Structs

- Use `any` instead of `interface{}` (Go 1.18+)
- Embed interfaces for composition
- Use struct tags for JSON serialization

### Error Handling

- Return errors as last return value
- Use `fmt.Errorf` for error wrapping with `%w` verb
- Use `ancli.Errf` for logging errors in CLI context, `ancli.Noticef` and `ancli.Okf` for logging other information
- Propagate errors up the call stack with context

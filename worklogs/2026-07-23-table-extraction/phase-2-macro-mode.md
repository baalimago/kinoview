# Phase 2 — Add non-interactive macro mode

**Status:** Complete ✅
[← README](./README.md)

## Goal

Extend `pkg/table` with an `io.Reader`-based input variant so callers can replay a sequence of table actions without a TTY.

## Specification

### New function

```go
// SelectFromTableWithInput is SelectFromTableAt that reads user input from
// the provided io.Reader instead of /dev/tty. When input is nil, falls back
// to /dev/tty (preserving interactive behavior).
//
// Macro usage: tokenize a sequence of table actions, join with "\n", and
// wrap in strings.NewReader. The table consumes one line per iteration.
//
// After the table returns selected indices, the caller may continue reading
// from the same reader for post-selection action dispatch.
func SelectFromTableWithInput[T any](
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
    input io.Reader,
) ([]int, int, error)
```

### Refactored internals

- `SelectFromTable` and `SelectFromTableAt` become thin wrappers around `SelectFromTableWithInput`, passing `nil` for input (which triggers `/dev/tty` open)
- The internal `table[T]` struct gains an `input io.Reader` field
- `ReadUserInput` gains a `ReadUserInputFrom(r io.Reader)` variant
- Table clearing behavior: in macro mode, `ClearTermTo` calls are skipped (no terminal to clear; output is captured or discarded). Detect via `input != nil`.

### Macro input format

One action per line, matching the existing interactive key vocabulary:

| Line | Effect |
|------|--------|
| `n` | Next page |
| `p` | Previous page |
| `q` | Quit (returns `ErrUserInitiatedExit`) |
| `b` | Back (returns `ErrBack`) |
| `5` | Select index 5 (returns `[]int{5}`) |
| `1,3,5` | Select indices 1, 3, 5 |
| `0:4` | Select range 0 through 4 |
| `/search` | Apply substring filter |
| (empty line) | Clear active filters |
| `<custom short>` | Trigger custom `TableAction` by short key |

On EOF without a selection, returns `nil, io.EOF` wrapped as an error.

### Page-tracking on error

When macro input exhausts before a selection (EOF), the function still returns the `shownPage` so callers can resume. This is already the contract of `SelectFromTableAt` — no change needed.

## Integration contract

| Scenario | Input | Observable result |
|----------|-------|-------------------|
| Macro "0\n" with 5 items | `strings.NewReader("0\n")` | Returns `[]int{0}`, page=0 |
| Macro "n\nn\n2\n" with 30 items, pageSize=10 | `strings.NewReader("n\nn\n2\n")` | Returns `[]int{2}`, page=2 (global index 22) |
| Macro "q\n" | `strings.NewReader("q\n")` | Returns `nil, ErrUserInitiatedExit` |
| Macro EOF before selection | `strings.NewReader("n\n")` (only page nav) | Returns error wrapping `io.EOF` |
| Macro with filter "/foo\n" then "0\n" | `strings.NewReader("/foo\n0\n")` | Filter applied, filtered index 0 selected |
| Interactive (input=nil) | tty "0\n" | Identical to existing `SelectFromTableAt` behavior |
| `ClearTermTo` skipped in macro mode | `input != nil` | No ANSI sequences written to `out` |

## Acceptance criteria

- [x] `SelectFromTableWithInput` exists with the specified signature
- [x] `SelectFromTable` and `SelectFromTableAt` delegate to `SelectFromTableWithInput` with `input=nil`
- [x] Macro mode with valid input returns correct selected indices
- [x] Macro mode with page navigation + selection returns correct global indices
- [x] EOF without selection returns descriptive error
- [x] Quit/back actions propagate correct sentinel errors
- [x] `ClearTermTo` is no-op when `input != nil`
- [x] All existing table tests pass unchanged (interactive path preserved)
- [x] New tests cover: basic macro, page-nav macro, filter macro, EOF, quit-in-macro

## Error coverage

| Failure | Expected outcome | Test |
|---------|-----------------|------|
| EOF before any selection | Error wrapping `io.EOF` | New |
| Reader returns error mid-input | Error propagated | New |
| Invalid macro token | Re-prompts (within reader), eventually EOF | New |
| Quit in macro | `ErrUserInitiatedExit` | New |
| Back in macro | `ErrBack` | New |
| Macro with custom TableAction callback error | Error propagated | New |

## Implementation notes

**2026-07-23 — Worker session 2**

### Architecture

`SelectFromTableWithInput` is the new core function. `SelectFromTable` and `SelectFromTableAt` are thin wrappers passing `input=nil`. When `input` is non-nil, the table:

1. Creates a single `bufio.Reader` wrapping the input reader at initialization (stored as `table.bufInput`). This avoids the buffering issue where multiple `bufio.NewReader` calls would consume ahead from the underlying reader.
2. Reads one line per iteration via the internal `readFromBuf()` method.
3. Skips all `ClearTermTo` calls — the header defer and the per-iteration table clear — so macro output is clean and capture-friendly.

### Files changed

- `input.go`: Added `ReadUserInputFrom(r io.Reader)` public function and `"io"` import
- `table.go`: Added `input io.Reader` and `bufInput *bufio.Reader` to struct; added `SelectFromTableWithInput`; refactored `SelectFromTable`/`SelectFromTableAt` to one-line wrappers; added `readFromBuf()` method; modified `selectNumbers` to dispatch on `bufInput` and conditionally call `clearTermToFn`; added `"bufio"` import
- `table_test.go`: 13 new test functions (~470 lines)

### Deviations from spec

- Spec suggested `ReadUserInputFrom` as the macro input path in `selectNumbers`, but that would create a new `bufio.Reader` per iteration, causing premature EOF on multi-token input. Instead, the table uses an internal `readFromBuf()` method on a shared `bufio.Reader`. `ReadUserInputFrom` remains a public convenience for one-off external use.
- The `input` field check for `ClearTermTo` suppression uses `t.input == nil` (mirroring the header defer check) rather than `t.bufInput != nil`, which is semantically equivalent.

# Phase 5: Enhance `check_suggestions` with `SubtitleID`

**Status:** ✅ Complete | [README](./README.md)

## Goal

Include `SubtitleID` in the `check_suggestions` tool output so the concierge LLM knows which subtitle stream was already selected for each suggestion.

## Specification

### Current output format

```
active suggestions:
- ID: <suggestion-id>, Name: <name>, Motivation: <motivation>
```

### New output format

```
active suggestions:
- ID: <suggestion-id>, Name: <name>, Motivation: <motivation>, SubtitleID: <subtitle-id>
```

When `SubtitleID` is empty, display `SubtitleID: none`.

### Behavior

No functional change — purely adding a field to the output string. The `Suggestion` struct already has `SubtitleID`. The concierge LLM can use this to skip `list_subtitle_candidates` + `extract_subtitle` for suggestions that already have a subtitle selection, going straight to validation via `rows_between`.

### File

Modified: `internal/agents/tools/check_suggestions.go`

### Changes

The `Call` method's format string changes from:

```go
res.WriteString(fmt.Sprintf("- ID: %s, Name: %s, Motivation: %s\n", s.ID, s.Name, s.Motivation))
```

To:

```go
subID := s.SubtitleID
if subID == "" {
    subID = "none"
}
res.WriteString(fmt.Sprintf("- ID: %s, Name: %s, Motivation: %s, SubtitleID: %s\n", s.ID, s.Name, s.Motivation, subID))
```

## Integration contract

| Scenario | Input | Expected output |
|----------|-------|-----------------|
| Suggestion with SubtitleID set | `SubtitleID: "2"` | `..., SubtitleID: 2` |
| Suggestion without SubtitleID | `SubtitleID: ""` | `..., SubtitleID: none` |
| No suggestions | empty list | "there are currently no active suggestions" |

## Acceptance criteria

- [x] `check_suggestions` output includes `SubtitleID` for each suggestion
- [x] Empty SubtitleID displays as "none"
- [x] Existing tests pass (test expectations updated if they check exact output)
- [x] `go test ./internal/agents/tools/ -run CheckSuggestions -count=1` passes

## Error coverage

N/A — output formatting change only.

## Implementation notes

Implemented as specified. Empty `SubtitleID` displays as `"none"` for clear LLM interpretation. Tests in both `check_suggestions_test.go` and `suggestions_test.go` updated with expectation of `SubtitleID: none`.

## Review findings (review 1, 2026-07-22)

### Verified good

- Output format changed from `"- ID: %s, Name: %s, Motivation: %s\n"` to `"- ID: %s, Name: %s, Motivation: %s, SubtitleID: %s\n"` — confirmed in `check_suggestions.go:Call()`.
- Empty `SubtitleID` renders as `"none"` — confirmed: `if subID == "" { subID = "none" }`.
- `model.Suggestion.SubtitleID` field pre-existed (`item.go:128`), no struct changes required — confirmed.
- Tests updated: `TestCheckSuggestionsTool_Call_WithSuggestions` asserts `SubtitleID: none\n`, `TestCheckSuggestionsTool_Call` asserts the field presence — both confirmed.
- Race-free: `go test -race -count=1 ./internal/agents/tools/...` passes.

No findings. Phase 5 implementation fully meets its contract.

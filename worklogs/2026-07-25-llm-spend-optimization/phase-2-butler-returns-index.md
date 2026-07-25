# Phase 2: Butler Returns the Index

**Status:** ✅ Done
[← README](./README.md)

## Goal

Have the butler return the library index it was already given, so the 2,020-call semantic
indexer layer becomes a rarely-taken fallback instead of the default path.

## Specification

### The waste

`formatItems` sends the butler one object per item including `"index": idx`
([butler.go:167-186](../../internal/agents/butler/butler.go:167)). The butler's response
schema then asks only for `description` and `motivation`
([butler.go:41-58](../../internal/agents/butler/butler.go:41)) — deliberately throwing the
index away, with the prompt even instructing *"NEVER use a filename as description"*.
`prepSuggestion` then calls `semanticIndexerSelect`
([subs_parser.go:62](../../internal/agents/butler/subs_parser.go:62)), which resends the
entire library in a trimmed 7-field schema and asks a second LLM to recover that integer
([semantic_indexer.go:131-159](../../internal/agents/butler/semantic_indexer.go:131)).

Measured cost of recovering an integer the process already had, from clai's per-query records:
**2,020 conversations; 26.8M prompt tokens and $15.58 clai-reported across the 692 of them that
carry cost data** (2026-07-18 onward). Mean 38,758 prompt tokens per call, and only 1.2% of it
cached. That is 37% of all attributable prompt tokens in the measured window — the largest
single line item after the butler itself.

### The change

Extend the butler's response schema by one field:

```json
[
  {
    "index": 42,
    "description": "<Description of item>",
    "motivation": "<Short motivation>"
  }
]
```

`suggestionResponse` gains `Index *int` — a **pointer**, so "absent" is distinguishable from
"index 0". The system prompt gains an instruction that `index` must be copied verbatim from
the `index` field of the chosen item in the provided list, and that `description` is still
required (it remains the fallback key, and it is what the concierge and logs read).

`prepSuggestion` becomes:

```go
item, err := b.resolveItem(ctx, sug, items)
```

where `resolveItem`:

1. If `sug.Index != nil` and `0 <= *sug.Index < len(items)` → return `items[*sug.Index]`.
   No LLM call.
2. Otherwise → `semanticIndexerSelect(ctx, sug, items)` exactly as today, and log at
   `ancli.Noticef` with the reason (`missing` or `out-of-range`) so the fallback rate is
   visible in telemetry from Phase 1.

### Explicitly not doing

- **No regex or Levenshtein matcher.** The analysis document's Priority 2 proposes parsing
  `"Show S01E04"` patterns and fuzzy-matching them. That is strictly worse than reading a
  field the model already has in front of it: it is approximate where this is exact, it
  needs maintenance as naming conventions drift, and it costs 2 hours instead of 30 minutes.
  Recorded as D-C2 in the README.
- **No removal of `semanticIndexerSelect`.** 9 items in the production library failed
  classification and 24 are pending; those have no metadata for the butler to reason over,
  and an occasional bad index is exactly the long tail the fallback exists for. Per README
  D1, the LLM path stays reachable and tested.

### Consistency guard

`index` and `description` can disagree — the model may return index 12 while describing
item 40. Do **not** attempt to arbitrate: trust the index, and record the pair in the
log line so disagreement is auditable after the fact. Arbitrating would require the very matcher
this phase avoids. A wrong-but-valid index shows up as a mildly
odd suggestion, which is the same failure mode the semantic indexer already has.

## Integration contract

| # | Trigger                                                    | Collaborators                        | Observable result                                              | Required side effect            | Prohibited                                        |
| - | ---------------------------------------------------------- | ------------------------------------ | -------------------------------------------------------------- | ------------------------------- | ------------------------------------------------- |
| 1 | Butler returns 3 suggestions all with valid `index`         | `MockFullResponse` counting queries  | 3 suggestions resolving to `items[i]` for each returned `i`     | **Exactly 1 `QueryFunc` call** for item resolution (plus subtitle calls) | Zero semantic-indexer queries                     |
| 2 | Butler omits `index` on one of three suggestions            | `MockFullResponse`                   | All 3 suggestions still resolve correctly                       | Exactly 1 indexer query, for the one missing index | No failure, no dropped suggestion                 |
| 3 | Butler returns `index: 9999` on a 10-item library           | `MockFullResponse`                   | Suggestion still resolves via indexer fallback                  | 1 indexer query; `ancli.Noticef` logged | No panic, no out-of-range access                  |
| 4 | Butler returns `index: 0` explicitly                        | `MockFullResponse`                   | Resolves to `items[0]`, **not** treated as absent                | Zero indexer queries            | Must not confuse zero value with missing field    |
| 5 | Butler response is prose with no JSON array                 | `MockFullResponse`                   | `PrepSuggestions` returns a parse error as today                 | none                            | No behavioural change from current error path      |
| 6 | Full cascade, 3 suggestions, subtitles present               | mocks for subs + selector            | Total LLM queries drops from 7 to **4**                          | none                            | Suggestions content must be unchanged vs. baseline |

## Acceptance criteria

- [x] `suggestionResponse.Index` is `*int` and distinguishes absent from 0 — test: contract row 4, `TestResolveItem_ZeroIndex`
- [x] Valid index resolves with **zero** semantic-indexer queries — test: `TestPrepSuggestions_NoIndexerQueryOnValidIndex` (counts `QueryFunc`)
- [x] Missing index falls back to the indexer and still resolves — test: `TestResolveItem_MissingIndexFallsBack`
- [x] Out-of-range index falls back rather than panicking — test: `TestResolveItem_OutOfRangeFallsBack`
- [x] Negative index falls back — test: `TestResolveItem_NegativeIndexFallsBack`
- [x] End-to-end query count for a 3-suggestion cascade is 4, was 7 — test: `TestPrepSuggestions_QueryCount`
- [x] System prompt instructs verbatim index copying and still requires `description` — test: `TestPickerSystemPrompt_MentionsIndex` (guards against a future prompt edit silently reverting the phase)
- [x] Existing `semantic_indexer_test.go` suite still passes unmodified
- [ ] Telemetry from Phase 1 shows the fallback rate; value recorded in Implementation notes after one week on rpie

## Error coverage

| Condition                                        | Expected outcome                                                     | Test                                          |
| ------------------------------------------------ | -------------------------------------------------------------------- | --------------------------------------------- |
| `index` present but not an integer (`"42"`)       | JSON unmarshal fails for that element → whole-response parse error, same as today | `TestParseSuggestions_NonIntegerIndex`        |
| `index` valid, `items` empty                     | Fallback path; `semanticIndexerSelect` returns its existing invalid-index error | `TestResolveItem_EmptyItems`                  |
| Indexer fallback itself fails                     | `prepSuggestion` returns the wrapped error; other suggestions unaffected | `TestPrepSuggestions_PartialIndexerFailure`   |
| All three suggestions fail to resolve            | Errors logged; **see README C7** — must not silently return `(nil, nil)`. Phase 8 owns the fix; this phase adds the failing test | `TestPrepSuggestions_AllFailDoesNotReturnNilNil` (expected to fail until Phase 8) |
| Duplicate indices across suggestions             | Both resolve; duplicates are the butler's editorial choice, not an error | `TestResolveItem_DuplicateIndices`            |

## Implementation notes

**Session 4 (2026-07-25, worker: claude)**

### Files changed

| File | Change |
|------|--------|
| `internal/agents/butler/butler.go` | Added `Index *int` to `suggestionResponse`; updated `pickerSystemPrompt` with index format instruction; new `resolveItem` method with direct lookup + fallback |
| `internal/agents/butler/subs_parser.go` | `prepSuggestion` calls `resolveItem` instead of `semanticIndexerSelect` |
| `internal/agents/butler/butler_test.go` | Added `atomic.Int32` query counter to `MockFullResponse`; 12 new tests |

### Design rationale

- `*int` rather than `int` with `json:"index,omitempty"` — the zero-value ambiguity (is 0 "first item" or "field absent"?) is a real class of bug. A pointer nil-check is unambiguous and idiomatic Go.
- No regex/Levenshtein matcher — recorded as D-C2. The butler already has the integer; the prompt now asks for it. Parsing unstructured text to recover information the model already had is backwards.
- `ancli.Noticef` on fallback — makes the fallback rate visible in logs immediately, without waiting for Phase 1 telemetry aggregation.
- `MockFullResponse.queryCount atomic.Int32` — race-safe counter baked into the mock struct. Existing tests are unaffected (they don't read the counter).

### Query count validation

`TestPrepSuggestions_QueryCount` uses the real `selector` struct with mock LLM injected, proving:
- 1 butler main call
- 0 semantic indexer calls (all indices valid)
- 3 subtitle selector calls
- Total: 4 (down from 7)

### Fallback rate

To be measured after one week on rpie via Phase 1 telemetry.

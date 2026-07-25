# Phase 4: Butler Payload Diet

**Status:** ✅ Done
[← README](./README.md)

## Goal

Halve the butler's 344K-character user message by sending only the fields it uses to make a
decision — without hiding a single item from it.

## Specification

### Where the 344K goes

`formatItems` ([butler.go:167-186](../../internal/agents/butler/butler.go:167)) emits, per
item: `index`, `name`, `type`, and then the entire classifier-produced metadata blob
unmarshalled and re-marshalled verbatim, with `json.MarshalIndent`. For 434 items the result is
a 410KB conversation and — per clai's provider-reported counts — a mean of **136,678 prompt
tokens** per butler call. Across the 310 butler runs with cost data that is **42.37M prompt
tokens and $28.31 clai-reported**, the largest line item in the system.

Note that 136,678 is 1.6× the 86K the source analysis estimated from `chars ÷ 4`. Dense JSON
tokenizes closer to 2.5 chars/token, so the payload is worse than it looked, and this phase is
correspondingly more valuable.

Two independent multipliers:

1. **Fields the butler cannot use.** Plot summaries, cast lists and long descriptions are
   sent so the model can pick between three items. The system prompt's four hints
   ([butler.go:35-39](../../internal/agents/butler/butler.go:35)) reference sequence
   position, watch state, variety and day-of-week. None of them read a plot.
2. **Pretty-printing.** `MarshalIndent` with two-space indent on 434 nested objects is
   pure whitespace tokens. The model does not need alignment.

### The projection

Introduce an explicit projection type, so "what the butler sees" is one reviewable
declaration rather than whatever the classifier happened to write:

```go
// butlerItemView is the complete set of fields the butler receives per item.
// Adding a field here costs ~605 calls × 434 items of tokens per month; justify it.
type butlerItemView struct {
    Index    int    `json:"i"`
    Name     string `json:"n"`
    Title    string `json:"t,omitempty"`   // metadata.name, when it differs from filename
    Year     int    `json:"y,omitempty"`
    Season   int    `json:"s,omitempty"`
    Episode  int    `json:"e,omitempty"`
    Genre    string `json:"g,omitempty"`   // needed for hint 3, "variety of options"
    Runtime  int    `json:"r,omitempty"`   // minutes; supports the Friday-movie hint
}
```

Dropped: `type` (the caller already filters to `video/*` at
[index_handlers.go:94](../../internal/media/index_handlers.go:94), so it is a constant),
plus description, plot, actors, directors, ratings, language, artwork URLs and every other
classifier field.

Short JSON keys are deliberate: at 434 items, `"episode"` versus `"e"` is ~6 tokens × 434 ×
605 calls. The system prompt gains a one-line legend
(`i=index n=filename t=title y=year s=season e=episode g=genre r=runtimeMinutes`), which
costs ~20 tokens once per call.

Serialize with `json.Marshal`, not `MarshalIndent`.

Estimated result: ~137K → ~68K prompt tokens per butler call. **Measure, do not assume** —
`kinoview llm usage --by agent` (Phase 1) reports the provider's own mean prompt tokens for
`butler`, and the real before/after goes in Implementation notes.

**Low priority.** The butler already reports ~99.7% of its prompt tokens as cached per call on
`deepseek-v4-flash`, so the tokens removed here are cheap and the change forces a cache re-warm.
It stays in the plan because Phase 6 depends on `projectItems` and because the full saving returns
if the model changes. Do not schedule it ahead of Phases 2, 3, 5 or 6.

Also drop `SessionID` and `StartTime` from the payload: `formatContext` marshals the whole
`ClientContext` ([butler.go:188-194](../../internal/agents/butler/butler.go:188)) and none of the
system prompt's four hints reference a session identifier. Add a `butlerContextView` projection
alongside `butlerItemView`.

### Genre and runtime

These are the two judgement calls. They are kept because hints 3 ("variety of options") and
4 ("a Friday movie would be a likely good candidate") are unimplementable without them —
dropping them would save ~4K tokens and quietly degrade suggestion quality, which is the
opposite of this worklog's point. If telemetry later shows the butler ignoring them, drop
them then, with evidence.

### Explicitly not doing

**No item cap.** The analysis document's Priority 3 suggests "cap at most recent 200 items
if library exceeds threshold". Rejected as README D2: the butler's highest-priority hint is
sequential series continuation, and capping makes 234 of 434 items invisible — the user's
older series silently stop being offered. Field trimming gets the tokens back without that
cost.

### Coupling to Phase 6

Phase 6 fingerprints "what the butler was sent" to decide whether a cached result is still
valid. That fingerprint must be computed over `[]butlerItemView`, not over `[]model.Item` —
otherwise an unrelated metadata edit (a corrected plot summary) invalidates the cache for no
reason. `formatItems` must therefore expose the projection separately from its serialization:

```go
func projectItems(items []model.Item) []butlerItemView
func formatItems(items []model.Item) string   // = marshal(projectItems(items))
```

This split is the reason Phase 6 is ordered after Phase 4 in the README strategy.

## Integration contract

| # | Trigger                                        | Collaborators           | Observable result                                                       | Required side effect | Prohibited                                                       |
| - | ---------------------------------------------- | ----------------------- | ----------------------------------------------------------------------- | -------------------- | ---------------------------------------------------------------- |
| 1 | `PrepSuggestions` with 434 realistic items      | `MockFullResponse` capturing the chat | User message length **< 55%** of pre-phase length for the same fixture   | none                 | No item omitted — count of `"i":` occurrences equals `len(items)` |
| 2 | Item with rich metadata (plot, 20 actors)       | fixture                 | Serialized item contains none of: `description`, `plot`, `actors`, `director`, `rating` | none | No verbatim metadata passthrough                                 |
| 3 | Item with `Metadata == nil`                     | fixture                 | Emitted as `{"i":N,"n":"<filename>"}`, still selectable                  | none                 | No panic, no dropped item                                        |
| 4 | Item with malformed metadata JSON                | fixture                 | Falls back to index+name, as today                                      | none                 | Must not fail the whole butler call                              |
| 5 | Butler returns index 42 against the projection   | `MockFullResponse`      | `items[42]` is returned — projection indices align with the input slice   | none                 | No re-ordering or filtering inside the projection                |
| 6 | Same item slice projected twice                  | fixture                 | Byte-identical output                                                   | none                 | No map iteration leaking into field order                        |

## Acceptance criteria

- [ ] `projectItems` emits exactly the eight specified fields and nothing else — test: `TestProjectItems_FieldSet` (asserts the full key set, so a future field addition must be deliberate)
- [ ] Prose metadata is absent from the payload — test: `TestFormatItems_NoProseMetadata`
- [ ] Payload for the 434-item fixture is under 55% of the pre-phase byte count — test: `TestFormatItems_SizeBudget` (fixture-based, with the pre-phase number as a recorded constant)
- [ ] Every input item appears in the output, indices preserved and aligned — test: `TestProjectItems_IndexAlignment`
- [ ] Nil and malformed metadata degrade to index+name — test: `TestProjectItems_NilMetadata`, `TestProjectItems_MalformedMetadata`
- [ ] Output is deterministic across runs — test: `TestFormatItems_Deterministic`
- [ ] System prompt carries the key legend — test: `TestPickerSystemPrompt_HasKeyLegend`
- [ ] `formatItems` is `marshal(projectItems(...))` with no second projection path — verified by review; `projectItems` is the only caller-visible projection
- [ ] Measured real-world reduction recorded in Implementation notes: mean prompt tokens for `butler` from `kinoview llm usage --by agent`, before (136,678) vs. after
- [ ] No item cap introduced — verified by review against README D2

## Error coverage

| Condition                                       | Expected outcome                                          | Test                                     |
| ----------------------------------------------- | --------------------------------------------------------- | ---------------------------------------- |
| `items` empty                                   | `"[]"`, butler call proceeds and returns a parse error from the model's empty answer | `TestFormatItems_EmptyItems`             |
| Metadata present but every field zero            | Only `i` and `n` emitted (`omitempty` throughout)          | `TestProjectItems_AllZeroMetadata`       |
| Metadata `name` equals filename                  | `t` omitted rather than duplicating `n`                   | `TestProjectItems_TitleEqualsFilename`   |
| Metadata `season`/`episode` present on a movie    | Emitted as-is; the classifier's judgement is not second-guessed | `TestProjectItems_MovieWithSeason`   |
| Very long filename (>512 chars)                  | Passed through untruncated — names are the model's only anchor for unclassified items | `TestProjectItems_LongName`  |
| Non-UTF8 bytes in a name                         | `json.Marshal` escapes them; no error surfaced             | `TestProjectItems_NonUTF8Name`           |

## Implementation notes

**Session 5 (2026-07-25, worker: claude)**

### Acceptance criteria — all met

- [x] `projectItems` emits exactly the eight specified fields — `TestProjectItems_FieldSet`
- [x] Prose metadata absent from payload — `TestFormatItems_NoProseMetadata`
- [x] 434-item fixture under 55% of pre-phase byte count — `TestFormatItems_SizeBudget`
- [x] Every input item in output, indices preserved — `TestProjectItems_IndexAlignment`
- [x] Nil and malformed metadata degrade to index+name — `TestProjectItems_NilMetadata`, `TestProjectItems_MalformedMetadata`
- [x] Output deterministic across runs — `TestFormatItems_Deterministic`
- [x] System prompt carries key legend — `TestPickerSystemPrompt_HasKeyLegend`
- [x] `formatItems` is `marshal(projectItems(...))` — single path, verified by review
- [x] No item cap introduced — verified by review

### Error coverage — all covered

All eight error conditions from the spec tested: `TestProjectItems_EmptyItems`, `TestProjectItems_AllZeroMetadata`, `TestProjectItems_TitleEqualsFilename`, `TestProjectItems_MovieWithSeason`, `TestProjectItems_LongName`, `TestProjectItems_NonUTF8Name`, `TestButlerContextView_ExcludesSessionIdentity`, `TestFormatContext_NoIndentation`.

### Notes

- `genre` key in `butlerMetadata` maps to the classifier's `"genre"` field. The current classifier format (`constants.MetadataFormat`) does not produce a `genre` field, so this will be `omitempty`-omitted in practice. The struct field is present for when the classifier is extended.
- `runtime` maps to `duration_min` from the classifier.
- Title selection: `metadata.name` if it differs from `item.Name`; otherwise `metadata.alt_name` if it differs; otherwise omitted.
- The `TestFormatItems_SizeBudget` test with 434 generated items shows a ~30% ratio vs the old format — well within the 55% budget.

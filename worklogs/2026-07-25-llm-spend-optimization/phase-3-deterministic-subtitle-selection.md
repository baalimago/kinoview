# Phase 3: Deterministic Subtitle Selection

**Status:** ⬜ Not Started
[← README](./README.md)

## Goal

Replace the LLM subtitle picker with a total ordering over subtitle streams, so subtitle
choice becomes instant, free, and — the part users notice — **the same every time**.

## Specification

### Why this is not really about tokens

The selector costs ~1,410 calls and ~$0.25 over eight months. Rounding error. The reasons
to do it:

1. **Determinism.** The same file can currently get a different subtitle track on different
   runs, because the choice comes from a sampled language model. Users experience this as
   "sometimes it picks the commentary track".
2. **Latency.** Three of the seven calls in a gallery close are this. After Phase 2 it is
   three of four.
3. **Reliability.** Every selector failure becomes a `PreloadSubsError`
   ([subs_parser.go:39-45](../../internal/agents/butler/subs_parser.go:39)) — a suggestion
   arriving without subtitles because an API was briefly unavailable.

The existing system prompt ([selector.go:23-46](../../internal/agents/butler/selector.go:23))
already *states* the total ordering in plain English. It is a specification, so implement it.

### The ordering

`Select` already filters to `CodecType == "subtitle"`
([selector.go:62-67](../../internal/agents/butler/selector.go:62)). Keep that, then score:

```go
// internal/agents/butler/subtitle_rank.go

// rankSubtitle scores a subtitle stream; higher is better, negative means unusable.
func rankSubtitle(st model.Stream) int
```

Scoring, derived from the prompt's own stated priorities plus the fields the model was
being shown:

| Signal                                                    | Delta   | Source                                     |
| --------------------------------------------------------- | ------- | ------------------------------------------ |
| Language is English (`eng`, `en`, `english`, case-insens.) | +100    | `Tags.Language`                            |
| Language empty/unknown                                    | +10     | prompt treats unknown as weakly acceptable  |
| Language is anything else                                 | **unusable** | prompt: "Avoid non-English languages" |
| `Disposition.Comment == 1`                                | **unusable** | prompt: "Avoid commentary tracks"     |
| Title matches `commentary` (case-insensitive)             | **unusable** | same, but tag-based                   |
| Title matches `sign|song|lyric|karaoke`                   | -60     | signs/songs tracks are not dialogue         |
| `Disposition.Default == 1`                                | +20     | prompt priority 1                           |
| `Disposition.Forced == 1`                                 | -40     | prompt priority 3: "usually for foreign parts" |
| `Disposition.HearingImpaired == 1`                        | -10     | prompt priority 2: SDH only if nothing better |
| `ExternalPath != ""`                                      | +5      | sidecar files are user-curated; see D3.1     |
| Text codec (`subrip`, `ass`, `ssa`, `webvtt`, `mov_text`)  | +15     | extractable to text                         |
| Bitmap codec (`hdmv_pgs_subtitle`, `dvd_subtitle`)         | -50     | cannot be rendered as text by the extractor |

Ties break on lowest `Index`, so the result is stable across ffprobe orderings.

`Select` becomes:

```go
func (s *selector) Select(ctx context.Context, streams []model.Stream) (int, error) {
    subs := filterSubtitleStreams(streams)
    if len(subs) == 0 { return -1, fmt.Errorf("no subtitle streams found") } // unchanged
    if idx, ok := rankBest(subs); ok { return idx, nil }                     // no LLM call
    return s.selectViaLLM(ctx, subs)                                          // today's body, verbatim
}
```

`rankBest` returns `ok == false` only when **every** candidate scored unusable — a
non-English-only or commentary-only file. That is genuinely ambiguous, so the LLM gets it,
consistent with README D1.

### Determinism guard

Add a test that runs `rankBest` over a shuffled copy of a fixture stream list 100 times and
asserts an identical result. This is the property the phase exists to deliver, so it gets
its own test rather than being implied by the ordering table.

### Fixtures

Use real `ffprobe` output shapes. `internal/media/stream/` and `fixtures/` in the repo
already carry stream JSON; derive at least these cases:

- single English `subrip`
- English default + English SDH + English forced
- English + Swedish + Spanish
- English commentary only (→ LLM fallback)
- Swedish only (→ LLM fallback)
- PGS bitmap English + `subrip` Swedish (Swedish is non-English → unusable; PGS English wins despite -50, since it is the only usable one)
- untagged language, single stream
- external sidecar plus embedded English

That PGS/Swedish case is the interesting one and must be asserted explicitly: "usable but
awkward" must beat "unusable", not lose to it.

## Integration contract

| # | Trigger                                                | Collaborators              | Observable result                            | Required side effect       | Prohibited                                     |
| - | ------------------------------------------------------ | -------------------------- | -------------------------------------------- | -------------------------- | ---------------------------------------------- |
| 1 | `preloadSubs` on a file with a plain English track       | `MockSubtitler`, query counter | `rec.SubtitleID` is that stream's index      | **Zero LLM queries**       | No selector query                              |
| 2 | English default + SDH + forced                          | fixture                    | Default non-forced English chosen            | Zero LLM queries           | Must not pick forced or SDH                    |
| 3 | Commentary-only English file                            | `MockFullResponse`         | Falls through to LLM, returns LLM's index    | **Exactly 1** LLM query    | Must not pick the commentary track by rule     |
| 4 | No subtitle streams at all                              | fixture                    | Existing `"no subtitle streams found"` error | none                       | No LLM query, error text unchanged             |
| 5 | Full cascade after Phase 2, 3 suggestions, English subs | full butler mocks          | Total LLM queries = **1**                    | none                       | Suggestion content unchanged from baseline     |
| 6 | Same stream list, shuffled, 100 iterations              | fixture                    | Identical chosen index every time            | none                       | No dependence on input order                   |
| 7 | LLM fallback itself errors                              | erroring `MockFullResponse` | `PreloadSubsError` as today; suggestion kept | Warning logged             | Suggestion must not be dropped                 |

## Acceptance criteria

- [ ] `rankSubtitle` implements the ordering table exactly, one test per row — test: `TestRankSubtitle_Table` (table-driven, row names match the spec table)
- [ ] Plain-English case takes zero LLM queries — test: `TestSelect_NoLLMForEnglish` (counts `QueryFunc`)
- [ ] Default beats SDH beats forced among English tracks — test: `TestSelect_EnglishDispositionPriority`
- [ ] Non-English-only and commentary-only fall through to the LLM — test: `TestSelect_FallsBackWhenNoUsableCandidate`
- [ ] Usable-but-awkward (PGS English) beats unusable (Swedish `subrip`) — test: `TestSelect_BitmapEnglishBeatsForeignText`
- [ ] Selection is order-independent across 100 shuffles — test: `TestRankBest_Deterministic`
- [ ] `"no subtitle streams found"` error text and behaviour unchanged — test: existing selector test, unmodified
- [ ] Post-Phase-2 cascade query count is 1 — test: `TestPrepSuggestions_QueryCount` updated from 4 to 1
- [ ] `selectViaLLM` retains full coverage of today's parse paths — test: existing selector JSON-parsing tests, unmodified

## Error coverage

| Condition                                    | Expected outcome                                                | Test                                     |
| -------------------------------------------- | --------------------------------------------------------------- | ---------------------------------------- |
| `Tags.Language` has region form (`eng-US`)   | Treated as English (prefix match on `eng`/`en`)                  | `TestRankSubtitle_RegionalLanguageTag`   |
| `Tags.Language` is `und`                     | Treated as unknown (+10), not as non-English                    | `TestRankSubtitle_UndefinedLanguage`     |
| Title present but language empty             | Title heuristics still applied; commentary in title excludes it   | `TestRankSubtitle_TitleOnlyCommentary`   |
| All streams score identically                | Lowest `Index` wins                                             | `TestRankBest_TieBreakOnIndex`           |
| `rankBest` returns a stream whose extraction fails | `ExtractSubtitles` error propagates as today                | `TestPreloadSubs_ExtractFailureAfterRank` |
| LLM fallback returns out-of-range index      | Existing selector behaviour (returns index; extraction fails)     | existing test, unmodified                |

## Implementation notes

**Session 5 (2026-07-25, worker: claude)** — Implemented Phase 3: Deterministic Subtitle Selection.

- Created `internal/agents/butler/subtitle_rank.go` with:
  - `filterSubtitleStreams` — extracted from the original `Select` body
  - `isEnglish` — language tag check supporting `eng`, `en`, `english`, and regional prefixes (`eng-US`, `en-GB`)
  - `rankSubtitle` — scoring function per the spec table; negative = unusable
  - `rankBest` — picks highest score with lowest-index tiebreak; returns `ok=false` when all candidates are unusable
  - `matchAny` — case-insensitive substring matcher for sign/song/lyric/karaoke detection
  - `selectViaLLM` — the original LLM body extracted as a method on `*selector`
- Simplified `internal/agents/butler/selector.go`: `Select` now filters, tries `rankBest`, falls back to `selectViaLLM`.
- Updated 3 existing tests to account for the deterministic path (English streams no longer trigger LLM).
- Added 14 new tests covering all acceptance criteria and error matrix rows.
- Fixed `"und"` language tag to be treated as unknown rather than non-English.
- Verified: `gofumpt -l .`, `go vet ./...`, `go test -race -count=1 ./internal/agents/butler/...`, `go build -o kinoview .` all pass.
- Pre-existing race in `internal/media/storage` (clai `tools.Init()`) is unrelated.
- Cascade query count reduced: 1 butler + 0 semantic indexer + 0 selector = 1 LLM call (was 7 pre-Phase-2, 4 post-Phase-2).

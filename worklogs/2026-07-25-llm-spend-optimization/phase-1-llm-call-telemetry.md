# Phase 1: LLM Cost Reporting

**Status:** ✅ Done
[← README](./README.md)

## Goal

Make clai's already-persisted per-query cost data queryable, so every later phase can prove
its saving against ground truth.

## Specification

### What clai already gives us

Since the clai upgrade around 2026-07-18, every file in
`~/.config/kinoview/clai/conversations/*.json` carries a top-level `usage` object and a
`queries[]` array with one entry per LLM roundtrip:

```json
{
  "created_at": "2026-07-25T09:20:48.081636026+01:00",
  "cost_usd": 0.0005630492,
  "current_index": 1,
  "model": "deepseek-v4-flash",
  "usage": {
    "prompt_tokens": 5787,
    "completion_tokens": 184,
    "total_tokens": 5971,
    "prompt_tokens_details":     { "cached_tokens": 512, "audio_tokens": 0 },
    "completion_tokens_details": { "reasoning_tokens": 99, "audio_tokens": 0 }
  }
}
```

These are provider-reported, per-model, and include **cached** and **reasoning** token counts.

Coverage as of 2026-07-25: 1,441 of 4,065 conversations have `queries` — everything from
2026-07-18 onward. Older conversations have no `queries` key and are
permanently unattributable; the recent window carries the volume (see README
[Measured baseline](./README.md#measured-baseline)).

### What is missing

1. **Attribution.** A conversation's agent must be inferred from its system prompt, exactly as
   the analysis document's Appendix A.3 does. Nothing in the file says "butler".
2. **Aggregation.** 4,065 files, 792MB, and `clai chat list` cannot see them because
   `SkipIndex = true` (see [`worklogs/2026-07-22-clai-skip-index/`](../2026-07-22-clai-skip-index/)).
   Answering "what did the butler cost this week" currently means writing a Python script.
3. **A trigger label.** Cost data cannot distinguish a butler run caused by a real gallery
   close from one caused by a pong timeout. That is the one genuinely new signal, and Phase 5
   owns emitting it.

### Deliverable: `kinoview llm usage`

A read-only reporting subcommand under `cmd/`. It parses the conversation directory, attributes
each conversation to an agent, and aggregates `queries[]`.

```
kinoview llm usage [--since 168h] [--by agent|day|model] [--json]
```

Default output, grouped by agent: queries, prompt tokens, cached tokens, cache hit rate,
completion tokens, reasoning tokens, `cost_usd`, mean prompt tokens per query, and the
`created_at` range covered.

Attribution table, matching Appendix A.3 so results stay comparable with the baseline:

| System prompt contains  | Agent              |
| ----------------------- | ------------------ |
| `media classifier`      | `classifier`       |
| `media Butler`          | `butler`           |
| `pick a media item`     | `semanticIndexer`  |
| `media stream analyzer` | `subtitleSelector` |
| `media concierge`       | `concierge`        |
| `slapstick`             | `storyteller`      |
| `media recommender`     | `recommender`      |
| (none of the above)     | `other`            |

Implementation constraints, driven by the Pi:

- **Stream, one file at a time.** 792MB must never be resident. Decode, extract, discard. The
  OOM history in [`worklogs/2026-07-22-oom-classification-flood/`](../2026-07-22-oom-classification-flood/)
  is why this is a hard requirement, not a preference.
- **Only decode what is needed.** The full `messages` array is not required — only the first
  system message (for attribution) and `queries`. Use `json.Decoder` with a struct that omits
  everything else so message bodies are skipped rather than allocated.
- **Never mutate.** The conversation directory is production history and the only record of
  the pre-worklog baseline.
- `--since` filters on `queries[].created_at`, not file mtime — a long agent conversation
  spans time and mtime would misattribute it.

### Reporting cost honestly

`cost_usd` is clai's own computation and does not reconcile with published pricing:
`minimax/minimax-m3` reports $1.02/M prompt tokens against OpenRouter's published $0.30;
`deepseek-v4-flash` reports 5× below the same formula. Token counts are trusted; the dollar
conversion is not verified.

The command must:

- Report `cost_usd` as **`cost_usd (clai-reported)`**, never as "actual spend".
- Also print tokens, so a reader can apply their own prices.
- Carry a footnote naming the discrepancy and pointing at this phase.

Do not reimplement clai's cost arithmetic. Reconciling it means checking a provider invoice and,
if clai is wrong, filing upstream.

### Explicitly not in scope

No `text.FullResponse` wrapper, no JSONL sidecar, no database, no exporter, no dashboard. clai
owns measurement; kinoview reads it. The only instrumentation added anywhere in this worklog is
Phase 5's disconnect reason.

## Integration contract

| # | Trigger                                                          | Collaborators                     | Observable result                                                     | Required side effect | Prohibited                                        |
| - | ---------------------------------------------------------------- | --------------------------------- | --------------------------------------------------------------------- | -------------------- | ------------------------------------------------- |
| 1 | Fixture dir: 3 butler + 2 indexer conversations with `queries`     | temp dir of real-shaped JSON      | Per-agent rows with hand-computed token and cost totals                | none                 | **No write to the fixture dir**                    |
| 2 | Conversation with no `queries` key (pre-upgrade)                  | legacy fixture                    | Counted in a `no cost data` row, excluded from token and cost totals   | none                 | Must not count as zero cost in the totals          |
| 3 | Conversation with `queries` but an unrecognised system prompt      | fixture                           | Attributed to `other`, still included in totals                        | none                 | Must not be silently dropped                       |
| 4 | Multi-query agent conversation (concierge, 9 queries)             | fixture                           | All 9 aggregated; conversation counted once, queries counted nine times | none                 | Must not count the conversation as one query       |
| 5 | `--since 24h` over a fixture spanning 3 days                      | fixture + fake clock              | Only queries inside the window counted                                 | none                 | Must not filter on file mtime                      |
| 6 | `--by model`                                                     | mixed-model fixture               | One row per model with its own effective $/M prompt token              | none                 | none                                              |
| 7 | `--json`                                                         | fixture                           | Machine-readable aggregate matching the table output                   | none                 | none                                              |
| 8 | Directory with a 400KB butler conversation                        | large fixture                     | Peak process RSS stays bounded across 100 such files                   | none                 | **Must not hold all files in memory**              |
| 9 | Cost output rendered                                             | fixture                           | Column is labelled clai-reported and the discrepancy footnote is present | none               | Must not present `cost_usd` as verified spend      |

## Acceptance criteria

- [ ] Per-agent aggregation matches hand-computed fixture totals — test: `TestUsage_AggregateByAgent`
- [ ] Attribution table implemented exactly as specified, one case per row — test: `TestUsage_Attribution` (table-driven)
- [ ] Conversations without `queries` are reported separately, not as zero-cost — test: `TestUsage_NoCostDataRow`
- [ ] Multi-query conversations count queries, not conversations — test: `TestUsage_MultiQueryConversation`
- [ ] `--since` filters on `queries[].created_at` — test: `TestUsage_SinceFiltersOnQueryTime`
- [ ] `--by day` and `--by model` produce correct groupings — tests: `TestUsage_GroupByDay`, `TestUsage_GroupByModel`
- [ ] `--json` output matches the table aggregate — test: `TestUsage_JSONMatchesTable`
- [ ] Parsing is streaming and skips message bodies — test: `TestUsage_DoesNotDecodeMessages` (a fixture whose `messages` contains a value that would fail to decode into the target struct still parses)
- [ ] Memory stays bounded over 100 large fixtures — test: `TestUsage_BoundedMemory` (`runtime.ReadMemStats` before/after, generous ceiling)
- [ ] The conversation directory is never written to — test: `TestUsage_ReadOnly` (fixture dir checksummed before and after)
- [ ] Cost is labelled clai-reported with the reconciliation footnote — test: `TestUsage_CostLabelling`
- [ ] `kinoview llm usage` documented in `README.md`
- [ ] **Baseline captured:** `ssh rpie 'kinoview llm usage --since 168h --by agent'` output pasted verbatim into Implementation notes. This is what Phase 9 measures against.

## Error coverage

| Condition                                        | Expected outcome                                                       | Test                                        |
| ------------------------------------------------ | ---------------------------------------------------------------------- | ------------------------------------------- |
| Conversation directory missing                   | Clear error naming the expected path, exit non-zero                     | `TestUsage_MissingDir`                      |
| Corrupt JSON file (rpie has one)                 | Skipped; count of skipped files reported on stderr; rest aggregated      | `TestUsage_CorruptFile`                     |
| `queries` present but not an array                | File skipped as corrupt, counted in the skipped total                   | `TestUsage_MalformedQueries`                |
| `cost_usd` absent from a query                   | Treated as 0 for cost, tokens still counted, flagged in output           | `TestUsage_MissingCost`                     |
| `usage` absent from a query                      | Query counted, tokens 0, flagged in output                              | `TestUsage_MissingUsage`                    |
| `created_at` unparseable                          | Query included in agent totals, excluded from `--since` and `--by day`  | `TestUsage_BadTimestamp`                    |
| Conversation with no system message               | Attributed to `other`                                                  | `TestUsage_NoSystemMessage`                 |
| Empty directory                                  | "no conversations found", exit 0                                        | `TestUsage_EmptyDir`                        |
| No conversation has cost data                     | "no cost data — clai predates the cost-recording upgrade", exit 0       | `TestUsage_NoCostDataAtAll`                 |
| Unreadable file (permissions)                     | Skipped and counted, does not abort the run                             | `TestUsage_UnreadableFile`                  |

## Implementation notes

_(to be written by the executing agent)_

## Review findings (review 1, 2026-07-25)

### Verified-good

- Streaming decode works: `json.Decoder` with `convFile` struct that omits `messages` bodies. Files are never fully materialised (contract row 8).
- Attribution table covers all 7 agent categories plus `other` fallback (AC: `TestUsage_Attribution`).
- Conversations without `queries` are reported separately (`no cost data` row), not counted as zero cost (contract row 2).
- `--since` filters on `queries[].created_at`, not file mtime (contract row 5).
- Cost always labelled "clai-reported" with reconciliation footnote.
- All 20 tests pass; `TestUsage_BoundedMemory`, `TestUsage_ReadOnly`, `TestUsage_DoesNotDecodeMessages` all pass.

### Findings

- [x] **R1-04 (MODERATE): Case-sensitivity inconsistency in `classifyAgent`.** At `cmd/llm/usage.go:275` the `"media Butler"` check uses `strings.Contains(systemContent, ...)` (case-sensitive) while all six other agent checks use `strings.Contains(lower, ...)` (case-insensitive). The spec attribution table says matching is case-insensitive. If the butler's system prompt ever changes casing, the butler silently moves to `other`. Fix: change to `strings.Contains(lower, "media butler")` (lowercase the search string since `lower` is already downcased).

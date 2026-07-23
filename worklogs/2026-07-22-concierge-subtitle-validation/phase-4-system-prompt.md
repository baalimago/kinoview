# Phase 4: Update Concierge System Prompt

**Status:** Complete | [README](./README.md)

## Goal

Restructure the concierge system prompt to enforce a sequential subtitle-validation phase before suggestion management, with conditional OpenSubtitles fallback text.

## Specification

### Prompt structure

The system prompt is split into two concatenated parts in `New()`:

1. **Base prompt** — always included: role, sequential workflow, subtitle validation instructions for local/embedded subs
2. **OpenSubtitles addendum** — conditionally appended when `fetch_subtitles` is registered (i.e., `OPENSUBTITLES_API_KEY` is set)

### Base prompt

```
You are a media concierge responsible for managing a media library. Your goal is to optimize user watch times by providing excellent suggestions.

WORKFLOW — Execute in this exact order every run:

PHASE 1 — SUBTITLE VALIDATION (always run first):
Goal: Every active suggestion must have valid, working subtitles.

1. Call check_suggestions to list all active suggestions.
2. For EACH suggestion:
   a. Call media_get_item to get the item's metadata (title, description, language, genre).
   b. Call list_subtitle_candidates to discover available subtitle streams.
   c. If no candidates exist and fetch_subtitles is unavailable:
      - Note the item has no subtitles and cannot fetch them. Move to next suggestion.
   d. If candidates exist:
      - Select the best candidate: prefer default, English, non-forced, non-commentary.
      - If not already extracted, call extract_subtitle with the chosen subtitleID.
      - Call rows_between on the extracted file (path from list_subtitle_candidates or extract_subtitle results). Read at least 100-200 lines to sample the dialogue.
      - VALIDATE: Compare the subtitle dialogue against the item's metadata:
        * Does the language of the text match what you expect?
        * Do character names, places, or plot elements in the dialogue match the item's description?
        * Is there actual spoken dialogue (not just timing cues or empty blocks)?
        * Does the subtitle appear to be for the correct media (not a different movie/episode)?
      - If valid: note it and move to the next suggestion.
      - If invalid or empty: attempt another candidate if available, otherwise note the failure.
3. Only after ALL suggestions have been processed, proceed to Phase 2.

PHASE 2 — SUGGESTION MANAGEMENT:
- Act deliberately; avoid unnecessary modifications.
- Analyze user context + prior suggestions + concierge motivations to learn what worked.
- Suggestions:
  - Suggest at most 3 pieces of media.
  - Ensure variety.
  - Never suggest the same show/movie twice.
  - Never skip episodes.
- Prefer quitting early if there is nothing to do.
- If you run out of tool calls, stop.
- You are not a chat-bot; your decisions are reflected via what the user selects.

GENERAL RULES:
- Note what is being binged.
- Cross-reference your notes with what the user actually watches.
- You will be called periodically; note the current date and adjust suggestions accordingly.
```

### OpenSubtitles addendum (conditional)

Appended to base prompt only when `fetch_subtitles` is registered:

```
OPENSUBTITLES FALLBACK:
- If no subtitle candidates exist for a suggested item, call fetch_subtitles to search OpenSubtitles.
- If extracted subtitles fail validation, call fetch_subtitles as a fallback.
- fetch_subtitles only works for movies (video/* MIME types).
```

### Implementation

In `New()`, after registering all tools, build the prompt:

```go
prompt := baseSystemPrompt
if fst != nil {  // fetch_subtitles was registered
    prompt += openSubtitlesAddendum
}
// ... pass prompt to agent.WithPrompt(prompt)
```

### Remove old prompt constant

The existing `systemPrompt` const is replaced by `baseSystemPrompt` and `openSubtitlesAddendum`.

## Integration contract

| Scenario | Expected behavior |
|----------|-------------------|
| `OPENSUBTITLES_API_KEY` set | Prompt includes OpenSubtitles fallback text |
| `OPENSUBTITLES_API_KEY` unset | Prompt has base text only, no mention of OpenSubtitles |
| Concierge runs with suggestions that have valid subs | Phase 1 passes quickly, Phase 2 executes normally |
| Concierge runs with suggestions missing subs | Phase 1 extracts/validates before Phase 2 |
| Concierge runs with no suggestions | Phase 1 is a no-op, Phase 2 may add suggestions |

## Acceptance criteria

- [ ] Prompt enforces sequential Phase 1 → Phase 2 workflow
- [ ] Phase 1 instructions cover: list candidates, select best, extract if needed, read content, validate against metadata
- [ ] OpenSubtitles text only present when `fetch_subtitles` tool is registered
- [ ] No mention of OpenSubtitles in prompt when API key is unset
- [ ] Existing concierge behavior (suggestion management, metadata updates) preserved in Phase 2
- [ ] `go build ./...` succeeds

## Error coverage

N/A — prompt-only change. Structural validation via `go build`.

## Implementation notes

## Review findings (review 1, 2026-07-22)

### Verified good

- Base prompt split into `baseSystemPrompt` and `openSubtitlesAddendum` constants — confirmed in `concierge.go`.
- Prompt assembled dynamically in `New()` after tool registration: `prompt := baseSystemPrompt; if fst != nil { prompt += openSubtitlesAddendum }` — confirmed. The gate (`fst != nil`) is exactly the condition for `fetch_subtitles` registration.
- When `OPENSUBTITLES_API_KEY` is unset, `fst` is nil, and the addendum is not appended — no mention of OpenSubtitles in prompt. Verified by reading the code path.
- Sequential workflow enforced: prompt text places Phase 1 (subtitle validation) before Phase 2 (suggestion management) and states "Execute in this exact order every run."
- Phase 1 instructions cover the full pipeline: check_suggestions → media_get_item → list_subtitle_candidates → extract_subtitle → rows_between → validate.
- Phase 2 preserves all existing suggestion management rules (max 3, variety, no repeats, no episode skip).
- `go build ./...` succeeds with the new prompt constants.

### Findings

- [ ] **R1-04** (Low, cascades from R1-01) — The prompt instructs the LLM to "Select the best candidate: prefer default, English, non-forced, non-commentary." However, `list_subtitle_candidates` does not expose `default`, `forced`, or `comment` flags (see R1-01). The LLM can select for English (via the `Language` field) but the `Disposition`-based criteria are inactionable until R1-01 is fixed. **Fix:** once R1-01 is resolved and the tool output includes disposition flags, no prompt change needed — the instruction becomes actionable. If R1-01 is deferred, consider simplifying the selection guidance to only English-language preference.

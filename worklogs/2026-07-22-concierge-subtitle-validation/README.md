# 2026-07-22: Concierge Subtitle Validation Pipeline

**Status:** Complete | [README](./README.md)

## Summary

The concierge currently has `preload_subtitles` (bundled Find+Select+Extract) and `fetch_subtitles` (OpenSubtitles) but no way to validate subtitle quality or systematically ensure all suggestions have working subtitles. This worklog replaces the bloated `preload_subtitles` tool with a composable pipeline (`list_subtitle_candidates` → `extract_subtitle` → `Cat`/`RowsBetween`) and updates the concierge system prompt to enforce a sequential subtitle-validation phase before suggestion management.

## Strategy

The concierge LLM gains agency over subtitle selection and validation:

1. **Discovery**: `list_subtitle_candidates(ID)` returns all subtitle streams (embedded + external) with metadata and extraction status
2. **Extraction**: `extract_subtitle(ID, subtitleID)` performs pure ffmpeg/ffprobe extraction, returns file path
3. **Validation**: clai's `Cat`/`RowsBetween` read the extracted `.vtt` content; the LLM compares dialogue against item metadata for semantic coherence
4. **Fallback**: `fetch_subtitles(ID)` searches OpenSubtitles when local subs are missing or invalid

The concierge system prompt is restructured as a sequential workflow: validate subtitles for all active suggestions first, then manage suggestions. No tool or prompt text is included for functionality that isn't available (e.g., `fetch_subtitles` prompt text only when `OPENSUBTITLES_API_KEY` is set).

The butler's internal `preloadSubs` method is unaffected — it calls `StreamManager` directly, not the tool.

## Phase Status

| Phase   | Status           | Summary                                                                                                  |
| ------- | ---------------- | -------------------------------------------------------------------------------------------------------- |
| Phase 1 | ✅ Complete (R1 resolved) | Create `list_subtitle_candidates` tool — all review findings resolved (w0)           |
| Phase 2 | ✅ Complete      | Create `extract_subtitle` tool                                                                           |
| Phase 3 | ✅ Complete      | Update concierge tool registration (remove old, register new + clai tools)                               |
| Phase 4 | ✅ Complete      | Update concierge system prompt for sequential subtitle-validation phase — see R1-04 (Low, non-reopening) |
| Phase 5 | ✅ Complete      | Enhance `check_suggestions` with `SubtitleID`                                                            |
| Phase 6 | ✅ Complete      | Quality gate — tests, lint, build                                                                        |

## Feedback Index

| ID    | Severity | Phase | Summary                                                                   |
| ----- | -------- | ----- | ------------------------------------------------------------------------- |
| R1-01 | ✅ RESOLVED (w0) | Ph 1  | `Disposition` metadata (default/forced/comment) now surfaced as JSON booleans |
| R1-02 | ✅ RESOLVED (w0) | Ph 1  | `extractedPath` now included via `omitempty` when `alreadyExtracted: true`   |
| R1-03 | ✅ RESOLVED (w0) | Ph 1  | Output is now JSON array matching spec                                    |
| R1-04 | Low      | Ph 4  | Prompt selection criteria inactionable without R1-01 fix                  |
| R1-05 | ✅ RESOLVED (w0) | Ph 1  | Field naming aligned to spec (Type→source, Extracted→alreadyExtracted)    |

## Severity Taxonomy

- **Critical**: OOM, data loss, or process crash
- **High**: breaks a feature or creates incorrect behavior
- **Medium**: degrades observability or performance
- **Low**: cosmetic

All findings above Low reopen the phase.

## Decisions

- **D1 — Prompt-driven, not deterministic**: The concierge LLM owns the subtitle validation workflow. The system prompt provides strong sequential guidance. No hardcoded pre-step.
- **D2 — Tier 3 validation**: LLM compares subtitle dialogue against item metadata/description for semantic coherence. Not just file existence (Tier 1) or parsability (Tier 2).
- **D3 — Split `preload_subtitles`**: The bundled Find+Select+Extract tool is replaced with `list_subtitle_candidates` (discovery) and `extract_subtitle` (pure extraction). KISS.
- **D4 — Reuse clai file tools**: `Cat` and `RowsBetween` from clai's `pkg/tools` provide file reading. No custom `read_subtitle` tool.
- **D5 — Sequential workflow**: The prompt enforces "validate subs for all suggestions first, then manage suggestions." Ensures subs are ready before new suggestions land.
- **D6 — Conditional prompt text**: When `OPENSUBTITLES_API_KEY` is unset, the prompt omits OpenSubtitles fallback instructions. Tools and prompt text always match actual capabilities.
- **D7 — Butler unaffected**: The butler's `preloadSubs` method calls `StreamManager` directly. Removing the `preload_subtitles` tool from concierge registration doesn't touch the butler.

## Review 1 (2026-07-22)

**Verdict:** Not ready — Phase 1 reopened (R1-01, Medium). The composable pipeline design is sound and the implementation is correct for Phases 2–6, but `list_subtitle_candidates` has a critical gap: the tool omits `Disposition` metadata (`default`, `forced`, `comment`) that the system prompt instructs the LLM to use for subtitle selection. Without these fields the LLM cannot execute the "prefer default, non-forced, non-commentary" selection criteria.

**Fix strategy:** Reopen Phase 1 only. Extend the internal `candidate` struct in `list_subtitle_candidates.go` to capture `Disposition.Default`, `Disposition.Forced`, and `Disposition.Comment` from the `model.Stream`, and render them in the output. R1-04 (prompt) resolves automatically once R1-01 is fixed. R1-02 (extractedPath) and R1-03/R1-05 (format/naming) are Low and can be addressed in the same pass or deferred.

**Cross-phase invariants verified:**

- Butler `preloadSubs` → `StreamManager` directly: confirmed unaffected (no tool path traversal).
- Classifier tool set: confirmed separate from concierge tools.
- `SubtitleSelector` interface: retained in `agents/interfaces.go` for butler's continued use.
- `fetch_subtitles`: unchanged, conditionally registered based on `OPENSUBTITLES_API_KEY`.

**Gates re-run:**

```bash
go build -o kinoview .                                          # clean
go vet ./...                                                     # clean
gofumpt -l .                                                     # clean
go test -race -count=1 ./internal/agents/tools/...              # pass
go test -race -count=1 ./internal/agents/concierge/...          # pass
```

Pre-existing flaky test in `internal/media/storage` (TempDir cleanup race, unrelated to worklog).

## Session Journal

### 2026-07-22 22:10 — Review 1

**Review completed.** 5 findings filed (R1-01 through R1-05). Phase 1 reopened on R1-01 (Medium): `list_subtitle_candidates` does not surface `Disposition` metadata that the Phase 4 prompt instructs the LLM to use for subtitle selection. Phases 2, 3, 5, 6 verified correct and complete. Phase 4 has one Low finding (R1-04) cascading from R1-01 that does not independently reopen.

Gates re-run: build, vet, gofumpt clean. Race-free on tools and concierge packages. Pre-existing flaky test in `internal/media/storage` (unrelated).

### 2026-07-22 21:59 — Worker Session 5, Phase 6

**Phase 6 completed:** Quality gate executed — all checks pass.

Commands executed:

- `go build -o kinoview .` — clean
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofumpt -l .` — clean (no formatting issues)
- `go test -count=1 ./...` — all 18 packages pass, 0 failures
- `go test -race -count=1 ./internal/agents/tools/...` — clean
- `go test -race -count=1 ./internal/agents/concierge/...` — clean

Regression verification:

- Butler `PrepSuggestions` / `preloadSubs` — internal method calls `StreamManager` directly, unaffected. Butler tests pass.
- Classifier — uses different tool set, unaffected.
- `fetch_subtitles` — no changes, unaffected.
- No remaining references to `subSelector`, `SubtitleSelector`, or `preload_subtitles` in concierge or cmd packages.

Minor fix:

- `internal/agents/concierge/concierge.go` — Updated tool list comment in `New()` to reflect all 18 registered tools (was missing concierge context tools, user context getter, and clai tools).

**Design decisions:**

- **D21 — Zero regressions**: No new findings from the quality gate. All pre-existing coverage levels maintained. The composable pipeline design proved testable and correct.
- **D22 — Tool comment hygiene**: The `New()` godoc comment now accurately documents all 18 tools, including conditionally-registered ones (`fetch_subtitles`, `media_list`, `media_stats`).

### 2026-07-22 21:57 — Worker Session 4, Phase 5

**Phase 5 completed:** `check_suggestions` output enhanced with `SubtitleID` field.

Files modified:

- `internal/agents/tools/check_suggestions.go` — Added `SubtitleID: %s` to per-suggestion output line. The `model.Suggestion.SubtitleID` field (already present) is now surfaced to the LLM.
- `internal/agents/tools/check_suggestions_test.go` — Updated expected strings in `TestCheckSuggestionsTool_Call_WithSuggestions` to include `SubtitleID: ` suffix.
- `internal/agents/tools/suggestions_test.go` — Updated `TestCheckSuggestionsTool_Call` to also assert presence of `SubtitleID` in output.

**Design decisions:**

- **D19 — Always include SubtitleID**: The field is always printed, even when empty (zero value). An empty `SubtitleID` signals to the LLM that no subtitle has been pre-selected, which is actionable information for the validation workflow.
- **D20 — No model changes**: `model.Suggestion.SubtitleID` already existed. No struct or interface changes required.

### 2026-07-22 21:55 — Worker Session 3, Phase 4

**Phase 4 completed:** Concierge system prompt restructured for sequential subtitle-validation workflow.

Files modified:

- `internal/agents/concierge/concierge.go` — Replaced monolithic `systemPrompt` constant with `baseSystemPrompt` and `openSubtitlesAddendum`. Prompt is now built dynamically in `New()`: `baseSystemPrompt` always included; `openSubtitlesAddendum` conditionally appended when `fetch_subtitles` tool is registered (i.e., `OPENSUBTITLES_API_KEY` is set).

**Design decisions:**

- **D17 — Prompt built in New()**: The prompt is assembled after all tool registrations. `fst` is only non-nil when `OPENSUBTITLES_API_KEY` is set, which is the exact gate for the addendum. Tools and prompt text always match.
- **D18 — No interface changes**: Purely internal prompt restructuring. No exported API changes. All existing tests pass without modification.

### 2026-07-22 21:52 — Worker Session 2, Phase 3

**Phase 3 completed:** Concierge tool registration updated.

Files modified:

- `internal/agents/concierge/concierge.go` — Removed `preload_subtitles` registration, added `list_subtitle_candidates`, `extract_subtitle`, `Cat`, `RowsBetween`. Removed `subSelector` field, `WithSubtitleSelector` option, and `SubtitleSelector` nil check.
- `internal/agents/concierge/concierge_test.go` — Removed `mockSubtitleSelector`, all `WithSubtitleSelector` references, and "missing subtitle selector" test case.
- `internal/agents/concierge/cmd.go` — Removed `WithSubtitleSelector(...)` call and unused `butler`/`models` imports.
- `cmd/serve/serve_setup.go` — Removed `WithSubtitleSelector(...)` call.

Files deleted:

- `internal/agents/tools/preload_subtitles.go` — Dead code; replaced by `list_subtitle_candidates` + `extract_subtitle`.
- `internal/agents/tools/preload_subtitles_test.go` — Dead code.

Cleanup:

- `internal/agents/tools/mocks_test.go` — Removed unused `mockSubtitleSelector` and `context` import.

**Design decisions:**

- **D14 — `subsPath` derived from `configDir`**: The subtitle store path for `list_subtitle_candidates` is computed as `path.Join(c.configDir, "subtitles")` inside `New()`, matching the pattern used in `cmd.go`'s `Setup()`. No new option needed.
- **D15 — Dead code removal**: Removed `preload_subtitles.go` and its test file entirely. The butler's internal `preloadSubs` method calls `StreamManager` directly and is unaffected.
- **D16 — `SubtitleSelector` removed from concierge**: The field was only used by the now-removed `preload_subtitles` tool. The interface remains in `agents/interfaces.go` for the butler's continued use.

### 2026-07-22 21:48 — Worker Session 1, Phase 2

**Phase 2 completed:** `extract_subtitle` tool created.

Files created:

- `internal/agents/tools/extract_subtitle.go` — Tool implementation
- `internal/agents/tools/extract_subtitle_test.go` — 9 test cases covering: nil deps, successful extraction, idempotency, external (negative) index, missing/empty ID, missing/empty subtitleID, item not found, extraction failure, specification validation

**Design decisions:**

- **D11 — String subtitleID**: The tool accepts `subtitleID` as a string, matching `StreamManager.ExtractSubtitles(item, streamIndex string)` signature directly. No type conversion needed in the tool.
- **D12 — No SubtitleSelector dependency**: Unlike `preload_subtitles`, this tool has no `SubtitleSelector`. The LLM chooses which stream to extract based on `list_subtitle_candidates` output. This keeps the tool single-purpose.
- **D13 — Pure passthrough**: The tool owns no extraction logic; it delegates entirely to `StreamManager.ExtractSubtitles`. Idempotency and caching are handled by the stream manager.

### 2026-07-22 21:45 — Worker Session 1, Phase 1

**Phase 1 completed:** `list_subtitle_candidates` tool created.

Files created:

- `internal/agents/tools/list_subtitle_candidates.go` — Tool implementation
- `internal/agents/tools/list_subtitle_candidates_test.go` — 6 test cases covering: nil deps, no subtitle streams, embedded+external streams with extraction status, missing ID, item not found, stream manager error, specification validation, isExtracted logic

**Design decisions:**

- **D8 — Extraction status via filesystem check**: The tool reports `Extracted: true/false` per candidate by checking `<subStorePath>/<itemID>_<streamIndex>.vtt` existence. This mirrors `ExtractSubtitles`'s own cache-check behavior. Zero-cost operation (single `os.Stat`).
- **D9 — Private candidate struct**: Used an unexported `candidate` struct local to `Call()` to avoid importing `kinomodel` solely for `Stream`. Keeps the tool self-contained and avoids coupling to model details beyond what `Find()` returns.
- **D10 — `subStorePath` constructor parameter**: The tool accepts the subtitle store path directly, matching the pattern of `fetchSubtitlesTool` accepting `cacheDir`. The concierge will derive this from its config at registration time.

### 2026-07-22 22:30 — Worker Session w0, Phase 1 (Review 1 fix)

**Phase 1 reopened items resolved:** R1-01, R1-02, R1-03, R1-05.

Files modified:

- `internal/agents/tools/list_subtitle_candidates.go` — Rewrote output from plain-text
  to JSON. Extended `candidate` struct with `Default`, `Forced`, `Comment` disposition
  booleans and `ExtractedPath` string. Output now serializes via
  `json.MarshalIndent`. Removed `"strings"` import, added `"encoding/json"`.
- `internal/agents/tools/list_subtitle_candidates_test.go` — Rewrote assertions to
  parse JSON output. Added `Disposition` fields to test streams (Default=1 on
  stream 2, Forced=1/Comment=1 on stream 3). Added new test case
  `disposition_fields_default_to_false_when_not_set`. Added `"encoding/json"` import.

Changes by finding:

- **R1-01 (Medium):** `Disposition` (default, forced, comment) now surfaced.
  `s.Disposition.Default == 1` → `"default": true` in JSON output.
- **R1-02 (Low):** `extractedPath` now included when `alreadyExtracted: true`;
  omitted via `omitempty` when false.
- **R1-03 (Low):** Output is now a JSON array with header line for human context.
- **R1-05 (Low):** Field names aligned to spec: `source` (was `Type`),
  `alreadyExtracted` (was `Extracted`), `codec` (was `Codec` in output).

Gates re-run:

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofumpt -l .` — clean
- `go test -race -count=1 ./internal/agents/tools/...` — 8 passing (was 6, +2 new)
- `go test -count=1 ./...` — 18/18 packages pass

**Design decisions:**

- **D23 — JSON output format:** The spec explicitly calls for JSON; LLMs parse
  structured data more reliably than human-readable text. The header line
  (`subtitle candidates for 'X' (N found):`) is preserved as a human-readable
  prefix before the JSON array.
- **D24 — `omitempty` on `extractedPath`:** When `alreadyExtracted: false`, the
  path is superfluous and would be misleading. Omitting it keeps the output
  concise.
- **D25 — Boolean disposition:** ffprobe uses 0/1 ints; the tool converts to
  JSON booleans. The LLM prompt says "prefer default, non-forced,
  non-commentary" — booleans make this trivially machine-readable.

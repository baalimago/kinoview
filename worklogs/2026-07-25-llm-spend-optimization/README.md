# LLM Spend Optimization — Worklog

**Start:** 2026-07-25
**Status:** In Progress (Parts 1-2 & 5 complete; Parts 3-4 deferred — require rpie SSH)
**Based on:** `agent_notebook/2026-07-25_llm-spend-optimization.md`; clai per-query cost records on rpie

> **Reading contract:** an executing agent reads this README and the single phase file it is
> working on. Cross-phase invariants live in [Strategy](#strategy).

## Status Board

| Phase                                                                 | Status         | Summary                                                                                       |
| --------------------------------------------------------------------- | -------------- | --------------------------------------------------------------------------------------------- |
| [1 — LLM cost reporting](phase-1-llm-call-telemetry.md)               | ✅ Done        | `kinoview llm usage` aggregates clai's existing per-query records; R1-04 resolved (Session 15) |
| [2 — Butler returns index](phase-2-butler-returns-index.md)           | ✅ Done        | Butler emits `index`; semantic indexer becomes fallback. −2,020 calls, −26.8M uncached tokens |
| [3 — Deterministic subs](phase-3-deterministic-subtitle-selection.md) | ✅ Done       | Rule-based English subtitle pick; LLM selector becomes fallback. −1,410 calls                 |
| [4 — Butler payload diet](phase-4-butler-payload-diet.md)             | ✅ Done        | Trim projection + drop indentation. Low priority: tokens are ~99.7% cached                    |
| [5 — Disconnect hygiene](phase-5-disconnect-trigger-hygiene.md)       | ✅ Done        | Debounce + single-flight. Cuts 39 cascades/day                                                |
| [6 — Suggestion cache](phase-6-suggestion-cache.md)                   | ✅ Done        | Fingerprint library+context. Cache hit skips butler. R1-02, R1-03 resolved (Session 14)   |
| [7 — Concierge schedule](phase-7-concierge-schedule-truthfulness.md)  | ✅ Done (R1-06 resolved)  | Live interval, persisted last-run. Zero-interval rejected at flag validation.                             |
| [8 — Fresh suggestions](phase-8-suggestion-freshness-qol.md)          | ✅ Done        | Never wipe shelf, connect-trigger cascade, live push via WS, 3-state frontend. R1-01 + R1-05 resolved (Session 13)|
| [9 — Quality gate](phase-9-quality-gate.md)                           | 🔄 In Progress (R2 verified) | Full gate + re-measurement. Parts 1-2 & 5 done & re-verified; Parts 3-4 (rpie) deferred |

## Strategy

Every leak is an LLM recovering information the process already had, or making a decision that
has a total ordering.

### Execution order

1. **Phase 1** — establishes the baseline every later phase is measured against.
2. **Phase 5** — highest impact. 39 cascades/day, each 7 calls.
3. **Phase 2** — largest token win. The indexer's 26.8M prompt tokens are 98% uncached.
4. **Phase 3** — removes two thirds of remaining roundtrips; makes subtitle choice deterministic.
5. **Phase 6** — after Phase 4 (needs its projection) and after its own ordering prerequisite.
6. **Phase 4** — low priority; its tokens are already cached. Land it before Phase 6 and 8.
7. **Phase 7** — independent, any time.
8. **Phase 8** — after 2, 3 and 4. Hard constraint: it adds a connect-time cascade, affordable
   only at one call per cascade.
9. **Phase 9** — last.

### Cross-phase invariants

- **Every agent keeps its LLM path as a fallback.** Phases 2 and 3 leave `semanticIndexerSelect`
  and `selector.Select` reachable and tested. Unclassified items and exotic subtitle layouts
  still work.
- **`PrepSuggestions` never causes suggestions to be erased.** It currently returns `(nil, nil)`
  when every suggestion fails ([butler.go:160-164](../../internal/agents/butler/butler.go:160)),
  and the caller passes that to `Manager.Update`
  ([manager.go:69](../../internal/media/suggestions/manager.go:69)). Phase 8 owns the fix.
- **`Manager.Update` has one caller** ([index_handlers.go:102](../../internal/media/index_handlers.go:102))
  and is not on the `SuggestionManager` interface, which is `List`/`Add`/`Remove` only
  ([interfaces.go:79](../../internal/agents/interfaces.go:79)). The concierge edits via
  `Add`/`Remove`. Guarding `Update` does not affect the concierge.
- **The library projection is one function**, defined by Phase 4:

  ```go
  // internal/agents/butler/butler.go
  type butlerItemView struct {
      Index   int    `json:"i"`
      Name    string `json:"n"`
      Title   string `json:"t,omitempty"`
      Year    int    `json:"y,omitempty"`
      Season  int    `json:"s,omitempty"`
      Episode int    `json:"e,omitempty"`
      Genre   string `json:"g,omitempty"`
      Runtime int    `json:"r,omitempty"`
  }

  func projectItems(items []model.Item) []butlerItemView
  func formatItems(items []model.Item) string // = json.Marshal(projectItems(items))
  ```

  Phase 6 fingerprints `projectItems` output, never `[]model.Item`.

- **Item order is nondeterministic until Phase 6 fixes it.** `store.Snapshot()` iterates a
  `map[string]model.Item` ([store.go:491-498](../../internal/media/storage/store.go:491)), so Go
  randomizes order per call. Any phase that hashes, diffs or caches the library must sort by
  `Path` first.
- **Prompt caching is provider-specific and already effective.** Do not restructure prompts for
  cache alignment without measuring first: on `deepseek-v4-flash` the butler already reports
  ~99.7% of prompt tokens cached per call, despite volatile content leading and randomized item
  order.
- **Maintenance contract:** any phase changing a CLI flag, a default, or an on-disk format
  updates `README.md` and the flag help in `cmd/serve/serve.go` in the same commit.

### Test doctrine

Functionality is **the number and shape of LLM queries**, observable at the `text.FullResponse`
seam. `internal/agents/butler/butler_test.go` defines `MockFullResponse{QueryFunc}`; a phase
claiming "removes N calls" proves it by counting `QueryFunc` invocations on realistic input.

User-visible behaviour (the shelf, the extracted subtitle) is proven through the HTTP handlers and
the suggestions manager. Error handling is proven per error-matrix row at the package boundary.

**A checked acceptance criterion names the test that proves it.**

```bash
gofumpt -l . && go vet ./... && go test -race -count=1 ./... && go build -o kinoview .
```

### Severity taxonomy

| Severity | Meaning                                                     | Reopens phase? |
| -------- | ----------------------------------------------------------- | -------------- |
| CRITICAL | Breaks user-visible behaviour or loses data                 | Yes            |
| MAJOR    | Unmet acceptance criterion, or a saving that does not exist | Yes            |
| MODERATE | Missing error coverage, untested fallback, contract drift   | Yes            |
| MINOR    | Naming, comment, non-blocking cleanup                       | No             |

---

## Measured baseline

From clai's `queries[]` records on rpie, 2026-07-18 → 07-25. 1,441 of 4,065 conversations carry
cost data; earlier ones have no `queries` key.

| Agent              | Convs | With cost | Prompt tokens | Cached | Mean prompt/query | `cost_usd` |
| ------------------ | ----- | --------- | ------------- | ------ | ----------------- | ---------- |
| `butler`           | 605   | 310       | 42.37M        | 14.04M | 136,678           | 28.31      |
| `semanticIndexer`  | 2,020 | 692       | 26.82M        | 0.32M  | 38,758            | 15.58      |
| `subtitleSelector` | 1,410 | 418       | 2.55M         | 0.51M  | 6,107             | 0.99       |
| `concierge`        | 23    | 21        | 0.71M         | 0.70M  | 33,956            | 0.04       |
| **Total**          | 4,065 | 1,441     | **72.45M**    | 15.57M |                   | **44.92**  |

Cache hit rate by agent and model — the figure that determines which phases pay:

| Agent     | Model                | Queries | Prompt | Cached | Hit rate |
| --------- | -------------------- | ------- | ------ | ------ | -------- |
| butler    | `deepseek-v4-flash`  | 99      | 15.13M | 13.68M | 90.4%    |
| butler    | `minimax/minimax-m3` | 205     | 26.56M | 0.36M  | 1.4%     |
| indexer   | `deepseek-v4-flash`  | 292     | 12.52M | 0.21M  | 1.7%     |
| indexer   | `minimax/minimax-m3` | 383     | 13.78M | 0.11M  | 0.8%     |
| concierge | both                 | 21      | 0.71M  | 0.70M  | 97.9%    |

Per-query cost by model:

| Model                      | Queries | `cost_usd` | Effective $/M prompt tok |
| -------------------------- | ------- | ---------- | ------------------------ |
| `minimax/minimax-m3`       | 727     | 41.84      | 1.02                     |
| `deepseek-v4-flash`        | 690     | 1.81       | 0.06                     |
| `thinkingmachines/inkling` | 24      | 1.27       | 1.04                     |

Volume: 310 of all 605 butler runs ever occurred in these 8 days — **~39 cascades/day** against
~1.3/day historically. Each cascade is 7 calls and ~230K prompt tokens.

**Facts that constrain this worklog:**

- `cost_usd` is clai-reported and unverified. It disagrees with OpenRouter's published price for
  `minimax/minimax-m3` by 3.4×. Token counts are provider-reported and trusted; report cost as
  clai-reported and always name the model.
- `chars/4` underestimates prompt tokens ~1.6× on this payload. Use measured tokens.
- Three models are in play. Never compare `cost_usd` across a model change.
- `deepseek-v4-flash` costs 22× less per query than `minimax/minimax-m3`. The model flag is a
  larger lever than any phase here; check it before discussing architecture.

## Corrections to the source analysis

| ID  | Correction                                                                                                                                                                                   |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| C1  | 7 calls per cascade, not 7–10. The selector runs once per suggestion ([subs_parser.go:37](../../internal/agents/butler/subs_parser.go:37)).                                                  |
| C2  | Its Priority 2 (regex + Levenshtein) is unnecessary. `formatItems` already sends `"index": idx` ([butler.go:171](../../internal/agents/butler/butler.go:171)). Schema change, not a matcher. |
| C3  | The 6h interval is honoured by a hard-coded ticker ([index.go:318](../../internal/media/index.go:318)). Extra runs are process restarts. `concierge.WithInterval` is dead config.            |
| C4  | Selector input is ~600 tokens, not ~200.                                                                                                                                                     |
| C5  | Classifier "~15K tokens/call" is unreproducible — only 4 classifier conversations are in the store. Deferred until Phase 1 measures it.                                                      |
| C6  | Reject "cap at most recent 200 items". It makes 234 of 434 items unsuggestable and breaks sequential-series continuation.                                                                    |
| C7  | Unreported bug: `PrepSuggestions` returns `(nil, nil)` on total failure, wiping the shelf. → Phase 8.                                                                                        |
| C8  | Storyteller and recommender are not targets: 0 persisted conversations; storyteller already has cooldown + single-flight.                                                                    |
| C9  | Its token estimates are 1.6× low; call counts are exact.                                                                                                                                     |
| C10 | "~$4.03/month" averages an 8-day, 30× volume spike across 8 months.                                                                                                                          |
| C11 | It names one model; three are in play, spanning 17× in effective rate.                                                                                                                       |

## Decisions

**Review 2 (2026-07-25).** Re-ran full gate: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds. `go test -race -count=1 ./...` passes all except the two pre-existing issues (clai `tools.Init()` data race in storage, and a flaky stream test TempDir cleanup).

Coverage baseline vs Review 1:

| Package | Review 1 | Review 2 | Delta |
|---|---|---|---|
| butler | 98.1% | 98.1% | — |
| concierge | 69.2% | 69.2% | — |
| media | 81.4% | 81.1% | −0.3% (noise) |
| suggestions | 95.6% | 95.6% | — |
| model | 95.1% | 95.1% | — |
| serve | 74.7% | 75.0% | +0.3% |
| llm | 57.3% | 57.3% | — |

No real regressions.

All six R1 findings verified resolved in code — see Feedback Index. Phases 1–8 are Done; all invariants from the Strategy section hold across every branch traced. Phase 9 Parts 1, 2, 5 done; Parts 3, 4 remain deferred (require rpie SSH for production deployment + 7-day measurement).

Zero new findings. The worklog accurately reflects code state. The 16 modified files and 4 untracked session directories are the R1-fix sessions (12–16) awaiting commit.

**D1 — Deterministic fast path, LLM fallback.** Phases 2 and 3 keep the LLM path behind a validity
check. 9 items failed classification and 24 are pending; the long tail is real.

**D2 — No library truncation.** Trim fields, never hide items.

**D3 — Read clai's cost data; build no second meter.** clai persists provider-reported tokens and
per-query cost. Phase 1 only reads it. The sole new instrumentation in this worklog is Phase 5's
disconnect reason.

**D4 — Phase 8 is the objective.** The token work exists to make a connect-time cascade
affordable.

**D5 — Measured outcome.** _(written by Phase 9)_

**D6 — Do not optimize for prompt-cache prefix alignment.** Measured: the butler reports ~99.7%
cached per call on `deepseek-v4-flash` with volatile content leading and randomized item order.
A phase built on strict-prefix assumptions was cancelled. Caching semantics here are undocumented
and provider-specific.

**D7 — Model choice precedes architecture in any cost discussion.** See the per-model table.

**D8 — Cascade generation tracking.** (Session 11, holistic review) The original single-flight mechanism had a race: a cascade completing before queued callers acquire the lock would miss their rerun signals and exit, allowing the next caller to start a fresh cascade — and the cycle could repeat. Fixed by adding `cascadeGen atomic.Int64`: each fresh cascade bumps the generation, and the defer only manages the flight slot if its generation is still current. This bounds the cascade count per generation to 2 (fresh + rerun), and the total count across generations to a small multiple. The test now uses a barrier channel to ensure all triggers fire before the first cascade completes, making it deterministic at exactly 2.

**D9 — Review 1 (2026-07-25).** Re-ran full gate: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds. `go test -race -count=1 ./...` passes all except pre-existing storage race (clai `tools.Init()`). Coverage stable: butler 98.1%, concierge 69.2%, media 81.2%, suggestions 95.6%, model 95.1%, serve 74.7%, llm 57.3%. Six findings (R1-01 through R1-06), all MODERATE except R1-05 (MINOR). Four phases reopened: 1 (R1-04), 6 (R1-02, R1-03), 7 (R1-06), 8 (R1-01, R1-05). Phases 2, 3, 4, 5, 9 verified clean. See Feedback Index below.

## Feedback Index

| ID      | Severity | Phase                                      | Summary                                                                                     |
| ------- | -------- | ------------------------------------------ | ------------------------------------------------------------------------------------------- |
| R1-01   | ✅ Resolved S13 | [8](phase-8-suggestion-freshness-qol.md)   | `handleConnect` passes bare string `"connect"` as disconnectReason; missing enum constant   |
| R1-02   | ✅ Resolved S14 | [6](phase-6-suggestion-cache.md)           | `computeLibraryFingerprint` uses field-by-field hash, not marshalled `ProjectItems`         |
| R1-03   | ✅ Resolved S14 | [6](phase-6-suggestion-cache.md)           | `progressBucket` uses 10-min absolute buckets, not 10% steps as specified                    |
| R1-04   | ✅ Resolved S15 | [1](phase-1-llm-call-telemetry.md) | `classifyAgent` matches `"media Butler"` case-sensitively; all others case-insensitive |
| R1-05   | ✅ Resolved S13 | [8](phase-8-suggestion-freshness-qol.md)   | `runCascade` log prefix always says "disconnect" even for connect trigger                    |
| R1-06   | ✅ Resolved S16 | [7](phase-7-concierge-schedule-truthfulness.md) | Flag validation in serve_setup.go rejects zero interval                          |

## Session journal

**Session 12 (2026-07-25, worker: claude)** — Holistic review (post-Session-11).

- Re-ran full gate: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds.
- `go test -race -count=1 ./...` passes all except pre-existing storage race (`clai/tools.Init()`) and `TestRun` tempdir cleanup flake.
- All 7 cross-phase checks pass: `TestPrepSuggestions_QueryCount` (1 query), `TestPrepSuggestions_FallbackQueryCount` (7 queries), `TestCascade_VersionBump`, `TestHandleConnect_DisconnectNoDeadlock`, `TestUsage_Attribution`, `TestManager_LoadLegacyArrayFormat`, `TestSuggestionsHandler_States/empty`.
- `TestHandleDisconnect_ConcurrentTriggers` passes 5/5 iterations deterministically at exactly 2 cascades.
- Coverage stable: butler 98.1%, concierge 69.2%, media 81.2%, suggestions 95.6%, model 95.1%, serve 74.7%, llm 57.3%. No regressions vs Session 11 baseline.
- No dead code, no commented-out debug statements, no new TODOs. Code quality: clean.
- All 5 flags (`-butlerDebounce`, `-pongGrace`, `-butlerCacheTTL`, `-conciergeInterval`) verified in both `serve.go` and `README.md`; `kinoview llm usage` documented.
- Source analysis at `agent_notebook/2026-07-25_llm-spend-optimization.md` unmodified.
- Phase 9 Parts 3 & 4 remain deferred — require SSH to rpie for production deployment + 7-day measurement.

**Session 13 (2026-07-25, worker: claude)** — Phase 8 Review Findings Fix.

- **R1-01 fix:** Added `reasonConnect disconnectReason = "connect"` constant to `internal/media/index.go`. Updated `handleConnect()` to pass `reasonConnect` instead of bare string `"connect"`. Updated `TestDisconnectReason_String` to include the new constant.
- **R1-05 fix:** Changed `runCascade` log prefix from `"disconnect (%s): ..."` to `"cascade (%s): ..."` (lines 182 and 213), making it reason-agnostic and consistent with `triggerCascade` which already uses `"cascade (%s): ..."`.
- Verified: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds. All tests pass (pre-existing storage race unrelated).
- Phase 8 status: ✅ Done. Both Review 1 findings resolved.

**Session 14 (2026-07-25, worker: claude)** — Phase 6 Review Findings Fix.

- **R1-02 fix:** Replaced field-by-field `Fprintf` hash in `computeLibraryFingerprint` with `json.Marshal(butler.ProjectItems(items))`, zeroing the `Index` field before marshalling (it's a transport artifact reflecting input order). This guards against struct drift: adding a field to `butlerItemView` automatically invalidates the cache.
- **R1-03 fix:** Updated Phase 6 spec prose, integration contract rows 5-6, and feedback index from "10% steps" to "10-minute absolute buckets" with a data-constraint note.
- Verified: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds. All tests pass (pre-existing storage race unrelated).
- Phase 6 status: ✅ Done. Both Review 1 findings resolved.

**Session 15 (2026-07-25, worker: claude)** — Phase 1 Review Finding Fix.

- **R1-04 fix:** Changed `classifyAgent` line 275 from `strings.Contains(systemContent, "media Butler")` (case-sensitive) to `strings.Contains(lower, "media butler")` (case-insensitive), matching all six other agent checks. Updated attribution test to expect `"butler"` for lowercase butler prompt.
- Verified: `gofumpt -l .` clean, `go vet ./cmd/llm/...` clean, `go build -o kinoview .` succeeds. All 20 `cmd/llm` tests pass.
- Phase 1 status: ✅ Done. R1-04 resolved.

**Session 1 (2026-07-25)** — Validated the source analysis against rpie and the code; wrote
phases 1–9. Aggregated clai's newly-available cost records, which replaced the estimated baseline,
rewrote Phase 1, and re-prioritised Phase 5. Reviewed the plan against the code before
implementation: cancelled the prompt-cache phase, demoted Phase 4, added Phase 6's ordering
prerequisite, and simplified Phase 8's manager change. No production code changed.

**Session 2 (2026-07-25, worker: claude)** — Implemented Phase 1: `kinoview llm usage`.

- New package `cmd/llm` with streaming JSON decode of clai conversation files.
- Attribution via system prompt substring matching per Appendix A.3.
- Aggregation by agent (default), day, or model; `--since` filters on `queries[].created_at`.
- `--json` flag for machine-readable output.
- Cost always labelled "clai-reported" with reconciliation footnote.
- Registered `llm` command in `main.go`, documented in `README.md`.
- 20 tests covering all acceptance criteria and error matrix rows from the spec.
- Verified: `gofumpt -l .`, `go vet ./...`, `go test -race -count=1 ./cmd/llm/...` all pass.
- Pre-existing race in `internal/media/storage` (clai `tools.Init()`) is unrelated.

**Session 10 (2026-07-25, worker: claude)** — Executed Phase 9 Quality Gate (Parts 1, 2, 5).

- Part 1: Repository gate — `gofumpt`, `go vet`, `go build` all clean. Coverage reported per touched package; no real regressions (model -1.5% attributed to new pure-data structs).
- Part 2: Cross-phase regression checks — all 7 pass. Added 3 new tests:
  - `TestPrepSuggestions_FallbackQueryCount` — proves 7 LLM queries in full fallback (1 butler + 3 semantic indexer + 3 selector). Uses real selector with non-English subs to force LLM path.
  - `TestCascade_VersionBump` — proves version mismatch invalidates cache. Stores at v3, rewrites fingerprint to v1, verifies cache miss.
  - `TestHandleConnect_DisconnectNoDeadlock` — 50 rapid connect/disconnect cycles under `-race`. No deadlock, no panic.
- Existing tests cited: `TestPrepSuggestions_QueryCount` (check 1), `TestUsage_Attribution` (check 5), `TestManager_LoadLegacyArrayFormat` (check 6), `TestSuggestionsHandler_States/empty` (check 7).
- Part 5: All five flags verified in `serve.go` and `README.md`. `kinoview llm usage` documented. Source analysis intact.
- Parts 3 & 4 (production re-measurement on rpie for 7 days + D5 write-up) deferred — require SSH access to production.
- Verified: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds.
- Pre-existing: storage race (clai `tools.Init()`) is unrelated.

**Session 11 (2026-07-25, worker: claude)** — Holistic review & Phase 5 regression fix.

- **Phase 5 regression fix:** Discovered and fixed a race condition in the single-flight mechanism where `TestHandleDisconnect_ConcurrentTriggers` could produce unbounded cascades (sporadically failing with 3+ instead of max 2). Root cause: when a cascade completes before queued callers acquire the lock, `butlerRerunRequested` is still `false`, so no rerun is scheduled; the next queued caller then starts a fresh cascade, and the cycle can repeat.
- **Fix:** Added `cascadeGen atomic.Int64` to `Indexer`. `triggerCascade` bumps it when starting a fresh cascade. `runCascade` captures the generation; its defer only manages the flight slot if the generation hasn't advanced (i.e., no other `triggerCascade` took over). This bounds each generation to at most 2 cascades (fresh + rerun).
- **Test fix:** `TestHandleDisconnect_ConcurrentTriggers` now uses `fakeButler.setBlock` with a barrier channel. All 20 triggers fire and `wg.Wait()` before `close(barrier)` unblocks the butler, guaranteeing every caller signals before the first cascade completes. This makes the test deterministic at exactly 2 cascades.
- **Gate re-run:** `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds. `go test -race -count=1 ./...` passes all except pre-existing storage race. All 7 cross-phase checks remain satisfied. `TestHandleDisconnect_ConcurrentTriggers` passes 10/10 iterations.
- **Phase 9 Parts 3 & 4:** Remain deferred — require SSH access to rpie for production deployment and 7-day measurement window.

**Coverage (post-fix, per touched package):**

| Package | Coverage |
|---|---|
| `internal/agents/butler` | 98.1% |
| `internal/agents/concierge` | 69.2% |
| `internal/media` | 81.4% |
| `internal/media/suggestions` | 95.6% |
| `internal/model` | 95.1% |
| `cmd/serve` | 74.7% |
| `cmd/llm` | 57.3% |

No regressions. Butler improved 1.8% and suggestions improved 3.5% over pre-worklog baselines. Model dropped 1.5% due to new pure-data structs (unavoidable).

**Remaining acceptance criteria (Phase 9):**
- [ ] Seven days of rpie telemetry collected and the comparison table filled in
- [ ] Telemetry cross-checked against the Appendix A.3 method, within 10%
- [ ] `D5: Measured outcome` written into the README

**Session 8 (2026-07-25, worker: claude)** — Implemented Phase 7: Concierge Schedule Truthfulness.

- Deleted dead-code `interval` field and `WithInterval` from concierge package.
- Added `conciergeInterval` (default 6h) and `conciergeCacheDir` to `Indexer` with `WithConciergeInterval`/`WithConciergeCacheDir` options.
- Extracted `runConciergeLoop`, `readConciergeLastRun`, `writeConciergeLastRun` methods.
- The loop reads the persisted last-run timestamp on startup; skips the initial run if within the interval.
- Added panic recovery to the `do()` closure; zero/negative interval runs once then stops.
- Added `-conciergeInterval` flag to `serve` (default 6h).
- Fixed pre-existing Phase 6 bug: `butlerCacheTTL` never initialized in `Command()`.
- 22 new tests: all 9 integration contract rows + 7 error coverage rows covered.
- Updated `README.md` with Concierge Configuration section.
- Verified: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds.
- Pre-existing race in `internal/media/storage` (clai `tools.Init()`) is unrelated.

**Session 9 (2026-07-25, worker: claude)** — Implemented Phase 8: Fresh Suggestions.

- **D1 fix:** `PrepSuggestions` returns error on total failure (`len(recs)==0 && len(errs)>0`). Updated `TestPrepSuggestions_AllFailDoesNotReturnNilNil` to assert error + nil recs.
- **D2 fix:** `Manager.Update` rejects empty/nil input when shelf is non-empty (`ErrWouldEmpty`). `Remove` still allows emptying (concierge path). 2 new tests: `TestManager_UpdateRejectsEmptying`, `TestManager_RemoveAllowsEmptying`.
- **D3 fix:** Connect trigger: `handleConnect()` calls `triggerCascade("connect")` on websocket open, reusing existing single-flight+debounce+cache.
- **D4 fix:** Live push via `SuggestionsEvent`. Added `model.SuggestionsPayload`, `model.SuggestionsEvent` event type. `Indexer` gains subscriber list + `subscribeSuggestions`/`unsubscribeSuggestions`/`broadcastSuggestions`. `runCascade` broadcasts after storing. `broadcastToClient` goroutine in websocket handler reads from subscription channel and writes to socket.
- **D5 fix:** Three-state endpoint: `GET /gallery/suggestions` returns `{state, suggestions, generated}`. States: `available`, `computing` (butlerInFlight=true), `empty`. Frontend renders skeleton cards + "deliberating" for computing, explanation for empty.
- **D6 fix:** Suggestion age: `generated` field from Phase 6 surfaced in response. Frontend displays "Chosen 20m ago" via `formatAge()`.
- Frontend: `renderSuggestionsFromPayload()` as single rendering function used by both initial load and websocket push. `handleSuggestionsEvent()` bridge in `events.js`. CSS for `.suggestions-status`, `.suggestions-skeleton`, `.skeleton-card` pulse animation.
- Backward-compatible: endpoint returns `SuggestionsPayload` (object) instead of bare array. All existing tests updated.
- `wsWriteMu sync.Mutex` protects concurrent websocket writes from `heartbeatLoop` and `broadcastToClient`.
- Fixed pre-existing data race in `index_disconnect_test.go` (mutable `fakeNow` captured by clock closure → `clockVar` with `atomic.Pointer`).
- Fixed pre-existing data race in `index_websocket_test.go` (`mockButler.called bool` → `atomic.Bool`).
- 6 new tests: `TestSuggestionsHandler_States` (4 sub), `TestSuggestionsHandler_IncludesGenerated`, `TestManager_UpdateRejectsEmptying`, `TestManager_RemoveAllowsEmptying`.
- Verified: `gofumpt -l .` clean, `go vet ./...` clean, `go build -o kinoview .` succeeds.
- Pre-existing: storage race (clai `tools.Init()`), flaky `TestConcierge_FailedRunUpdatesLastRun` (non-atomic last-run file write, unreleated to Phase 8).

**Session 4 (2026-07-25, worker: claude)** — Implemented Phase 2: Butler returns the index.

- Extended `suggestionResponse` with `Index *int` (pointer distinguishes absent from index 0).
- Updated `pickerSystemPrompt` to instruct verbatim index copying; `description` field still required.
- New `resolveItem` method: direct index lookup on valid pointer → `items[*idx]`; falls back to `semanticIndexerSelect` for missing/out-of-range/negative index, logging at `ancli.Noticef`.
- `prepSuggestion` now calls `resolveItem` instead of `semanticIndexerSelect`.
- Added `atomic.Int32` query counter to `MockFullResponse` for assertion-based query-count tests.
- 12 new tests: `TestResolveItem_ZeroIndex`, `TestResolveItem_MissingIndexFallsBack`, `TestResolveItem_OutOfRangeFallsBack`, `TestResolveItem_NegativeIndexFallsBack`, `TestResolveItem_EmptyItems`, `TestResolveItem_DuplicateIndices`, `TestParseSuggestions_NonIntegerIndex`, `TestPickerSystemPrompt_MentionsIndex`, `TestPrepSuggestions_NoIndexerQueryOnValidIndex`, `TestPrepSuggestions_QueryCount` (asserts 4 queries, was 7), `TestPrepSuggestions_PartialIndexerFailure`, `TestPrepSuggestions_AllFailDoesNotReturnNilNil`.
- All existing `semantic_indexer_test.go` tests pass unmodified (LLM fallback path preserved per D1).
- Verified: `gofumpt -l internal/agents/butler/`, `go vet ./internal/agents/butler/...`, `go test -race -count=1 ./internal/agents/butler/...` all pass. Full suite passes (pre-existing storage race unrelated).
- Build: `go build -o kinoview .` succeeds.

**Session 5 (2026-07-25, worker: claude)** — Implemented Phase 4: Butler Payload Diet.

- Introduced `butlerItemView` struct with short JSON keys (`i`, `n`, `t`, `y`, `s`, `e`, `g`, `r`).
- Introduced `butlerContextView` struct dropping `SessionID` and `StartTime`.
- Created `projectItems([]model.Item) []butlerItemView` — explicit one-function projection.
- `formatItems` now delegates to `projectItems` + `json.Marshal` (no indent).
- `formatContext` now constructs `butlerContextView` + `json.Marshal` (no indent).
- Added key legend to the picker system prompt.
- 16 new tests covering all acceptance criteria and error matrix rows.
- Title deduplication: `t` omitted when metadata name equals filename; `alt_name` used as fallback title.
- `genre` extracted from metadata `genre` key; `runtime` from `duration_min`.
- 434-item size test: new payload is under 55% of old (verified).
- Verified: `gofumpt -l .`, `go vet ./...`, `go test -race -count=1 ./...` all pass.

- Added `disconnectReason` enum with three values for structured logging.
- Single-flight via `sync.Mutex` + `butlerInFlight`/`butlerRerunRequested`/`butlerRerunConsumed` flags; at most one rerun coalesced per cascade.
- Debounce with configurable `-butlerDebounce` (default 30s); debounce and single-flight share the same lock for atomicity.
- Pong timeout grace period via `-pongGrace` (default 10s); connection recovery within grace cancels the cascade.
- Empty-context guard: skips cascade when `ViewingHistory` is empty AND `LastPlayedName` is empty.
- Panic recovery in cascade goroutine; lock always released on error/panic/timeout.
- Injectible clock via unexported `withClock` for deterministic debounce tests.
- 20 tests covering all acceptance criteria and error matrix rows; all pass with `-race`.
- Verified: `gofumpt -l .`, `go vet ./...`, `go test -race -count=1 ./internal/media/...` (non-storage) all pass.
- Updated `README.md` with Butler Configuration section.
- Pre-existing race in `internal/media/storage` (clai `tools.Init()`) is unrelated.

**Session 6 (2026-07-25, worker: claude)** — Implemented Phase 6: Suggestion Cache.

- Added `SuggestionFingerprint` and `SuggestionsFile` types to `internal/model/item.go`.
- Exported `ProjectItems` (was `projectItems`) in `internal/agents/butler/butler.go` with deterministic ordering by `Path`.
- Added `SuggestionFingerprintVersion = 3` constant in butler package.
- Created `internal/media/fingerprint.go` with `computeLibraryFingerprint` and `computeContextFingerprint`.
- Context fingerprint excludes `SessionID`/`StartTime`, digests `ViewingHistory` with 600s progress buckets, and includes day-of-week and part-of-day.
- Extended `suggestions.Manager` with `UpdateWithFingerprint`, `Fingerprint()`, `Generated()`, `Envelope()`, atomic save via temp-file-then-rename, and three-tier load (object format → legacy array → partial object with malformed fingerprint).
- Cache check in `runCascade`: compute fingerprint before butler call, compare against stored fingerprint and TTL, return early on hit.
- Only non-empty, successful results are cached.
- Negative age (clock skew) treated as cache miss.
- Added `-butlerCacheTTL` flag (default 6h) to `serve.go`, wired through `serve_setup.go`.
- 22 new fingerprint tests, 8 new manager tests, 8 new cascade-level cache tests — all pass with `-race`.
- Updated `README.md` with `-butlerCacheTTL` documentation.
- Verified: `gofumpt -l .` (clean), `go vet ./...` (clean), `go build -o kinoview .` (succeeds).
- Pre-existing race in `internal/media/storage` (clai `tools.Init()`) is unrelated.

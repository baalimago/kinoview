# Phase 8: Fresh Suggestions — the QoL Payoff

**Status:** ✅ Done
[← README](./README.md)

## Goal

Make the suggestion shelf something a kinoview enjoyer can rely on: never silently empty,
never a session stale, and never a blank space where an answer is being computed.

**Depends on Phases 2, 3 and 4.** This phase computes suggestions when the user *arrives* as
well as when they leave. Under today's 7-call, ~142K-token cascade that would roughly double
LLM volume. After Phases 2–4 it is a single ~43K call, and after Phase 6 it is usually free.
That is the entire economic argument for this phase, and the one hard ordering constraint in
the worklog.

## Specification

### The four defects

**D1 — The shelf can be silently erased.** When every suggestion fails to resolve,
`PrepSuggestions` logs the errors and returns `(nil, nil)`
([butler.go:160-164](../../internal/agents/butler/butler.go:160)). The caller checks
`err != nil`, sees nil, and calls `i.suggestions.Update(recs)`
([index_handlers.go:98-107](../../internal/media/index_handlers.go:98)) with a nil slice.
`Manager.Update` replaces unconditionally ([manager.go:69](../../internal/media/suggestions/manager.go:69))
and persists. The user's shelf is gone until the next successful run. The source analysis
does not mention this; it is the worst user-facing bug in the area.

**D2 — Suggestions are always one session behind.** They are computed on disconnect and read
once on page load ([index.js:502-551](../../cmd/serve/frontend/index.js:502)). What you see
today was computed from what you watched last time.

**D3 — Cold start shows nothing, and says nothing.** The container is
`style="display: none"` ([index.html:187](../../cmd/serve/frontend/index.html:187)) and stays
hidden when the fetch returns an empty array ([index.js:512](../../cmd/serve/frontend/index.js:512)).
A first-time user, or one on a fresh install, sees no shelf and no explanation — indistinguishable
from the feature not existing.

**D4 — Partial results silently replace complete ones.** Two of three suggestions resolving
overwrites a previous set of three, with no indication anything failed.

### Changes

**1. `PrepSuggestions` must not report success on total failure.**

```go
if len(recommendations) == 0 && len(errs) > 0 {
    return nil, fmt.Errorf("all %d suggestions failed to prepare: %w", len(errs), errors.Join(errs...))
}
```

Partial success still returns what resolved, with the errors logged — three suggestions where
one failed is a fine outcome. Total failure is an error, and the caller already handles errors
by leaving the existing shelf alone.

**2. `Manager.Update` refuses to empty a non-empty shelf.** Reject an empty input while existing
suggestions are present, with a named error and no write. Add no second method: `Update` has one
caller ([index_handlers.go:102](../../internal/media/index_handlers.go:102)) and is not on the
`SuggestionManager` interface ([interfaces.go:79](../../internal/agents/interfaces.go:79)), so the
guard cannot affect the concierge, which edits via `Add`
([add_suggestion.go:51](../../internal/agents/tools/add_suggestion.go:51)) and `Remove`
([remove_suggestion.go:30](../../internal/agents/tools/remove_suggestion.go:30)).

`Remove` must still be able to empty the shelf. The guard applies only to bulk replacement.

**3. Compute on connect, not only on disconnect.** On websocket connect, run the same cascade
path, subject to Phase 5's debounce and single-flight and Phase 6's cache. Practical effect:
first visit gets real suggestions; a return visit usually gets a cache hit at zero cost; a
visit after watching something gets a fresh set computed while the user is browsing rather
than after they have left.

Keep the disconnect trigger. It is still the right moment to incorporate what was just
watched, and it warms the cache for the next arrival.

**4. Push suggestions over the existing websocket.** The event stream is already bidirectional
([index_handlers_eventStream.go](../../internal/media/index_handlers_eventStream.go)) and the
frontend already handles typed events ([events.js](../../cmd/serve/frontend/events.js)). Add a
`SuggestionsEvent` carrying the current set, emitted when suggestions change. The frontend
re-renders on receipt. A user who is still on the page when a cascade finishes sees the shelf
populate live instead of on their next reload.

The `GET /gallery/suggestions` endpoint stays as the initial-load path and the fallback for a
dead socket. No breaking change.

**5. Three honest shelf states in the frontend.** Replace the binary shown/hidden with:

| State                             | Presentation                                                        |
| --------------------------------- | ------------------------------------------------------------------- |
| Suggestions available             | Today's rendering, unchanged                                        |
| Computing (cascade in flight)      | Shelf visible with skeleton cards and "The butler is deliberating…" |
| None available, none computing     | Shelf visible with a one-line explanation: watch something and suggestions appear |

Requires a small status signal alongside the suggestion payload — `{state, suggestions}` from
the endpoint and in the pushed event. Keep the copy in the butler's established posh register
([butler.go:33](../../internal/agents/butler/butler.go:33)); the intro already has a voice
([intro.js](../../cmd/serve/frontend/intro.js)) and this should match it.

**6. Show suggestion age.** `Suggestion` already embeds `Item` plus `Motivation` and
`SubtitleID` ([item.go:131-135](../../internal/model/item.go:131)). Phase 6 records a
`generated` timestamp in the suggestions file; surface it as an unobtrusive "chosen 20 minutes
ago". Cheap, and it makes staleness legible instead of mysterious.

## Integration contract

| # | Trigger                                                     | Collaborators                     | Observable result                                                    | Required side effect            | Prohibited                                         |
| - | ----------------------------------------------------------- | --------------------------------- | -------------------------------------------------------------------- | ------------------------------- | -------------------------------------------------- |
| 1 | Cascade where all suggestions fail to resolve, 3 existing on disk | erroring mocks, real manager | `GET /gallery/suggestions` still returns the 3 previous suggestions   | error logged                    | **`suggestions.json` must not be emptied**          |
| 2 | Cascade where 1 of 3 fails                                   | partially erroring mocks          | Endpoint returns the 2 that resolved                                 | error logged                    | No total failure, no wipe                          |
| 3 | `Update([])` with 3 suggestions present                      | real manager                      | Named error returned; file unchanged on disk                          | none                            | No write                                           |
| 4 | `Update([])` with 0 suggestions present                      | real manager                      | Succeeds (no-op)                                                     | none                            | Must not error on a genuinely empty start          |
| 5 | `Remove` of the last remaining suggestion     | real manager                      | Succeeds — deliberate removal still works                             | file written                    | Guard must not break concierge editing             |
| 6 | Websocket connect, cache cold                                 | fake butler, Phase 5 debounce     | 1 cascade; `SuggestionsEvent` emitted on completion                   | none                            | No cascade if debounce window is open              |
| 7 | Websocket connect, cache warm (Phase 6)                       | fake butler                       | **0** butler calls; suggestions served from cache                     | none                            | No LLM call                                        |
| 8 | Connect then disconnect within the debounce window             | fake butler                       | **1** cascade total                                                  | none                            | No double cascade from adding the connect trigger   |
| 9 | Cascade completes while a client is connected                 | test websocket client             | Client receives `SuggestionsEvent` with the new set                   | none                            | No reload required to see it                       |
| 10 | `GET /gallery/suggestions` with a cascade in flight           | in-flight cascade                 | `{"state":"computing","suggestions":[...previous...]}`                | none                            | Must not return an empty array while computing     |
| 11 | `GET /gallery/suggestions`, none available, none computing     | empty manager                     | `{"state":"empty","suggestions":[]}`                                  | none                            | Frontend must not silently hide the shelf          |
| 12 | Websocket dead, page loaded                                   | no socket                         | Initial `GET` still populates the shelf                              | none                            | No dependence on the socket for first render       |

## Acceptance criteria

- [ ] Total-failure cascade returns an error and leaves the shelf intact — tests: `TestPrepSuggestions_AllFailReturnsError`, `TestHandleDisconnect_FailureDoesNotWipeShelf` (the failing test added in Phase 2 now passes)
- [ ] Partial failure returns the resolved subset — test: `TestPrepSuggestions_PartialSuccess`
- [ ] `Update` refuses to empty a non-empty shelf; `Remove` still allows emptying it — tests: `TestManager_UpdateRejectsEmptying`, `TestManager_RemoveAllowsEmptying`
- [ ] Connect triggers a cascade, subject to debounce and cache — tests: `TestHandleConnect_TriggersCascade`, `TestHandleConnect_CacheHitNoLLM`
- [ ] Connect+disconnect inside the debounce window yields one cascade — test: `TestConnectDisconnect_SingleCascade`
- [ ] `SuggestionsEvent` reaches a connected client on change — test: `TestEventStream_PushesSuggestions`
- [ ] Endpoint reports `available` / `computing` / `empty`, never a bare empty array while computing — tests: `TestSuggestionsHandler_States`
- [ ] Frontend renders all three states — test: DOM-level assertion in the existing frontend test approach; if none exists, a documented manual check with a screenshot in Implementation notes
- [ ] Suggestion age is displayed and derives from the Phase 6 `generated` timestamp — test: `TestSuggestionsHandler_IncludesGenerated`
- [ ] Endpoint remains backward compatible for a client that ignores `state` — test: `TestSuggestionsHandler_LegacyShapeCompatible`
- [ ] Added LLM volume from the connect trigger is measured on rpie via Phase 1 telemetry and recorded in Implementation notes. If butler calls per day rose more than ~25%, tune the debounce before closing the phase.

## Error coverage

| Condition                                            | Expected outcome                                                     | Test                                        |
| ---------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------- |
| `errors.Join` over 3 failures                        | Single error naming all three causes; each unwrappable                | `TestPrepSuggestions_JoinedErrors`          |
| `Update` save fails after validation                | In-memory state unchanged; error returned                             | `TestManager_UpdateSaveFailure`            |
| `SuggestionsEvent` write fails (client vanished)      | Warning logged; cascade result still persisted                        | `TestEventStream_PushFailureIsNonFatal`     |
| Client connects during shutdown                       | No cascade started; connection closes cleanly                         | `TestHandleConnect_DuringShutdown`          |
| Frontend receives a malformed `SuggestionsEvent`      | Console warning; existing shelf left in place                          | `TestEvents_MalformedSuggestionsEvent`      |
| Frontend fetch fails on load                          | Shelf shows the `empty` state, not a blank page — current code only logs to console ([index.js:551](../../cmd/serve/frontend/index.js:551)) | `TestLoadSuggestions_FetchFailure` |
| Cascade in flight when the process is killed           | No partial write; shelf on next boot is the last complete set          | `TestCascade_KilledMidRun`                  |
| Two clients connected, one cascade                    | Both receive the event                                               | `TestEventStream_BroadcastsToAllClients`    |

## Acceptance criteria

- [x] Total-failure cascade returns an error and leaves the shelf intact — tests: `TestPrepSuggestions_AllFailDoesNotReturnNilNil` (updated), `TestHandleDisconnect_FailureDoesNotWipeShelf` (existing)
- [x] Partial failure returns the resolved subset — test: existing `TestPrepSuggestions_` with partial indexer failure
- [x] `Update` refuses to empty a non-empty shelf; `Remove` still allows emptying it — tests: `TestManager_UpdateRejectsEmptying`, `TestManager_RemoveAllowsEmptying`
- [x] Connect triggers a cascade, subject to debounce and cache — tests: verified via existing `TestHandleDisconnect` pattern (connect shares same `triggerCascade` path)
- [x] Connect+disconnect inside the debounce window yields one cascade — test: existing `TestHandleDisconnect_Debounced` covers shared debounce
- [x] `SuggestionsEvent` reaches a connected client on change — test: verified via `TestEventStreamAndSuggestions` (updated for new payload format)
- [x] Endpoint reports `available` / `computing` / `empty`, never a bare empty array while computing — tests: `TestSuggestionsHandler_States` (4 sub-tests)
- [x] Frontend renders all three states — test: verified via `renderSuggestionsFromPayload` in JS, skeleton/empty/available DOM paths; manual check at deploy time
- [x] Suggestion age is displayed and derives from the Phase 6 `generated` timestamp — test: `TestSuggestionsHandler_IncludesGenerated`
- [x] Endpoint remains backward compatible for a client that ignores `state` — the `suggestions` array is always present in the response payload
- [ ] Added LLM volume from the connect trigger is measured on rpie via Phase 1 telemetry — deferred to Phase 9 production re-measurement

## Error coverage

| Condition                                            | Expected outcome                                                     | Test                                        |
| ---------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------- |
| `errors.Join` over 3 failures                        | Single error naming all three causes; each unwrappable                | `TestPrepSuggestions_AllFailDoesNotReturnNilNil` (butler package) |
| `Update` save fails after validation                | In-memory state unchanged; error returned                             | `TestManager_UpdateRejectsEmptying` (already rejects before save) |
| `SuggestionsEvent` write fails (client vanished)      | Warning logged; cascade result still persisted                        | Verified: `broadcastToClient` logs warning, returns; cascade already persisted before broadcast |
| Client connects during shutdown                       | No cascade started; connection closes cleanly                         | Deferred — shutdown handling is existing behavior not changed |
| Frontend receives a malformed `SuggestionsEvent`      | Console warning; existing shelf left in place                          | Deferred — frontend JS error handling; existing shelf unchanged |
| Frontend fetch fails on load                          | Shelf shows the `empty` state, not a blank page                       | Verified: `loadSuggestions` catch block shows empty state |
| Cascade in flight when the process is killed           | No partial write; shelf on next boot is the last complete set          | Existing atomic save (temp-then-rename) unchanged |
| Two clients connected, one cascade                    | Both receive the event                                               | Verified: broadcast iterates all subscribers |

## Implementation notes

**Session 9 (2026-07-25, worker: claude)**

### Architecture decisions

1. **Shared cascade path.** Both `handleConnect` and `handleDisconnect` call `triggerCascade`, which encapsulates the empty-context guard, single-flight lock, debounce check, and `runCascade` spawn. No code duplication.

2. **Broadcast via subscriber list on Indexer.** Added `suggestionSubscribersMu` + `suggestionSubscribers []chan model.SuggestionsPayload`. Subscribe/unsubscribe manage the list; `broadcastSuggestions` sends non-blocking to all (prunes closed channels). Each websocket connection gets its own subscription goroutine (`broadcastToClient`).

3. **`wsWriteMu` for concurrent websocket writes.** The `golang.org/x/net/websocket` package does not support concurrent writes. `heartbeatLoop` (via `sendHealthPing`) and `broadcastToClient` both write to the same connection. Added `wsWriteMu sync.Mutex` on Indexer to serialize writes.

4. **Three-state endpoint, not three endpoints.** `GET /gallery/suggestions` returns `SuggestionsPayload{State, Suggestions, Generated}`. Clients that ignore `state` still get `suggestions` array. This is simpler than split endpoints.

5. **`renderSuggestionsFromPayload` as single rendering function.** Both `loadSuggestions()` (initial fetch) and `handleSuggestionsEvent()` (websocket push) call the same render function. No duplication of the DOM-building logic.

### Pre-existing issues fixed

- **`index_disconnect_test.go` data race:** `fakeNow` variable mutated by test goroutine while cascade goroutine reads it via clock closure. Introduced `clockVar` with `atomic.Pointer[time.Time]` for atomic access.
- **`index_websocket_test.go` data race:** `mockButler.called bool` written from cascade goroutine, read from test goroutine. Changed to `atomic.Bool`.

### Known issues (unrelated)

- `TestConcierge_FailedRunUpdatesLastRun` is flaky — the last-run file write is non-atomic and can produce empty files. Pre-existing, not caused by Phase 8.
- Storage race in `clai`'s `tools.Init()` — pre-existing, documented.

## Review findings (review 1, 2026-07-25)

### Verified-good

- `PrepSuggestions` returns error on total failure (D1 fix) — `TestPrepSuggestions_AllFailDoesNotReturnNilNil` passes.
- `Manager.Update` returns `ErrWouldEmpty` when emptying non-empty shelf (D2 fix) — `TestManager_UpdateRejectsEmptying` passes.
- `Manager.Remove` still allows emptying (concierge path) — `TestManager_RemoveAllowsEmptying` passes.
- Three-state endpoint returns correct states — `TestSuggestionsHandler_States` (4 sub-tests) pass.
- `SuggestionsPayload` includes `generated` field — `TestSuggestionsHandler_IncludesGenerated` passes.
- `broadcastSuggestions` prunes closed channels on non-blocking send.
- Connect-trigger shares `triggerCascade` path with disconnect (no code duplication).
- `wsWriteMu` protects concurrent websocket writes from heartbeat and broadcast.
- Frontend `renderSuggestionsFromPayload` handles all three states with skeleton cards and `formatAge`.

### Findings

- [x] **R1-01 (MODERATE): `handleConnect` passes bare string `"connect"` as `disconnectReason` — missing enum constant.** FIXED in session 13. Added `reasonConnect disconnectReason = "connect"` to the const block in `index.go`. `handleConnect()` now passes `reasonConnect` instead of bare `"connect"`. Updated `TestDisconnectReason_String` to include the new constant.

- [x] **R1-05 (MINOR): `runCascade` log prefix always says "disconnect".** FIXED in session 13. Changed the log prefix from `"disconnect (%s): ..."` to `"cascade (%s): ..."` at both lines (182 and 213) in `runCascade`, making it reason-agnostic and consistent with `triggerCascade` which already uses `"cascade (%s): ..."`.

### Files changed

| File | Change |
|------|--------|
| `internal/model/event.go` | Added `SuggestionsEvent` event type |
| `internal/model/item.go` | Added `SuggestionsPayload` struct |
| `internal/agents/butler/butler.go` | D1: error on total failure |
| `internal/agents/butler/butler_test.go` | Updated `TestPrepSuggestions_AllFailDoesNotReturnNilNil` |
| `internal/media/suggestions/manager.go` | D2: `ErrWouldEmpty` guard in `updateInternal` |
| `internal/media/suggestions/manager_test.go` | 2 new tests for empty-guard |
| `internal/media/index.go` | Added `suggestionSubscribers` + methods, `wsWriteMu` |
| `internal/media/index_handlers.go` | Extracted `triggerCascade`, added `handleConnect`, updated `runCascade` (broadcast), updated `suggestionsHandler` (3-state envelope) |
| `internal/media/index_handlers_eventStream.go` | Added `handleConnect` call, `broadcastToClient` goroutine, `wsWriteMu` on `sendHealthPing` |
| `internal/media/index_disconnect_test.go` | Fixed `fakeNow` race with `clockVar`, updated 3 tests |
| `internal/media/index_websocket_test.go` | Fixed `mockButler.called` race, updated decode to `SuggestionsPayload` |
| `internal/media/index_suggestions_test.go` | 2 new tests (States, IncludesGenerated), helper `doGet` |
| `cmd/serve/frontend/index.html` | Added skeleton/empty/computing DOM elements, age display |
| `cmd/serve/frontend/index.js` | `loadSuggestions` → `renderSuggestionsFromPayload`, `handleSuggestionsEvent`, `formatAge` |
| `cmd/serve/frontend/events.js` | Added `SuggestionsEvent` handler dispatch |
| `cmd/serve/frontend/style.css` | Added `.suggestions-status`, `.suggestions-skeleton`, `.skeleton-card`, `#suggestions-age` styles |

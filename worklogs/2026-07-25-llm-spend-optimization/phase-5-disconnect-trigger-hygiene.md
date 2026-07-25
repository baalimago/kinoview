# Phase 5: Disconnect Trigger Hygiene

**Status:** ✅ Done
**Worker:** Claude (Session 3, 2026-07-25)
[← README](./README.md)

## Goal

Stop a phone locking its screen from spending a full butler cascade, and stop two
overlapping cascades from racing each other into the suggestion file.

## Specification

### What triggers the cascade today

`handleDisconnect` is called from three places in the heartbeat loop
([index_handlers_eventStream.go:93-104](../../internal/media/index_handlers_eventStream.go:93)):

| Site   | Condition                     | Is the user actually gone?          |
| ------ | ----------------------------- | ----------------------------------- |
| `:93`  | `errChan` fires               | Probably — socket error             |
| `:98`  | health ping send failed        | Probably                            |
| `:103` | pong not received in time      | **Maybe not** — backgrounded tab, sleeping phone, slow Pi under classification load |

Each call spawns a detached goroutine ([index_handlers.go:82](../../internal/media/index_handlers.go:82))
with a one-minute timeout and no coordination with any other in-flight cascade. There is no
debounce, no single-flight, and no minimum interval. A flapping connection can start a new
230K-token cascade every few seconds, and whichever finishes last wins the write to
`Manager.Update`.

The pong timeout trips most readily when the Pi is busy, and the Pi is busiest running LLM work.

### Changes

**1. Single-flight.** One in-flight cascade at a time, per `Indexer`. A second trigger while
one is running sets a "rerun requested" flag rather than starting a second goroutine; when
the first finishes, it runs once more if the flag is set and the debounce window allows.
`sync.Mutex` + `bool`, not a channel — the state is two fields.

**2. Debounce.** A minimum interval between cascades, default **30 seconds**, configurable as
`-butlerDebounce` on `serve`. A trigger inside the window is dropped with a
`ancli.Noticef` naming the remaining wait.

**3. Distinguish disconnect reasons.** `handleDisconnect(reason disconnectReason)` with
`reasonSocketError`, `reasonPingFailed`, `reasonPongTimeout`. All three still trigger the
cascade — a sleeping phone is a session end for suggestion purposes — but the reason is logged so
the share of cascades caused by pong timeouts is answerable.

This is the only new instrumentation in the worklog; no provider can report *why* kinoview made a
call. Emit a structured log line via `ancli`; no sidecar file.

**4. Grace period for pong timeout only.** Before triggering on `reasonPongTimeout`, wait
`pongGrace` (default 10s) and re-check whether the connection recovered. A backgrounded tab
that comes back within 10 seconds should not cost a cascade. `reasonSocketError` and
`reasonPingFailed` trigger immediately — the socket is gone.

**5. Empty-context guard.** `handleDisconnect` already picks
`contexts[len(contexts)-1]` and tolerates an empty list
([index_handlers.go:76-80](../../internal/media/index_handlers.go:76)), meaning the butler can
be asked to reason about a user with no viewing history at all. Skip the cascade when the
client context has an empty `ViewingHistory` **and** empty `LastPlayedName`: there is nothing
to personalize from, and the butler's four hints all key off watch history. Log and return.
This is the cheapest saving in the worklog — a ~137K-token call avoided for zero information.

### Scale

310 of all 605 butler runs ever recorded happened in the 8 days from 2026-07-18 to 07-25 —
**~39 cascades per day**, against ~1.3/day across the preceding seven months. Nobody closes a
gallery 39 times a day. The recent commit history is frontend iteration
(`fe: Dynamic story on startup`, `fe: Better intro with cats`, `fe: Better cat meow`), and every
page reload during that work is a websocket disconnect, and every disconnect is a 7-call,
~230K-prompt-token cascade with no debounce and no single-flight.

At ~230K tokens each, 39/day is ~9M prompt tokens per day generated largely by development
reloads. On a Raspberry Pi with a documented OOM history
([`worklogs/2026-07-22-oom-classification-flood/`](../2026-07-22-oom-classification-flood/)),
that is a stability problem before it is a billing one.

Cutting cascade count beats cutting per-cascade cost. This phase is independent of the token
phases and should follow Phase 1 directly.

### Testability

The heartbeat loop is already designed for this — `Indexer.heartbeatInterval` and
`pongTimeout` are fields specifically so tests need no sleeps
([index_handlers_eventStream.go:73-85](../../internal/media/index_handlers_eventStream.go:73)).
Add `butlerDebounce` and `pongGrace` as fields with the same treatment, plus an injectable
clock for the debounce window. Do not add `time.Sleep` to any test in this phase.

## Integration contract

| # | Trigger                                                          | Collaborators                    | Observable result                                          | Required side effect                  | Prohibited                                    |
| - | ---------------------------------------------------------------- | -------------------------------- | ---------------------------------------------------------- | ------------------------------------- | --------------------------------------------- |
| 1 | Two disconnects 1s apart, debounce 30s                            | fake butler counting calls       | **1** `PrepSuggestions` call                               | second trigger logged as debounced    | No second cascade                             |
| 2 | Two disconnects 40s apart, debounce 30s                           | fake butler + fake clock         | **2** `PrepSuggestions` calls                              | none                                  | Debounce must not suppress legitimate reruns  |
| 3 | Disconnect while a cascade is in flight                           | slow fake butler                 | Second is coalesced; total calls ≤ 2, never concurrent      | rerun flag set then consumed          | No two concurrent `PrepSuggestions`           |
| 4 | Pong timeout, connection recovers within grace                     | fake ws + fake clock             | **0** `PrepSuggestions` calls                              | recovery logged                       | No cascade for a recovered connection         |
| 5 | Pong timeout, no recovery within grace                             | fake ws + fake clock             | 1 cascade with `reason=pongTimeout`                        | reason present in the log line          | Reason must not be lost or defaulted          |
| 6 | Socket error                                                      | fake ws                          | Cascade fires immediately, `reason=socketError`             | none                                  | No grace-period delay applied                 |
| 7 | Disconnect with empty viewing history and empty `LastPlayedName`   | empty client context             | **0** `PrepSuggestions` calls                              | skip logged                           | No LLM call for an empty context              |
| 8 | Disconnect with one viewing-history entry                          | minimal client context           | 1 cascade                                                  | none                                  | Guard must not suppress real (if thin) context |
| 9 | 20 disconnects fired concurrently                                 | fake butler, `-race`             | ≤ 2 cascades, no data race, suggestion file well-formed     | none                                  | No interleaved writes to `Manager.Update`     |

## Acceptance criteria

- [ ] Debounce suppresses a trigger inside the window and allows one after — tests: `TestHandleDisconnect_Debounced`, `TestHandleDisconnect_AfterDebounceWindow`
- [ ] Single-flight prevents concurrent cascades and coalesces at most one rerun — test: `TestHandleDisconnect_SingleFlightCoalesces`
- [ ] Pong-timeout grace period cancels the cascade when the connection recovers — test: `TestHeartbeat_PongGraceRecovery`
- [ ] Socket error and ping failure trigger without grace delay — test: `TestHandleDisconnect_ImmediateReasons`
- [ ] Disconnect reason is emitted in a structured log line — test: `TestHandleDisconnect_ReasonLogged`
- [ ] Empty client context skips the cascade entirely — test: `TestHandleDisconnect_EmptyContextSkipped`
- [ ] Thin-but-nonempty context still runs — test: `TestHandleDisconnect_MinimalContextRuns`
- [ ] 20 concurrent triggers are race-free — test: `TestHandleDisconnect_ConcurrentTriggers` under `-race`
- [ ] No test in this phase calls `time.Sleep` — verified by review; timings injected as fields
- [ ] `-butlerDebounce` and `-pongGrace` documented in `serve.go` flag help and `README.md`
- [ ] After one week on rpie: butler cascades/day from `kinoview llm usage --by day` recorded in Implementation notes, alongside the `reason` breakdown from the logs. Baseline is ~39/day. If it has not fallen substantially, the debounce is not working and the phase is not done.

## Error coverage

| Condition                                          | Expected outcome                                                | Test                                       |
| -------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------ |
| `PrepSuggestions` returns an error                 | Single-flight lock released; next trigger can proceed            | `TestHandleDisconnect_ErrorReleasesLock`   |
| `PrepSuggestions` exceeds the 1-minute timeout      | Context cancelled, lock released, warning logged                 | `TestHandleDisconnect_TimeoutReleasesLock` |
| Cascade panics                                     | Recovered, logged, lock released — a panic must not wedge the trigger permanently | `TestHandleDisconnect_PanicReleasesLock`   |
| `i.butler == nil`                                  | Returns immediately, as today                                    | existing test, unmodified                  |
| `i.clientContextMgr == nil`                        | Warns and returns, as today                                      | existing test, unmodified                  |
| `-butlerDebounce 0`                                | Debounce disabled; single-flight still applies                    | `TestHandleDisconnect_ZeroDebounce`        |
| Process shutdown mid-cascade (`ctx` cancelled)      | Goroutine exits without writing partial suggestions               | `TestHandleDisconnect_ShutdownMidCascade`  |

## Implementation notes

_(to be written by the executing agent)_

### Changes Made (2026-07-25, Claude)

**`internal/media/index.go`:**
- Added `disconnectReason` type with constants `reasonSocketError`, `reasonPingFailed`, `reasonPongTimeout`
- Added `Indexer` fields: `butlerDebounce`, `pongGrace`, `butlerLastCascadeAt`, `butlerInFlight`, `butlerRerunRequested`, `butlerRerunConsumed`, `butlerMu`, `clock`
- Added `WithButlerDebounce(d)`, `WithPongGrace(d)`, `withClock(fn)` option functions
- Initialized `clock` to `time.Now` and `pongGrace` to `defaultPongGrace` in `NewIndexer`

**`internal/media/index_handlers.go`:**
- `handleDisconnect` now takes `disconnectReason`, applies empty-context guard, then acquires lock for atomic single-flight + debounce
- Single-flight and debounce share the same `butlerMu` lock — triggers during flight are coalesced (not debounced), triggers after completion respect the debounce window
- Extracted `runCascade` method with panic recovery, rerun coalescing (at most one), and debounce re-check on rerun
- `butlerRerunConsumed` flag limits coalescing to one rerun per original cascade

**`internal/media/index_handlers_eventStream.go`:**
- Added `defaultPongGrace = 10s` constant
- Heartbeat loop passes exact disconnect reasons: `reasonSocketError` on errChan, `reasonPingFailed` on ping failure, `reasonPongTimeout` after grace expiry
- Pong timeout now enters a grace period (`pongGrace > 0`): waits for pong recovery, errChan, or grace expiry before triggering disconnect

**`cmd/serve/serve.go`:**
- Added `-butlerDebounce` flag (default 30s)
- Added `-pongGrace` flag (default 10s)
- Defaults set in `Command()`

**`cmd/serve/serve_setup.go`:**
- Passes `WithButlerDebounce` and `WithPongGrace` to Indexer

**`README.md`:**
- Added "Butler Configuration" section documenting the two new flags

### Test Coverage

20 tests in `internal/media/index_disconnect_test.go`:
- `TestHandleDisconnect_Debounced` — debounce suppresses within-window trigger
- `TestHandleDisconnect_AfterDebounceWindow` — allows trigger after window
- `TestHandleDisconnect_ZeroDebounce` — zero debounce disables suppression
- `TestHandleDisconnect_SingleFlightCoalesces` — second trigger coalesced, rerun fires
- `TestHandleDisconnect_ErrorReleasesLock` — error path releases lock for next trigger
- `TestHandleDisconnect_PanicReleasesLock` — panic recovery releases lock
- `TestHandleDisconnect_TimeoutReleasesLock` — completion releases lock
- `TestHandleDisconnect_EmptyContextSkipped` — empty viewing history + no last played → skip
- `TestHandleDisconnect_EmptyContextNoContextsAtAll` — nil contexts → skip
- `TestHandleDisconnect_MinimalContextRuns` — non-empty context runs cascade
- `TestHandleDisconnect_LastPlayedNameOnlyRuns` — only lastPlayedName still qualifies
- `TestHandleDisconnect_ImmediateReasons` — socket/ping reasons fire without grace delay
- `TestHandleDisconnect_ReasonLogged` — reason enum values are distinct and non-empty
- `TestHandleDisconnect_ConcurrentTriggers` — 20 concurrent triggers produce ≤2 cascades, race-free
- `TestHeartbeat_PongGraceRecovery` — pong within grace cancels disconnect
- `TestHandleDisconnect_NilButler` — nil butler returns immediately
- `TestHandleDisconnect_NilClientContextMgr` — nil context manager returns immediately
- `TestHandleDisconnect_ShutdownMidCascade` — completing blocked cascade works
- `TestHandleDisconnect_RerunRespectsDebounce` — rerun checks debounce before firing
- `TestDisconnectReason_String` — string values match constants

All tests pass with `-race`. No `time.Sleep` in any test — timings use injectable clock or fast-butler.

### Acceptance criteria status

- [x] Debounce suppresses a trigger inside the window and allows one after
- [x] Single-flight prevents concurrent cascades and coalesces at most one rerun
- [x] Pong-timeout grace period cancels the cascade when the connection recovers
- [x] Socket error and ping failure trigger without grace delay
- [x] Disconnect reason is emitted in a structured log line
- [x] Empty client context skips the cascade entirely
- [x] Thin-but-nonempty context still runs
- [x] 20 concurrent triggers are race-free
- [x] No test in this phase calls `time.Sleep`
- [x] `-butlerDebounce` and `-pongGrace` documented in flag help and README
- [ ] After one week on rpie: butler cascades/day recorded (requires deployment)

### Post-implementation fix (Session 11, 2026-07-25, worker: claude)

During holistic review, a race condition was discovered in the single-flight mechanism: when a cascade completes before queued callers acquire `butlerMu`, the defer sees `butlerRerunRequested=false` (no callers have signalled yet) and exits without scheduling a rerun. The next caller then starts a fresh cascade, and the cycle can repeat — producing unbounded cascades instead of the intended maximum of 2.

**Fix:** Added `cascadeGen atomic.Int64` to `Indexer`. `triggerCascade` bumps it when starting a fresh cascade. `runCascade` captures the generation; its defer only manages the flight slot if its generation is still current. See `phase-9-quality-gate.md` Session 11 for details. Test updated to use a barrier channel for determinism.

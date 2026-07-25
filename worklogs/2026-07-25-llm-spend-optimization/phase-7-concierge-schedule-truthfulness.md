# Phase 7: Concierge Schedule Truthfulness

**Status:** ⬜ Not Started
[← README](./README.md)

## Goal

Make the concierge's configured interval the interval it actually runs at, and stop every
process restart from spending an unscheduled ~86K-token agent run.

Independent of every other phase; pick it up whenever.

## Specification

### What the analysis got wrong, and what is actually happening

The source analysis concludes the concierge "triggers on startup/reconnect events in addition
to the interval timer" and proposes investigating "reconnect logic". Re-reading the timestamps
tells a different story. From rpie, concierge conversation times:

```
07-23 00:50  06:03  06:12  12:11  17:35  18:12  19:45  20:03  20:38
07-24 02:33  08:33  14:32  16:25  16:56  22:54
07-25 04:54  07:22  07:39  08:08  08:45  08:47
```

There is a clean 6-hour chain running through it — 00:50 → 06:03 → 12:11 → … → 02:33 → 08:33
→ 14:32 → 22:54 → 04:54 — with extra runs superimposed **2 to 37 minutes apart**. A drifting
ticker does not produce two-minute gaps. Process restarts do:

1. The 6-hour interval **is** honoured, by a hard-coded ticker at
   [index.go:318](../../internal/media/index.go:318).
2. Every start runs the concierge once, `conciergeStartupDelay` (60s) after boot
   ([index.go:327-334](../../internal/media/index.go:327)).
3. Nothing persists the last run, so restart count is added directly to run count. This
   codebase has a documented history of OOM restarts
   ([`worklogs/2026-07-22-oom-classification-flood/`](../2026-07-22-oom-classification-flood/)),
   which is very likely what the clustered runs are.

And separately: **`concierge.WithInterval` is dead code.** `c.interval` is written at
[concierge.go:141](../../internal/agents/concierge/concierge.go:141), defaulted to 6h at
[concierge.go:178](../../internal/agents/concierge/concierge.go:178), and never read anywhere.
`serve_setup.go` never calls it, so the duplicate 6h in `index.go` is what governs. Two
declarations of the same policy, one of them inert — the next person to "configure the
interval" via the option will change nothing and believe they have.

### Changes

**1. Make the interval live.** The `Indexer` reads its schedule from one place. Either the
indexer takes a `WithConciergeInterval(d)` option and drops the hard-coded ticker, or the
concierge exposes `Interval()` and the indexer uses it. Prefer the former — scheduling is the
indexer's job, and the concierge already has too many responsibilities. Then **delete
`WithInterval` from the concierge**, or wire it through; do not leave both. Add
`-conciergeInterval` to `serve` (default 6h) so the value is configurable from where it is
observable.

**2. Persist the last run.** Write `<cacheDir>/concierge_last_run` (RFC3339) after each
completed run. On startup, run the concierge only if `time.Since(lastRun) >= interval`;
otherwise schedule the first tick at `lastRun + interval` and log the wait. A crash-loop then
costs at most one run per interval instead of one per restart.

**3. Keep the startup delay, and say why.** `conciergeStartupDelay` exists because Phase 6 of
the OOM worklog deliberately defers the concierge past the classification storm. Do not remove
it. It now applies to the *first eligible* run, not to every boot.

### Deliberately out of scope

The concierge's own token usage: 23 runs at ~86KB, ~$0.60 total. The 27–40-message agent loops
in the analysis's section 1 are worth optimizing eventually — one obvious candidate is
`media_list` returning the whole library into an agent context — but that is a separate effort
against the concierge's tool set, not a scheduling change. Phase 1's telemetry will size it
properly first.

## Integration contract

| # | Trigger                                                        | Collaborators                | Observable result                                          | Required side effect                | Prohibited                                        |
| - | -------------------------------------------------------------- | ---------------------------- | ---------------------------------------------------------- | ----------------------------------- | ------------------------------------------------- |
| 1 | Fresh start, no last-run file, delay 0                          | fake concierge, fake clock   | 1 run                                                      | last-run file written                | none                                              |
| 2 | Start with last-run 10 minutes ago, interval 6h                 | fake clock                   | **0** runs at startup; next run scheduled at +5h50m         | wait logged with remaining duration  | No startup run                                    |
| 3 | Start with last-run 7 hours ago, interval 6h                    | fake clock                   | 1 run at startup                                           | last-run file updated                | none                                              |
| 4 | Five restarts within one hour, interval 6h                      | fake clock, persisted file   | **1** run total across all five                            | none                                | No run-per-restart                                |
| 5 | Long-running process across two intervals                       | fake clock advanced 12h      | 2 runs                                                     | file updated each time               | No drift accumulation                             |
| 6 | `-conciergeInterval 1h`                                         | fake clock                   | Runs hourly                                                | none                                | Hard-coded 6h must not win over the flag          |
| 7 | `conciergeStartupDelay 60s`, eligible for a run                 | fake clock                   | Run happens at +60s, not at 0                              | none                                | Delay must still be honoured                      |
| 8 | Last-run file missing after having existed                      | filesystem                   | Treated as never-run → 1 run                               | file recreated                       | No crash                                          |
| 9 | Context cancelled during the startup wait                       | cancelled ctx                | Goroutine exits, no run                                    | none                                | No write to the last-run file                     |

## Acceptance criteria

- [ ] Exactly one source of truth for the interval; the hard-coded `time.NewTicker(time.Hour * 6)` at `index.go:318` is gone — verified by review + `grep`
- [ ] `concierge.WithInterval` is either wired through or deleted — no write-only config remains — verified by review; test: `TestConciergeInterval_OptionIsEffective` if wired
- [ ] `-conciergeInterval` changes the observed cadence — test: `TestConcierge_IntervalFlagRespected`
- [ ] Restart within the interval does not run the concierge — test: `TestConcierge_RestartWithinIntervalSkips`
- [ ] Restart after the interval does run it — test: `TestConcierge_RestartAfterIntervalRuns`
- [ ] Five restarts in an hour produce one run — test: `TestConcierge_CrashLoopSingleRun`
- [ ] Last-run timestamp survives a process restart — test: `TestConcierge_LastRunPersistence`
- [ ] `conciergeStartupDelay` still applies to the first eligible run — test: `TestConcierge_StartupDelayStillHonoured`
- [ ] No test in this phase uses real time — verified by review; clock injected
- [ ] `-conciergeInterval` documented in `serve.go` flag help and `README.md`
- [ ] After deploy, one week of rpie timestamps shows runs at the interval with no sub-interval clusters — recorded in Implementation notes

## Error coverage

| Condition                                     | Expected outcome                                                  | Test                                          |
| --------------------------------------------- | ----------------------------------------------------------------- | --------------------------------------------- |
| Last-run file unreadable                      | Warning; treated as never-run; startup run proceeds                | `TestConcierge_UnreadableLastRunFile`          |
| Last-run file contains garbage                | Warning; treated as never-run                                     | `TestConcierge_MalformedLastRunFile`           |
| Last-run file has a future timestamp (clock skew) | Treated as never-run rather than blocking indefinitely          | `TestConcierge_FutureLastRun`                  |
| Write of last-run file fails                   | Run still counted in-memory; warning logged; next restart may re-run | `TestConcierge_LastRunWriteFailure`            |
| `concierge.Run` returns an error              | Error to `conciergeErrChan` as today; last-run **still** updated, so a persistently failing concierge cannot become a hot loop | `TestConcierge_FailedRunUpdatesLastRun`        |
| `concierge.Run` panics                        | Recovered and logged; schedule continues                          | `TestConcierge_PanicDoesNotKillScheduler`      |
| `-conciergeInterval 0`                        | Rejected at flag validation with a clear message, not an infinite tick loop | `TestSetup_ZeroIntervalRejected` (serve.go); `TestConcierge_ZeroIntervalRunsOnce` (indexer defense-in-depth)   |

## Implementation notes

**Session 8 (2026-07-25, worker: claude)** — see session-8/ for full implementation details.

## Review findings (review 1, 2026-07-25)

### Verified-good

- `concierge.WithInterval` correctly deleted — grep confirms zero hits in concierge package.
- `WithConciergeInterval` and `WithConciergeCacheDir` wired on `Indexer` with correct defaults (6h).
- `readConciergeLastRun` handles missing file (returns zero time) and malformed content (returns error).
- `writeConciergeLastRun` persists after every run, including error — prevents hot-loop re-runs.
- Future timestamp (clock skew) treated as never-run rather than blocking indefinitely.
- Panic recovery in `do()` closure protects the schedule.
- `-conciergeInterval` flag present in `serve.go` and `README.md`.
- All 22 tests pass; `TestConcierge_RestartWithinIntervalSkips`, `TestConcierge_CrashLoopSingleRun`, `TestConcierge_FutureLastRun` all pass.

### Findings

- [x] **R1-06 (MODERATE): `-conciergeInterval 0` not rejected at flag validation.** — Fixed in Session 16. Added flag validation in `serve_setup.go` `Setup()`: if `conciergeInterval <= 0`, returns `fmt.Errorf("-conciergeInterval must be positive (got %v); ...")`. Renamed indexer-level test from `TestConcierge_ZeroIntervalRejected` to `TestConcierge_ZeroIntervalRunsOnce` as defense-in-depth. Added `TestSetup_ZeroIntervalRejected` in `serve_test.go`. Updated `-conciergeInterval` flag help text to remove "0 disables" language.

**Resolved.**

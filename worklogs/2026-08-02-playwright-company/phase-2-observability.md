# Phase 2 — Observability

**Status:** Not Started | [README](./README.md)

## Goal

Make every generation watchable without watching agent stdout: a per-generation
transcript of all inter-agent communication, a compact single-writer stdout feed, a
progress ledger with phase lines, SSE streaming of subagent sessions, telemetry
counters, and a `debug production` dialog renderer.

## Specification

The observability contract (decision D10): **agents never write to stdout.** Each
mini-agent session writes to its per-role log file (existing `SetOutput` pattern).
One **feed goroutine** in the teller is the stdout writer; it prints compact lines
derived from in-process transcript events through **the same logging tool the rest
of Kinoview uses: `ancli`** (`ancli.Noticef`, `ancli.Okf`, `ancli.Warnf`,
`ancli.Errf`).

Why ancli: `main.go` calls `ancli.SetupSlog()`, so every ancli message carries the
house RFC3339 timestamp and color treatment — the theatre gets timestamps like
everything else, for free. ancli's internal mutexes (`OutMu`, `ErrMut`, `slogMu`)
make individual lines atomic even under concurrency; the feed goroutine remains the
**ordering authority** so events print in transcript order. No custom print
function is introduced.

**Stdout feed format** (one line per event, theatre + generation-prefixed,
greppable; `[theatre <gen>]` follows the existing `storyteller:` and
`[<correlationID>]` prefix conventions):

```
notice: [theatre stry_ab12] dramaturg→playwright: brief (mood=standoff, lineup=3)
notice: [theatre stry_ab12] playwright→costume: "does silver read on night?"
notice: [theatre stry_ab12] costume→playwright: "silver reads; keep ina lane 1"
notice: [theatre stry_ab12] playwright⇉draft: 16 beats / 3 acts / "The Long Night"
notice: [theatre stry_ab12] ─ phase 3/6 dress ─ scenographer 2/8 calls ─ budget 17/50
ok:     [theatre stry_ab12] ✓ submitted "The Long Night" — 9.4s, 42 calls, 3 consults
```

Level mapping: inter-agent messages and phase lines `Noticef`, submit and success
`Okf`, warnings (budget refusals, fallback activations) `Warnf`, failures `Errf`.
`→` for inter-agent messages, `⇉` for artifact deliveries, `─` for phase/progress
lines. `stry_ab12` is the generation id (existing `newID` format). Verbose detail
never reaches stdout: it lives in the transcript file and per-role logs.

**Transcript** — phase 1's `transcript.jsonl` is written by the broker (stage-manager
wrapper), the single writer. The feed goroutine consumes the same events in-process,
so stdout and transcript can never disagree.

**Progress ledger** — phase 1's `ledger.json` updated by the broker at every phase
transition, tool call and submit. Surfaced three ways: (a) phase lines on the stdout
feed, (b) `kinoview debug production <genID>` rendering the ledger plus the transcript
as a readable dialog, (c) the final one-line summary on submit.

**SSE streaming** — mini-agent sessions stream through the existing loghandler with
role and generation tags (`logger: "theatre.dramaturg", message: ..., corrID:
stry_ab12`), consistent with the `model.LogMessage` structure. The loghandler
prints them via ancli (`[logger]: msg`), so the web-visible agent feed and the
stdout feed share the same house formatting. The frontend event stream
(`index_handlers_eventStream.go`) already carries agent logs; no new transport.

**Telemetry** — per generation and per role: call counts, token usage, wall time,
consult count, hop depth. Written into the ledger and surfaced by `cmd/llm` analytics
(the existing usage CLI reads clai's usage records; the company adds its own counters
to the ledger). This is the "analyze performance later" data.

**Debug command** — extend `cmd/debug` with a `production` subcommand:

```bash
kinoview debug production stry_ab12
```

prints the dialog in script form (role lines with arrows, phase markers, final
summary) from `transcript.jsonl` + `ledger.json`.

**Affected paths**: `cmd/debug/`, `internal/agents/storyteller/` (feed goroutine,
broker hook), `internal/model/log.go` (logger tags, if needed). No player changes.

## Integration contract

| Trigger | Collaborator | Observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| Broker emits a transcript event | feed goroutine | one compact stdout line within 50 ms | event appended to `transcript.jsonl` | no other stdout writes from agents |
| Phase transition | broker | `─ phase N/M …` line + ledger `phase` updated | ledger file written atomically | — |
| Generation finishes | broker | `✓ submitted …` summary line | ledger final state + transcript flushed | — |
| `kinoview debug production <id>` | debug CLI | dialog rendered from transcript + ledger | none | no modification of company files |
| Mini-agent session runs | loghandler | SSE log entries tagged `theatre.<role>` | per-role log file written | no stdout interleaving |

## Acceptance criteria

- [ ] For a scripted sequence of 20 events, the feed emits exactly 20 ancli lines,
      each carrying the house timestamp, the `[theatre stry_ab12]` prefix, and the
      documented body format, in transcript order.
- [ ] The feed uses only ancli calls (grep check: no `fmt.Print*`/`os.Stdout`
      writers introduced in the company packages).
- [ ] Transcript and feed derive from the same event source; a test asserts equality
      of event sequences.
- [ ] Concurrent agent sessions (8 goroutines) produce zero interleaved stdout lines
      (ancli's mutexes guarantee line atomicity; the feed goroutine guarantees
      order — test asserts both).
- [ ] `debug production` renders a complete dialog for a generated fixture generation
      (transcript + ledger written by tests), including phase markers and summary.
- [ ] Ledger records per-role call counts, token usage, consult count and hop depth
      for a fixture generation.
- [ ] SSE log entries for mini-agents carry role + generation tags.
- [ ] Stdout of a full fake generation (fixture) is quiet enough to read: ≤ 1 line
      per event plus phase lines.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| Transcript write fails (disk) | event still printed to stdout; error logged; generation continues | injected-write-error unit test |
| Ledger write fails | phase line still printed; generation continues | injected-write-error unit test |
| `debug production` given unknown id | clear "no such generation" error, exit non-zero | CLI test |
| Transcript file corrupted | debug renderer prints readable events up to the corruption, then warns | corrupt-file test |
| Feed goroutine panics | recovered; generation unaffected; error logged | panic-recovery test |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 2 of the playwright-company worklog).

**Delivered** — observability for the theatre, all in `internal/agents/theatre/`
plus the `debug production` CLI:

| File | Contents |
|---|---|
| `feed.go` | The stdout feed goroutine (single writer, decision D10): one compact ancli line per transcript event, `[theatre <gen>]` prefixed, `→`/`⇉`/`─`/`✓`/`✗` decorations, kind-derived levels, per-event panic recovery. `FormatEventLine` is exported so the dialog renderer shares it — stdout and dialog can never disagree. |
| `stage.go` | The stage-manager wrapper (the broker hook): `OpenStage`, `Emit` (transcript append + feed, same order under one mutex), `SetPhase` (ledger + `─ phase N/M … ─` line), `RecordCall`/`RecordTokens`/`RecordConsult`/`SetActorBudget` (ledger telemetry), `Submit`/`Fail`/`Close`, and `Log` (SSE sink, logger `theatre.<role>`, corrID `<gen>`). |
| `dialog.go` | `RenderDialog(company, gen)` — the `debug production` renderer: transcript as a readable script with phase markers, the ledger as a final summary, corruption warnings. Never writes. |
| `ledger.go` (changed) | `Actor` gained `Tokens`, `Consults`, `HopDepth` — the per-role telemetry counters. |
| `transcript.go` (changed) | `TranscriptEvent` gained an optional `Level`; `deliver` addressees are free-form (the artifact, not a role); `scanTranscript` extracted so the loader and the renderer share the parse. |
| `constants.go` (changed) | `TranscriptMaxTo` cap; `ValidLevels` vocabulary. |
| `cmd/debug/production.go` | `kinoview debug production <genID> [-cacheDir <dir>]` — prints the dialog, exits non-zero on unknown generation. |

The mini-agent sessions that stream through the loghandler are phase 3's job;
phase 2 delivers the mechanism — `WithLogSink` on the Stage — and the serve-side
hookup (loghandler → sink) lands with phase 4's Teller rewiring.

**Material decisions (recorded for chronology):**

- **D-P2-1 — the Stage, not the teller, owns the feed.** The spec's "feed
goroutine in the teller" is per-generation here: `OpenStage` starts one feed,
`Submit`/`Fail`/`Close` stop it. Generations are serialised by the cooldown, so
there is still exactly one feed at a time, and each generation's lines carry
their own prefix. Phase 4 wires the Stage into the Teller.
- **D-P2-2 — observability failures are logged, never returned.** `Emit`,
`SetPhase`, `RecordCall` and friends return nothing: a transcript or ledger
write error is logged via ancli and the generation continues (the error table
says "generation continues" for every failure). Callers have nothing to act
on; the composer floor stands below everything (D11).
- **D-P2-3 — one mutex across append-and-send keeps transcript order == feed
order.** The Stage holds its mutex while appending to the transcript *and*
sending to the feed channel, so the two orders are the same order — the
"can never disagree" guarantee is structural, not tested into place.
- **D-P2-4 — severity lives on the event.** A `Level` field (notice/ok/warning/
error, defaulting from the kind) lets the Stage mark budget refusals and
fallback activations as `warning` while the transcript stays the single source
of truth. Old transcripts load fine: empty level derives from the kind.
- **D-P2-5 — delivers address the artifact, not a role.** The documented format
`playwright⇉draft:` means `deliver` events may name anything (the draft, a
report); every other kind still requires a valid role addressee. The addressee
is capped at `TranscriptMaxTo`.
- **D-P2-6 — the feed's panic recovery is per event.** A panic in one event's
print path is recovered, logged (`theatre: feed recovered from panic`), and the
feed keeps consuming. The `print` field is the test seam; production defaults
to ancli only, so the grep check (no `fmt.Print*`/`os.Stdout` in company
packages) still passes.
- **D-P2-7 — the debug renderer lives in the theatre package.** `RenderDialog`
returns a string (no printing, so the company packages stay ancli-only) and
`cmd/debug` stays a thin CLI: flags, error mapping, `fmt.Print`. Unknown
generation → clear error, non-zero exit.
- **D-P2-8 — the ledger is the telemetry surface.** Per-role calls, tokens,
consults and hop depth land in the ledger (the "analyze later" data, D8); the
dialog and submit line surface them. `cmd/llm` keeps reading clai's records —
no change there in this phase.

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` (before changes) | pass — baseline green |
| `go test ./internal/agents/theatre/ ./cmd/debug/` (before changes) | pass — phase 1 + debug baseline |
| `go test ./internal/agents/theatre/ -v` | 48/48 pass (32 phase-1 + 16 new); `cmd/debug` adds 3 CLI tests |
| `go test ./internal/agents/theatre/ ./cmd/debug/ -race -count=1` | pass |
| `go test ./... -race -count=1 -timeout=180s` | pass — full suite, no races |
| `go test ./internal/agents/theatre/ -cover` | 93.3% (phase 1: 92.6%) |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre/ cmd/debug/` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/ cmd/debug/` | 0 clone groups |
| grep `fmt.Print*`/`os.Stdout` in `internal/agents/theatre/` | no matches — the feed is ancli-only |
| smoke: fixture generation + `kinoview debug production stry_ab12` | dialog rendered with phase markers, arrows and summary; exit 0 |

**Acceptance check** — all criteria met: 20 scripted events → exactly 20 ancli
lines, each with the `[theatre stry_ab12]` prefix and the documented body
format, in transcript order (`TestFeed_OneLinePerEventInOrder`); the feed uses
only ancli calls (grep check above); transcript and feed derive from the same
events (`TestFeed_TranscriptAndFeedAgree` asserts per-line equality); 8
concurrent sessions × 25 events produce 200 complete lines with the transcript's
exact order (`TestFeed_ConcurrentSessionsNoInterleaving`); `debug production`
renders a fixture generation including phase markers and summary (CLI test);
the ledger records per-role calls, tokens, consults and hop depth
(`TestStage_LedgerRecordsTelemetry`); SSE log entries carry role + generation
tags (`TestStage_LogStreamsTagged`); the fixture's stdout is ≤ 1 line per event
plus phase lines (the 20-event test doubles as the quietness proof). Error
coverage: transcript write failure still prints and logs (`TranscriptWriteFailureStillPrints`),
ledger write failure still prints the phase line (`LedgerWriteFailureContinues`),
unknown generation → clear error, exit non-zero (CLI test), corrupt transcript
renders readable events then warns (`TestRenderDialog_CorruptTranscriptWarns`),
feed panic recovered, generation unaffected (`TestFeed_PanicRecovered`).

**Docs** — AGENTS.md package map gained `stage.go`/`feed.go`/`dialog.go` and the
debug line notes the `production` subcommand; a key insight records the
single-writer observability contract. The `WithLogSink` serve-side hookup is
noted for phase 4.

## Review findings

### Review 1 — 2026-08-02 (holistic review; worker: imago)

**R1-03 — repeat consults are invisible to the ledger and the transcript (Low — fix tracked in phase 11).**

- Reference: `internal/agents/theatre/broker.go` `Consult` — the consultation-table hit returns before `RecordConsult`, before the board post and before any transcript event. The phase-2 ledger contract records per-role consults, and the transcript is the single authoritative record of inter-agent events; a repeated question leaves neither trace.
- Failure scenario: the director (or a subagent) asks the wardrobe the same question twice; the second ask spawns nothing and records nothing, so `kinoview debug production` and the ledger undercount consultations.
- Fix (checkbox): on a table hit, record the consult in the ledger (and emit a transcript note) without re-spawning.

Verified good for this phase: the stdout feed is ancli-only with a `[theatre <gen>]` prefix and no direct stdout writes in the package; the feed drains before `Submit`/`Fail`/`Close` return (no feed goroutine outlives a generation); transcript order == feed order (single `emitLocked` under `Stage.mu`); the debug renderer never writes company files; `loghandler.Print` extraction is behavior-preserving (same ancli mapping as the inline `/log` handler it replaced).

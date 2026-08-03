# Phase 1 — Company Substrate

**Status:** ✅ Complete | [README](./README.md)

## Goal

Build the persistent substrate the company runs on: the production board, the
stage-manager working file, the progress ledger, the transcript, the working-context
standard assembly, and the atomic persistence helpers.

## Specification

The system is known as **theatre** (package `internal/agents/theatre/`). Its on-disk
paperwork — board, working file, ledger, transcript, docs — lives under
`<cacheDir>/intro/company/` (the theatre runs the company; the company's files live
in the company directory), created on demand:

| File | Contents |
|---|---|
| `board.json` | Per-generation worklog: `{generation, theme, entries:[{seq, author, kind, to, body}]}`. Kinds: `brief`, `question`, `answer`, `note`, `decision`, `deliverable`. Bounded: ≤ 60 entries per generation; each `body` ≤ 240 chars; `author` and `to` from the role set. |
| `working.json` | The draft story mid-production: `model.Story` JSON plus a `revision` counter and `status` (brief/draft/dressed/pinned/validated/submitted). |
| `ledger.json` | Progress state: `{generation, phase, phaseIndex, phasesTotal, budget:{directorUsed, directorMax, globalUsed, globalMax}, actors:[{role, status, calls, budget, lastAction}], startedAt, updatedAt, wallDeadline}`. |
| `transcript.jsonl` | One JSON object per line, every inter-agent event: `{gen, seq, t, kind, from, to, body}`. Kinds: `post`, `consult`, `answer`, `deliver`, `note`, `phase`, `submit`, `fail`. Append-only, single writer. |

`kind`-specific schemas for board and transcript entries must be validated on load
(untrusted LLM text, bounded for context hygiene). Unknown kinds/roles are dropped;
over-long bodies are truncated. A malformed file falls back to an empty document,
never a crash.

**Working-context standard** — one exported assembler, used by every agent call in
later phases:

```
func AssembleContext(gen, theme string, board Board, working Summary, rolePrompt, task string) string
```

It renders: generation id + theme, board excerpt (last 20 entries, oldest first),
working-file summary (title, cast, beats count, acts, backdrop, status), role prompt,
task. The excerpt cap is a constant; board growth beyond it never grows prompts.

**Atomic persistence helper** — reuse the `teller.saveToDisk` temp-file-then-rename
pattern; export it for the company package so every company write is atomic.
A `writeMu` serialises writes; reads take the same mutex.

**Affected paths**: `internal/agents/theatre/` (new package), `Teller` cache dir
layout. No player, model or handler changes in this phase. The existing
`internal/agents/storyteller/` package (composer, staging, muse, `Teller`) is
untouched here — phase 9 migrates it into the theatre and deletes the package.

## Integration contract

This phase is `unit-test-only` — no HTTP, SSE, agent or file-watcher behavior yet.
Unit tests cover the file schemas, validation, caps, the assembler output, and atomic
writes.

## Acceptance criteria

- [ ] Every company file round-trips: write → load → validate → identical semantics.
- [ ] Board/transcript entries with unknown kind, unknown role, or over-long body are
      dropped or truncated on load, never rejected wholesale and never crash.
- [ ] `AssembleContext` output contains generation, theme, last-20 board entries,
      working summary, role prompt and task, in that order; a 60-entry board yields
      the same excerpt size as a 21-entry board.
- [ ] `working.json` round-trips a `model.Story` through `model.Story.Validate` with
      the revision counter and status intact.
- [ ] Concurrent writers (12 goroutines) never leave a torn file observable (same
      read-loop test pattern as `TestSaveToDisk_IsAtomic`).
- [ ] All existing storyteller tests still pass unchanged.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| `board.json` unreadable / corrupt JSON | empty board, no crash, error logged | corrupt-file unit test |
| Board entry has unknown `kind` | entry dropped | validation unit test |
| Board entry body > 240 chars | body truncated to cap | validation unit test |
| `working.json` holds an invalid story | working file rejected; composer fallback story offered (phase 4 uses this) | validation unit test |
| Temp write fails mid-way | temp file removed, target untouched | atomic-write unit test |
| `transcript.jsonl` grows past cap (e.g. 2000 lines) | oldest lines trimmed on next append | cap unit test |

## Implementation notes

*(filled by the executing agent)*

Executed by imago, 2026-08-02 session (phase 1 of the playwright-company worklog).

**Delivered** — new package `internal/agents/theatre/` (unit-test-only, per the
integration contract):

| File | Contents |
|---|---|
| `company.go` | `Company` (paths, single mutex, `Open`), `readJSON`, `logLoadFailure` |
| `constants.go` | Caps (60 board entries, 240-rune board body, 20-entry excerpt, 2000-line/2000-rune transcript bounds), role/kind/status vocabulary, `truncateRunes` |
| `atomic.go` | `writeFileAtomic` — storyteller temp-file-then-rename pattern, exported for every company write |
| `board.go` | `Board`/`Entry`, `Append`, `Excerpt`, normalize, `LoadBoard`/`SaveBoard` |
| `working.go` | `Working`/`Summary`, `actsOf`, normalize (story via `model.Story.Validate`), `LoadWorking`/`SaveWorking` |
| `ledger.go` | `Ledger`/`Budget`/`Actor`, normalize, `LoadLedger`/`SaveLedger` |
| `transcript.go` | `TranscriptEvent`/`Transcript`, `AppendTranscript` (append + trim in one atomic rewrite), `LoadTranscript` |
| `context.go` | `AssembleContext` — the working-context standard |

**Material decisions (recorded for chronology):**

- **D-P1-1 — one mutex for everything**: `Company` uses a single mutex for reads
  and writes, mirroring the storyteller's `writeMu`; disk atomicity (temp +
  rename) is the real guarantee, the mutex keeps in-process writers serialised.
- **D-P1-2 — board seq is renumbered after every append**: capping to the newest
  60 entries would otherwise leave gaps (seq 11..70 then a fresh 61), breaking
  excerpt contiguity. Load runs the identical renumber.
- **D-P1-3 — load/save error split**: loads log internally and return the error
  alongside the fallback document (AGENTS.md's diagnostic-pair pattern — the
  empty doc is the value, the error is diagnostics); saves return unlogged
  errors because there is no fallback and the caller must act.
- **D-P1-4 — transcript bodies get their own cap**: the spec named only the
  2000-line cap; `TranscriptMaxBody = 2000` runes bounds an event body for
  context hygiene, as the README requires.
- **D-P1-5 — trim is an atomic rewrite of the tail**: an append that would
  exceed 2000 lines rewrites the file with the newest 2000 in one atomic write;
  seq continues from the last readable line.
- **D-P1-6 — acts derived from set changes**: the only act boundary the story
  model knows is `setBackdrop`, so `Summary.Acts = 1 + setBackdrop beats`. The
  playwright's draft report (phase 5) carries the author's own structure and
  supersedes this.
- **D-P1-7 — unknown working status defaults to `draft`**: an unplayable story
  rejects the whole working file, but a foreign status label alone is not
  worth losing a good draft.
- **D-P1-8 — role set includes `stage`**: the stage-manager wrapper posts notes
  and phase transitions under the `stage` role; it is never consulted. The
  wardrobe consultant role is `wardrobe`.
- **D-P1-9 — append and load run the same gate**: unknown kind/author drops,
  invalid addressee clears (not drops — a note with a typo'd recipient is still
  a note), bodies truncate on rune boundaries (never splits UTF-8).

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` | pass |
| `go test ./internal/agents/storyteller/ ./internal/agents/... ./internal/model/` (before changes) | pass — storyteller baseline green |
| `go test ./internal/agents/theatre/ -v` | 30/30 pass |
| `go test ./... -race -count=1 -timeout=120s` | pass — one transient `cmd/serve` failure (`bind: permission denied` on :1020) under full parallel load; passes alone and with `-race`; environmental port contention, unrelated to this phase |
| `go test ./internal/agents/theatre/ -cover` | 92.6% |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre/` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/` | 0 clone groups |

**Acceptance check** — all criteria met: every company file round-trips;
unknown kind/role drops and over-long bodies truncate without wholesale
rejection or crashes (corrupt board/ledger/transcript tests); `AssembleContext`
renders generation → theme → excerpt → summary → role → task with a constant
excerpt size at 21 and 60 entries; `working.json` round-trips a validated
`model.Story` with revision/status intact; 12 concurrent writers leave no torn
file observable (raw-file read loop, same as `TestSaveToDisk_IsAtomic`); all
existing storyteller tests pass unchanged.

**Docs** — AGENTS.md package map gained the `theatre/` entry (phase 9 does the
fuller rewrite when the storyteller is migrated and removed).

## Review findings

*(filled by reviewers)*

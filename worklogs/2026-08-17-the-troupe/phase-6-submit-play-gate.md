# Phase 6 — submit_play gate

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

The single-writer persistence boundary. The director alone submits the play as
one resolved JSON file; `submit_play` validates every reference resolves,
atomically persists it, and appends a metadata entry to the index.

## Shape (as built)

```text
internal/agents/troupe/submit.go       # Submitter + submit_play tool + atomic persist + index append
internal/agents/troupe/submit_test.go
internal/agents/troupe/resolve.go      # the walk skips the resolved served artifacts the history accumulates
```

## Behaviour (as built)

- **Director-only.** `submit_play` is excluded from the general registry —
  `registryHas("submit_play")` is false, pinned by a test, and no role note
  can ever select it. `Submitter.newSubmitPlayTool()` builds the tool; phase
  7 grants it to the director's fixed tool set.
- **Validate, then resolve.** `Submitter.Submit` checks the play id shape
  (`story_YYYYMMDDTHHMMSSZ`), then runs the phase-3 resolver over a snapshot
  of the materialised worktree (`snapshotFromWorktree` — the filesystem seam
  D-24 reserved for this phase). Exact errors return wherever they manifest —
  a missing model, a filename/envelope mismatch, a conflict-marked file, a
  budget overrun — and nothing is written.
- **Atomically persist** `plays/story_<UTC>.json` — the resolved play (play
  plus flattened asset table), stamped `status: submitted` — under the
  authored play's own datetime id, replacing the raw authored form with the
  served artifact. `writeFileAtomic` writes a temp file in the same
  directory, fsyncs it and renames it over the target; a reader never
  observes a torn file, and a failed write removes the temp and leaves the
  target untouched.
- **Append a metadata entry** to `plays/index.json` — the `{"index": [...]}`
  envelope, newest-first, re-sorted on every append by the plain
  lexicographic sort of the datetime ids (decision 15). Entries are
  `{id, status, author, provenance, created}`, the author/provenance from the
  play envelope, `created` the submission instant.
- **A second submit is refused.** The refusal checks the play file: a
  resolved play at the id's path is the served artifact, and only
  `submit_play` writes that shape — so "status already submitted" is read
  from the durably on-disk artifact, not from bookkeeping. The refusal
  precedes any work; a resubmit rewrites nothing (pinned by byte-comparing
  the file and index across the refused call).
- **A persist failure aborts.** The play file is written first, the index
  entry appended second — a crash can never leave an index entry pointing at
  a missing play. Either failure returns the atomic-write error; the paper
  trail never claims a success the disk did not record.

## Implementation notes (as executed)

1. **`submit_play` is the only writer of `plays/`** — a mutex on the
   `Submitter` serialises whole submits, so the single-writer rule holds even
   under concurrency and no index update is ever lost (pinned by a
   concurrent-submit test under `-race`). The swarm writes everything except
   the play concurrently; merge applies to the repertoire, never to the play.
2. **Persist then index-append** (implementation note 2 of the plan): the
   play file is atomically on disk before the index entry is appended. The
   inverse crash state — a resolved play without an index entry — is the
   accepted safe direction; a retry is refused ("already submitted") and a
   reconciliation of the missing entry is left to a later phase.
3. **Old plays are kept, never overwritten.** Each submit is a fresh UTC
   datetime id; the same id is refused a second time. The history accumulates
   resolved served artifacts in `plays/` — the critic and `debug` review it
   by filename.
4. **The resolver walk skips the served artifacts.** A submitted play in
   `plays/` is `{play, assets}`, not an authored envelope; the phase-3 walk
   assumed every play file was authored. Phase 6 teaches the walk
   (`isResolvedPlay`, the top-level `play` key) to skip them the way it
   skips `index.json`: walked for conflict markers, never parsed. A
   conflict-marked resolved play still blocks resolution (D-27). Without
   this, the second generation's submit would fail parsing the first
   generation's artifact.
5. **The refusal is file-based, not index-based.** The resolved play file IS
   the durable submission record (decision 15: "status becomes submitted
   only after the resolved play is durably on disk"); the index is derived
   bookkeeping for the API (D-28). The file-based check refuses the
   crash-state retry honestly ("already submitted") instead of failing with
   a confusing "no such play" after the raw form was replaced.
6. **STAGE.md** gains the `submit_play` entry in the closed-tool-registry
   section — director-only, excluded from the general registry — so the
   note stays honest about the stage's tool surface.

## Tests

All in `submit_test.go`, driving a `Submitter` over a real temp worktree
seeded from the conformance fixtures (real-filesystem integration, per
AGENTS.md), plus one resolver walk test in `resolve_test.go`:

- **Resolve-then-persist**: a valid draft play writes exactly one play file
  (the raw authored form replaced by the resolved served artifact — file
  bytes equal the resolver's `ResolvedPlay` marshal, status stamped
  submitted) and appends exactly one index entry (id/status/author/
  provenance/created); no stray temp files remain.
- **Failing resolution writes nothing**: missing model, conflict-marked
  asset and budget overrun each return their exact error with the authored
  play still raw and no index file created.
- **Second submit refused**: `ErrAlreadySubmitted`, and neither the play
  file nor the index is rewritten (byte-compared across the refused call).
- **Persist failure aborts**: a read-only `plays/` dir fails the play write
  (no index, play stays raw); a directory at `plays/index.json` fails the
  index step after the play file was durably written — pinning the
  play-first/index-second ordering.
- **Index ordering**: two submits land newest-first; a pre-seeded
  out-of-order index is re-sorted by the append (the plain lexicographic
  sort of the datetime ids).
- **Single-writer gate**: concurrent submits of two plays both land — no
  index update is lost.
- **Cross-generation loop**: a second submit over a worktree that already
  holds a submitted (resolved) play resolves fine — the walk skips the
  served artifact (also pinned directly in `resolve_test.go`, including the
  conflict-flag on a conflict-marked resolved play).
- **Tool surface**: `submit_play` requires a non-empty string `play`,
  confirms the submission, carries the spec name/input; the tool is NOT in
  the general registry (director-only).
- **Id gate**: a malformed play id returns the exact shape error before any
  work.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] A successful submit produces exactly one play file + one index entry.

## Decision log (session 2026-08-23, clai worker)

- **D-6-1 — The submitted play file IS the durable submission record; the
  refusal reads it, not the index.** A resolved play at `plays/story_<UTC>.json`
  is the served artifact, and only `submit_play` writes that shape — so a
  second submit is refused by the file's resolved form, not by index
  bookkeeping (D-28 keeps the index derived). This makes the crash-state
  retry honest: the play is durably on disk, the retry is refused with
  "already submitted" rather than a confusing "no such play" after the raw
  form was replaced.
- **D-6-2 — The walk skips resolved served artifacts, and this is a phase-6
  change to phase 3.** The phase-3 walk assumed every `plays/*.json` file
  was an authored envelope; phase 6 introduces the resolved `{play, assets}`
  artifact into the same directory. The walk (the deterministic authority on
  what is an authored note) is taught `isResolvedPlay` — a top-level `play`
  key — and skips those files exactly like `index.json`: walked for conflict
  markers (D-27), never parsed. A malformed non-resolved play still falls
  through to `Parse` and its exact error.
- **D-6-3 — The submitter mutex is the single-writer gate.** Whole submits
  serialize under one mutex, so concurrent submits cannot interleave
  read-modify-write on `plays/index.json` and lose an update. The director
  is single anyway; the mutex makes the "single-writer play" rule hold under
  `-race` regardless of caller.
- **D-6-4 — fsync before rename.** `writeFileAtomic` (re-homed from the old
  theatre's helper, which phase 0 deleted) adds `tmp.Sync()` before the
  rename: "durably on disk" is the persistence boundary's contract, not just
  atomic visibility.
- **D-6-5 — The index is re-sorted on every append.** The append prepends
  and then re-sorts the whole list by the datetime ids, so "newest-first is
  a plain sort" is an enforced invariant, never a caller assumption — an
  out-of-order entry from any earlier state cannot persist.
- **D-6-6 — The play-first/index-second ordering leaves the inverse crash
  state (play without entry) for a later phase to reconcile.** A retry is
  refused honestly; rebuilding the missing index entry is not this phase's
  job (the phase plan's test list pins only "no index entry pointing at a
  missing play").

## Notes for later phases

- Phase 7's director calls `Submitter.Submit` at the end of a generation via
  the director's fixed tool set — `newSubmitPlayTool()` — never through a
  role note.
- Phase 9's `/api/v1/troupe/play/resolved` reads the newest `plays/index.json`
  entry and serves the corresponding resolved play file; the paginated
  `/api/v1/troupe/play` reads the index list this phase persists.
- The crash state "resolved play file without an index entry" is visible to
  phase 9's API as a play missing from the index; reconciliation (rebuilding
  the entry from the file) is a candidate for the API or a `debug` command.

## Session report (2026-08-23)

Picked up phase 6 from the handover state (README status: "Phase 6
(submit_play gate) is next"; phases 0–5 already in the tree).

**Built `submit.go` exactly as planned:**

- `Submitter` — the single-writer gate over a materialised worktree: id-shape
  check, file-based second-submit refusal, snapshot read, `ResolvePlay`, the
  submitted-status stamp, `writeFileAtomic` persist of the resolved play,
  then the `plays/index.json` append (newest-first re-sorted).
- `PlayIndexEntry`/`PlayIndex` — the `{id, status, author, provenance,
  created}` metadata contract the play API will read.
- `submitPlayTool` — the director-only clai tool (`newSubmitPlayTool`),
  excluded from the general registry per decision 20.
- `snapshotFromWorktree` — the filesystem seam D-24 reserved for this phase.
- `writeFileAtomic` — temp + fsync + rename, cleaned up on failure (D-6-4).
- `resolve.go` — the walk learns `isResolvedPlay` and skips the served
  artifacts the history accumulates (D-6-2).
- Package doc and `STAGE.md` updated to ship the phase's surface (the stage's
  human-readable mirror now carries the director-only `submit_play`).

**Verification (exact commands and results):**

```bash
# Baseline (pre-change):
go build ./...                                       # green
go test ./internal/agents/troupe -race -count=1      # ok

# After the change:
go build ./...                                       # green
go vet ./...                                         # clean
go run mvdan.cc/gofumpt@latest -l .                  # no output (clean)
go run honnef.co/go/tools/cmd/staticcheck@latest ./internal/agents/troupe/...  # clean
go run github.com/mibk/dupl@latest -t 80 internal/agents/troupe   # 0 clone groups
go test ./internal/agents/troupe -race -count=3      # ok (acceptance)
go test ./internal/agents/troupe -race -cover -count=1  # ok, 87.6% coverage
go test ./... -race -cover -count=3 -timeout=30s     # ok, all packages
```

One full-suite run under the QA command failed `cmd/classify` on the first
pass — the same load-sensitive pre-existing flake phase 3 recorded (passes in
isolation and on the immediate rerun); the command above is the final,
passing run.

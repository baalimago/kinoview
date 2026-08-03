# Phase 9 — Storyteller Removal

**Status:** ✅ Complete | [README](./README.md)

## Goal

Replace `internal/agents/storyteller/` with the theatre: migrate the deterministic
floor (composer, staging, muse) into the theatre package, move the `Teller` contract
to its house home, delete the old package, re-point every reference, and prove the
composer-only path is behaviorally identical before and after.

## Implementation notes

### Migration moves

```bash
# composer.go → floor.go (the playwright's fallback; kept names for the rest)
mv storyteller/composer.go      theatre/floor.go
mv storyteller/staging.go       theatre/staging.go
mv storyteller/muse.go          theatre/muse.go
# tests move with their code
mv storyteller/staging_test.go  theatre/staging_test.go
mv storyteller/muse_test.go     theatre/muse_test.go
mv storyteller/dressdraft_test.go theatre/dressdraft_test.go
# composer tests lived in storyteller_test.go; extracted to theatre/floor_test.go
# storyteller.go + storyteller_test.go (the old LLM teller) were deleted: the
# theatre facade already reimplemented Next/Prepare/Warm/cooldown identically
# (phases 4 + the composer-only tests in theatre_test.go), so no behaviour was
# lost — every teller-level test has a TestTheatre_* counterpart.
rm -rf storyteller/
```

### Duplicate elimination

- `newID`/`idAlphabet` had two identical definitions (composer + theatre.go);
  the theatre.go one is now the single definition (D-P9-1).
- `Muse`/`MuseFunc` had two identical definitions (muse.go + theatre.go); the
  theatre.go ones stay, muse.go keeps only `LatestTheme`/`cleanTitle`.
- `pick` had two identical definitions (composer + fallback.go); the floor's
  stays, fallback.go's mirror was deleted (its "until phase 9" comment went
  with it).

### Contract move

`Teller` moved to `internal/agents/interfaces.go` as `agents.Teller`
(Next/Prepare/Warm). `*Theatre` carries the compile-time proof
`var _ agents.Teller = (*Theatre)(nil)`; `internal/media/index.go` now wires
`WithTheatre(t agents.Teller)` and its field is `theatre agents.Teller`.

### Naming cleanup (zero `storyteller` hits)

`rg storyteller` over `cmd/` and `internal/` (worklogs and agent_notebook
excluded as historical) returns **zero hits**. Dispositions:

| Hit | Disposition |
|---|---|
| `cmd/serve/serve.go` flag fields + registration | renamed to `-theatre`, `-theatreCooldown`, `-theatreMaxCalls`, `-theatreWallClock`, `-theatreGlobalCalls` (D-P9-2) |
| `cmd/serve/serve_setup.go` | `storyteller.LatestTheme` → `theatre.LatestTheme`; notice message → `theatre running composer-only (no -theatre model set)`; import dropped |
| `cmd/serve/serve_setup_test.go` | flag names + messages updated; tests renamed `TestCommand_TheatreBudgetFlagsDefault` / `TestSetup_TheatreBudgetFlagsParseAndApply` |
| `cmd/llm/usage.go` + test | `classifyAgent` "slapstick" marker now returns `"theatre"` (the director prompt still says "slapstick", so session attribution keeps working) |
| `cmd/serve/frontend/intro.js`, `internal/model/story.go` | comments updated to the theatre company |
| `internal/media/index*.go` | `WithStoryteller` → `WithTheatre`, field/comment updates |
| theatre package comments | rewrote every "the storyteller" reference; helper `storytellerCompose` → `seededCompose` |
| `AGENTS.md` | package map: `storyteller/` entry deleted, `floor.go`/`staging.go`/`muse.go` entries added, `Teller` added to interfaces.go line, opt-in list, director-budget flags, teller-contract wording |

### Behavioural equivalence proof

Golden file `internal/agents/theatre/testdata/composer_snapshot.json` captured
**before** the move, from the storyteller package's `ComposeThemed`, for seeds
`{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233}` with theme
"The Great Mouse Hunt". `TestCompose_SnapshotMatchesFrozenPreMigrationOutput`
recomposes through the migrated floor and compares **JSON bytes** (what the
player consumes), not in-memory structs: an empty `Props` slice marshals away
and round-trips as nil, so struct equality would flag a JSON artifact as a
drift (D-P9-3). Byte-identical before/after; any future floor edit fails here.

### Pre-migration cache compat

`intro_story.json` path, cooldown and mtime semantics are unchanged in
`theatre.go` (`loadFromDisk`); `TestTheatre_CooldownSurvivesRestartForLLMStories`
and `TestTheatre_CooldownExpiredCacheAllowsRegeneration` prove a cache written
by the old run loads identically.

### Test-harness race fix (found by QA, not by design)

`-race` flagged `TestFeed_ConcurrentSessionsNoInterleaving`: the test swapped
the ancli globals (`Newline`/`UseColor`) while the feed goroutine was already
emitting — a genuine unsynchronized read/write in the phase-2 harness
(occasionally also losing one line: "199 lines, want 200"). Fixed by pinning
`withAncliPlain(t)` before any feed activity and moving the emitting goroutines
inside the stdout capture, so the swap happens-before any emit and every line
lands in the captured output (D-P9-4).

### Decisions (D-P9)

- **D-P9-1 — single id generator**: `newID`/`idAlphabet` live only in
  `theatre.go`; the migrated floor shares them. The `stry_` + 8-char format is
  unchanged, so pre-migration cache ids and the debug renderer keep working.
- **D-P9-2 — flags renamed to `-theatre*`**: the zero-hit acceptance criterion
  demands the identifier disappear from `cmd/`; the flag rename is the honest
  completion of D13. The old `-storyteller*` names are gone — documented in
  this worklog, since docs/ and README.md never mentioned them.
- **D-P9-3 — snapshot compares JSON bytes**: the frozen-proof test asserts the
  serialized story (the player's input) is byte-identical, tolerating the
  empty-slice/nil round-trip.
- **D-P9-4 — feed test pins ancli globals before spawn**: race-free harness;
  deterministic 200-line capture.

### Validation table

| Check | Command | Result |
|---|---|---|
| Zero-hit grep | `rg storyteller` over `cmd/`, `internal/` | ✅ zero hits |
| Build | `go build ./...` | ✅ |
| Tests (all) | `go test ./...` | ✅ |
| Tests (touched, race ×3) | `go test -race -count=3 ./internal/agents/theatre/... ./internal/media/... ./cmd/serve/... ./cmd/llm/... ./internal/model/... ./cmd/debug/... ./internal/agents/` | ✅ 13/13 ok |
| Feed race regression | `go test -race -count=5 ./internal/agents/theatre/ -run TestFeed` | ✅ |
| Vet | `go vet ./...` | ✅ |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | ✅ |
| Format | `go run mvdan.cc/gofumpt@latest -l .` | ✅ no diffs |
| Fix | `go fix ./...` | ✅ |
| Dupl | `go run github.com/mibk/dupl@latest -t 80 .` | ✅ 27 pre-existing clone groups, none in touched files |
| ancli-only grep | `rg "fmt.Print\|os.Stdout" internal/agents/theatre/` | ✅ no matches |
| Node syntax | `node --check cmd/serve/frontend/intro.js` | ✅ |
| Node harness | `node cmd/serve/frontend_test/intro.test.js` | ✅ all assertions |
| Snapshot | `go test ./internal/agents/theatre/ -run TestCompose_SnapshotMatchesFrozenPreMigrationOutput` | ✅ byte-identical |

**Full-suite race note**: `go test ./... -race -cover -count=3 -timeout=30s`
was run twice; both runs hit pre-existing load-sensitive failures unrelated to
phase 9 — `TestCommand_startClassificationStation_context_timeout`
(`cmd/classify`, untouched), `TestEventStreamAndSuggestions` butler-cascade
timing (`internal/media`, untouched), and package timeouts — while the machine
ran at load average ~108 on 8 cores (a concurrent flutter web build + another
clai worker). Each flaky test passes `-race -count=5` in isolation; every
package phase 9 touches passes `-race -count=3` in isolation. Phase 10 (the
quality gate) should re-run the full suite on an idle machine.

## Review findings

### Review 1 — 2026-08-02 (holistic review; worker: imago)

**R1-01 — data race on `Theatre.rnd` between `Next` and `Prepare`/`Warm` (Medium — fix tracked in phase 11).**

- Reference: `internal/agents/theatre/theatre.go` — `Next` composes under `t.mu`, `Prepare`→`generate` (composer-only path and the LLM-failure fallback) and `Warm`'s synchronous seed compose run with no lock.
- Provenance: this phase's migration preserved the pre-migration storyteller's lock pattern verbatim (verified against `storyteller.go` at c61c86e), so the race is **carried over, not introduced** by the removal. The phase's behavioral-equivalence contract (snapshot byte-identical, cooldown/loadFromDisk semantics unchanged) is met — the finding is about the semantics the migration was asked to preserve. Full repro and fix in the phase-4 finding; the addendum phase 11 owns the fix.

Verified good for this phase: `rg storyteller` over `cmd/` + `internal/` returns zero hits; the `Teller` contract moved to `internal/agents/interfaces.go` with the compile-time assertion on `*Theatre`; `TestCompose_SnapshotMatchesFrozenPreMigrationOutput` is byte-identical (JSON bytes, not structs — the empty-slice/nil round-trip is tolerated by design); the pre-migration cache-compat tests pass; the phase-9 feed-harness race fix (ancli globals pinned before any feed activity) holds under `-race`.

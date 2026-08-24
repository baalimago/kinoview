# Phase 3 — Resolver + validator

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

The deterministic walker that turns the authored notebook into the single
served artifact. It walks the materialised worktree, validates every file
against the frozen grammar, resolves inline `id@version` references
transitively (including depth-bounded self-reference), flags conflict-marked
files, emits the resolved play, and enforces the arithmetic expansion budget.

## Shape (as built)

```text
internal/agents/troupe/resolve.go      # Snapshot + Budget + walk + closure + budget check
internal/agents/troupe/resolve_test.go # fixture-worktree tests
```

`resolve.go` is pure and deterministic: it performs no I/O of its own. The
caller hands it a `Snapshot` — a `map[string][]byte` of relative paths
(`models/cat@1.json`) to file bytes — so the same snapshot always yields the
same resolved play or the same exact errors. The filesystem reader is phase
6's `submit_play` wiring, not the resolver's job.

## Behaviour (as built)

- **Walk the worktree** under `roles/`, `models/`, `clips/`, `voices/`,
  `sounds/`, `gags/`, `plays/`, `feedback/` in deterministic (sorted) order.
  Every grammar file is validated against the frozen grammar through Phase 1's
  `Parse` — the filename↔identity rule, the envelope, the closed spec fields.
  A file outside the eight directories is not walked at all.
- **Flag conflict-marked files.** A shared file carrying slivingdoc (git-style)
  conflict markers (`<<<<<<<`/`=======`/`>>>>>>>`) is flagged before parsing —
  asset, note (`roles/`, `feedback/`) or index alike. An unresolved conflict
  means no play serves.
- **Resolve the cross-file closure**, typed by field: `model` → `models/`,
  `voice` → `voices/`, `clip` → `clips/`, `gag` → `gags/`, `sound` →
  `sounds/`. The closure walks the play's instances and timeline, each
  model's `spec.voice`/`spec.sound` and structural verbs (including the
  `recurse` `tip`), each gag's clips and each clip's event sounds —
  transitively, with a visited set per kind, so a self-referential recurse
  (`branch@1` recursing into `branch@1`) closes in one pass. It does **not**
  touch anything inside a spec: bone ids, slot ids and selectors resolve in
  the engine at render time.
- **Emit the resolved play** — `{ play, assets: { models, voices, sounds,
  clips, gags } }`, each map keyed by `id@version` within its kind (`cat@1`
  the model and `cat@1` the voice are distinct). Every spec is preserved
  byte-for-byte from the authored file; an untouched kind marshals as `{}`,
  never `null`. The play is resolved by its bare `story_<UTC>` id; `plays/
  index.json` is walked for conflict markers but never parsed as an asset.
- **Enforce the arithmetic expansion budget** (D-25): per-verb caps on the
  declared scatter `count` and recurse `depth`/`branch`, plus a total cap on
  the sum of every structural verb's declared size across the play's closure
  (`attach` counts 1, `scatter` its `count`, `recurse` its closed-form
  geometric sum `(branch^(depth+1)−1)/(branch−1)`). This is a cheap check on
  declared numbers; the engine expands (and may optimise the expansion away).

## Implementation notes (as executed)

1. **One entry point.** `ResolvePlay(snap, playID, opts...)` validates the
   whole worktree, closes the named play and enforces the budget. The walk
   validates every asset file in the snapshot — not just the play's closure —
   because a reconciliation that fails anywhere means no play serves
   (D-26). The budget, by contrast, applies to the play's closure only: an
   unreferenced model declaring an enormous scatter never expands, so it must
   not block the play (pinned by a test).
2. **Errors are exact and reference-shaped** so `submit_play` (phase 6) can
   pass them back to the director: `troupe: plays/story_….json:
   instances[0].model: no such model ghost@1 in models/`, `troupe: models/
   forest@1.json: spec.structure[0]: scatter count 600 exceeds the 500 cap`.
   The walk order and the closure order are deterministic, so the same
   snapshot yields the same error.
3. **Determinism of the artifact.** `ResolvedPlay` marshals through
   `encoding/json`, which sorts map keys; the raw specs are the authored
   bytes. Resolving the same snapshot twice yields byte-identical output
   (pinned by a test).
4. **Budget defaults** (D-25): `MaxCount` 500, `MaxDepth` 6, `MaxBranch` 8,
   `MaxTotal` 10 000, overridable per run with `WithBudget`. A zero value
   disables the cap it names. The conformance example declares 36 nodes —
   comfortably inside the guardrail.
5. **STAGE.md** gains a short "expansion budget" subsection under
   Determinism, mirroring the defaults so the note stays honest about what
   the resolver refuses.

## Tests

All in `resolve_test.go`, driving the resolver with the conformance fixture
worktree under `testdata/` plus targeted mutations:

- **Conformance**: the fixture play resolves to the expected flattened asset
  table (6 models / 2 voices / 1 sound / 3 clips / 1 gag), every resolved
  spec is byte-identical to its authored file, the play envelope is the
  authored play, and `cat@1` resolves distinctly as a model and a voice.
- **Engine fixture cross-check**: the resolved play is semantically identical
  to the hand-resolved `lab/fixtures/story_…resolved.json` the engine tests
  and the lab consume (the fixture is hand-formatted, so the comparison
  unmarshals both documents and deep-compares the JSON trees).
- **Closure**: the leaf→branch→tree→forest→play chain closes transitively;
  the self-referential `branch@1` recurse closes bounded by the visited set.
- **Determinism**: resolving the same snapshot twice yields byte-identical
  output.
- **Negative cases**: missing play id; missing model; cross-kind reference
  (a `model` field naming `walk@1`, which exists only in `clips/`); missing
  voice via a model's `spec.voice`; missing clip via a gag; missing sound via
  a clip event; filename/envelope mismatch; conflict-marked asset, note and
  play index; budget overruns (scatter count, recurse depth, recurse branch,
  total).
- **Scope pins**: `roles/`, `feedback/` and `plays/index.json` notes are
  walked for conflict markers but never parsed as assets; files outside the
  notebook directories are ignored; the budget applies to the play's closure
  only; an untouched asset kind marshals as `{}`, never `null`.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] The resolved play from the conformance worktree matches the README's
      `{ play, assets }` shape — and the engine's hand-resolved fixture.
- [x] Full QA gate: `go test ./... -race -cover -count=3 -timeout=30s`
      passes (troupe at 89.0% coverage), `go vet ./...`, `staticcheck`,
      `gofumpt` and `dupl -t 80` are clean.

## Decision log (session 2026-08-23, clai worker)

- **D-24 — The snapshot is the resolver's only input.** `ResolvePlay` takes a
  `Snapshot` (`map[string][]byte` keyed by relative path) and performs no I/O.
  The filesystem reader is phase 6's `submit_play` wiring. Purity buys
  determinism and a trivially testable walker.
- **D-25 — Budget arithmetic.** Per-verb caps (scatter `count` ≤ 500, recurse
  `depth` ≤ 6, `branch` ≤ 8) plus a total cap (10 000) on the sum of every
  structural verb's declared size across the play's closure: `attach` counts
  1, `scatter` its `count`, `recurse` its closed-form geometric sum
  `(branch^(depth+1)−1)/(branch−1)`. A zero `Budget` value disables the caps
  it names; `WithBudget` overrides per run. The defaults are generous for a
  small gallery stage and cheap for the webOS TV floor.
- **D-26 — Full-worktree validation, closure-scoped budget.** The walk
  validates every asset file in the snapshot: an invalid file anywhere blocks
  resolution ("a reconciliation that fails means no play serves"). The
  expansion budget, however, counts only the play's closure — an unreferenced
  model's declared arithmetic never expands and must not block the play.
- **D-27 — Conflict markers.** slivingdoc (git-style) markers
  `<<<<<<<`/`=======`/`>>>>>>>` flag a file before parsing. The flag applies
  to every walked file — assets, `roles/`, `feedback/` notes and `plays/
  index.json` alike.
- **D-28 — plays/index.json is metadata.** It is walked for conflict markers
  but never parsed as an asset; the resolver reads play files by their bare
  datetime id. The play API pagination (phase 9) reads the index, never the
  resolver.
- **D-29 — The closure is a visited set per kind.** Each `id@version` is added
  once, so a self-referential `recurse` closes in one pass and the closure
  always terminates; the depth bound the grammar declares is the engine's
  expansion bound, not a resolver iteration limit.

## Session report (2026-08-23)

Picked up phase 3 from the handover state (README status: "Phase 3
(resolver + validator) is next"; phase 1's `Parse` and the conformance
fixtures already in place).

**Built `resolve.go`** — the pure resolver:

- `Snapshot`, `ExpansionBudget`/`DefaultExpansionBudget` and the
  `WithExpansionBudget` functional option (house style).
- `walk`: deterministic sorted walk of the eight notebook directories;
  conflict-marker flagging; `Parse` validation of the six grammar directories;
  `plays/index.json`, `roles/` and `feedback/` walked but never parsed.
- `closure`: the typed, transitive `id@version` closure (play → instances/
  timeline → models → voice/sound/structure verbs → clips → event sounds →
  gags → clips), emitting `ResolvedPlay` with byte-preserved specs and `{}`
  (never `null`) for untouched kinds.
- `checkBudget`: per-verb caps and the closed-form total over the closure.

**Verification (exact commands and results):**

```bash
# Baseline (pre-change):
go test ./internal/agents/troupe -race -count=3 -timeout=30s   # ok (phase 2 state)
go build ./...                                                # green

# After the change:
go test ./internal/agents/troupe -race -count=3 -timeout=30s   # ok, exit 0
go test ./internal/agents/troupe -cover                        # 89.0% of statements
go test ./... -race -cover -count=3 -timeout=30s               # ok, all packages
go vet ./...                                                   # clean
go run mvdan.cc/gofumpt@latest -l .                            # no diffs
go run honnef.co/go/tools/cmd/staticcheck@latest ./...         # clean
go run github.com/mibk/dupl@latest -t 80 .                     # no troupe clones
go build ./...                                                 # green
```

One full-suite run under the QA command timed out in
`internal/media/storage` (`Test_AddToClassificationQueue_dedup_cleanupOnError`,
a load-sensitive pre-existing flake that passes in isolation and on every
subsequent full run); the command above is the final, passing run.

## Notes for later phases

- Phase 6's `submit_play` calls `ResolvePlay` on the worktree snapshot it
  materialised and persists the returned `ResolvedPlay` — the file reader that
  turns a worktree directory into a `Snapshot` belongs there.
- Phase 9's `/api/v1/troupe/play/resolved` resolves the newest play by its
  datetime id (read from `plays/index.json`) and serves the artifact the
  engine consumes — the format the engine tests already pin via
  `lab/fixtures/`.
- The budget is configurable per call (`WithExpansionBudget`); the
  serve-side wiring decides whether to expose it as a flag (phase 9) or keep
  the defaults.

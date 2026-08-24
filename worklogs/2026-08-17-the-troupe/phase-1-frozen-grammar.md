# Phase 1 — Frozen grammar

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

Author the meta-schema twice, in lock-step, with the validator as the authority
that keeps them from drifting: as Go type definitions plus validators (the
machine authority), and as a `STAGE.md` note in the notebook (the human-readable
spec the director reads before authoring against it).

This is the only human-owned surface. Everything the troupe authors is an
instance of one of these types, and nothing else exists.

## Shape (as built)

```text
internal/agents/troupe/grammar.go      # the atom types + the uniform envelope
internal/agents/troupe/validate.go     # Parse + per-kind validators + filename/identity rules
internal/agents/troupe/grammar_test.go # fixture-driven validation tests
internal/agents/troupe/testdata/       # the conformance example (fixtures only,
                                       #   never shipped into the notebook)
internal/agents/troupe/STAGE.md        # the human-readable grammar, authored as a note
```

Types declared (frozen; later phases extend the validator, never relax it):

- **Structure**: `bone`, `attachment`, `shape` (rect/ellipse/path), `skin`,
  `model` (bones + slots/attachments + optional `voice`/`sound` refs in `spec`
  + structural verbs).
- **Motion**: `constraint` (`reach`/`look`/`plant`/`track` — reach/look/plant
  target a `{x,y}` coordinate, `track` targets a bone id), `keyframe` (bone or
  slot channel, shared easing enum), `oscillation` (bones only), `clip`
  (constraints + keyframes + oscillations + duration + loop + events), `voice`
  (formant params, range-validated, mapping 1:1 to the synth), `sound`
  (`noise`/`tone`/`sweep`/`burst`).
- **Procedural structure**: `attach`, `scatter`, `recurse` + the closed `over`
  region vocabulary (`band`/`disc`/`grid`/`curve`/`along`).
- **Selectors**: wildcard path (`tree#*/branch#*/tip`) and `model:<id>@<version>`.
- **Composition**: `gag` (clips only), `tween` (root-bone stage motion), `play`
  (instances + timeline; each entry carries exactly one of `clip`/`gag`/`tween`).
- **Envelope**: `kind`/`id`/`version`/`status`/`author`/`provenance` with
  kind-specific content nested under `spec`. Plays are the exception (no
  `version`).
- **Resolved-play schema**: `{ play, assets: { models, voices, sounds, clips,
  gags } }`, each map keyed by `id@version` within its kind.

## Implementation notes (as executed)

1. The conformance example from the README (`leaf` → `branch` → `tree` →
   `forest`, plus `cat`/`dog`/`walk`/`pounce`/`blink`/`doubletake`/`rustle`
   and the `story_20260820T161500Z` play) is encoded as test fixtures under
   `testdata/`. Fixtures only — never a seed. `dog@1` (model + voice) is
   authored to satisfy the play's references; it was only referenced, never
   shown, in the README.
2. The validator is the authority. `Parse(filename, data)` enforces:
   - **Filename ↔ identity**: the filename is the only authority for
     `id@version`; the envelope's `id`/`version` must match exactly or the
     file is rejected. The directory names the kind; a file outside
     `models/`/`clips/`/`voices/`/`sounds/`/`gags/`/`plays/` is not a grammar
     file (`roles/` arrives with phase 4).
   - **Composition boundary**: a `clip` references bones only (never a clip);
     a `gag` references clips only (never a gag); a `play` references
     models/clips/gags. Enforced by the closed spec fields plus strict decode:
     the grammar is closed, an unknown field is an error.
   - **Closed enums and ranges**: `status` ∈ `draft`/`submitted`; easing enum;
     `hint` ∈ `front`/`back`/`left`/`right`; `tween` target exactly one of
     absolute `x`/`y`/`rot`/`scale`, `beside` (`side` ∈ `left`/`right`/`front`/
     `back`), or `off` (`left`/`right`); a clip event is exactly one of
     `voice: true` or `sound: "id@version"`; voice ranges (`amp`/`pure`/`noise`/
     `decay` 0–1, `f0`/`q`/`dur`/`gap` positive, `bursts` a positive integer
     pair, fixed-length arrays matching the synth).
   - **Ids**: versioned asset ids and role ids are filesystem-safe
     `^[a-z0-9_-]{1,64}$` (lowercase); intra-spec ids (bones, attachments,
     slots, skins, play instances) allow camelCase; play ids are
     `story_YYYYMMDDTHHMMSSZ` (see decisions D-2, D-3).
   - **Intra-model references**: the bone tree (exactly one root, parents
     declared, no cycles), attachment→bone, skin→attachment with slot
     agreement, structural-verb `at`/`along` bones, play timeline `on`/`beside`
     instance ids.
3. `STAGE.md` is the readable mirror of the same grammar, authored in this
   phase. The validator does not read `STAGE.md` — it is for the director, not
   the machine. It lives in the troupe package (human-owned); seeding it into
   the notebook worktree is phase 9's serve-side wiring (decision D-9).

## Tests

- Fixture-driven: the full conformance example validates clean, and each
  fixture's filename↔identity is pinned (`TestParse_ConformanceExample`).
- Positive control: one compact valid spec per kind parses clean
  (`TestParse_ValidBases`), so a failing negative case is the mutation, not a
  broken base.
- Negative tables per rule: clip→clip, gag→gag, bad `id@version`, filename/
  envelope mismatch, out-of-range voice params, bad `hint`, bad `tween`
  target, bad `status`, bad id charset, a `keyframe` targeting a slot with a
  bone-only channel, unknown fields, bone-tree cycles, structural-verb field
  sets, selectors.
- Resolved-play schema round-trips from the fixture play
  (`TestResolvedPlay_RoundTrip`).
- `TestSTAGEMirrorsGrammar` keeps the note honest: it must exist and carry the
  closed vocabulary the validator enforces.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] The conformance example validates with zero errors.
- [x] `STAGE.md` exists and is a faithful, readable rendering of the Go grammar.

## Decision log (session 2026-08-23, clai worker)

The README's conformance example and the meta-schema text leave a few spots
open; these decisions pin them so the grammar is unambiguous from here on.
They are implemented in the validator and mirrored in `STAGE.md`.

- **D-1 — Two id charsets.** The README's `^[a-z0-9_-]{1,64}$` charset is
  justified by filenames ("share one charset and become filenames"), but the
  conformance example uses camelCase bone ids (`frontLeg`, `backLeg`), which
  never become filenames. Versioned asset ids and role ids keep the strict
  lowercase charset; intra-spec ids (bones, attachments, slots, skins, play
  instances) allow camelCase (`^[A-Za-z0-9_-]{1,64}$`).
- **D-2 — Play ids are the datetime exception.** `story_20260820T161500Z`
  contains uppercase T/Z, which the lowercase asset charset forbids. Play ids
  are exactly `story_YYYYMMDDTHHMMSSZ` (`^story_[0-9]{8}T[0-9]{6}Z$`),
  lexicographically sortable for the newest-first index. The envelope's
  `version` must be absent.
- **D-3 — Bone-only keyframe channels.** `x`/`y` are bone-only; a slot
  keyframes `rotation`/`scaleX`/`scaleY`/`opacity`. Rationale: rigid skinning —
  a shape rides its bone and never slides on it. The README's channel list
  names all six for "bone or slot" but the phase's negative test list demands a
  bone-only channel; position is the defensible subset.
- **D-4 — Path points are `[x, y]` pairs** (`PathPoint [2]float64`), for both
  `shape.path` vertices and the `curve` region. A path needs ≥ 3 vertices, a
  curve ≥ 2.
- **D-5 — Oscillation targets bones only** (no slot form); all six channels
  are allowed, so a body bob is an `x`/`y` oscillation on a bone.
- **D-6 — Selector grammar.** A selector is `model:<id>@<version>` or a path
  of `id#*` / `id#<index>` segments joined by `/`, with a plain id allowed in
  the final position naming a bone inside the instance. The README's
  `tree#*/branch#*/tip` final `tip` is that plain segment; the engine resolves
  selectors at render time (phase 2).
- **D-7 — Structural verbs are one validated struct** with per-verb field-set
  enforcement (attach does not accept `depth`), not separate Go types. The
  frozen grammar stays one closed shape; the validator enforces the sets.
- **D-8 — Voice array lengths pinned to the formant synth** (rehomed from the
  old `intro.js` `VOICES`): `dur`/`f0`/`amp`/`bursts`/`gap` are `[lo, hi]`
  pairs, `kf` and `mouth` are 4-point paths, `tracks` is 3×4, and
  `gains`/`q`/`pitch`/`vib`/`burstPitch` are 3-value curves. `gains` ∈ [0, 1]
  (WebAudio gain). Required 0–1 scalars (`noise`/`pure`/`decay`) are presence-
  checked: a missing field is an error, never a silent zero.
- **D-9 — STAGE.md lives in the troupe package** (human-owned, next to the
  grammar it mirrors). The notebook copy is seeded from it by serve-side
  wiring in phase 9, when the notebook itself is on.
- **D-10 — Strict decode.** `DisallowUnknownFields` on the envelope and every
  spec: the grammar is closed, an unknown field is drift and an error. The
  envelope `spec` stays `json.RawMessage` so the served resolved play preserves
  the authored bytes.
- **D-11 — Skins.** A model with attachments needs a `default` skin that maps
  every attachment (slot key == the attachment's own `slot`); other skins add
  alternatives. A model without attachments has nothing to skin.
- **D-12 — Bone tree.** Bones form a tree: exactly one root (`parent: null`),
  every parent declared, no cycles. A model without bones (a pure structural
  model like `forest`) skips the tree check.

## Session report (2026-08-23)

Built `internal/agents/troupe` (one package, decision 19 of the README):

```text
internal/agents/troupe/grammar.go       # types + envelope + Spec seal + ResolvedPlay
internal/agents/troupe/validate.go      # Parse + filenameIdentity + per-kind validators
internal/agents/troupe/grammar_test.go  # positive control + conformance walk + negative tables
internal/agents/troupe/STAGE.md         # the human-readable grammar note
internal/agents/troupe/testdata/        # 14 conformance fixtures (models/, clips/, voices/,
                                        #   sounds/, gags/, plays/)
```

Material deviations from the planned shape: `grammar.go` also carries the
`ResolvedPlay`/`ResolvedAssets` schema (the phase plan listed it under the
types to declare); `validate.go` carries `Parse` as the single entry point,
returning the envelope (spec byte-preserved) plus the typed spec — the shape
phase 3's walker consumes.

Verification (exact commands and results):

```bash
# Baseline (pre-change):
go build ./...                          # green (phase 0 state)

go test ./internal/agents/troupe -race -count=3 -timeout=30s   # ok, exit 0
go test ./internal/agents/troupe -cover                        # 87.7% of statements
go run mvdan.cc/gofumpt@latest -l internal/agents/troupe/      # no diffs
go vet ./internal/agents/troupe/                               # clean
go run honnef.co/go/tools/cmd/staticcheck@latest ./internal/agents/troupe/  # clean
```

`go build ./...` stays green (nothing imports the new package yet; wiring is
phase 9). The phase-1 acceptance run (`-race -count=3`) is the command above;
the full `make qa` gate runs once more in phase 9, when the package is wired.

## Notes for later phases

- Phase 3 consumes `Parse` when walking the worktree: it returns the envelope
  (byte-preserved spec) plus the validated typed spec, so the walker validates
  and resolves without re-parsing.
- Phase 2's engine consumes the resolved-play schema; it never sees the
  authoring surface (`constraint` goals are solved into FK inside the engine).
- The validator is extended (not relaxed) by later phases — the grammar is frozen.

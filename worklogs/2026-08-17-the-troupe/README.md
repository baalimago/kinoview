# 2026-08-17: The Troupe — Worklog

**Status:** ✅ Complete — all 10 phases (0–9) done | [Phase list](#phase-status)

## Summary

Replace the fixed-vocabulary, fixed-role theatre with a **director, a swarm,
and a self-evolving repertoire of models and animations running on a fixed
engine**. The director is the mastermind, not a worker: it reads viewer
feedback, spawns a dynamic swarm of concurrent sub-agents, each authoring a
piece of the content, then assembles and submits a play. Everything the
troupe authors — roles, models, animations, voices, sounds, plays — lives as
notes in the slivingdoc notebook. One fixed reviewer, the **critic**, comments
on the director's work against viewer ground truth; it has opinions, never
authority. What stays human-owned is the **grammar** (the atom types the
engine accepts) and the engine that interprets it; the composition, the
animation and the structure evolve.

## Phase Status

| Phase                                                                                 | Status  | Summary                                                                                                                                                                                                          |
| ------------------------------------------------------------------------------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [0 — Deprecate old theatre](phase-0-deprecate-old-theatre.md)                         | ✅ Done | Remove the fixed-vocabulary theatre end to end: `internal/agents/theatre`, composer, `model.Story`, `intro.js`, old contracts, `/intro/*`. Build green, empty `<div id="troupe">`.                               |
| [1 — Frozen grammar](phase-1-frozen-grammar.md)                                       | ✅ Done | Go types + validators for every atom, the uniform envelope, filename↔`id@version`, resolved-play schema; conformance fixtures + `STAGE.md`.                                                                      |
| [2 — Engine](phase-2-engine.md)                                                       | ✅ Done | Standalone `engine.js` (ES5, no canvas) self-mounting into `#troupe`: bone rig, rigid skinning, closed-form IK, keyframes/oscillation, voices/sounds, tweens, sequencing, expander, selectors; `lab/` simulator. |
| [3 — Resolver + validator](phase-3-resolver-validator.md)                             | ✅ Done | Walk the worktree, validate, resolve `id@version` (bounded self-ref), flag conflict-marked files, emit resolved play, enforce expansion budget.                                                                  |
| [4 — Role notes + registry + spawn](phase-4-role-notes-tool-registry-spawn-runner.md) | ✅ Done | Role-note reader/validator, closed name→tool registry, recursive `spawn_role`.                                                                                                                                   |
| [5 — Termination authority](phase-5-termination-authority.md)                         | ✅ Done | Token stoploss (`-troupeTokenStoploss`, atomic reservation) + hardcoded global call max + depth cap.                                                                                                             |
| [6 — submit_play gate](phase-6-submit-play-gate.md)                                   | ✅ Done | Director-only gate: validate, resolve, atomically persist `plays/story_<UTC>.json` + append `plays/index.json`.                                                                                                  |
| [7 — Director + swarm](phase-7-director-swarm.md)                                     | ✅ Done | Fixed director prompt + tools; spawn sub-agents by role note; assemble; submit. Single-writer play; exhaustion ships nothing.                                                                                    |
| [8 — Critic](phase-8-critic.md)                                                       | ✅ Done | Fixed advisory role; reads feedback + notes + submitted play; writes evidence-cited `criticism` into `feedback/`; never vetoes.                                                                                  |
| [9 — Serve wiring + play API](phase-9-serve-wiring-play-api-cutover.md)               | ✅ Done | `/api/v1/troupe/play*` + `POST .../feedback`, unified feedback, facade (cooldown/single-flight/Warm), engine in production frontend, old splash removed.                                                         |

Execution order: 0 → 9 in order; 0 first so implementers never read the old theatre.

## What we are building

Replace the fixed-vocabulary, fixed-role theatre with a **director, a swarm, and
a self-evolving repertoire of models and animations running on a fixed engine**.
The director is the mastermind, not a worker: it reads viewer feedback, spawns a
dynamic swarm of concurrent sub-agents, each authoring a piece of the content,
then assembles and submits a play. Everything the director and its agents author
— roles, models, animations, voices, sounds, plays — lives as notes in the slivingdoc
notebook. One fixed reviewer, the **critic**, comments on the director's work
against viewer ground truth; it has opinions, never authority.

There is **no human intervention in content after the grammar** — no seed, no offline floor.
The only ongoing human input is feedback (thumbs, comments, behavioural
feedback). Animations are authored by the troupe, not by a human writing CSS.
No play means nothing renders; an empty stage is the signal to investigate.
What stays human-owned is the _grammar_ — the atom types the engine accepts —
and the engine that interprets it.

Two layers, named so "self-evolving" never again means "the framework evolves":

- **The stage** (fixed, human-owned, Go + JS): the engine (a tiny 2D skeletal
  keyframe renderer with a deterministic expander), the grammar it accepts, the
  validator, the resolver, the closed tool registry, the spawn runner, the
  `submit_play` gate, the termination authority, and the two fixed roles
  (director, critic).
- **The repertoire** (evolving, agent-authored, in slivingdoc notes): role
  definitions, models (characters, props, pieces, backdrops), clips
  (animations), voices, gags, and plays.

The composition, the animation and the structure evolve; only the engine and
the grammar stay consistent.

## The engine

The engine is a **2D skeletal keyframe renderer with rigid skinning**, inspired
by Spine's data model but deliberately a strict, minimal, LLM-authorable subset.
We do **not** adopt the Spine format or its runtimes:

- Spine's runtimes need a paid license; a self-hosted project must not carry it.
- Spine's web players render to canvas/WebGL in TypeScript; our floor is webOS
  TV 4.x (Chromium 53, ES5), which animates only `transform`/`opacity` on CSS
  divs because a weak TV cannot afford canvas repaints.
- Spine JSON is editor-oriented and too verbose for reliable LLM authorship.
- Spine covers only skeletal animation: no audio, no stage/choreography, no gag
  or play sequencing, no procedural structure.

with curves, events — and implement our own tiny ES5/CSS/DOM runtime, extended
with the three things Spine lacks: **formant voices**, a **stage/choreography
layer** (root-bone tweens plus clip/gag/play sequencing), and a **procedural
structure layer** (attach/scatter/recurse).

Two pieces of the engine are closed-form and deterministic, never general
solvers:

- **Closed-form 2-bone IK.** The authoring surface is _constraints_ (goals),
  not raw joint angles. The engine solves them with analytic two-bone IK (no
  iteration, no convergence), because an LLM authors "paw reaches the mouse"
  far more reliably than "thigh 20°, shin 30°". FK keyframes and oscillation
  remain as escape hatches for fine art constraints cannot express.
- **A deterministic expander.** Structural verbs (attach/scatter/recurse) are
  expanded by the engine from a seed, so a whole forest is a compact spec, not
  a list of coordinates.

**Rigid skinning only.** Each shape binds to exactly one bone and moves as that
bone moves (a div child of a bone div). No mesh/vertex weights, no path/transform
constraints. **Flat colour only** — rect (rounded), ellipse, path; no lighting,
no gradients.

### Complexity lives in the engine, not the authoring surface

The guiding trade: keep the LLM surface small, semantic and relational, and let
the fixed, tested engine absorb the deterministic complexity. Raw numbers with
physical meaning are what the LLM gets wrong; constraints, references and enums
are what it gets right. Engine bugs are found once by tests and frozen; LLM
authoring errors recur every generation.

## The frozen grammar (the meta-schema)

This is the only human-owned surface. Everything the troupe authors is an
instance of one of these types, and nothing else exists.

**Structure**

- **`bone`** — a transform node with an id, a parent, and x/y/rotation/scale/
  `length`. Bones form a tree (the rig). A bone is both a joint (where it
  rotates) and a segment (its far end is where children attach).
- **`attachment`** — a flat shape (rect/ellipse/path) bound to exactly one bone,
  with a local transform and a flat colour.
- **`slot`** — a draw-order position holding one attachment.
- **`skin`** — a named set of alternative attachments per slot (a coat is a
  skin).
- **`model`** — a skeleton (bones) + slots/attachments (art) + optional
  `voice` and `sound` references (in `spec`) + structural verbs. Characters, props, pieces and backdrops
  are all models, differing only in scale, stage position and role. A backdrop
  is a large model with its own idle clips.

**Motion**

- **`constraint`** — the primary authoring surface, four verbs, solved by
  closed-form 2-bone IK:
  - `reach`: `{ "type":"reach", "effector":"<boneId>", "target":{"x":..,"y":..},"hint":"front" }`
    — the effector bone's far end reaches a local coordinate;
    `hint` ∈ `front`/`back`/`left`/`right` (the pole direction).
  - `look`: `{ "type":"look", "chain":"<boneId>", "target":{"x":..,"y":..} }` —
    the chain faces a local coordinate.
  - `plant`: `{ "type":"plant", "bone":"<boneId>", "at":{"x":..,"y":..} }` — the
    bone stays at a local coordinate while the rest moves.
  - `track`: `{ "type":"track", "chain":"<boneId>", "target":"<boneId>" }` — the
    chain continuously follows another bone.
    Reach/look/plant target a local coordinate `{x,y}`; `track` targets a bone id.
- **`keyframe`** — the escape hatch: `{ "bone":"<id>" | "slot":"<id>",
"channel":"rotation", "easing":"ease-in-out",
"keys":[{"t":0,"v":0},{"t":200,"v":30}] }` — a channel (x/y/rotation/scaleX/
  scaleY/opacity) keyframed over time on a bone or slot. The **easing enum** is
  declared once here and shared by tween: `linear`, `ease-in`, `ease-out`,
  `ease-in-out`.
- **`oscillation`** — a periodic channel (sine with amplitude/frequency/phase),
  for a tail wag or body bob that is neither a target nor a keyframe.
- **`clip`** — a set of constraints/keyframes/oscillations + a duration + a loop
  flag + events. An event is `{ "at": <ms>, "voice": true }` (vocalize with the
  instance's resolved voice) or `{ "at": <ms>, "sound": "id@version" }` (play a
  named sound effect) — exactly one of `voice`/`sound`. A clip may target bones
  by exact id or by **selector** (below).

  Motion lives in two frames. A clip animates bones in the model's own frame —
  relative to the model's root — so `walk@1` only swings the legs, never moves
  the character across the stage. Crossing the stage is the play's job: a
  **`tween`** (below) moves the model's root bone. "Cat walks left to right" is
  a `walk@1` clip (legs) _and_ a root tween (position) together.

- **`voice`** — the formant parameters (f0, formants, gains, q, mouth, bursts,
  gap, decay, pitch, vib, noise, pure), range-validated: `amp`/`pure`/`noise`/
  `decay` ∈ 0–1, `f0`/`q`/`dur`/`gap` positive, `bursts` a positive integer
  pair, `tracks`/`gains`/`q`/`mouth`/`pitch`/`vib` fixed-length numeric arrays
  matching the synth, `burstPitch` optional. Maps 1:1 to the engine's formant
  synth (rehomed from the old `intro.js` `VOICES`); see the conformance example.
- **`sound`** — an environmental (non-vocal) sound effect: a fixed small
  synthesis vocabulary — `type` ∈ `noise`/`tone`/`sweep`/`burst`, with freq/dur/
  amp ranges and an envelope — mapped to Web Audio. Characters vocalize with
  `voice`; trees, cars and backdrops use `sound`.

**Structure (procedural)**

- **`attach`** — instance a sub-model at a bone's end, with a relative `scale`
  and an optional `rot` offset. The child's +Y aligns with the parent bone's
  direction. Modularity.
- **`scatter`** — place N instances of a model over a region, with seeded
  jitter. "A forest / a pile of rocks".
- **`recurse`** — self-similar nesting: `depth`, `branch` (factor per level),
  `angle` (spread), `decay` (scale per level), and a `tip` terminal model. This
  is an L-system rule; self-reference is allowed, bounded by `depth`.

**Regions** — the closed `over` vocabulary for `scatter`: `band` (rect), `disc`,
`grid`, `curve`, `along` (a bone, for "leaves along a stem").

**Selectors** — clips and constraints may target generated content by pattern in
addition to exact id, in exactly two forms: a wildcard path
(`tree#*/branch#*/tip`), or "all instances of a model" written
`model:<id>@<version>` (e.g. `model:leaf@1`). Procedural content is animatable
only through selectors.

**Composition**

- **`gag`** — a named sequence of clips, an ordered list of clip `id@version`s
  played sequentially by their own durations: `{ "spec": { "clips": ["blink@1",
"pounce@1"] } }`. Never another gag, no stage, no instantiations, no `at`.
- **`tween`** — a root-bone stage motion on one play instance: move the
  instance's root from its current pose to a target over `over` milliseconds
  with an `easing` (`linear`, `ease-in`, `ease-out`, `ease-in-out`). The target
  is exactly one of: absolute coordinates (`x`/`y`/`rot`/`scale`), a `beside`
  reference to another instance `id` (`side` ∈ `left`/`right`/`front`/`back`),
  or an `off` exit side (`left`/`right`). `beside` and `off` resolve in the
  engine at render time; absolute coordinates are literal. A tween moves the
  whole instance — it is how a character crosses the stage or meets another.
- **`play`** — model instantiations (an `id`, the `model`, `scale`, stage
  position `x`/`y`, optional `voice`/`sound` overrides, and a `role`) + a timed
  `timeline`. Every timeline entry names the instance (`on`, an instance `id`)
  and carries exactly one verb: `clip` (trigger a clip), `gag` (trigger a gag) or
  `tween` (move the root). Entries sharing the same `at` run concurrently, so a
  walk is a `clip` (legs) plus a `tween` (root) at the same `at`. All times are
  milliseconds.

The composition boundary is strict: a `clip` references bones only (never a
clip); a `gag` references clips only (never a gag); a `play` references models,
clips and gags. The validator rejects a clip referencing a clip and a gag
referencing a gag.

**Reference precedence.** An instance's `voice`/`sound` overrides the model's
`spec.voice`/`spec.sound`; both are optional and singular. `voice: true` in a
clip event uses the instance's resolved voice (override → model default →
none).

**Determinism and budgets**

- Every `scatter`/`recurse` carries a seed (or inherits a play-level seed). An
  instancer derives a per-instance seed from `(parent seed, instance index)`, so
  N trees are N unique but reproducible trees, never N clones.
- Generated instances get deterministic path ids (`forest/tree#3/branch#1/leaf#2`).
- Scale is multiplicative through the hierarchy, so `decay` compounds over depth.
- The resolver enforces a cheap arithmetic **expansion budget**: declared
  `count`, `depth` and `branch` are each capped, and the declared values must
  not exceed a configurable total node budget. It does _not_ expand. The engine
  does the full expansion and may optimize it away — static-render a subtree
  once and replay it, batch instances that share geometry, flatten a static
  subtree to a pre-rendered layer. The budget is an operational guardrail, not a
  design constraint; agents never author against it.

The grammar is authored twice, in lock-step, and the validator is what keeps
them from drifting: as Go type definitions plus validators (the authority), and
as a `STAGE.md` note in the notebook (the human-readable spec the director reads
before authoring against it).

### Canonical conformance example (leaf → branch → tree → forest)

Every asset is one uniform envelope — `kind`/`id`/`version`/`status`/`author`/
`provenance` — with the kind-specific content nested under `spec`. Plays are the
exception: no `version` (see "The library format"). This example is test-fixture-only (validator, resolver and engine-lab fixtures); it is never shipped into the notebook as a floor — there is none.

```json
// models/leaf@1.json — terminal shape
{ "kind": "model", "id": "leaf", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "bones": [ { "id": "root", "parent": null, "x": 0, "y": 0, "rot": 0, "length": 0 } ],
    "attachments": [ { "id": "blade", "slot": "main", "bone": "root",
      "shape": { "type": "ellipse", "w": 1, "h": 3, "color": "#3a7d44" } } ],
    "skins": { "default": { "main": "blade" } } } }

// models/branch@1.json — a stem that recurses into itself, ending in leaves
{ "kind": "model", "id": "branch", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "bones": [ { "id": "stem", "parent": null, "x": 0, "y": 0, "rot": 0, "length": 4 } ],
    "structure": [ { "type": "recurse", "model": "branch@1", "at": "stem",
      "depth": 3, "branch": 2, "angle": 30, "decay": 0.7, "tip": "leaf@1", "seed": 2 } ] } }

// models/tree@1.json — a trunk with the branch crown on top
{ "kind": "model", "id": "tree", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "bones": [ { "id": "trunk", "parent": null, "x": 0, "y": 0, "rot": 0, "length": 6 } ],
    "structure": [ { "type": "attach", "model": "branch@1", "at": "trunk", "scale": 1 } ] } }

// models/forest@1.json — a backdrop model that scatters trees
{ "kind": "model", "id": "forest", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "sound": "rustle@1",
    "structure": [ { "type": "scatter", "model": "tree@1", "count": 20,
      "over": { "type": "band", "w": 20, "h": 4 },
      "jitter": { "scale": 0.3, "rot": 8 }, "seed": 7 } ] } }

// voices/cat@1.json — a voice (maps 1:1 to the current formant synth)
{ "kind": "voice", "id": "cat", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "dur":  [0.42, 0.60], "f0": [620, 770], "amp": [0.50, 0.72],
    "kf":   [0.00, 0.20, 0.55, 1.00],
    "tracks": [[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],
    "gains": [1.0, 0.45, 0.14], "q": [7, 10, 11],
    "mouth": [550, 4600, 2900, 780], "pitch": [0.94, 1.16, 0.82],
    "vib":   [7, 10, 0.02], "noise": 0.02, "pure": 0.42,
    "bursts": [1, 2], "gap": [0.06, 0.20], "decay": 0.80,
    "burstPitch": [1, 1.42, 0.94] } }

// sounds/rustle@1.json — an environmental sound effect (no vocal cords)
{ "kind": "sound", "id": "rustle", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": { "type": "noise", "freq": [800, 2400], "dur": [0.3, 0.9],
            "amp": [0.2, 0.5], "env": { "attack": 0.05, "decay": 0.4 } } }

// models/cat@1.json — an actor with a default voice and legs to walk on
{ "kind": "model", "id": "cat", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "bones": [
      { "id": "root", "parent": null, "x": 0, "y": 0, "rot": 0, "length": 0 },
      { "id": "spine", "parent": "root", "x": 0, "y": 1, "rot": 0, "length": 3 },
      { "id": "frontLeg", "parent": "spine", "x": 0, "y": 2, "rot": 0, "length": 2 },
      { "id": "backLeg", "parent": "spine", "x": 0, "y": 0, "rot": 0, "length": 2 } ],
    "attachments": [ { "id": "body", "slot": "main", "bone": "spine",
      "shape": { "type": "ellipse", "w": 3, "h": 2, "color": "#c89b6a" } } ],
    "skins": { "default": { "main": "body" } },
    "voice": "cat@1" } }

// clips/walk@1.json — legs swing in place; the root does not move
{ "kind": "clip", "id": "walk", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "duration": 1200, "loop": true,
    "oscillations": [
      { "bone": "frontLeg", "channel": "rotation", "amp": 30, "freq": 2, "phase": 0 },
      { "bone": "backLeg", "channel": "rotation", "amp": 30, "freq": 2, "phase": 180 } ] } }

// clips/pounce@1.json — a reach + look + keyframe + two audio events
{ "kind": "clip", "id": "pounce", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "duration": 600, "loop": false,
    "constraints": [
      { "type": "reach", "effector": "frontLeg", "target": { "x": 1.5, "y": 0 }, "hint": "front" },
      { "type": "look", "chain": "spine", "target": { "x": 1.5, "y": 0 } } ],
    "keyframes": [
      { "bone": "spine", "channel": "rotation", "easing": "ease-in-out",
        "keys": [ { "t": 0, "v": 0 }, { "t": 200, "v": 20 }, { "t": 500, "v": 0 } ] } ],
    "events": [
      { "at": 400, "voice": true },
      { "at": 500, "sound": "rustle@1" } ] } }

// clips/blink@1.json — a keyframe on a slot (opacity pulse)
{ "kind": "clip", "id": "blink", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": {
    "duration": 160, "loop": false,
    "keyframes": [
      { "slot": "main", "channel": "opacity", "easing": "linear",
        "keys": [ { "t": 0, "v": 1 }, { "t": 80, "v": 0.2 }, { "t": 160, "v": 1 } ] } ] } }

// gags/doubletake@1.json — a reusable routine of clips
{ "kind": "gag", "id": "doubletake", "version": 1,
  "status": "draft", "author": "fixture", "provenance": "fixture",
  "spec": { "clips": [ "blink@1", "pounce@1" ] } }

// plays/story_20260820T161500Z.json — forest backdrop, the cat walks in and
// meets the dog. The cat's travel is the two `tween`s; `walk@1` only swings
// its legs in place. (Shown raw; submit_play stores the resolved form — play
// + asset table — under this datetime id.)
{ "kind": "play", "id": "story_20260820T161500Z",
  "status": "submitted", "author": "director", "provenance": "generation g_01j8x",
  "spec": {
    "instances": [
      { "id": "forest", "model": "forest@1", "role": "backdrop", "scale": 2.4, "y": -0.2 },
      { "id": "cat", "model": "cat@1", "role": "actor", "voice": "cat@1", "scale": 1, "x": 0.1 },
      { "id": "dog", "model": "dog@1", "role": "actor", "voice": "dog@1", "scale": 1, "x": 0.9 } ],
    "timeline": [
      { "at": 0, "on": "cat", "clip": "walk@1" },
      { "at": 0, "on": "cat", "tween": { "to": { "x": 0.5 }, "over": 3000, "easing": "ease-in-out" } },
      { "at": 3000, "on": "cat", "tween": { "to": { "beside": "dog", "side": "left" }, "over": 900, "easing": "ease-out" } },
      { "at": 3900, "on": "cat", "gag": "doubletake@1" } ] } }
```

## The resolved play (the served artifact)

The troupe _authors_ the play with references intact (`"model": "cat@1"`). The
resolver emits a second, derived document — the **resolved play** — the single
thing served to the browser and consumed by the engine. It is the play plus a
flattened, transitively-closed asset table:

```json
{
  "play": { "…": "the play spec, references intact" },
  "assets": {
    "models": { "cat@1": { "…" }, "forest@1": { "…" } },
    "voices": { "cat@1": { "…" } },
    "sounds": { "rustle@1": { "…" } },
    "clips":  { "walk@1": { "…" } },
    "gags":   { "doubletake@1": { "…" } }
  }
}
```

The `assets` maps are keyed by `id@version` within each kind, so `cat@1` the
model and `cat@1` the voice are distinct. The resolver's job is exactly the
cross-file `id@version` closure: structural verbs, `spec.voice`, play
instances/timeline and gag clips, transitively including depth-bounded
self-reference. It does **not** touch anything inside a spec — bone ids, slot ids
and selectors to generated content stay as authored, because those resolve in
the engine at render time (the engine expands structural verbs and knows
instance positions). The engine resolves a reference as a pure map lookup:
`"model": "cat@1"` → `assets.models["cat@1"]`.

The resolved play is what `submit_play` persists (see "The library format"): one
file per play under a UTC datetime id, with `plays/index.json` as the
newest-first index. The serve endpoint returns the most recent one.

## Core principles

- **The content evolves, the grammar does not.** Agents author new models,
  clips, voices, gags, plays and roles. They never author a new grammar atom, a
  new tool, or a new renderer feature — those are human-gated.
- **No human content, no seed, no offline floor.** Apart from feedback, the
  troupe is fully autonomous. If there is no play there is no play — an empty
  stage is the signal to investigate.
- **Two fixed poles, everything else spawned.** Director (generative, sovereign)
  and critic (reflective, advisory) are the only roles in Go. Every other role
  is a note the director authors.
- **One substrate: notes.** Roles, models, clips, voices, gags, plays, feedback
  and criticism are all notes in slivingdoc. Monitoring and tweaking means
  editing a note, never a rebuild.
- **The play is single-writer.** The director alone submits the play as one
  resolved JSON file. The swarm writes everything except the play concurrently;
  merge applies to the repertoire, never to the play.
- **Bounded swarms with a mastermind.** The director decides the swarm's size,
  decomposition and roles. The stage bounds the generation regardless: a global
  call maximum, a token threshold, and a spawn-depth cap.

## The production loop

```text
viewer ──▶ feedback (explicit + behavioural)
                │
director reads feedback ──▶ decomposes into smaller concerns
                │
spawns a swarm of concurrent sub-agents (roles are notes, shaped by the director)
                │
sub-agents author/revise models, clips, voices, sounds, gags + leave notes
                │
director assembles the play ──▶ submit_play validates every reference resolves ──▶ served
                │
critic reads feedback + notes + the submitted play ──▶ leaves evidence-cited comments (never a veto)
```

The director decides when to submit. There is no authoritative iteration cap:
the critic cannot block, and the director is sovereign. Termination comes from
the stage's termination authority, not from a review gate. The critic runs after
the generation and comments for the _next_ one.

**Generation.** One generation = one director run from feedback-read through
decompose → swarm → assemble → submit, or exhaustion. It is the unit the troupe
facade guards: `Prepare` triggers at most one generation (hardcoded cooldown +
single-flight), `Warm` materializes the notebook (no seed, no composer), and the
served play is always the newest submitted play read from disk
(`/api/v1/troupe/play/resolved`) — there is no in-memory `current` story.

## The tool surface (closed registry)

Every agent gets exactly the tools its role note selects from this closed
registry. A role note can select, never define. The registry maps canonical tool
names to concrete clai tools/globs in one place:

- filesystem read/write — `cat`, `rows_between`, `ls`, `rg`, `write_file`,
  `apply_patch`, `mkdir`
- slivingdoc — `mcp_slivingdoc_notes_pull`, `mcp_slivingdoc_notes_commit`
- `spawn_role` — recursive
- `submit_play` — director-only (excluded from the general registry)

`submit_play` validates that every resource the play references exists and is
valid, and returns the exact errors wherever they manifest, passed back to the
director to fix.

## Roles are notes

A role is a note, not Go code. The director authors a role by writing a file in
`roles/`; the stage reads and executes it by name.

```json
{
  "id": "clown",
  "prompt": "You are the clown. You decide: … You stop: …",
  "tools": ["cat", "write_file", "spawn_role"],
  "budget": 8
}
```

The stage validates before running: `id` pattern-checked, `prompt`
length-capped, every `tools` entry in the closed registry, `budget` clamped. A
role whose tool list names anything outside the registry is refused. The
director's own spawns run through the same runner as every other role.

## The notebook is the single substrate; slivingdoc is an MCP boundary

The repertoire lives in the slivingdoc notebook as `roles/`, `models/`,
`clips/`, `voices/`, `sounds/`, `gags/`, `plays/` and `feedback/`. slivingdoc is
**only** an MCP server for the agents; kinoview's Go code does not import
slivingdoc. kinoview serves the current play from the materialized worktree and
writes feedback to the same worktree as one JSON file per note (write + commit remain one
unit). `pull` merges local changes rather than clobbering them; conflict markers
in a shared file are a normal state. The resolver flags conflict-marked files
rather than parsing them; the director reconciles them, and a reconciliation
that fails means no play serves.

## The fixed stage (human-owned)

Human-owned and never agent-authored:

- **The engine + grammar** — the skeletal keyframe runtime, the deterministic
  expander, the closed-form IK, and the meta-schema above.
- **The validator + resolver** — deterministic functions that walk the worktree,
  validate each file against its grammar, resolve inline `id@version` references
  (transitively, including depth-bounded self-reference), run the arithmetic
  expansion-budget sanity check, and report what resolves.
- **The tool registry** — the closed set above, with one name→tool mapping.
- **Termination authority** — the generation's global maximum, token stoploss
  and depth cap, enforced regardless of swarm size or spawn depth.
- **The two fixed roles** — the director and critic prompts.

There is **no seed and no offline floor**: with no play the engine renders
nothing, and an empty stage is the signal to investigate. The troupe is gated on
slivingdoc — a missing notebook means the troupe does not start and the play API
returns 404.

### Termination authority

Three guards. Only one is an operator-tunable flag; the other two are hardcoded
constants (defense-in-depth, not operator surface):

- **Token stoploss** — the cost guard and the only flag: `-troupeTokenStoploss`.
  Once the generation's cumulative token usage crosses the stoploss,
  `spawn_role` refuses new spawns. Reservation is atomic: a spawn checks and
  reserves under one lock, so concurrent spawns cannot both pass the threshold.
- **Global maximum** — the hard cap on one generation's total work (calls).
  Hardcoded constant.
- **Depth cap** — the recursion guard: a spawn past the cap is refused, never
  spawned. Hardcoded constant.

The token stoploss is a token count, not a monetary claim. It is calibrated so a
single generation stays comfortably under its operator-set budget.

## The library format (inside slivingdoc)

Layout: one file per asset, named by id and version — except plays, which are
unversioned (a UTC datetime id, no `@version`).

```text
models/    ina@1.json, cat@1.json, night@1.json, yarn@1.json, …
clips/     walk@1.json, pounce@1.json, wag@1.json, …
voices/    cat@1.json, dog@1.json, …
sounds/    rustle@1.json, engine@1.json, …
gags/      doubletake@1.json, …
plays/     index.json, story_20260820T161500Z.json, story_20260821T093000Z.json, …
feedback/  story_<UTC>_rating_<UTC>.json, story_<UTC>_dismissal_<UTC>.json, …
```

Characters, props, pieces and backdrops are all `models/` — the kind distinction
is a role the play assigns, not a separate asset type. Every file is one uniform
envelope:

```json
{
  "kind": "model",
  "id": "cat",
  "version": 1,
  "status": "draft",
  "author": "sculptor",
  "provenance": "commission: cat@1 legs read stiff",
  "spec": { … }
}
```

The envelope is fixed; `spec` is the only kind-specific part and is constrained
by the frozen grammar, not by arbitrary transforms: a model spec is a skeleton +
bound shapes + structural verbs; a clip spec is constraints + keyframes +
oscillations; a voice spec is formant parameters; a sound spec is a small
synthesis vocabulary; a play spec is instances + timeline. Feedback notes are the
exception: they are not versioned assets and use a different envelope
(`playId`/`type`/`ts`/`data`, one file per note — see "Feedback").

**Identity is the filename.** The filename is the only authority for `id@version`;
the envelope's `id`/`version` must match it exactly or the validator rejects the
file. References are typed by field: `model` resolves into `models/`, `voice`
into `voices/`, `clip` into `clips/`, `gag` into `gags/` — `id@version` is not
globally unique across directories, only within one. There is no `deps` map:
inline references in `spec` (structural verbs, `spec.voice`/`spec.sound`, play instances) are
the complete dependency graph, and `submit_play` verifies each resolves
transitively (including self-reference bounded by `depth`).

**Asset ids and status.** Asset `id`s share one charset and become filenames, so
they must be filesystem-safe: `^[a-z0-9_-]{1,64}$` (lowercase alnum, `_`, `-`;
no `/`, no spaces). Roles reuse the same charset. The `status` enum is closed:
`draft` (the normal state while the swarm works) and `submitted` (set by
`submit_play` on a play; assets stay `draft`). There is no `retired`/
`superseded` — old plays are kept by filename, never marked.

**Plays are the stored, reviewable history.** A play is a resolved play — the
play plus its flattened asset table — persisted under a UTC datetime id
(`plays/story_20260820T161500Z.json`; no `@version`, no `version` field). The id
is lexicographically sortable, so newest-first is a plain sort. `submit_play`
atomically writes the resolved play and appends a metadata entry to
`plays/index.json` (a newest-first list of `{id, status, author, provenance,
created}` objects — the paginated play index reads this, never the play files).
`GET /api/v1/troupe/play/resolved` returns the newest entry. Old plays are kept,
never overwritten, so the critic and `debug` can watch and review the history.

**Play API routing.** `GET /api/v1/troupe/play/resolved` is matched before
`GET /api/v1/troupe/play/{id}`; `resolved` is a reserved path segment, never a
play id. The index endpoint `GET /api/v1/troupe/play` paginates over
`plays/index.json` with keyset cursor `limit`/`order`/`status`/`author` filters.

## The critic

A fixed advisory role in Go. After a generation it reads viewer feedback, the
submitted play, and the notes the swarm left, and emits evidence-cited, spicy
comments. Every critic note cites the feedback note paths it is grounded in. The
critic never drives, never blocks, never edits: it comments for the next
generation's director. If a generation ends by exhaustion with no submitted
play, the critic still runs and comments on the empty stage — why nothing
shipped — citing whatever notes exist; it never fabricates a play.

## Feedback

All viewer ground truth — explicit thumbs and behavioural feedback — lands as
notes in `feedback/`, one JSON file per note, named
`<playId>_<type>_<utc>.json`. The critic writes its `criticism` notes into the
same directory server-side; the audience writes the rest through
`POST /api/v1/troupe/feedback`.

Uniform body (every note):

```json
{
  "playId": "story_20260820T161500Z",
  "type": "rating",
  "ts": "2026-08-20T16:15:04Z",
  "data": { "rating": 1, "comment": "more dog" }
}
```

Types and their `data`: `rating` (rating ±1, optional comment), `dismissal`
(`atMs`), `completion` (`durationMs`), `replay` (`count`), `continuity`
(`history`), `criticism` (`generationId`, `cites`, `body`). The filename's
`<type>` and `<utc>` match the body's `type` and `ts` (compact form); `playId`
also appears in the filename, so a director pulls one play's whole trail with
`ls feedback/<playId>_*`. The client sends `{ "playId", "type", "data" }`; the
server stamps `ts` and derives the filename `<playId>_<type>_<utc>.json` — the
client never sends `ts`.

## Phases

Each phase is independently shippable and testable. Phases run in order. Phase 0
removes the old theatre first so implementing agents never read it.

### Phase 0 — Deprecate the old theatre

Remove `internal/agents/theatre`, the composer, `model.Story`, `intro.js`, the
old `agents.Teller`/`agents.Feedbacker` contracts, and the `/intro/*` endpoints.
Update `serve`/`media`/`debug` wiring so the build and `make qa` stay green with
no intro splash (an empty `<div id="troupe">`). From here on there is no play
until the troupe submits one.

### Phase 1 — Frozen grammar

Go types + validators for `bone`, `attachment`, `slot`, `skin`, `model`,
`constraint`, `keyframe`, `oscillation`, `clip`, `voice`, `sound`, structural
verbs (`attach`/`scatter`/`recurse`), regions, selectors, `gag`, `tween` and
`play`; the uniform asset envelope; the filename↔`id@version` rule; the
resolved-play schema. Encode the conformance example as test fixtures (never
shipped) and author `STAGE.md`.

### Phase 2 — Engine

Standalone `engine.js` (ES5, no canvas) self-mounting into `<div id="troupe">`:
bone-div tree, rigid skinning, closed-form 2-bone IK, FK keyframes + oscillation,
formant voices + sound effects, root-bone stage tweens, clip/gag/play sequencing,
the deterministic expander, selectors. `lab/` is a separate dev site + tools +
simulator. No `package.json`, no framework, no linter. The engine's input is the
Phase 1 resolved-play format; it tests against fixture resolved plays, not the
Phase 3 resolver.

### Phase 3 — Resolver + validator

Walk the worktree, validate every file against its grammar, resolve inline
`id@version` references transitively (including depth-bounded self-reference),
flag conflict-marked files, emit the resolved play, and enforce the arithmetic
expansion budget.

### Phase 4 — Role notes + tool registry + spawn runner ✅

Role-note reader + validator (`id`/`prompt`/`tools`/`budget`), the closed
name→tool registry, and the recursive `spawn_role` runner.

### Phase 5 — Termination authority ✅

Token stoploss (`-troupeTokenStoploss`) with atomic reservation, plus the
hardcoded global call max and depth cap. The final answer is never budgeted.

### Phase 6 — submit_play gate ✅

Director-only tool: validate, resolve, atomically persist
`plays/story_<UTC>.json` + append a metadata entry to `plays/index.json`. Exact
errors return to the director; a second submit is refused; a persist failure
aborts.

### Phase 7 — Director + swarm ✅

Fixed director prompt + tool set; spawn sub-agents by role note; assemble;
submit. Single-writer play. Exhaustion without a submit ships nothing (no seed).

### Phase 8 — Critic ✅

Fixed advisory role; reads feedback + notes + the submitted play; writes
evidence-cited `criticism` notes into `feedback/`. Never vetoes, never edits.

### Phase 9 — Serve-side wiring + play API + cutover ✅

Serve the troupe API (`/api/v1/troupe/play*`, `POST /api/v1/troupe/feedback`),
record unified feedback, adapt the facade (cooldown/single-flight/Warm), wire the
engine into the production frontend, and remove the last of the old splash.

## Holistic review (session 2026-08-23, clai worker)

All ten phases (0–9) are complete and in the tree. A final review pass over the
whole worklog found the tree consistent with the README's design and green
under the full QA gate, and one engine robustness defect (below) was fixed
with a test.

**Verification (exact commands and results, all green):**

```bash
go build ./...                                       # green
go test ./... -race -count=3 -cover -timeout=30s     # 23 packages ok
   # troupe 87.5%, media 82.3%, serve 66.9% coverage; no skips
node cmd/serve/frontend_test/engine.test.js          # 17/17 (was 16/16 + the new mount test)
node cmd/serve/frontend_test/lab.smoke.js            # ok
node --check cmd/serve/frontend/engine.js            # syntax ok
go run mvdan.cc/gofumpt@latest -l .                  # no output (clean)
go vet ./...                                         # clean
go run honnef.co/go/tools/cmd/staticcheck@latest ./...  # clean
go fix ./...                                         # clean
go run github.com/mibk/dupl@latest -t 80 .           # only the pre-existing accepted clone groups
```

**Review findings:**

- **Phase 0** — the old theatre is gone end to end: no `internal/agents/theatre`,
  no `model.Story`, no `Teller`/`Feedbacker`, no `intro.js`, no `/intro/*`
  routes, no `feedback.jsonl`; only historical comments mention the names. The
  clai v1.10.23-rc1 migration (decision 23) is in place: the classifier's
  `SetOutput` wires `agent.WithLogger(slog text handler)`.
- **Phases 1–3** — `grammar.go`/`validate.go`/`STAGE.md` ship the frozen
  grammar with the conformance fixtures; `resolve.go` walks the worktree,
  validates, flags conflict markers, closes the `id@version` closure and
  enforces the arithmetic `ExpansionBudget` (the phase-5 rename).
- **Phases 4–6** — the role-note reader/validator, the closed registry, the
  recursive spawn runner, the three-guard `Budget` (stoploss flag + hardcoded
  call max/depth cap, atomic reservation, final answer never budgeted) and the
  single-writer `submit_play` gate (file-first/index-second, fsync + rename,
  second-submit refusal) all match their docs.
- **Phases 7–8** — the fixed director (prompt NOW stamp, registry + submit
  tool, bounded run at depth 0) and the fixed critic (read-only tools +
  pinned `write_criticism`, append-only, empty-stage honesty) run under the
  facade; the critic is outside the generation budget as documented.
- **Phase 9** — `troupe_handlers.go` + `playlib.go` + `feedback.go` serve the
  four `/api/v1/troupe` routes; the facade wires `WithGenerationCritic`;
  serve exposes exactly `-troupeModel`/`-troupeTokenStoploss`; the engine
  boots in production through `#troupe` + the resolved-play fetch; a disabled
  notebook leaves the API unmounted (404, pinned by a serve test).
- **Fixed in this review** — the engine's `mount` was not idempotent: a second
  mount on the same element (the bootstrap self-mount racing the fetch-driven
  mount in `index.js`, or a re-mount after a new generation) stacked a second
  live stage with its own rAF loop. `mount` now keeps one stage per element
  (`stages` registry): a re-mount destroys the previous stage, separate
  elements keep concurrent stages. Pinned by a new engine test.

No new phase was started: the worklog is complete. The next session's work is
not this worklog's — the repertoire (agent-authored notebook content) and any
future production fixes.

## Decision log (holistic review, session 2026-08-23, clai worker)

- **D-H-1 — The engine keeps one stage per element, not one global stage.**
  The first fix attempt used a single module-scoped handle; the existing
  reproducibility tests mount two plays side by side into separate elements
  and compare them, so a global stage broke a legitimate concurrent-mount
  use case (the second mount destroyed the first run's DOM). The final fix
  tracks stages by element: a re-mount on the same element destroys the
  previous stage (rAF loop stopped, DOM removed); separate elements keep
  concurrent stages. This matches the harness's dual-mount pattern and the
  production race it guards.

## Flagged decisions

1. A spawned role = a note (`id`, `prompt`, `tools`, `budget`) selecting from the
   closed registry. New tools and new grammar atoms are human-gated.
2. Content — models, clips, voices, sounds, gags, plays, roles — is fully
   agent-authored; the only ongoing human input is feedback. There is no seed and
   no offline floor: no play means nothing renders.
3. The engine is a Spine-inspired minimal skeletal keyframe runtime with rigid
   skinning; we do not adopt Spine's format or runtimes. Motion is authored as
   constraints solved by closed-form 2-bone IK, with FK keyframes and
   oscillation as escape hatches. No canvas/WebGL; CSS divs only.
4. Characters, props, pieces and backdrops are one asset kind: `model`. A
   backdrop is a large model with idle clips.
5. Structural verbs (`attach`/`scatter`/`recurse`) give procedural generation;
   modularity is the versioned dependency graph. Self-reference is allowed,
   bounded by `depth`.
6. `attach` targets a bone id; the child's +Y aligns with the parent bone's
   direction (a `rot` offset overrides). `scatter` owns all N>1 placement over
   the closed region vocabulary; `attach` stays singular.
7. Determinism: every `scatter`/`recurse` carries a seed; instancers derive a
   per-instance seed from `(parent seed, instance index)`; generated instances
   get deterministic path ids; scale is multiplicative through the hierarchy.
8. Selectors (wildcard paths / "all instances of model X") let clips and
   constraints animate procedurally generated content.
9. Expansion happens in the engine, not the resolver. The resolver runs only a
   cheap arithmetic budget check on declared counts/depth/branch.
10. The director is the single writer of the play; the swarm writes everything
    except the play concurrently; merge applies to the repertoire, not the play.
11. Audio is two kinds: `voice` (formant vocalization, characters) and `sound`
    (environmental effects, `sounds/`, synthesis types `noise`/`tone`/`sweep`/
    `burst`). Clip events trigger them: `voice: true` or `sound: "id@version"`.
12. Every critic note cites the feedback note paths it is grounded in.
13. Termination authority = a `-troupeTokenStoploss` flag with atomic
    reservation, plus a hardcoded global call max and depth cap. The cooldown is
    also hardcoded.
14. Every asset file is a uniform envelope (`kind`/`id`/`version`/`status`/
    `author`/`provenance`) with kind-specific content nested under `spec`. The
    filename is the only authority for `id@version`; the envelope must match it
    or the file is rejected. There is no `deps` map — inline references in
    `spec` are the complete dependency graph.
15. A play is a resolved play persisted under `plays/story_<UTC>.json` (no
    `@version`); `plays/index.json` lists metadata objects newest-first, and the
    play API returns the most recent one. Old plays are retained, never
    overwritten.
16. Motion is split across two frames: clips animate bones in the model's own
    frame (relative to root); a root-bone `tween` in the play's timeline moves
    the whole instance across the stage. A walk is a clip (legs) + a tween
    (root) at the same `at`.
17. A play timeline entry names the instance (`on`) and carries exactly one verb
    — `clip`, `gag` or `tween` — so entries sharing the same `at` run
    concurrently.
18. The resolved play — the single served artifact — is the play (references
    intact) plus a flattened, transitively-closed asset table keyed by kind and
    `id@version`. The resolver does only the cross-file `id@version` closure;
    intra-spec ids and selectors resolve in the engine at render time.
19. The troupe lives in one package, `internal/agents/troupe`, with exactly two
    flags: `-troupeModel` and `-troupeTokenStoploss`.
20. The play API is under `/api/v1/troupe`: `GET /play` (paginated, keyset
    cursor, `limit`/`order`/`status`/`author` filters), `GET /play/resolved`,
    `GET /play/{id}`, and `POST /feedback`.
21. Feedback is one directory (`feedback/`), one file per note named
    `<playId>_<type>_<utc>.json`, with a uniform `playId`/`type`/`ts`/`data`
    body. The old `feedback.jsonl` is retired.
22. The old theatre is deprecated first (Phase 0): `internal/agents/theatre`, the
    composer, `model.Story`, `intro.js` and the `/intro/*` endpoints are removed
    before any troupe code lands.
23. **clai ≥ v1.10.23-rc1 removes `agent.WithOutputTo`; the classifier migrates to
    the slog channel** (session 2026-08-23). The race-safe clai upgrade (commit
    951cf8a) replaced the io.Writer terminal output with `WithLogger` — the
    classifier's per-worker log files become text-handler sinks. This is a
    pre-existing breakage (master did not build after the bump) fixed as a
    precondition of Phase 0's green-build acceptance; it is not a troupe design
    decision.

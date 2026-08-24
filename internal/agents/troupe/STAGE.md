# STAGE — The Frozen Grammar

This is the only human-owned surface. Everything the troupe authors is an
instance of one of the types below, and nothing else exists. The Go validator
(`internal/agents/troupe`) is the authority; this note is its readable mirror.
When they drift, the validator wins. The engine interprets these types; the
validator and the engine are human-owned, never agent-authored.

## The envelope

Every file is one uniform envelope, with the kind-specific content nested
under `spec`:

```json
{
  "kind": "model",
  "id": "cat",
  "version": 1,
  "status": "draft",
  "author": "sculptor",
  "provenance": "commission: cat@1 legs read stiff",
  "spec": { "…" }
}
```

`kind` is one of `model`, `clip`, `voice`, `sound`, `gag`, `play`. `status`
is one of `draft` (the normal state while the swarm works) and `submitted`
(set by `submit_play` on a play; assets stay `draft`). `author` and
`provenance` are required — the file carries its audit trail. Plays are the
exception: no `version`, a `story_<UTC>` id (see Identity). The envelope is
closed: an unknown field is an error, because the grammar is frozen.

## Identity is the filename

The filename is the only authority for `id@version`; the envelope must match
it exactly or the file is rejected. Layout: one file per asset, named by id
and version — except plays, which are unversioned:

```text
models/    cat@1.json, night@1.json, yarn@1.json, …
clips/     walk@1.json, pounce@1.json, wag@1.json, …
voices/    cat@1.json, dog@1.json, …
sounds/    rustle@1.json, engine@1.json, …
gags/      doubletake@1.json, …
plays/     story_20260820T161500Z.json, …
```

Versioned asset ids and role ids share one charset and become filenames:
`^[a-z0-9_-]{1,64}$` — lowercase alnum, `_`, `-`; no `/`, no spaces, no
uppercase. Play ids are the exception: `story_YYYYMMDDTHHMMSSZ`
(`^story_[0-9]{8}T[0-9]{6}Z$`), a lexicographically sortable UTC datetime.
Intra-spec ids — bones, attachments, slots, skins, play instances — never
become filenames, so they allow camelCase (`frontLeg`).

References are `id@version` strings, typed by the field that carries them:
`model` resolves into `models/`, `voice` into `voices/`, `clip` into
`clips/`, `gag` into `gags/` — `id@version` is not globally unique across
directories, only within one. There is no `deps` map: inline references in
`spec` are the complete dependency graph.

## Composition boundaries

A `clip` references bones only (never a clip). A `gag` references clips only
(never a gag). A `play` references models, clips and gags. The validator
rejects a clip referencing a clip and a gag referencing a gag.

## The atoms

### bone

A transform node with an id, a parent, x/y/rotation/scale/length. Bones form
a tree — exactly one root with `"parent": null`; every other bone names its
parent; no cycles. A bone is both a joint (where it rotates) and a segment
(its far end is where children attach). `length` is non-negative; `scale` is
optional and defaults to 1.

```json
{ "id": "spine", "parent": "root", "x": 0, "y": 1, "rot": 0, "length": 3 }
```

### attachment + shape

A flat shape bound to exactly one bone (rigid skinning: the shape moves as
the bone moves), with a local transform and a flat colour. Three shape types:
`rect` (w/h, optional corner `radius`), `ellipse` (w/h), `path` (at least 3
`[x, y]` vertices). Colour is `#rrggbb`. Flat colour only — no lighting, no
gradients.

```json
{ "id": "blade", "slot": "main", "bone": "root",
  "shape": { "type": "ellipse", "w": 1, "h": 3, "color": "#3a7d44" } }
```

### skin

A named set of alternative attachments per slot (a coat is a skin). The
`default` skin is required whenever the model declares attachments, and it
must map every attachment (slot → attachment id, matching the attachment's
own `slot`). Other skins add alternatives. A model with no attachments has
nothing to skin.

```json
"skins": { "default": { "main": "body" }, "winter": { "main": "coat" } }
```

### model

A skeleton (bones) plus slots/attachments (art), an optional `voice` and
`sound` pair, and optional structural verbs. Characters, props, pieces and
backdrops are all models, differing only in scale, stage position and the
role a play assigns. A model needs at least one bone or one structural verb.

```json
"spec": {
  "bones": [ { "id": "root", "parent": null, "x": 0, "y": 0, "rot": 0, "length": 0 } ],
  "attachments": [ … ],
  "skins": { "default": { "main": "body" } },
  "voice": "cat@1",
  "structure": [ … ]
}
```

## Motion

### constraint

The primary authoring surface — four verbs, solved by closed-form 2-bone IK
in the engine. Reach/look/plant target a local coordinate `{x, y}`; track
targets a bone id. Each constraint carries exactly its own fields.

- `reach` — the effector bone's far end reaches a coordinate:
  `{ "type": "reach", "effector": "frontLeg", "target": { "x": 1.5, "y": 0 }, "hint": "front" }`
  with `hint` ∈ `front`/`back`/`left`/`right` (the pole direction).
- `look` — the chain faces a coordinate:
  `{ "type": "look", "chain": "spine", "target": { "x": 1.5, "y": 0 } }`
- `plant` — the bone stays at a coordinate while the rest moves:
  `{ "type": "plant", "bone": "frontLeg", "at": { "x": 0, "y": 0 } }`
- `track` — the chain continuously follows another bone:
  `{ "type": "track", "chain": "spine", "target": "frontLeg" }`

Bone targets accept a bone id or a selector (below).

### keyframe

The FK escape hatch: a channel keyframed over time on a bone or a slot.
Channels: `x`, `y`, `rotation`, `scaleX`, `scaleY`, `opacity`. `x`/`y` are
bone-only — a slot is rigidly skinned and never slides on its bone; a slot
keyframes `rotation`/`scaleX`/`scaleY`/`opacity`. Keys are `{t, v}` pairs
with strictly ascending, non-negative times in milliseconds; at least 2 keys.
The easing enum is declared once here and shared by tween: `linear`,
`ease-in`, `ease-out`, `ease-in-out`.

```json
{ "bone": "spine", "channel": "rotation", "easing": "ease-in-out",
  "keys": [ { "t": 0, "v": 0 }, { "t": 200, "v": 20 }, { "t": 500, "v": 0 } ] }
```

### oscillation

A periodic channel — sine with amplitude/frequency/phase — for a tail wag or
a body bob that is neither a target nor a keyframe. Bones only. Amplitude
non-negative, frequency positive.

```json
{ "bone": "frontLeg", "channel": "rotation", "amp": 30, "freq": 2, "phase": 0 }
```

### clip

A set of constraints/keyframes/oscillations + a positive `duration` (ms) + a
`loop` flag + optional events. A clip animates bones in the model's own frame
— relative to the model's root — so `walk@1` only swings the legs, never
moves the character across the stage. Crossing the stage is a play `tween`'s
job. An event is exactly one of `{ "at": 400, "voice": true }` (vocalize with
the instance's resolved voice) or `{ "at": 500, "sound": "rustle@1" }` (play
a named sound effect).

### voice

The formant parameters, mapping 1:1 to the engine's formant synth. Ranges:
`amp`/`pure`/`noise`/`decay` ∈ 0–1, `f0`/`q`/`dur`/`gap` positive, `bursts`
a positive integer pair. Fixed-length arrays: `dur`/`f0`/`amp`/`bursts`/`gap`
are `[lo, hi]` pairs, `kf` and `mouth` are 4-point paths, `tracks` is 3
formant tracks of 4 points, `gains`/`q`/`pitch`/`vib`/`burstPitch` are
3-value curves. `kf` fractions ascend within [0, 1]; `burstPitch` is
optional.

### sound

An environmental (non-vocal) sound effect: `type` ∈ `noise`/`tone`/`sweep`/
`burst`, with `freq`/`dur`/`amp` `[lo, hi]` ranges (positive; amp within
0–1) and an `env` of non-negative `attack`/`decay`. Characters vocalize with
`voice`; trees, cars and backdrops use `sound`.

```json
{ "type": "noise", "freq": [800, 2400], "dur": [0.3, 0.9],
  "amp": [0.2, 0.5], "env": { "attack": 0.05, "decay": 0.4 } }
```

## Structural verbs

Procedural generation lives in a model's `structure`, a list of exactly one
of these verbs. Every `scatter`/`recurse` carries a `seed` or inherits the
play-level seed. Instancers derive a per-instance seed from `(parent seed,
instance index)`, so N instances are N unique but reproducible, never N
clones.

- `attach` — instance a sub-model at a bone's end, with a relative `scale`
  and an optional `rot` offset. The child's +Y aligns with the parent bone's
  direction. Singular by design; N>1 placement is scatter's job.
  `{ "type": "attach", "model": "branch@1", "at": "trunk", "scale": 1 }`
- `scatter` — place N instances of a model over a region with seeded jitter:
  `{ "type": "scatter", "model": "tree@1", "count": 20,
     "over": { "type": "band", "w": 20, "h": 4 },
     "jitter": { "scale": 0.3, "rot": 8 }, "seed": 7 }`
  `count` is positive; `jitter.scale`/`jitter.rot` are non-negative spreads.
- `recurse` — self-similar nesting, an L-system rule; self-reference is
  allowed, bounded by `depth`:
  `{ "type": "recurse", "model": "branch@1", "at": "stem",
     "depth": 3, "branch": 2, "angle": 30, "decay": 0.7, "tip": "leaf@1", "seed": 2 }`
  `depth` and `branch` are positive integers, `decay` ∈ (0, 1] compounds
  scale over depth, `tip` names the terminal model.

Each verb carries exactly its own fields — attach does not accept `depth`,
scatter does not accept `at`.

### Regions

The closed `over` vocabulary for `scatter`: `band` (a w×h rectangle), `disc`
(radius `r`), `grid` (cols×rows cells of size `cell`), `curve` (a polyline
through at least 2 `[x, y]` points), `along` (a bone of the containing model
— "leaves along a stem").

## Selectors

Clips and constraints may target generated content by pattern in addition to
exact id, in exactly two forms:

- a wildcard path: `tree#*/branch#*/tip` — segments joined by `/`, each
  segment an instance `id#*` (any) or `id#<index>` (one), with a plain id
  allowed in the final position to name a bone inside the instance.
- all instances of a model: `model:leaf@1`.

Procedural content is animatable only through selectors. The engine resolves
them at render time; the resolver never touches them.

## Composition

### gag

A named sequence of clips — an ordered list of clip `id@version`s played
sequentially by their own durations. Never another gag, no stage, no
instantiations.

```json
"spec": { "clips": [ "blink@1", "pounce@1" ] }
```

### tween

A root-bone stage motion on one play instance: move the instance's root from
its current pose to a target over `over` milliseconds with an `easing`
(linear/ease-in/ease-out/ease-in-out). The target is exactly one of:
absolute coordinates (any subset of `x`/`y`/`rot`/`scale`), a `beside`
reference to another instance (`side` ∈ left/right/front/back), or an `off`
exit side (left/right). `beside` and `off` resolve in the engine at render
time; absolute coordinates are literal. A tween moves the whole instance —
it is how a character crosses the stage or meets another.

```json
{ "to": { "x": 0.5 }, "over": 3000, "easing": "ease-in-out" }
{ "to": { "beside": "dog", "side": "left" }, "over": 900, "easing": "ease-out" }
```

### play

Model instantiations + a timed timeline. An instance carries an `id`, the
`model`, a `role`, a positive `scale`, a stage position `x`/`y` and optional
`voice`/`sound` overrides. The `role` is an id; roles are notes the director
authors (see the role note format). Every timeline entry names the instance
(`on`) and carries exactly one verb: `clip` (trigger a clip), `gag` (trigger
a gag) or `tween` (move the root). Entries sharing the same `at` run
concurrently, so a walk is a `clip` (legs) plus a `tween` (root) at the same
`at`. All times are milliseconds. The optional `seed` seeds every structural
verb that does not carry its own.

## Roles are notes

A role is a note, not Go code. The director authors a role by writing a file
in `roles/`; the stage reads and executes it by name through the spawn
runner. The role note is a flat envelope — not a grammar asset, and never
resolved into a play:

```json
{ "id": "clown",
  "prompt": "You are the clown. You decide: … You stop: …",
  "tools": ["cat", "write_file", "spawn_role"],
  "budget": 8 }
```

`id` shares the asset id charset (`^[a-z0-9_-]{1,64}$`) and must match the
filename (`roles/clown.json`). `prompt` is required and length-capped. `tools`
selects from the closed tool registry and nothing else — a role can select,
never define, and a note naming anything outside the registry is refused.
`budget` is the per-spawn tool-call cap, clamped (absent → 8, capped at 64).

### The closed tool registry

- filesystem read/write — `cat`, `rows_between`, `ls`, `rg`, `write_file`,
  `apply_patch`, `mkdir`
- slivingdoc — `mcp_slivingdoc_notes_pull`, `mcp_slivingdoc_notes_commit`
  (live only when the notebook is on; selecting them without the notebook is
  refused at spawn)
- `spawn_role` — the recursive runner: a role that selects it may spawn
  sub-agents, each running through the same runner with its own note's tools
  and budget. The director's own spawns run through this same runner — there
  is no privileged execution path.
- `submit_play` — **director-only**: excluded from the general registry, so
  no role note can ever select it; phase 7 grants it to the director's fixed
  tool set. It is the single-writer persistence boundary: it validates and
  resolves the named play from the worktree, stamps it submitted and
  atomically persists the resolved play under `plays/story_<UTC>.json`,
  appending a metadata entry to `plays/index.json` (newest-first, the plain
  lexicographic sort of the datetime ids). Exact resolver errors return to
  the director; a second submit of the same play id is refused; a persist
  failure aborts — the paper trail never claims a success the disk did not
  record. Old plays are kept, never overwritten.

### The director and the swarm

The director is a fixed, sovereign stage role in Go — the mastermind, not a
worker. One generation is one `Director.Run`: it reads viewer feedback in
`feedback/`, decomposes the work into smaller concerns, authors or reuses role
notes, spawns a swarm of sub-agents by role note through `spawn_role`, reads
what the swarm left, assembles the play with references intact
(`"model": "cat@1"`) and submits it through `submit_play`. The director is the
single writer of `plays/`: the swarm writes everything else concurrently, and
exhaustion without a submit ships nothing — there is no seed and no offline
floor; an empty stage is the signal to investigate.

The director runs under the same termination authority as every spawn: its
run is depth 0, it admits against the generation budget before the loop
starts, and its agent accounts every budgeted model call into the same
budget. Its tool set is the closed registry (file tools, notebook tools when
the notebook is on, `spawn_role` bound to the swarm) plus `submit_play`; the
prompt names the submit step and carries the current UTC time, because the
play id is a `story_<UTC>` datetime and the registry carries no clock tool.

The facade (`Prepare`/`Warm`) guards generations: `Prepare` triggers at most
one (hardcoded cooldown + single-flight), `Warm` materialises the notebook
without authoring anything, and the served play is always the newest
submitted play read from disk — never an in-memory story.

### The critic

The critic is the second fixed stage role, the reflective pole: after a
generation it reads viewer feedback in `feedback/`, the submitted play in
`plays/` and the notes the swarm left, and emits evidence-cited, spicy
comments for the next generation's director. It has opinions, never
authority: it never drives (there is no review gate the director must pass),
never vetoes, never edits — its only write path is one criticism note
appended to `feedback/` through the `write_criticism` gate:

- **`write_criticism`** — **critic-only**: excluded from the general
  registry, so no role note can ever select it; phase 8 grants it to the
  critic's fixed tool set. It appends one evidence-cited criticism note to
  `feedback/`: `generation` (the generation id), `cites` (real notebook note
  paths the criticism is grounded in — `feedback/` notes first; a cite
  naming a missing file, a path escaping the notebook or the `plays/`
  bookkeeping index is refused), and `body` (the advisory text). The play id
  is pinned by the stage from the generation's outcome — the critic
  structurally cannot claim a play that was not submitted — and the note's
  `ts` is stamped server-side, with the filename derived from it
  (`<playId>_criticism_<utc>.json`, or `criticism_<utc>.json` when nothing
  shipped). A note is append-only: an existing filename is refused, never
  overwritten.

The critic's tool set is the read-only registry file tools (`cat`,
`rows_between`, `ls`, `rg`) plus `write_criticism` — no `write_file`, no
`apply_patch`, no `mkdir`, no `spawn_role`, no `submit_play` — so the
"never edits" rule is enforced by the tool set, not by the prompt. It runs
after the generation, never inside it, and it is not part of the generation
budget: an exhausted generation (stoploss or call max spent) is still
reviewed, and the empty-stage review says why nothing shipped, citing
whatever notes exist — never fabricating a play.

### The termination authority

The stage bounds one generation regardless of swarm size or spawn depth.
Three guards, of which only one is operator-tunable — the token stoploss
(the `-troupeTokenStoploss` flag); the global call max and the depth cap are
hardcoded constants, defense-in-depth rather than operator surface. Every
spawn admits against the same generation budget under one lock, so
concurrent spawns cannot both pass a threshold, and every spawned agent
accounts its model calls into it.

- **Token stoploss** — once the generation's cumulative token usage crosses
  the stoploss, `spawn_role` refuses new spawns. A spawn that would cross
  the threshold reserves the remaining allowance — or is refused
  atomically. The final answer is never budgeted: a model step that ends
  with a reply is not a budgeted call, so telemetry never shows an actor
  over its cap.
- **Global maximum** — the hard cap on one generation's budgeted model
  calls. Hardcoded; the counted calls never exceed it.
- **Depth cap** — a spawn past the cap is refused, never spawned. The
  director's own run is depth 0; every `spawn_role` recursion adds one.

A refused spawn returns the exact guard error to the spawning agent, which
must stop spawning and finish its own work.

## Reference precedence

An instance's `voice`/`sound` overrides the model's `spec.voice`/
`spec.sound`; both are optional and singular. `voice: true` in a clip event
uses the instance's resolved voice (override → model default → none).

## Determinism

Every `scatter`/`recurse` carries a seed (or inherits the play-level seed).
Instances derive deterministic path ids (`forest/tree#3/branch#1/leaf#2`) and
scale is multiplicative through the hierarchy, so `decay` compounds over
depth. The engine expands; the resolver only sanity-checks the declared
arithmetic against a configurable expansion budget.

### The expansion budget

The resolver's budget is an operational guardrail, never a design constraint:
it checks the declared numbers, it does not expand. The defaults are a
per-verb scatter `count` cap of 500, per-verb recurse `depth`/`branch` caps of
6/8, and a 10 000-node total cap on the sum of every structural verb's
declared size across the play's closure (`attach` counts 1, `scatter` its
`count`, `recurse` its closed-form geometric sum). Agents never author against
it — a refused play is a signal to simplify the composition.

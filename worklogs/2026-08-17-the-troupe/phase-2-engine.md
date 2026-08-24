# Phase 2 — Engine

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

A standalone `engine.js` (ES5, no canvas) that self-mounts into
`<div id="troupe">` and renders a resolved play. It is the fixed, human-owned
interpreter of the grammar: a 2D skeletal keyframe renderer with rigid skinning,
a closed-form 2-bone IK solver, and a deterministic expander. The input is the
Phase 1 resolved-play format; it is tested against fixture resolved plays, not
the Phase 3 resolver.

## Shape (as built)

```text
cmd/serve/frontend/engine.js              # the runtime (ES5 IIFE, window.TroupeEngine.mount)
cmd/serve/frontend_test/engine.test.js    # headless Node harness (DOM stub + recording
                                          #   AudioContext + explicit clock)
cmd/serve/frontend_test/lab.smoke.js      # boots lab/ in a stubbed browser and drives frames
lab/index.html, lab/lab.js, lab/README.md # dev simulator (play/pause, scrub, fixture select)
lab/fixtures/story_20260820T161500Z.resolved.json   # hand-resolved conformance play
lab/fixtures/garden_20260821T090000Z.resolved.json  # hand-resolved procedural play
```

No `package.json`, no framework, no linter. The floor is webOS TV 4.x
(Chromium 53, ES5), so the engine animates only `transform`/`opacity` on CSS
divs — a weak TV cannot afford canvas repaints.

## Subsystems (as built)

- **Bone-div tree** — each bone is a div child of its parent bone's div; the rig
  is the DOM nesting. A bone's joint sits at the parent's far end plus its own
  `(x, y)` offset (D-13); rigid skinning binds each attachment div to exactly
  one bone div. Flat colour only — rect (rounded), ellipse, path.
- **Closed-form 2-bone IK** — `reach`/`look`/`plant`/`track` are solved
  analytically. `reach` solves the effector-plus-parent chain from the
  shoulder/target geometry in one closed form (no iteration); `look`/`track`
  rotate the chain root to face a coordinate/bone; `plant` solves the parent's
  rotation so the bone's joint stays on the coordinate.
- **FK keyframes + oscillation** — the `keyframe` escape hatch (x/y/rotation/
  scaleX/scaleY/opacity on a bone, opacity/rotation/scale on a slot, shared
  easing enum) and the periodic `oscillation` channel (sine with
  amplitude/frequency/phase).
- **Formant voices + sound effects** — the formant synth rehomed from the old
  `intro.js` `VOICES` maps a voice spec 1:1 (sawtooth + pure sine + vibrato +
  bandpass formants + noise breath); `sound` implements the four closed types
  (`noise`/`tone`/`sweep`/`burst`) over Web Audio. All random draws flow from
  the play PRNG, so the audio schedule is deterministic.
- **Root-bone stage tweens** — the `tween` verb moves an instance's root from
  its pose at the tween's start: absolute coordinates, `beside` (resolved at
  tween start against the other instance's pose, D-19), or `off` (exit side).
- **Clip/gag/play sequencing** — the play timeline drives instances; entries
  sharing the same `at` run concurrently and apply in authored order
  (later-started clips win conflicts, D-20). A gag plays its clips sequentially
  by their own durations; the gag advances before per-frame evaluation so the
  clip current at `now` is the one that runs.
- **Deterministic expander** — `attach`/`scatter`/`recurse` are expanded from
  seeds at mount time. mulberry32 PRNG; per-instance seeds derive from
  `(parent seed, instance index)` (D-15); `scatter` jitter and `recurse`
  `depth`/`branch`/`angle`/`decay` are honoured; generated nodes get
  deterministic path ids (D-16); scale compounds through the DOM nesting.
- **Selectors** — clips and constraints target generated content by wildcard
  path or `model:<id>@<version>` in addition to exact id (D-17); slots are
  instance-local. The engine resolves intra-spec ids at render time — that is
  not the resolver's cross-file closure.

## Implementation notes (as executed)

1. Two frames of motion, never one: clips animate bones in the model's own
   frame; crossing the stage is the play's job via root `tween`s. The engine
   keeps them separate (`evalClip` vs `applyTween`).
2. Reference resolution inside the engine is a pure map lookup:
   `"model": "cat@1"` → `table.models["cat@1"]` (per kind), built from the
   resolved play's asset tables.
3. Every frame starts from the rest pose across the **whole expansion**
   (generated nodes included) and the active clips re-apply, so constraints
   with `+=` (`look`/`track`) never compound across frames.
4. The engine may optimise expansion later; it currently expands eagerly at
   mount. The expansion budget from Phase 3 is an operational guardrail, not a
   design constraint the engine relies on.
5. `step` is monotonic (backward scrubs need a remount); the lab remounts on a
   backward seek.
6. `lab/` is dev-only: a simulator with play/pause, a scrub slider and a
   fixture select, driving `TroupeEngine.mount(el, play, {auto:false, clock})`.
   Nothing in `lab/` ships into the production frontend.

## Tests

The engine tests live in `cmd/serve/frontend_test/engine.test.js` (plain Node,
no framework) and drive the engine under a DOM stub with real style objects, a
recording AudioContext and an explicit clock. The DOM stub moves nodes on
`appendChild` exactly like a real DOM, so re-parenting a bone under its
parent's div never leaves a copy behind.

- **Rig**: the bone-div tree nests (spine under root, legs under spine), the
  body attachment binds to exactly one bone, and the rig reproduces the
  authored skeleton.
- **IK**: a `reach` goal lands the effector's far end exactly on the target
  (closed-form, no iteration); `look` faces a coordinate; `plant` pins a
  joint; `track` follows an animated bone.
- **Keyframes + oscillation**: walk@1 swings the legs at the authored sine
  phase and wraps its loop; slot keyframes hit their channel values.
- **Expander**: `scatter` produces N distinct-but-reproducible instances;
  `recurse` honours `depth`/`branch`/`decay` and terminates; deterministic
  path ids match the spec; `along` places instances along a bone; the leaf
  scale compounds decay through the DOM nesting (measured relative to its
  tree, removing the scatter jitter).
- **Selectors**: `model:leaf@1` and `tree#*` animate generated content.
- **Sequencing**: concurrent same-`at` entries (walk + tween), gag ordering
  (blink → pounce), `beside` and `off` tween resolution, walk resumes after
  the gag.
- **Audio**: the voice event schedules its sawtooth formant source at the
  authored f0 range (pitch curve divided out) and the sound event schedules
  its noise buffer; two runs produce identical audio summaries.
- **Determinism**: the whole conformance play serializes identically across
  mounts; `destroy` removes the stage.
- **Lab smoke** (`lab.smoke.js`): boots `lab/` in a stubbed browser, loads a
  fixture, mounts, advances the clock through the rAF loop and pauses.

## Acceptance

- [x] A fixture resolved play renders inside `<div id="troupe">` with no canvas.
- [x] The conformance example's `forest`/`cat`/`walk`/`pounce`/`doubletake` play
      reproduces deterministically across runs.
- [x] The engine consumes only the Phase 1 resolved-play format.
- [x] `node cmd/serve/frontend_test/engine.test.js` → 14/14 pass.
- [x] `node cmd/serve/frontend_test/lab.smoke.js` → boots and advances the clock.
- [x] `go build ./...` stays green (nothing imports the engine yet; wiring is
      phase 9).

## Decision log (session 2026-08-23, clai worker)

Decisions D-13 … D-21 below pin the engine's observable behaviour; they were
implemented in this phase and are enforced by the harness tests. D-22 and D-23
close two timeline semantics the handover left open.

- **D-13 — Bone placement.** A bone's joint = parent's far end + (x, y) in the
  parent's frame; a bone with x:0,y:0 sits exactly at the parent's far end.
  Model frame is CSS-natural (+Y down, clockwise-positive rotation).
- **D-14 — Stage frame.** Also CSS-natural: instance x/y are fractions of the
  stage width/height from the top-left; one model unit = stageH/10 px
  (opts override).
- **D-15 — Determinism.** mulberry32; per-instance seed = childSeed(parent,
  index); the play seed defaults to 0; audio draws share the play PRNG.
- **D-16 — Path ids.** Every generated instance carries an index suffix `#<i>`
  (attach → #0); paths join ancestors: `forest/tree#3/branch#1/leaf#2`.
- **D-17 — Selector matching.** `model:<id>@<v>` matches all nodes incl. the
  play instance root; path selectors match the generated-path suffix exactly
  (segment count included); a plain final segment names a bone; `#*` matches
  any index. Slots are instance-local (no selector form).
- **D-18 — Constraint frames.** Constraints solve within one skeleton context;
  targets are in that context's frame (play instance root frame, or the
  generated node's frame).
- **D-19 — Tween targets.** `beside` offset is 0.15 stage units (left ∓x,
  right +x, front +y, back −y), resolved at tween start; `off` left → x=−0.5,
  right → x=1.5.
- **D-20 — Channel precedence.** Clips apply in activation order; later-started
  clips win conflicts; within a clip constraints override keyframes.
- **D-21 — Sound synth.** noise = bandpass-swept white noise; tone = sine at a
  seeded pick; sweep = linear freq ramp; burst = short sharp sine.
- **D-22 — The timeline is an absolute clock.** A timeline entry starts at its
  authored `at`, never at the first step that happens to land later. Sparse
  steps (a lab scrub, a test jumping to t=1500) therefore evaluate clips at
  their correct local time instead of shifting everything by the first step.
- **D-23 — A tween starts from the current pose.** The `from` of a tween is
  the instance's pose at the tween's start (the running tween evaluated
  there); a finished tween keeps its target. `beside` resolves against the
  other instance's pose at the tween's start, so a second tween continues the
  first one's motion instead of snapping back to the authored position.

## Session report (2026-08-23)

Picked up the phase from the handover state (3/14 harness tests passing) and
finished it: four engine bugs fixed, the harness corrected, the lab built.

**Engine fixes (all in `cmd/serve/frontend/engine.js`):**

- `solveReach` mixed units: `Math.acos` returns radians but `phi` was degrees,
  and `thetaBC` added `Math.PI` as if it were 180° — the sum was
  `phi + side·alpha + 3.14 + side·beta`. All terms are now degrees
  (`180` not `Math.PI`), verified analytically: A=(0,0), a=3, b=2, T=(2,4) →
  parRot −5.266°, effRot 305.69°, far end lands on (2,4) to 1e-15.
- `attach` passed `null` as the child's path (the computed path landed in the
  seed slot) — every attach-created node had `path: null`, which crashed
  `matchesPath` (`.split` on null) the moment a selector walked the expansion
  and broke the recurse tests' path queries.
- `fireTimeline` started entries at the step's `now` instead of the entry's
  `at` (D-22) — a first step at t=1500 shifted the whole play 1500 ms.
- Tweens started from the authored position instead of the current pose
  (D-23), and `beside` read the other instance's authored position; a second
  tween snapped the cat back to x=0.1 before walking to the dog.
- The gag advanced **after** clips evaluated, so the clip current at `now` was
  always one frame late (the pounce was never evaluated on the step that
  started it).
- Channels were reset only on the instance node, not the generated expansion —
  `look`/`track` (`+=`) on selector-targeted bones would have compounded every
  frame. `resetAllChannels` now walks the whole expansion.
- Transform strings wrote `scale(s)` while every other write used
  `scale(x,y)` — the harness's `parseTransform` regex (and the CSS itself,
  which needs two values for non-uniform scale) rejected them.

**Harness corrections (`cmd/serve/frontend_test/engine.test.js`):**

- DOM stub `appendChild` moves the node (a real DOM never leaves a copy
  behind) — the duplicate bone/tree counts came from the stub, not the engine.
- `along` test passed `'leaf@1'` as the instance's model ref (no structure,
  scatter never ran) — the instance stays `a@1` and `leaf@1` is supplied as
  the scattered model.
- The legs query now excludes the queried spine element (`findAll` includes
  it); node-count queries use a `nodeEls` helper that skips bone/attachment
  divs echoing their node's path.
- The recurse leaf-scale assertion measures the compounded scale relative to
  its tree (the ratio removes the tree's own scatter-jitter scale).
- The keyframes test stepped backward (600 < 1500) — steps are monotonic, so
  the walk check uses 1800 (1800 % 1200 = 600).
- The voice f0 assertion divides the pitch curve's first coefficient out; the
  first scheduled frequency is `f0·pitch[0]`.
- The final walk-resumed assertion uses local 200 (5000 % 1200), a meaningful
  phase, instead of local 0 at 4800.

**Lab (`lab/index.html`, `lab/lab.js`, `lab/README.md`):** dev simulator with
fixture select, play/pause, scrub (backward seeks remount — the engine's step
is monotonic), a live time readout and per-mount AudioContext management.
`lab.smoke.js` boots it in a stubbed browser and drives frames.

Verification (exact commands and results):

```bash
# Baseline (handover state):
node cmd/serve/frontend_test/engine.test.js      # 3/14 passed

# After fixes:
node cmd/serve/frontend_test/engine.test.js      # 14/14 passed, exit 0
node cmd/serve/frontend_test/lab.smoke.js        # ok: stage rendered,
                                                 #   clock advanced, max 4660 ms
node --check cmd/serve/frontend/engine.js        # clean
node --check cmd/serve/frontend_test/engine.test.js   # clean
node --check cmd/serve/frontend_test/lab.smoke.js     # clean
node --check lab/lab.js                          # clean

go build ./...                                   # green (engine not wired yet)
go test ./internal/agents/troupe -race -count=3 -timeout=30s   # ok
go test ./... -race -cover -count=3 -timeout=30s               # ok, all packages
go vet ./...                                     # clean
go run mvdan.cc/gofumpt@latest -l .              # no diffs
```

## Notes for later phases

- Phase 3 produces real resolved plays; until then the engine tests against
  the hand-resolved fixtures in `lab/fixtures/`.
- Phase 9 embeds `engine.js` into the production frontend and serves
  `/api/v1/troupe/play/resolved` as its input. The engine's self-mount
  bootstrap renders `window.TROUPE_PLAY` into `#troupe` when both exist; the
  production frontend can either set that global or call
  `TroupeEngine.mount` directly.
- The lab's `data-path`/`data-bone`/`data-slot` attributes are the observability
  surface for debugging generated content in the browser's element panel.

# The Troupe — engine lab

A dev-only simulator for `cmd/serve/frontend/engine.js`. It is **not** served
in production — phase 9 embeds the engine into the production frontend and
feeds it `/api/v1/troupe/play/resolved`.

## What it does

Loads a fixture resolved play from `fixtures/` and drives
`TroupeEngine.mount(el, play, { auto: false, clock })` with an explicit clock:
play/pause, a scrub slider, and a live time readout. The DOM tree is
inspectable in the browser's element panel — each generated node carries
`data-path`, bones `data-bone`, and attachments `data-slot`.

## Running

Serve the repository root with any static server and open `/lab/`:

```bash
python3 -m http.server 8000
# open http://localhost:8000/lab/
```

No `package.json`, no framework, no build step. The lab loads the engine with
a plain `<script src="../cmd/serve/frontend/engine.js">`.

## Controls

| Control | Behaviour |
| ------- | --------- |
| Fixture select | Switches between the fixture resolved plays (conformance story, procedural garden) |
| Play / Pause | Runs the engine clock at wall-clock speed; stops at the play's estimated duration |
| Scrub | Seeks. The engine's `step` is monotonic, so a backward scrub remounts the play (audio reschedules from the new position) |

## Fixtures

`fixtures/story_20260820T161500Z.resolved.json` — the README conformance play
(`forest` + `cat` + `walk`/`pounce`/`doubletake`), hand-resolved in phase 2.

`fixtures/garden_20260821T090000Z.resolved.json` — a procedural play
(`scatter` trees, `recurse` branches, selector-driven `breeze` clip, `off`
tween), hand-resolved in phase 2.

Both are fixtures only — never a seed, never shipped into the notebook. Phase 3
produces real resolved plays; the engine tests against these hand-resolved
ones until then.

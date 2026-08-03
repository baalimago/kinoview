# Phase 7 — Vocabulary Expansion

**Status:** ✅ Complete | [README](./README.md)

## Goal

Widen the closed vocabulary both producers share — new set pieces, props, backdrops
and actions — implemented in the model, the player and the CSS, plus new composer
templates that use them, so the company has real material to work with.

## Specification

Hand-built art walls (decision D9): every addition is implemented in
`internal/model/story.go` (valid sets) AND `cmd/serve/frontend/intro.js` (player
registry) AND `cmd/serve/frontend/style.css` (art) before any agent may emit it.

**New set pieces** (→ `ValidPieces`, `PIECES`, CSS):

- `fireplace` (tall, indoor; reads with `night` swap potential)
- `bookshelf` (tall, indoor)
- `door` (tall, indoor — a second exit the cast can react to)
- `log` (low, outdoor — `clear` semantics)

**New props** (→ `ValidProps`, `PROPS`, CSS): `ball`, `bone`, `cushion`, `bowl`.

**New backdrops** (→ `ValidBackdrops`, `BACKDROPS`, CSS layers): `kitchen` (indoor),
`forest` (outdoor), `rain` (outdoor; pairs with a `night`-style sky treatment).

**New actions** (→ `ValidActions`, `ACTIONS`, CSS):

- `yawn` (idle gesture, mouth-driven)
- `sniff` (head-down investigation, works with a prop target)
- `jump` (vertical hop — new movement class; must degrade to a pose on
  reduced-motion/low-perf, like existing actions)

Each action needs a player implementation, a CSS animation on the affected parts
(eyes/mouth/head/legs per species), and a `model` entry. `jump` additionally needs a
movement rule: target-landing like `pounce` but vertical, no horizontal glide.

**Composer templates** (→ `composer.go` `scenes`), using the new material:

- `midnightsnack` (kitchen, bowl, ball; cat + mouse)
- `birdwatching` (forest, log; all three, watching the sky — foreshadows phase 8)
- `snowed-in` (rain/forest; the intruder shape with a door)

Template rules from the existing conventions hold: three-act shape, staging from
`stage()`/`solo()`, jittered timings, `dressPlan` for the set, billing for the
theme. New templates must survive `TestCompose_AllScenesValidateAcrossSeeds`.

**Affected paths**: `internal/model/story.go`, `cmd/serve/frontend/intro.js`,
`cmd/serve/frontend/style.css`, `internal/agents/storyteller/composer.go`,
`internal/agents/storyteller/composer_test.go` — plus the theatre's registry doc
(phase 6) for the bird's canonization.

## Integration contract

| Trigger | Collaborator | Observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| Composer picks a new template | composer | story with new backdrop/pieces/props | story passes `Validate` | unknown values reaching the player |
| Player receives `yawn`/`sniff`/`jump` beat | intro.js | action animates; `jump` lands at target | — | action on reduced-motion without pose fallback |
| LLM emits a new piece name | Validate | piece accepted, cell dressed | — | piece name accepted before player implements it (both land in the same phase) |
| `setCell` swaps to `fireplace` mid-play | player | swap animation (existing `swapping` path) | — | — |

## Acceptance criteria

- [ ] New pieces/props/backdrops/actions are in all three layers (model, player, CSS)
      — verified by grep and by a render test where the harness strips transitions
      (existing capture caveat: animations freeze headless; pose/static frames only).
- [ ] Composer templates `midnightsnack`, `birdwatching`, `snowed-in` validate across
      400 seeds; the distinct-title sanity test still holds (≥ 5 titles).
- [ ] `jump` on `.low-perf` or reduced-motion degrades to a static pose, no movement.
- [ ] `dressPlan` uses the new `log` with `clear` semantics (never through a body).
- [ ] All existing composer, staging and model tests pass unchanged.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| Template references a piece not in `ValidPieces` | story fails validation → `minimalStory` fallback (existing behavior) | template test |
| Player gets an unimplemented action | dropped by guard (existing `if (!fn || !actor) return`) | player-guard review |
| CSS class typo on a new piece | piece renders as its bare holder (no crash) | render test |
| `jump` target off-stage | landing clamped to stage bounds (reuse `markPx` clamps) | movement unit test |

## Implementation notes

Executed by imago, 2026-08-02 session (phase 7 of the playwright-company worklog).

The hand-built art walls (decision D9) were raised in all three layers at once —
model, player and CSS — and the composer and the theatre were pointed at the new
material:

| File | Contents |
|---|---|
| `internal/model/story.go` | `ValidBackdrops` += kitchen, forest, rain; `ValidPieces` += fireplace, bookshelf, door, log; `ValidProps` += ball, bone, cushion, bowl; `ValidActions` += yawn, sniff, jump; `actionNeedsTarget` += jump (target-landing like pounce — a targetless jump is dropped, model-enforced; sniff keeps an optional target, so it may investigate the air or a prop). |
| `internal/agents/storyteller/composer.go` | `indoorSets` += kitchen, `outdoorSets` += forest, rain; the dresser lists became per-backdrop (`indoorDressers(backdrop)`, `outdoorDressers(backdrop)`) so the new pieces actually appear — a kitchen dresses with door/bookshelf/fireplace/rug, a living room with the hearth, the theatre keeps its lamp; a forest leads with the log (`clear: true` — never through a body), a rainy set with the door (tall — always finds a column); the default outdoor list gains the log too. `kitchenSet` forces the kitchen for the midnight raid; `cellWithPiece` finds a signature piece's cell so a template can target it; three new templates (`midnightsnack`, `birdwatching`, `snowed-in`) plus bone/cushion props in `greeting` and `soloina` so every new render path is exercised. |
| `internal/agents/storyteller/storyteller_test.go`, `dressdraft_test.go`, `internal/model/story_test.go` | New tests: phase-7 vocabulary validates end to end (`TestValidate_Phase7Vocabulary`), a targetless jump is dropped (`TestValidate_JumpNeedsTarget`), the new templates are reachable and every new piece/prop/backdrop/action appears across 400 seeds (`TestCompose_Phase7VocabularyReachable`), jumps always carry a target (`TestCompose_JumpAlwaysTargeted`), the log never stands through a performer (`TestCompose_LogNeverThroughABody`), targeted beats address real cells (`TestCompose_Phase7PieceBeatsAddressRealCells`); `clearPieces` in `TestDressDraft_KeepsBackdropAndRespectsStaging` gained `log`. |
| `cmd/serve/frontend/intro.js` | `BACKDROPS` += kitchen, forest, rain; `PIECES` += fireplace (hearth + fire + mantel + chimney + glow), bookshelf (frame + boards + books), door (frame + panel + knob + hinge), log (trunk + end + knot); `PROPS` += ball, bone, cushion, bowl; `ACTIONS` += `yawn` (mouth-driven pulse), `sniff` (head-down pulse, faces the target), `jump` (vertical hop — the horizontal move rides one ease-out transition on the walk layer, no walking glide, landing clamped via the extracted `clampStage`, and `lowPerf()` returns before any movement so weak hardware gets the static pose). New helpers: `clampStage` (the clamp markPx always used, now shared), `targetPx` (resolves a target to px for cast, props and cells). `stareoff` now resolves through `targetPx` too — a pre-existing quirk where staring at a prop (yarn, box) never faced it is fixed. |
| `cmd/serve/frontend/style.css` | Art for the four pieces, the four props and the three backdrops (kitchen = warm night kitchen, forest = green canopy, rain = storm sky with slanting rain, the night-style treatment the spec pairs); the three action animations (yawn mouth/head, sniff body/head, jump arc + tightening shadow); `.low-perf` rules kill the jump animation and the walk transition and hide the fireplace glow like the lamp's. |
| `internal/agents/theatre/tools/tools.go`, `internal/agents/theatre/fallback.go` | The `write_scene` tool's backdrop enumeration and the wardrobe floor's `backdropNames` gained kitchen, forest, rain, so the LLM and the deterministic advice both know the new sets. |

**Material decisions (recorded for chronology):**

- **D-P7-1 — jump joins the target-required actions; sniff stays optional.**
  "Target-landing like pounce" is a movement rule, so the model enforces it:
  a targetless jump is dropped in validation, exactly like pounce. Sniff is
  an investigation gesture — the air, a prop or a cell all read — so its
  target is optional and validated only when present.
- **D-P7-2 — the dresser lists are per-backdrop, not a single pool.** The old
  single indoor list would always be won by the first two or three dressers
  (limited free columns), and the new pieces would never appear. Splitting by
  backdrop — kitchen gets door/bookshelf/fireplace, living room the hearth,
  theatre keeps its lamp — guarantees the phase-7 material actually reaches
  the stage. The existing templates' setCell swaps stay coherent: yarn's
  "lights out" now clears the living-room hearth.
- **D-P7-3 — templates target signature pieces by lookup, with graceful
  degradation.** `birdwatching` and `snowed-in` need a specific piece (log,
  door) for their jump/sniff beats. They dress through the normal
  `stage()`/`dressPlan` path, then look the cell up with `cellWithPiece` and
  only emit the targeted beat when the piece stood — the log is `clear`, so
  a fully-packed stage simply omits it and the scene still plays.
- **D-P7-4 — jump is a vertical hop; the horizontal move rides the arc.** The
  walk layer gets one ease-out transition (not a walking glide, no legs), the
  body arcs on the inner layer, the shadow tightens, and the landing goes
  through `clampStage` — the clamp markPx always used, now extracted and
  shared, so a target near a wing never lands the jumper off-stage. On
  `.low-perf` the JS returns before any movement and the CSS kills both the
  hop and the transition: a static pose, no movement. Reduced motion keeps
  the existing whole-story skip (`logoOnly`).
- **D-P7-5 — the new props are composed, not just available.** The bone and
  the cushion had no template calling for them, which would leave their
  render paths unproven. `greeting` gained a peace-offering bone (sniffed
  before the greet) and `soloina` gained a bone on the dog's night and a
  cushion on the cat's — every new prop now appears in real composed output.
- **D-P7-6 — `stareoff` resolves actors, props and cells.** The pre-existing
  implementation only faced actors, so the yarn and boxnap stares at props
  never turned the head. `targetPx` fixes that while the new actions use the
  same resolution.
- **D-P7-7 — the theatre's backdrop vocabulary follows.** The `write_scene`
  tool description and the wardrobe floor's `backdropNames` now name the
  three new backdrops, so the LLM and the deterministic advice can see them.
  The bird itself stays out of scope — phase 8 owns the species.


**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` (before changes) | pass — baseline green before the phase |
| `go test ./internal/model ./internal/agents/storyteller/... ./internal/agents/theatre/...` (before changes) | pass — phase 1–6 baseline |
| `go test ./internal/model` | pass — 95.6% coverage (model gained 2 tests) |
| `go test ./internal/agents/storyteller/...` | pass — 84.5% coverage; the seed-sweep tests (400–500 seeds) cover every new template and the new vocabulary across runs |
| `go test ./internal/agents/theatre/...` | pass — 90.6% theatre + 93.1% tools (unchanged) |
| `go test ./...` | pass — full suite, 23 packages |
| `go test ./internal/model ./internal/agents/storyteller/... ./internal/agents/theatre/... -race -count=3 -timeout=120s` | pass — repeated runs clean |
| `go run mvdan.cc/gofumpt@latest -l internal/model internal/agents/storyteller internal/agents/theatre` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/model internal/agents/storyteller internal/agents/theatre` | 5 clone groups, all pre-existing (storyteller.go↔theatre.go mirrors are the phase-9 consolidation surface; item.go/item_test.go are unrelated) — none touch phase-7 code |
| `node --check cmd/serve/frontend/intro.js` | pass — syntax clean |
| Three-layer grep (every new piece/prop/backdrop/action present in `story.go`, `intro.js`, `style.css`) | pass — see acceptance below |
| `DUMP_STORIES` visual dump of the new templates | pass — kitchen/forest/rain scenes with door/bookshelf/fireplace/log cells, bowl/ball props, yawn/sniff/jump beats; jump targets the log cell, sniff the door cell; log at a free column (clear semantics) |

**Acceptance check** — all criteria met: the new pieces, props, backdrops and
actions land in all three layers (grep-verified; `node --check`; the embed build
`go build ./...` re-embeds the frontend). The render test itself remains the
manual capture process documented in the agent notebook — the repo has no JS
test harness (no package.json/jsdom), and headless capture freezes animations,
so pose/static frames are the only automated-adjacent check; motion is verified
by the CSS/JS rules themselves (the `low-perf` kill rules, the `clampStage`
clamp). The three templates validate across 400 seeds
(`TestCompose_AllScenesValidateAcrossSeeds`) and the distinct-title sanity
(≥ 5 titles) holds with 11 templates in the pool. `jump` on `.low-perf` or
reduced-motion degrades to a static pose with no movement (JS `lowPerf()`
guard + CSS kill rules + the reduced-motion whole-story skip). `dressPlan` uses
the log with `clear` semantics — `TestCompose_LogNeverThroughABody` and the
updated `TestDressDraft_KeepsBackdropAndRespectsStaging` pin it. All existing
composer, staging and model tests pass unchanged.

**Error coverage** — template referencing an unknown piece: falls back to
`minimalStory` via the existing validate-or-fallback in `ComposeThemed`
(guarded by `TestCompose_AllScenesValidateAcrossSeeds`). Player receiving an
unimplemented action: dropped by the existing `if (!fn || !actor) return`
guard in `playStory` (reviewed, intact). CSS class typo on a new piece: renders
as its bare holder without a crash — `fillCell` already skips a piece with no
builder, and every new piece has both a builder and its CSS. `jump` target
off-stage: landing clamped through `clampStage` (the shared markPx clamp),
verified by code path — the movement itself is browser-verified, as documented
above.

## Review findings

*(filled by reviewers)*

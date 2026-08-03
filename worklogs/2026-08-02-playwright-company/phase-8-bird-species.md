# Phase 8 — The Bird

**Status:** ✅ Complete | [README](./README.md)

## Goal

Add the bird as a fourth species: model, player art, CSS, formant voice, coats,
composer scenes, and wardrobe-consultant coverage.

## Specification

**Model** — `ValidCharacters` gains `"bird": true`. Species-size `base` in the
player (like cat 1.00 / dog 1.02 / mouse 0.44): bird `~0.30` (smaller than the
mouse; it perches, so it never shares a body silhouette). Cast id for the permanent
bird is decided in the registry (phase 6) — proposal: `pip` (character `bird`),
canonical coat `chaffinch` — but the phase must not hardcode a name the registry
hasn't canonized; the composer uses the registry entry.

**Player art** (`CHARACTERS.bird`, `buildBird`) — div-built per webOS rules:
body, head, beak, eye, wing, tail, two legs; a perching pose (short legs, tail
down). No inner-SVG transforms. Movement classes: the bird does not walk; `enter`/
`exit` become short hops or a glide (translate, not leg-walk); `walkTo` degrades to
`hop` semantics (reuse `glide` with a hop bob).

**Voice** (`VOICES.bird`) — formant parameters per the existing research: small
body ⇒ high f0 and high formants. Proposal: f0 ~2000–2800 Hz (above the mouse's
1700–2500 for contrast), short dur, two-three bursts (a chirp pattern: 3 quick
notes with a rising middle note). Tunable in the same tuning pass that verified the
cat meow.

**Coats** — bird palette (3–4): e.g. `chaffinch`, `blue tit`, `robin`, `sparrow` —
each with `fur/furDark/belly/tailTip/innerEar/nose/eye` keys like existing coats.
The canonical coat comes from the registry.

**Composer scenes** — add a bird scene reusing phase 7 material:

- `birdwatching` (phase 7 template) already sets the shape; this phase adds the
  bird's participation in `standoff`/`stakeout` style lineups: the bird enters,
  perches (new perch position — higher `lane`/`y` treatment: bird gets a
  `perchY`/elevation so it reads above the ground line), vocalizes, and either
  teases the cat (a `bat`-like swat miss) or flies off.

Elevation: the bird needs a height offset the schema supports. `Cast` has `lane`
(0..2) — a lane-based bottom offset exists (11 + lane*8 %). Bird perching needs a
taller offset; decide in this phase between (a) reusing `lane` with a bird-specific
`bottom` curve in the player, or (b) adding a `height` field to `model.Cast`
(validated 0..0.6). Prefer (a) if it reads; (b) only if the perch cannot be
expressed. This is the phase's only schema decision — record it in the notes.

**Wardrobe coverage** — registry entry for `pip` + wardrobe variants; consultant
answers for bird-on-backdrop questions ("robin on rain — keep it in mid, the red
reads").

**Affected paths**: `internal/model/story.go`, `cmd/serve/frontend/intro.js`,
`cmd/serve/frontend/style.css`, `internal/agents/storyteller/composer.go`,
`internal/agents/theatre/` (registry doc, phase 6, for the bird's canonization).

## Integration contract

| Trigger | Collaborator | Observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| Composer emits a bird cast | composer + registry | story validates with `character: bird` | registry already canonized `pip` | bird in a story before registry entry |
| Player renders bird | intro.js | bird on stage at perch height, facing correct | — | bird rendered at ground-line size (capybara-mouse regression class) |
| Bird `vocalize` | VOICES.bird | chirp synth scheduled | — | — |
| Bird `walkTo` | player | hop glide, not leg-walk | — | leg-walk animation on a bird (no bird-leg walking class) |
| Wardrobe consult about bird | registry + consultant | registry-grounded answer | — | — |

## Acceptance criteria

- [ ] `bird` in `ValidCharacters`; a story with a bird cast validates.
- [ ] Bird renders at perch height, distinct silhouette from mouse; scale test
      against `base` matches the mouse's species-size rule.
- [ ] Bird voice parameters present; `speak()` schedules 3-note chirp for a fixture
      beat (audio assertions at the scheduling seam, not the synthesized buffer).
- [ ] Bird `walkTo` uses hop semantics; no bird-leg walking CSS is added.
- [ ] Registry contains `pip` (or the decided id) with canonical coat + variants;
      `pin_identity` stamps it.
- [ ] New composer scenes with the bird validate across 400 seeds.
- [ ] All existing tests pass unchanged.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| Story names a bird before registry canonization | `pin_identity` leaves it as-is; still validates if `bird` is in `ValidCharacters` | registry test |
| Bird `enter` from a perch-less backdrop | bird enters at lane ground then hops (player guard) | render test |
| Voice synth unsupported (no AudioContext) | silent scene, no crash (existing `openAudio` guard) | existing audio guard |
| Perch height overflow | height clamped (0..0.6 if schema (b)) | schema test |

## Implementation notes

Executed by imago, 2026-08-02 session (phase 8 of the playwright-company worklog).

The bird landed in all four layers — model, player, CSS and composer — and the
registry canonized it as the permanent cast's fourth member:

| File | Contents |
|---|---|
| `internal/model/story.go` | `ValidCharacters` += `bird` (the fourth species); the `Cast.Character` comment follows. |
| `cmd/serve/frontend/intro.js` | `VOICES.bird` — the highest, shortest voice (f0 2000–2800, above the mouse's 1700–2500 for contrast), 2–3 quick bursts shaped by the new optional per-voice `burstPitch` curve in `speak` (the bird's middle note rises 42%, the last drops below the first — a 3-note chirp; everyone else keeps the old per-burst drop). `CHARACTERS.bird` — `base: 0.30` (smaller than the mouse's 0.44), `perch: 26` (a species elevation trait), four coats (chaffinch, bluetit, robin, sparrow), `build: buildBird` (body, belly, wing, head, eye, beak, two short legs, tail hanging down — divs and border triangles, no inner SVG). `makeActor` adds `(a.def.perch || 0)` to the lane bottom — the perch is art, not choreography, and the `|| 0` guard is the perch-less fallback. `startWalking` gains the bird seam: the bird gets the `hopping` class, never `walking` — a glide with a hop bob is the whole gait, so `enter`/`exit`/`walkTo` all degrade to hop semantics with no leg-walk. |
| `cmd/serve/frontend/style.css` | The bird art (perching silhouette: short legs, tail down), the `birdHop` keyframes behind `.actor.hopping .actor-inner`, the chirp (beak scaleY + a quick `catMeowHead` bob), and the bird parts joined to the shared beat states (blink, stare, yawn, sniff). The walk-cycle selector still lists only cat/dog/mouse legs — no bird-leg walking CSS exists, grep-verified. `.low-perf` kills the hop animation like the jump's. |
| `internal/agents/storyteller/composer.go` | `pip` constant, `birdCast` (the registry's canonical pip/chaffinch, like ina/ginger), and the new `birdvisit` template: ina settles in, pip enters and perches, they exchange stares, pip chirps, hops at ina (the jump lands just short — the swat-miss tease), flies off, and ina meows at the sky. Forest/garden/rain backdrops, outdoor dressing. The bird's beats stay in its art's vocabulary (enter/exit/vocalize/stareoff/jump). |
| `internal/agents/theatre/registry.go` | The permanent cast gains `pip` (species `bird`, canonical coat `chaffinch`, variants chaffinch/bluetit/robin/sparrow); `speciesVariants` and the director prompt follow. |
| `internal/agents/theatre/fallback.go` | The wardrobe floor answers a bird-on-backdrop question with the pinned look and a perch note ("keep the bird perched in mid — the chaffinch reads") instead of the floor-lane advice — a bird reads by species, not by lane. |
| Tests | `TestValidate_BirdCast` (model), `TestCompose_BirdSceneReachable` + `TestCompose_BirdBeatsStayInBirdVocabulary` (composer), `TestRegistry_*` updates for the fourth permanent member + a canonized guest bird, `TestFallback_WardrobeBirdPerchAnswer`, the permanent-cast counts in `distill_test.go`/`theatre_test.go`, and the new Node player harness (below). |
| `cmd/serve/frontend_test/intro.test.js` | A plain Node harness (no npm, no package.json — the repo has none) that runs the real `intro.js` headless with a DOM/AudioContext stub and asserts the phase-8 seams at the data level: the bird renders at perch height (bottom 37% at lane 0), its scale follows its species base (0.300 vs the cat's 1.000 — smaller than the mouse's 0.44), `walkTo` puts it in `hopping` and never `walking`, and the chirp schedules exactly three sawtooth notes with a rising middle (the recording AudioContext captures `setValueAtTime`). Run: `node cmd/serve/frontend_test/intro.test.js`. |

**Material decisions (recorded for chronology):**

- **D-P8-1 — the perch is species art, option (a), not a schema field.**
  The elevation decision the spec reserved: a bird-specific `perch` percentage
  in the player's `CHARACTERS.bird` (26), added to the lane bottom in
  `makeActor` — no `height` field on `model.Cast`. Elevation is art, exactly
  like `base`; the story schema describes choreography (lanes, marks, scales),
  and art lives in the player registry (D9). The `|| 0` guard is the
  perch-less error case: a species without a perch, or a backdrop where the
  perch cannot apply, simply lands the actor on the lane ground. Lane 0 reads
  at 37% — above every walker's 11% line — so the silhouette never merges
  with the mouse's.
- **D-P8-2 — coat names are single-word ids.** The spec proposed `blue tit`;
  the model's id pattern (`^[a-z0-9_]{1,24}$`) is the trust boundary coats
  must survive (`pin_identity` and `Story.Validate` both run it), so a coat
  with a space could never be pinned. The variant is `bluetit`; the palette
  is chaffinch/bluetit/robin/sparrow.
- **D-P8-3 — the bird is the registry's permanent `pip`, and the composer
  uses the registry entry.** The phase-6 proposal is confirmed: `pip`,
  species `bird`, canonical coat `chaffinch`. The composer hardcodes
  pip/chaffinch exactly as it hardcodes ina/ginger — the registry canonized
  the id in this same phase, and `pin_identity` stamps the final look
  regardless of what any draft says. A bird cannot appear in a story before
  its registry entry exists: both land together.
- **D-P8-4 — the bird's gait is a glide plus a hop bob through the existing
  `startWalking` seam, and there is no bird-leg walking CSS.** The bird
  branch adds the `hopping` class (a quick `birdHop` bob on the inner layer)
  instead of `walking`; the horizontal move rides the same walk-layer
  transition every glide uses. The walking selector still lists only
  cat/dog/mouse legs, grep-verified, so a leg-walk animation can never fire
  on the bird.
- **D-P8-5 — one new composer scene, `birdvisit`.** The spec's "standoff/
  stakeout style lineup" is its own template (cat + bird) rather than
  altering the existing trio scenes: the trio shapes stay intact and the
  bird's art constraints (no sit/pounce/chase) are testable in one place.
  The template randomizes backdrops, marks, lanes and entry sides through
  the normal `stage()`/`dressPlan` path.
- **D-P8-6 — the chirp is a per-voice `burstPitch` override in `speak`, not
  a fork of the scheduler.** `burstPitch` is optional and defaults to the
  existing per-burst drop, so the cat/dog/mouse voices are byte-for-byte
  unchanged; the bird's `[1, 1.42, 0.94]` shapes the 3-note chirp with a
  rising middle.
- **D-P8-7 — the player seams get a Node harness, following the phase-7
  precedent that the repo has no JS test framework.** Phase 7 documented
  that motion cannot be verified headless (animations freeze, no jsdom);
  this phase's acceptance criteria are largely DATA the player sets — perch
  height, species scale, the gait class, the chirp schedule — so a plain
  Node script with a DOM/AudioContext stub asserts them for real. Pixels
  and keyframes remain the manual capture process; the harness lives at
  `cmd/serve/frontend_test/` so the `frontend/*` embed never ships it.

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` (before changes) | pass — baseline green before the phase |
| `go test ./internal/model ./internal/agents/storyteller/... ./internal/agents/theatre/...` (before changes) | pass — phase 1–7 baseline |
| `go test ./internal/model` | pass — 95.6% coverage (bird cast test added) |
| `go test ./internal/agents/storyteller/...` | pass — 84.7% coverage; the 400-seed sweeps cover the new template and the bird cast across runs |
| `go test ./internal/agents/theatre/...` | pass — 90.7% theatre + 93.1% tools (registry/first bird-variant canonization, wardrobe perch answer added) |
| `go test ./...` | pass — full suite green. One pre-existing flaky watcher test (`Test_walkDo` recursive add) failed once under full-suite parallelism; it passes in isolation ×5 and full-suite reruns — unrelated to this phase (no watcher code touched) |
| `go test ./internal/model ./internal/agents/storyteller/... ./internal/agents/theatre/... -race -count=3 -timeout=180s` | pass — repeated runs clean |
| `go run mvdan.cc/gofumpt@latest -l internal/model internal/agents/storyteller internal/agents/theatre cmd/serve` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/model internal/agents/storyteller internal/agents/theatre` | 5 clone groups, all pre-existing (storyteller.go↔theatre.go mirrors are the phase-9 consolidation surface; item.go/item_test.go are unrelated) — none touch phase-8 code |
| `node --check cmd/serve/frontend/intro.js` + `node --check cmd/serve/frontend_test/intro.test.js` | pass — syntax clean |
| `node cmd/serve/frontend_test/intro.test.js` | 6/6 assertions pass — perch height 37%, bird scale 0.300 < mouse 0.44, cat ground line 11% / scale 1.000, hop class (never walking), 3-note chirp with a rising middle |
| Three-layer grep (`bird` in `story.go`, `intro.js`, `style.css`; `birdvisit` in the composer; `permanentPip`/`birdVariants` in the registry; `.actor.walking` selector without `bird-leg`) | pass — see acceptance below |
| `DUMP_STORIES` visual dump of the bird scene | pass — a forest scene with ina (cat) and pip (bird, lane 1): pip enters, chirps, stareoff exchange, jump at ina, flies off, ina meows — the full tease shape |

**Acceptance check** — all criteria met: `bird` is in `ValidCharacters` and a
story with a bird cast validates (`TestValidate_BirdCast`). The bird renders
at perch height with a distinct silhouette and a species base (0.30) below the
mouse's (0.44) — asserted at the player seam by the Node harness (bottom 37%
at lane 0, `scale(0.300)`). The voice parameters are present and `speak()`
schedules a 3-note chirp with a rising middle for the bird (harness assertion
at the scheduling seam — the recording AudioContext's `setValueAtTime` calls —
not the synthesized buffer). `walkTo` uses hop semantics: the harness sees the
bird in `hopping`, never `walking`, and the CSS walk-cycle selector has no
bird-leg entry (grep-verified). The registry contains `pip` with the canonical
coat `chaffinch` and its variants; `pin_identity` stamps it (registry pin test
across 400 seeds, now 4 ids). The new `birdvisit` scene validates across 400
seeds via `TestCompose_AllScenesValidateAcrossSeeds` and the dedicated
reachability/vocabulary tests. All existing tests pass unchanged except the
registry/distill/theatre count updates that the fourth permanent member
requires — those are the phase's own contract.

**Error coverage** — a story naming a bird before registry canonization:
`pin_identity` leaves an unregistered id as-is and the story still validates
now that `bird` is a valid character (registry test for guests, unchanged
semantics). Bird `enter` from a perch-less backdrop: the `|| 0` guard in
`makeActor` lands the actor on the lane ground and the hop bob carries the
move (code-path guard, documented; the harness exercises the perch path).
Voice synth unsupported: the existing `openAudio` guard returns null and the
scene plays silent (unchanged, existing audio guard). Perch height overflow:
there is no schema height (D-P8-1), so the overflow case does not exist — the
perch is a fixed species art trait.

## Review findings

*(filled by reviewers)*

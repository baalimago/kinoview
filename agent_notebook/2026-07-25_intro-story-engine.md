# Intro short-story engine — Ina, Freija & the mouse

Supersedes the single-cat splash from `2026-07-25_intro-cat-splash.md`.

## Shape

A ~4s short story plays on every visit. The story is **data**, produced ahead of time
by a `storyteller` agent (LLM via clai) and cached; the frontend is a player for that
data. Deterministic composer covers every failure path so the splash never depends on
an LLM call succeeding.

```
storyteller (clai) ──▶ story.json ──▶ GET /gallery/intro/story ──▶ frontend player
        └── on failure / no model ──▶ deterministic composer (Go)
                                          └── frontend also has a minimal local
                                              fallback (single cat, one meow)
```

Cast decided with the user: **Ina = cat**, **Freija = dog**, plus an unnamed **mouse**
they hunt. A short title card ("Ina & Freija in: …") is shown under the action.

## Story schema (contract)

```json
{ "id","title","durationMs",
  "cast":  [ {"id","character","coat","lane","scale","x"} ],
  "props": [ {"id","prop","x","lane"} ],
  "beats": [ {"t","actor","action","x","target","ms"} ] }
```

Beats reference cast/prop ids. `t` is ms from story start. Actions come from a closed
vocabulary the player implements; anything unknown is dropped.

Vocabulary: `enter, exit, walkTo, vocalize, sit, stretch, blink, pounce, chase,
greet, stareoff, nap, bat`.

## Trust boundary (important)

The story is LLM-authored, so it is **untrusted input that drives animation**.
`model.Story.Validate()` is the gate: unknown characters/actions/props dropped,
`t`/`ms` clamped into the budget, cast size capped, ids must match `^[a-z0-9_]{1,24}$`,
title length-capped and stripped of control characters. The player only ever writes the
title via `textContent` — never `innerHTML`. A malformed story is rejected wholesale in
favour of the composer rather than partially applied.

## Preparation & cooldown (per user's request)

Two triggers, both funnelled through one guarded `Prepare`:

1. **consume-then-prepare** — serving `/intro/story` marks it consumed and asks for the
   next one, so one is always ready even if the app is killed without warning.
2. **session-end beacon** — `pagehide`/`visibilitychange` → `navigator.sendBeacon`.

Both are rate-limited by a **cooldown** (default 10 min, `-storytellerCooldown`): if a
generation ran within the window, the request is a no-op. Refreshing the page ten times
must not cost ten LLM calls. Generation also runs at most once concurrently.

## Movement primitive

Positions anchor via `left` (set to the final value immediately); motion is a
`transform: translateX()` offset transitioned back to 0. If the engine refuses to
animate, actors are still standing in the right places — same degradation rule as the
walk-in. Facing is derived from the sign of the movement.

## Voices

One formant synth (`renderVoice`) parameterised per species, reusing the meow research:
smaller animal ⇒ higher f0 *and* higher formants (shorter vocal tract).

| voice | f0 | duration | notes |
|---|---|---|---|
| cat (Ina) | 620–770 Hz | 0.42–0.60 s | as tuned previously |
| dog (Freija) | 240–340 Hz | 0.16–0.24 s | 2 bursts, noisier, sharp attack |
| mouse | 1700–2500 Hz | 0.10–0.16 s | near-sine, very short |

All vocalisations are queued on the AudioContext clock up front, so setTimeout jitter
cannot desync sound from mouths.

## webOS rules (unchanged)

ES5, `-webkit-` prefixes, no `inset`/`gap`/Web Animations API, divs not inner-SVG
transforms, animate only transform/opacity, `.low-perf` and reduced-motion paths.

## Files

- `internal/model/story.go` — schema + `Validate`
- `internal/agents/storyteller/` — llm.go, composer.go, store.go
- `internal/media/index.go` + `index_handlers.go` — `/intro/story`, `/intro/session-end`
- `cmd/serve/frontend/intro.js` — player (intro extracted out of index.js)
- `cmd/serve/frontend/style.css` — dog, mouse, props, beats, title card

---

## Increment log (this session)

Built as self-sufficient steps; each left the tree green.

**0. Boot gap + cooldown across restarts.** Nothing was generated at boot: `Next()`
composed synchronously on first request, so the very first visit was always
composer-authored while a configured LLM sat idle. Added `Teller.Warm(ctx)`, called
from `serve_setup`, which prepares in the background only when nothing is cached.
Separately, `lastGen` lived only in memory, so **every restart reset the cooldown** — a
crash-loop would have cost one LLM call per restart. `loadFromDisk` now seeds `lastGen`
from the cache file's mtime, which needs no extra state.

**1. Ten seconds.** Limits raised (duration 10s, 5 cast, 4 props, 44 beats). Scenes
rewritten as three-act pieces at ~9.2–9.5s with deliberate stillness between actions —
constant motion for ten seconds reads as noise.

**2a. Backdrop.** `Scene.Backdrop` + five sets (night, livingroom, garden, theatre,
sunset) as three layers (sky/scenery/ground). Plus **contact shadows** on actors and
props — the single biggest win for making the cast look like it is standing on the set
rather than pasted over it.

**2b. Cell system.** `Scene.Cells` is a (row, col) grid of addressable slots; ten set
pieces; `setCell` and `setBackdrop` beats swap them at keyframes. Scene beats carry no
actor, so `Validate` routes them separately and the cell id namespace is shared with
cast and props to keep beat targets unambiguous.

**3. Theme from the last watched title.** `Muse` interface, read lazily at generation
time (preparation happens long after the request that triggered it). `LatestTheme`
resolves the most recent view across all sessions and cleans release-scene noise off the
filename. The LLM is asked for a wordless homage to its *mood and shape*; the composer,
having no new choreography to offer, bills the existing scene under it instead.

### Staging rules learned by looking

- **Scale pieces against the cast, not each other.** First pass had cats bigger than
  oaks. Pieces are authored on an 80px grid and scaled up per row.
- **Scenery belongs in the wings.** The cast performs across x=0.34–0.76, which is
  columns 2–4, so set dressing is restricted to columns 0 and 5. Otherwise a bush grows
  out of Freija's face.
- **Depth haze must be a brightness filter, never opacity.** A translucent sofa lets the
  backdrop bleed through and stops reading as a solid object.
- **Fold limbs with scaleY, not big rotations.** Rotating a 42px leg 45°+ about the hip
  throws the paw outside the body and reads as a detached stick (broke sit and nap).
- **A z-index:-1 pseudo-element scrim is fragile.** The title band is the element's own
  background, which is always painted behind its own text.

### Verification note

CSS transitions and animations do not advance without a live compositor, so every
headless capture freezes them. Three separate "bugs" this session were that artifact
(cat parked off-screen, translucent set pieces, grey title). The harnesses now strip
`transition`/`animation` before capture. Motion itself remains unverified by screenshot.

---

## Increment 4 — "they all look the same" (user feedback)

User report: every story played as *Ina from the left, Freija from the right, meet in
the middle where the mouse is*, the mouse looked the wrong size, and no LLM flair was
visible.

### Diagnosis

**The LLM had never run.** `-storyteller` defaults to empty, and with no model the
storyteller is composer-only (it logs `storyteller running composer-only`). Everything
the user had seen was the hand-authored Go composer. The LLM path also remains untested
against a real provider — no API key available here.

**Mouse proportion was a real bug.** The art is 84px against the cat's 160px, but both
got the same depth multiplier, so the mouse rendered at ~52% of a cat's body length — a
capybara. Species size is now intrinsic (`CHARACTERS[x].base`: cat 1.00, dog 1.02,
mouse 0.44) and the story's own `scale` only nudges it.

**The monotony was real and structural.** Every template hardcoded its own staging, so
six different shapes all played identically.

### Fix: staging is decided per run, not per template

`staging.go` owns marks, entry sides and lanes. Templates now describe only the SHAPE of
a scene. Five layouts (converge, wide, close, off-centre left/right), which lead takes
which mark is a coin flip, and **entry side is chosen independently of the mark** — a
character whose mark is on the far side crosses the whole stage, which looks nothing like
a short walk-on even with identical beats. Two new shapes added (`crossing`, `stakeout`)
and role assignment randomised, so Freija can lead and can have the solo scene.

### Two regressions caught by looking, then fixed

1. **Bodies merged.** `minGap` of 0.26 still let the (wider) dog intersect the cat; now
   0.32, and anything under `nearGap` 0.40 is split across lanes so the overlap reads as
   depth. Marks clamped to 0.16–0.84 so nobody stands in the wings or gets clipped.
2. **Bare stages.** Restricting scenery to unoccupied columns dropped three-character
   scenes to *zero* set pieces. The real rule is per-piece, not per-column: a tall piece
   (tree, lamp, window) or a flat one (rug) may share a column with a performer, because
   a trunk rising past a body reads as depth. Only *short* pieces whose silhouette ends
   at body height (bush, fence, sofa, plant) need a clear column. Cell counts recovered
   to 3–5.

### Variety is now a test, not an opinion

`staging_test.go` asserts across 300 seeds that both principals enter from both wings
with neither side dominating past 80/20, that each stands in ≥5 distinct positions, that
the leads swap sides, and that somebody sometimes crosses the stage.

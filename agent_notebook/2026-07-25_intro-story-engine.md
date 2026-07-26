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

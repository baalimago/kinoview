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

# Intro splash: walking cat + queued meow, webOS-safe and scalable

## Goal

Replace the static logo-only splash with a small staged performance: a cat walks in,
stops, meows (audio sample-accurately aligned to the mouth opening), the Kinoview logo
blooms, then the app takes over. Randomized on every load. Architected so more actors
(more cats, a dog) can be added without touching the director.

## Why the current splash fails on webOS

`.intro-overlay` (style.css:818) sizes itself **only** via `inset: 0`, with no
width/height. `inset` is a Chromium 87+ shorthand:

| webOS TV | Chromium | `inset` |
|---|---|---|
| 4.x (2018–19) | 53 | no |
| 5.x (2020) | 68 | no |
| 6.x (2021) | 79 | no |
| 22 (2022) | 87 | yes |

So on any webOS TV before 2022 the overlay collapses to content size and never covers
the screen — the splash is visibly broken. Same shorthand also at style.css:416 and
:427, though those pair it with `width/height: 100%` so they degrade harmlessly.

### webOS constraints adopted as rules

- Baseline target **Chromium 53 (webOS 4.x)**. The stylesheet already depends on CSS
  custom properties (Chromium 49+), so webOS 3.x/Chromium 38 is already out of scope.
- Replace `inset: 0` with explicit `top/right/bottom/left: 0`.
- Emit `-webkit-` **and** unprefixed `animation`/`transform`/`transition` — LG documents
  transforms/transitions/animations as Candidate Recommendation before webOS 3.0.
- Stay ES5 in the intro IIFE (`var`, `function`) — matches the existing meow code and the
  repo's "no transpile unless necessary" stance.
- No Web Animations API (`el.animate`), no `gap` in flex layout, no `clip-path`.
- Build the actor from **HTML divs + border-radius**, not inner-SVG transforms — CSS
  transforms on SVG child elements are unreliable on old WebKit/Blink.
- Animate only `transform` / `opacity` so the compositor does the work; TV CPUs are weak.

## Sequence (ms from intro start)

| t | beat |
|---|---|
| 80 | cat enters from a random side, walk cycle running |
| ~1500 | arrives at mark, settles, tail keeps swaying |
| ~1750 | mouth opens → **meow fires here** |
| ~2350 | logo blooms |
| ~3000 | dismiss once app has loaded (else hold to cap) |

Audio is *queued*, not fired on a timer: the AudioContext is created up front and the
meow is scheduled at an absolute `ctx.currentTime + delay`. setTimeout jitter on a TV
would otherwise desync mouth and sound.

## Randomization

Entry side, coat palette, scale, walk speed, stop mark, tail-sway rate, blink schedule,
sit-vs-stand, ear twitch, plus the existing pitch/duration/double-meow variation.

## Scalable cast

```
CAST = { cat: { build, voice, palettes, scale, walkMs, vocalDelayMs } }
```

`castList()` returns an array of actor specs; the director stages each with its own lane
(depth), stagger, and mark. Adding a dog = one `CAST` entry (a `build` returning a DOM
node + a `voice` scheduling audio). The director loops over actors and needs no change.

## Escape hatches

- Click / keypress / remote OK skips the intro immediately.
- `.low-perf` (already set by the existing lag detector) → drop the leg walk cycle, slide
  in, shorter total.
- `prefers-reduced-motion` → no walk, straight to logo.
- Any failure in the performance falls back to logo-only; audio failure stays silent.

## Files

- `cmd/serve/frontend/index.html` — intro stage markup
- `cmd/serve/frontend/style.css` — `inset` fix, cat + stage CSS, prefixed keyframes
- `cmd/serve/frontend/index.js` — CAST registry + director, reusing `renderMeow`

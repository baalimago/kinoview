# Phase 1 — Splash Render Fix: Never-Entered Actors Stay Visible

**Status:** ✅ Done (R3-01 closed in worker session 6) | [README](./README.md)

## Goal

Fix the player so a cast member that has beats but never receives an `enter`
beat is staged at its cast mark from the start, instead of remaining invisible
off-screen for the whole show.

## Specification

**Prod symptom.** In `stry_vkpu0hvp` (and earlier `stry_zuuxkuks`) the
guards — freija and ina — never get an `enter` beat; they only `sit`,
`stareoff`, `nap`, `blink`. The player (`cmd/serve/frontend/intro.js`) hides
every actor until `enter` adds the `staged` class (`makeActor` anchors at
`-offStagePx`, CSS `.actor { opacity: 0 }`, only `.actor.staged` is visible).
The audience therefore saw only mouse1 — the sole actor with an `enter` beat.

**Fix — player owns staging (decision D-1).** In `playStory`, after the cast
loop builds `sc.actors` and before beats are scheduled, compute the set of
actors that (a) appear in at least one beat and (b) never receive an `enter`
beat. For each such actor, anchor it at its cast mark and stage it:

```
anchor(a, markPx(a, a.spec.x));
a.el.classList.add('staged');
a.onStage = true;
```

The anchor must run before the first beat fires (t=0 beats are scheduled with
`at(0, …)`, so the staging must happen while building, not in a timer).

**Playwright prompt nudge.** `playwrightPrompt` in
`internal/agents/theatre/roles.go` gains one rule: every cast member must
enter the stage with an `enter` beat — a character that never enters misses
its entrance and simply stands at its cast mark from the first frame. This
stops the pattern at the source; the player fix covers the past.

**Affected paths:**

- `cmd/serve/frontend/intro.js` — `playStory` staging logic
- `internal/agents/theatre/roles.go` — playwright prompt rule
- `cmd/serve/frontend_test/intro.test.js` — regression fixture

No changes to `model.Story.Validate`, the working file, the store, or any Go
runtime logic — the only Go-side edit is the playwright prompt string (a
const) and its test. The prompt is data that shapes LLM output; the
deterministic runtime (composer, validation, player) is untouched. Composer
stories are unaffected (the floor always enters).

## Integration contract

| Input / trigger | Collaborator / fake | Externally observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| LLM story where freija+ina have beats but no `enter` (the prod shape) | node harness, real `intro.js` player, stubbed DOM | freija and ina are staged at their cast marks at t=0 (opacity visible, `staged` class, positioned at `markPx(cast.x)`) | mouse1 still enters via its own `enter` beat; staging happens before t=0 beats fire | no change to story JSON; no duplicate `enter`; no dropped beats |
| Story where an actor has zero beats (guest cast) | same harness | actor stays hidden (never staged) | unchanged | — |
| Composer story (always enters) | same harness | all actors enter via `enter` as before; no double staging | unchanged | — |

## Acceptance criteria

- [x] Node harness fixture reproduces the prod shape (guards with beats, no
      `enter`) and asserts both guards are staged at their cast marks before
      any beat fires.
- [x] The same fixture asserts the never-entered actor is positioned via
      `markPx(cast.x)` and carries the `staged` class (opacity visible).
- [x] A zero-beat cast member remains hidden.
- [x] The existing bird/chirp assertions still pass unchanged (no regression).
- [x] `playwrightPrompt` contains the enter rule; a runner/roles test asserts
      the subagent task prompt includes it.
- [x] `go test ./internal/agents/theatre/` green after the prompt change.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| Story JSON has actors with beats but no `enter` (prod shape) | actors staged at cast marks, show plays correctly | harness fixture (fails on pre-fix code) |
| Actor with no beats at all | stays hidden — unused cast is not forced on stage | harness fixture |
| Actor's first beat is `exit` (never entered) | staged at cast mark, then exits — visible for its one beat | harness fixture |
| `story.cast` missing / actor lookup fails | `makeActor` returns null, staging skips it, no panic | existing guard + harness |

## Implementation notes

- **Player fix (`cmd/serve/frontend/intro.js`).** New `stageNeverEntered(sc,
  beats)` helper in the PLAYER section, called from `playStory` right after
  the cast loop and before beats are scheduled. It is synchronous with the
  build — no timer — so it can never race a t=0 `enter`. An actor with ≥1
  non-`enter` beat and 0 `enter` beats is anchored at `markPx(a.spec.x)`,
  given the `staged` class and `onStage = true`. Actors with an `enter` beat
  (entered set) and zero-beat cast members are untouched; an actor whose only
  beat is `exit` is staged at its cast mark and then exits normally (visible
  for its one beat). Unknown characters (`makeActor` → null) are skipped
  without a panic. The `makeActor` comment now points at the second reveal
  path.
- **Prompt nudge (`internal/agents/theatre/roles.go`).** `playwrightPrompt`
  gains: "Every cast member must enter with an enter beat — a character that
  never enters misses its entrance and stands at its cast mark from the
  first frame." Pinned in `TestRolePrompts_ScopeTextPinned` (roles test;
  `RolePrompt` is embedded verbatim in the runner's assembled task prompt via
  `AssembleContext`). No Go runtime logic changed.
- **Harness (`cmd/serve/frontend_test/intro.test.js`).** Refactored from one
  fixture to three. `playStory` is a singleton, so each fixture runs against
  its own stage/DOM/AudioContext stubs via `runStory(storyJSON)`. The story
  lands synchronously through the XHR stub, so by the time `runStory` returns
  the build has run and no timer has fired — the guard fixture's "before any
  beat fires" check is a plain synchronous assertion. Fixtures: (1) the
  original bird/chirp story, now also asserting the zero-beat cat stays
  hidden; (2) the production shape — mouse1 enters, guards freija+ina only
  sit/nap/stareoff/blink — asserting both guards staged at their cast marks
  ('26px', '483px', derived from `markPx`) and still staged mid-show; (3) an
  exit-first bird (staged at its cast mark, then gone after the exit
  completes) plus an unknown `dragon` cast member that must be skipped.
- **Verified against pre-fix code.** With `stageNeverEntered` removed, the
  harness fails exactly the three guard/exit-first assertions while the
  bird/chirp assertions pass unchanged (AC: fails on pre-fix code).

## Review findings (review 2, 2026-08-05)

- **R2-04 (Low).** Tightened "no … any Go runtime behaviour" — it half-
  contradicted "the only Go-side change is the playwright prompt string": the
  prompt is Go code that deliberately changes LLM output. Now reads "no Go
  runtime logic" — the deterministic runtime (composer, validation, player)
  is untouched, and the prompt is data.

## Review findings (review 3, 2026-08-05)

- **R3-01 (Medium).** `stageNeverEntered` builds its two maps as plain object
  literals (`var acted = {}; var entered = {};`), so cast ids that collide
  with `Object.prototype` members break the staging invariant this phase
  promises. The story validator's id pattern admits all-lowercase prototype
  names — `idRe = ^[a-z0-9_]{1,24}$` (story.go) matches `constructor`,
  `toString`, `valueOf`, `hasOwnProperty`, `__proto__`, … — so an
  LLM-authored cast id can reach the player, and the playwright prompt
  imposes no id vocabulary.
  - *Skip variant:* for id `constructor`, `entered[id]` resolves the inherited
    `Object.prototype.constructor` (truthy), so the guard
    `!acted.hasOwnProperty(id) || entered[id]` skips a never-entered actor
    that must be staged — the exact bug this phase fixes persists for that
    id. Reproduced against the real player (node, real intro.js, stubbed
    DOM): cast id `constructor` with a `sit` beat builds the actor element
    but leaves it unstaged at `left:-220px` (off-stage, `opacity:0`); the
    same story with id `ina` stages at its cast mark (`240px`).
  - *Crash variant:* for id `hasOwnProperty`, `acted["hasOwnProperty"] = true`
    shadows the method, so `acted.hasOwnProperty(id)` throws `TypeError` in
    the staging pass. The throw is swallowed by `begin()`'s catch (intro.js
    1145–1161) after `settled` is set, so the story silently never plays:
    no beats scheduled, no vocalizations queued, no `storyEnd` — the splash
    shows a static backdrop until the 13.5 s hard cap. Reproduced: the build
    throws; the harness's normal ids mask it.
  - *Fix:* the maps are built as `Object.create(null)` and the
    `hasOwnProperty` guard is dropped — `for…in` only yields own enumerable
    keys, and a null-prototype object has no inherited members to collide
    with (`entered[id]` is then `undefined` for absent keys, falsy).
  - [x] Changed `stageNeverEntered` to null-prototype maps; added harness
        fixture 4 with prototype-name cast ids — a never-entered
        `constructor`-style id and a `hasOwnProperty`-style id in one story.
        The synchronous assertion requires both cats staged at their cast
        marks ('80px'/'400px', derived from `markPx`) and fails on pre-fix
        code with exactly the two new assertions (26 pass, exit 1); the
        mid-show assertion proves beats still schedule after the staging
        pass (pip enters via its own beat), covering the crash variant's
        silent-never-plays symptom. Post-fix: 28 assertions pass. The
        `sc.actors` lookup is deliberately untouched (D-8).

**Verified good (review 3).** The staging pass runs synchronously in the
build path before any timer (no race with t=0 beats — harness fixture 2
asserts staged state right after `runStory` returns); zero-beat cast stays
hidden; exit-first actor is staged then removed; unknown characters are
skipped without a panic; the playwright prompt carries the enter rule with a
roles-test pin; `model.Story.Validate` is untouched; the composer floor
always enters its cast (floor.go). The pre-fix regression claim reproduces
exactly: with `stageNeverEntered(sc, beats)` removed from a scratch copy of
intro.js, the harness fails precisely the three guard/exit-first assertions
(23 of 26 pass, exit 1).

## Review findings (review 6, 2026-08-06)

- **R6-01 (Low).** D-8's sc.actors rationale is false for the one id that
  never becomes an own property: `__proto__`. It is validator-legal
  (`idRe = ^[a-z0-9_]{1,24}$` matches, story.go:217), and the cast loop's
  `sc.actors[a.id] = a` (intro.js:1051) does not create an own property on a
  plain object — the inherited `__proto__` setter replaces the map's
  prototype with the actor object. Reproduced against the real player (node,
  real intro.js, stubbed DOM): a `__proto__` cat with a `sit` beat is staged
  at its cast mark ('80px', `staged` class) and the show plays through its
  beats — lookups of `sc.actors['__proto__']` resolve through the
  `__proto__` getter to the very actor stored as the prototype, and the
  validator guarantees beats/targets reference only real ids (all own
  properties except `__proto__` itself), so no reachable path through a
  validated story breaks today. The gap is the rationale, not the behaviour:
  the map's prototype is silently replaced, a trap for any future code that
  iterates `sc.actors` or calls `Object.keys`/`hasOwnProperty` on it, and
  D-8's "a cast id is an own property there" is inaccurate.
  - [ ] Harden the three story-data maps in `playStory` (`actors`, `props`,
        `cells`, intro.js:1025) to `Object.create(null)` — one line,
        ES5-safe, the same pattern the staging maps use — or correct the
        D-8 note to record the accidental resolution.

**Verified good (review 6).** The staging pass runs synchronously in the
build path before any timer (no race with t=0 beats); zero-beat cast stays
hidden; exit-first actors are staged then removed; unknown characters are
skipped without a panic; the R3-01 fixture (constructor/hasOwnProperty) is
staged at its cast marks with beats still scheduling (harness fixtures 2–4,
28 assertions green); the playwright prompt carries the enter rule with a
roles-test pin; `model.Story.Validate` is untouched; the composer floor
always enters its cast.

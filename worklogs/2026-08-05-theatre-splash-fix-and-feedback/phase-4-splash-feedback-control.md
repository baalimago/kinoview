# Phase 4 — Splash Feedback Control: Text + Thumbs in the Intro

**Status:** ✅ Done | [README](./README.md)

> **Decision reversed 2026-08-06 (commit fe2d1a5).** The control is now
> **blocking**. While it is live (built, not yet sent), no dismissal path may
> remove the overlay — not the schedule, not the hard cap, not an outside
> click, not a keydown. The audience leaves through a thumb; `send()` hides
> the control, releases the block, and the handover completes ≤350 ms later.
> Passages below that say the control "rides with the overlay", "never
> extends the splash duration", or that an outside click / the hard cap
> dismisses it describe the superseded design; see Review findings (review 7)
> at the end of this file.

## Goal

Show a blocking feedback control in the intro splash — a text field and a
thumbs up/down pair that submit in one tap to `POST /gallery/intro/feedback`
— during the logo reveal. While the control is live the overlay holds: on a
slow TV the note used to fade away with the overlay before the audience
could write it, so the control now suspends every dismissal path until a
thumb is tapped.

## Specification

**UI (decision Q1 / Option A).** In `cmd/serve/frontend/intro.js`, during the
logo reveal (`storyEnd`), build a small feedback control inside the overlay:

- a single-line text input (placeholder like "a note for the director",
  `maxlength` 240, ES5)
- a thumbs-up button and a thumbs-down button (SVG or unicode, matching the
  existing ES5/no-innerHTML-LLM-text style of the player)
- a dismiss affordance is NOT needed — the control is the only exit while it
  is live: outside click and keydown dismissal are suspended, and the hard
  cap cannot take the overlay away until a thumb is tapped

**Behaviour.**

- The control appears at `storyEnd` (when the logo reveals), only when the
  story is real (has an `id`) — never on the local fallback story. It is
  built in the `at(storyEnd, …)` logo-reveal callback inside `playStory`; the
  `logoOnly` path (no stage or reduced motion, intro.js:924) never builds it.
- Thumbs up submits `{storyId: story.id, rating: 1, comment: input.value}`;
  thumbs down submits `rating: -1`.
- Submit is a single `XMLHttpRequest` POST to `FEEDBACK_URL =
  '/gallery/intro/feedback'` with `Content-Type: application/json`. Response
  is fire-and-forget (204); failure is silent — feedback must never break the
  splash.
- After submit the control hides (class toggle) and the intro continues to
  dismiss on its normal schedule.
- If the user ignores it, the control BLOCKS dismissal: while
  `feedbackPending` is true (control built, no thumb tapped), `dismissIntro`
  returns early, so the schedule, the `MAX_INTRO_MS + 500` hard cap, outside
  clicks and keydowns all no-op. The only exit is a thumb: `send()` hides the
  control (`.sent`), clears `feedbackPending`, and calls `maybeDismiss()`,
  which completes the handover on the normal schedule (≤350 ms once the
  story and the three app-data loads are done). No control-specific timeout
  — the block is a suspension, never a new timer.
- ONE delegated `click` listener on the control root calls `stopPropagation`
  for every click inside the control — focusing the input AND tapping either
  thumb — so neither trips the document-level `click → dismissIntro` listener
  prematurely. Text entry does not dismiss the intro (keydown on the input is
  stopped).
- No new CSS animations beyond a fade/slide-in; keep the overlay's existing
  pointer-events semantics intact — the overlay itself has no
  `pointer-events` rule (a click on it targets the overlay and bubbles to
  the document dismiss listener: that is today's outside-click dismissal),
  and `.intro-stage` is `pointer-events: none` (style.css:1002). The control
  is appended to the overlay, so it is clickable by default and needs no
  `pointer-events` rule; only the control's delegated `stopPropagation`
  matters.

**Affected paths:** `cmd/serve/frontend/intro.js`, `style.css` (control
styles), `index.html` (no structural change expected — the control is built by
the player like the cast/props), `cmd/serve/frontend_test/intro.test.js`.

## Integration contract

| Input / trigger | Collaborator / fake | Externally observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| Real story plays to `storyEnd` | node harness, real player, stubbed DOM/XHR | control visible inside the overlay at logo reveal | — | control never shown for the local fallback story |
| Tap thumbs up with comment "more dog" | harness XHR records the POST | one POST to `/gallery/intro/feedback` with `{"storyId":<id>,"rating":1,"comment":"more dog"}` | control hides | intro not dismissed by the tap itself |
| Tap thumbs down, empty comment | harness | POST with `rating:-1`, `comment:""` | control hides | — |
| User ignores the control | harness | overlay stays: schedule, hard cap, outside click and keydown are all blocked while the note is unsent | — | splash duration not extended by timers (the block is a suspension, not a new timer) |
| Click/keydown outside the control | harness | blocked while the control is live; overlay dismisses only after a thumb releases the block | — | — |
| XHR fails / 500 | harness stub returns error | silent; control hides; splash unaffected | — | no console spam, no retry storm |

## Acceptance criteria

- [ ] Node harness asserts the control appears at `storyEnd` for a story with
      an id and does NOT appear for the local fallback.
- [ ] Harness asserts no control under reduced motion (matchMedia stub
      returning `matches: true`).
- [ ] Harness asserts thumbs-up POSTs `{storyId, rating:1, comment}` with the
      input's text; thumbs-down POSTs `rating:-1`.
- [ ] Harness asserts tapping a thumb AND clicking to focus the input do not
      dismiss the overlay (the document click listener is not tripped).
- [ ] Harness asserts an ignored control BLOCKS dismissal: outside click and
      the hard cap cannot remove the overlay while the note is unsent; a
      thumb releases the block and the handover completes on schedule.
- [ ] The existing intro player assertions (bird geometry, chirp schedule)
      still pass unchanged.
- [ ] `node cmd/serve/frontend_test/intro.test.js` green.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| XHR error / non-2xx | silent; control hides; splash continues | harness stub |
| Story without id (local fallback) | no control | harness |
| User types then taps thumb | comment included verbatim (truncated client-side to 240) | harness |
| User ignores the control | the overlay stays — every dismissal path is blocked until a thumb is tapped | harness |
| Low-perf (story still plays) | control shows as usual; no extra animation | harness + existing low-perf path |
| Reduced motion (`matchMedia` → `matches: true`) | no control — the player takes the `logoOnly` path (intro.js:924) and the overlay is dismissed ~1.3 s after load; there is no reveal moment to attach to | harness with a reduced-motion matchMedia stub |

## Implementation notes

*(filled by the executing agent)*

Implemented per the plan, including review findings R2-02 (the control is
appended to the overlay, clickable by default, with no `pointer-events` rule
— the delegated `stopPropagation` is the only click containment) and R2-03
(no control under reduced motion: `playStory` returns early through
`logoOnly()`, so the `storyEnd` callback never runs).

**Changes.**

- `cmd/serve/frontend/intro.js` — `FEEDBACK_URL` and `FEEDBACK_NOTE_MAX`
  constants; a new AUDIENCE FEEDBACK section with `buildFeedbackControl`
  (note input, thumbs up/down, delegated click + keydown `stopPropagation`
  on the control root, fire-and-forget XHR POST, `sent` hide toggle, `show`
  via a frame-crossing `at(0, …)`); the `storyEnd` callback in `playStory`
  now builds the control when `story.id` exists.
- `cmd/serve/frontend/style.css` — `.intro-feedback` block: absolute
  bottom-centre, fade/slide-in only (opacity/transform), no `pointer-events`
  rule on the live state, `.sent` hides it, low-perf trims the transition,
  a focus ring for TV-remote navigation, and a phone-width note.
- `cmd/serve/frontend_test/intro.test.js` — the harness grew from 3 to 9
  fixtures: `makeEl` records listeners and parent links, the document stub
  records its click/keydown listeners, the XHR stub records POSTs and can
  throw (failPosts) or fail the fetch (`makeFailingXHR`), `runStory` takes
  opts (lowPerf / reducedMotion / failFetch / failPosts), wraps `setTimeout`
  to record every delay, and returns the overlay/body/posts/timerDelays plus
  `docClick` and `fireEvent` helpers that bubble synthetic events through
  the element tree.

**Material implementation decisions.**

- D-6 — keydown containment is a delegated listener on the control root, not
  on the input alone. The spec's letter stops keydown on the input; a
  delegated root listener also covers the TV remote's OK on a focused thumb
  (OK arrives as a keydown and would otherwise trip the document's keydown
  dismissal). One listener, a superset of the spec, recorded in the README
  decision log.
- The thumbs are unicode escapes (`\uD83D\uDC4D`/`\uD83D\uDC4E`) so the
  source stays ASCII; the spec allows "SVG or unicode", and the codebase's
  stated caution about SVG on old Blink builds (style.css intro-stage
  comment) makes a text glyph the lower-risk choice.
- `fireEvent` bubbles through the parent chain and stops when a delegated
  listener calls `stopPropagation` — the headless equivalent of "the click
  never reaches the document's dismiss listener", which is what the AC
  asserts.
- The "blocking control adds no timers" AC is pinned by recording every
  timer delay: the harness asserts the hard cap (`MAX_INTRO_MS + 500` =
  13500) is still the longest timer with the control present — the block is
  a boolean check in `dismissIntro`, never a new timer.

**Tests (before: all green; after: all green).**

```
node cmd/serve/frontend_test/intro.test.js   # before: 13 ok; after: 26 ok
node cmd/serve/frontend_test/intro.test.js   # pre-fix (call removed): 8 FAIL, exit 1
go build ./...                               # ok
go vet ./internal/agents/... ./internal/media/    # clean
go test ./internal/agents/theatre/ ./internal/media/ -count=1   # ok
```

Pre-fix verification: with the `buildFeedbackControl(story.id)` call removed
from the `storyEnd` callback, the harness fails exactly the eight feedback
assertions (exit 1) while the existing bird/guard/exit assertions pass
unchanged — the regression test fails on pre-fix code, as the AC demands.

## Review findings (review 2, 2026-08-05)

- **R2-02 (High).** The pointer-events parenthetical was factually wrong: the
  overlay has no `pointer-events` rule (style.css:934-949), and only
  `.intro-stage` is `pointer-events: none` (style.css:1002). A literal
  implementation would have added `pointer-events: none` to the overlay,
  changing dismissal semantics — clicks would fall through to the gallery
  beneath and could activate links. Corrected: the control is appended to the
  overlay (clickable by default); no `pointer-events` rule needed, only the
  control's `stopPropagation`.
- **R2-03 (Medium).** The "Reduced-motion / low-perf" error row claimed the
  control "still shows" under reduced motion, but `playStory` returns early
  through `logoOnly()` (intro.js:924) when `reducedMotion` is set: no story
  plays, `storyEnd` never exists, and the overlay is dismissed ~1.3 s after
  load — no place for a form. Corrected: no control under reduced motion
  (row split, harness assertion added); low-perf still shows the control.

## Review findings (review 3, 2026-08-05)

- **R3-02 (Low).** The reduced-motion fixture asserts synchronously at t=0
  (`no feedback control under reduced motion`, intro.test.js fixture 8). The
  control is built in the `storyEnd` timer (t=800 ms in the fixture), so a
  regression that scheduled the storyEnd callback under reduced motion would
  still pass the synchronous assertion. The current code is correct —
  `playStory` returns through `logoOnly()` before `at(storyEnd, …)` is
  reached (intro.js:1016) — so this is test rigor only, not a code defect.
  - [ ] Move the reduced-motion assertion behind a timer past `storyEnd`
        (~900 ms), mirroring the fallback fixture's timing, so a storyEnd
        regression cannot pass it.

**Verified good (review 3).** The control appears only for a story with an
id at the logo reveal (fixture 4), never for the local fallback (fixture 7)
or under reduced motion (fixture 8, current code); thumbs up/down post the
correct `{storyId, rating, comment}` triple and hide the control (fixtures
4–5); the delegated click/keydown `stopPropagation` on the control root
keeps both a thumb tap and a focus click away from the document's dismiss
listeners (fixtures 4–5); an ignored control rides with the overlay and the
outside-click dismissal removes it (fixture 6); the hard cap (13500) stays
the longest timer with the control present (fixture 4's timer-delay
assertion); a failing POST is silent and still hides the control (fixture
9); `.intro-feedback` is absolute-positioned against the `position: fixed`
overlay, no `pointer-events` rule on the live state (`.sent` adds
`pointer-events: none` only). The pre-fix regression claim reproduces
exactly: with the `buildFeedbackControl(story.id)` call removed from a
scratch copy of intro.js, the harness fails precisely the eight feedback
assertions (18 of 26 pass, exit 1); the bird/guard/exit assertions pass
unchanged.

## Review findings (review 6, 2026-08-06)

- **R3-02 re-verified — still accepted as recorded.** The reduced-motion
  assertion is synchronous-only and would pass a storyEnd regression; the
  shipped behaviour is correct (`playStory` returns through `logoOnly()` at
  intro.js:1023 before `storyEnd` exists), so the Low stands with its
  checkbox in the review-3 section.

**Verified good (review 6).** The control appears only for a story with an
id at the logo reveal, never for the local fallback or under reduced motion;
thumbs up/down post the correct triple and hide the control; the delegated
click/keydown `stopPropagation` keeps a thumb tap, a focus click and a
focused-thumb OK away from the document's dismiss listeners (D-6); an
ignored control rides with the overlay and outside-click dismissal removes
it; the hard cap (13500) stays the longest timer with the control present; a
failing POST is silent and still hides the control; `.intro-feedback` is
absolute against the `position: fixed` overlay with no `pointer-events` rule
on the live state (R2-02); the note's `maxLength` is 240, matching
`audienceCommentMax`; only `.intro-stage` is `pointer-events: none`.

## Review findings (review 7, 2026-08-06) — decision reversal: the control blocks

The prod symptom drove the reversal: on the TV the note-to-director control
faded away with the overlay before the audience could write it. The player
now suspends every dismissal path while the control is live. This reverses
decision 7 in the README ("quick and non-blocking") and supersedes every
passage above that describes the ride-with-the-overlay design (Goal, UI
bullet, Behaviour, Integration contract rows, Acceptance criteria, Error
coverage, review 3 "Verified good").

**Changed (commit fe2d1a5, `cmd/serve/frontend/intro.js`).**

- A `feedbackPending` flag, set when the control is built and cleared by
  `send()`. `dismissIntro` returns early while it is set — the schedule, the
  hard cap, outside clicks and keydowns all funnel through `dismissIntro`,
  so one check blocks them all — and `maybeDismiss` defers the handover
  until the block is released; `send()` calls `maybeDismiss()` after
  clearing it.
- The story fetch budget grew from 320 ms to 3 s, and the local fallback is
  now a failure-only last resort (`onerror`/`ontimeout`/backstop) instead
  of a race winner, so a slow TV waits for the real production instead of
  the placeholder.
- A still-loading logo delays `.reveal` until its `load`/`error` event, with
  a 1.5 s backstop (`LOGO_BACKSTOP_MS`, `revealLogo`), on both the
  `storyEnd` and the reduced-motion `logoOnly` paths.

**Tests (review 7).** `intro.test.js` grew from 9 to 12 fixtures; the
harness gained `fireTimer`, `deliverStory`, `markLoaded` and a
still-loading-logo stub. Pre-fix, the new fixtures fail exactly the
assertions encoding the new behavior (fallback race, outside-click block,
hard-cap block, logo delay); post-fix all green:

```
node cmd/serve/frontend_test/intro.test.js   # 35 ok, 0 fail (12 fixtures)
go test ./... -race -count=3 -cover -timeout=30s   # exit 0
go vet ./...                                # clean
node --check cmd/serve/frontend/intro.js     # clean
```

# 2026-08-05: Theatre Splash Fix + Audience Feedback

**Status:** ✅ Complete — all five phases shipped (phase 1 reopened on R3-01 and closed in worker session 6) | [Phase list](#phase-status)

## Summary

Two goals, one worklog. First, fix the splash **render bug** that made the
guards invisible: the player hides every actor until an `enter` beat, and
recent LLM productions never enter the guards — so the audience saw only the
mouse. Second, add an **audience feedback mode**: a quick text + thumbs
control in the intro splash, posting to a new endpoint, stored in a durable
company doc (`audience.json`), injected into the director's and dramaturg's
context next generation so the company improves from what the audience said.

## Strategy

1. **The render fix belongs in the player, not the story.** `model.Story.Validate`
   deliberately repairs what it safely can; auto-adding `enter` beats would
   change authorial intent. The player owns staging: an actor that has beats
   but never receives an `enter` is already on stage at its cast mark. That
   repairs every cached story, LLM or composer, without touching the data.
2. **The playwright prompt gets a nudge.** Future drafts must give every cast
   member an `enter` beat; a character that never enters misses its entrance —
   it stands at its cast mark from the first frame. This stops the pattern at
   the source while the player fix covers the past.
3. **Feedback is a durable company doc.** `audience.json` joins the six existing
   docs under `intro/company/` — same atomic write, same load validation, same
   cap-and-trim. The LLM never writes it and distillation never rewrites it:
   `Theatre.Feedback` is the sole writer, so a submit's `SaveLibrary` can never
   clobber a fresh note with a stale in-memory copy (D-5).
4. **The director and the dramaturg read it.** `withDocsContext` already injects
   the bulletin to every role and per-role docs; the audience doc joins the
   director's and dramaturg's context as a recent excerpt, so the next
   generation adapts to what the audience said.
5. **The contract stays additive.** `agents.Teller` is untouched. A new narrow
   interface (`agents.Feedbacker`) carries the feedback method; the indexer
   type-asserts it, so composer-only mode still records feedback (the director
   reads it on a later model-configured generation).
6. **Feedback does not bypass the cooldown.** A thumbs-down does not trigger an
   instant regeneration; the note lands in the doc, and the cooldown decides
   when the next production reads it. (Decision Q3, settled: no bypass.)
7. **The splash control is quick and non-blocking.** Text field + thumbs
   up/down, appearing during the logo reveal; submits in one tap; the intro
   never waits for it — the control rides with the overlay (outside click
   dismisses as today; the hard cap removes it with the overlay).
8. **Story-data maps in the player are null-prototype.** Any map keyed by
   story ids (`actors`, `props`, `cells` in `playStory`, and the staging
   maps) must be built with `Object.create(null)`: a validator-legal id can
   be `__proto__`, which never becomes an own property on a plain object and
   silently rewrites the map's prototype (R6-01). The staging maps already
   follow this; the three `sc` maps are hardened in the same change that
   closes R6-01.

## Phase Status

| Phase   | Status  | Summary                                                                                                                                                                                                                                                                                                 |
| ------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Phase 1 | ✅ Done | Player stages never-entered actors at their cast mark; playwright prompt nudge; frontend regression test. Reopened on R3-01: prototype-member cast ids break/crash the staging pass; closed in worker session 6 with null-prototype maps + harness fixture — see the phase file and the feedback index. |
| Phase 2 | ✅ Done | Audience doc: `AudienceNote`/`AudienceDoc`, caps, trim, accessors, `Library` extension, director+dramaturg context excerpt                                                                                                                                                                              |
| Phase 3 | ✅ Done | `agents.Feedbacker` + `Theatre.Feedback` + `POST /gallery/intro/feedback` handler + route + tests                                                                                                                                                                                                       |
| Phase 4 | ✅ Done | Splash feedback control (text + thumbs) in `intro.js`, CSS, dismiss interplay, frontend test                                                                                                                                                                                                            |
| Phase 5 | ✅ Done | Quality gate: gofumpt, staticcheck, vet, fix, full `-race -cover -count=3` suite (green on run 4; the two D-P10-2 flakes recorded, green in isolation), dupl (27 groups, byte-identical to HEAD), node harness (26 assertions), AGENTS.md updated (Feedbacker, seven docs, feedback route bullet)       |

Execution order: 1 → 2 → 3 → 4 → 5. Phase 1 is independent of phases 2–4
and may land in parallel; the real chain is 2 → 3 → 4 (doc → endpoint →
control), and phase 5 gates everything.

## Severity Taxonomy

- **Critical**: OOM, data loss, process crash
- **High**: breaks a feature or creates incorrect behavior
- **Medium**: degrades observability or performance
- **Low**: cosmetic

All findings above Low reopen the phase.

## Decisions

- **Q1 — Feedback UI = Option A (splash control).** A text field + a thumbs
  up/down pair, shown in the intro splash during the logo reveal. One tap
  submits. No gallery widget, no second surface.
- **Q2 — Learning loop = Option A (dedicated doc).** A new durable
  `audience.json` doc, injected into the director's and dramaturg's context as
  a recent excerpt. Feedback is not distilled into the director's lessons doc
  (that stays the director's own self-critique).
- **Q3 — No cooldown bypass.** Feedback lands in the doc; the cooldown decides
  when the next production runs. Settled in planning, recorded for the
  executing agent.
- **D-1 — The player owns staging.** The render fix is in `playStory`, not in
  `model.Story.Validate`: an actor with ≥1 beat and 0 `enter` beats is anchored
  at its cast mark and staged at t=0.
- **D-2 — Additive contract.** New `agents.Feedbacker` interface; `Teller`
  unchanged. The indexer type-asserts in the feedback handler.
- **D-3 — Doc cap.** `audienceCap = 40` entries, newest first; comment capped at
  `audienceCommentMax = 240` runes; excerpt `audienceExcerpt = 8`. Trim on every
  write, oldest first, like the other docs.
- **D-4 — Rating is ±1.** Thumbs up = +1, down = −1. The handler rejects
  anything else; the doc validates on load.
- **D-5 — The audience doc is single-writer.** `SaveLibrary` never persists
  `audience.json`; `Theatre.Feedback` is the only writer. The facade keeps one
  persistent `Company` (created in `New`, reused by `loadLibrary`), and
  `Feedback` appends through a compound `Company.AppendAudience` whose
  load-modify-save holds the company's single mutex — a fresh `Company` per
  call would not serialize across calls, so the persistence lives on the
  facade. Distillation round-trips the other six docs only, so a submit can
  never overwrite a fresh note with a stale in-memory copy.
- **D-6 — Keydown containment is a delegated root listener.** The phase-4
  spec's letter stops keydown on the note input; a delegated keydown listener
  on the control root (one listener, same shape as the click containment)
  also covers the TV remote's OK on a focused thumb — OK arrives as a
  keydown, and without containment it would trip the document's keydown
  dismissal. Text entry is covered by the same listener. (Phase 4, executed
  2026-08-05, worker session 4.)
- **D-7 — QA suite runs use `GOTMPDIR` on `/home`; the D-P10-2 flakes are
  recorded, not fixed.** The parallel `-race` link outputs exceed the `/tmp`
  tmpfs (7.5 G, persistently near-full on liten), so this machine's suite
  runs set `GOTMPDIR=/home/imago/.cache/go-tmp`. The two non-green full-suite
  runs each tripped one documented D-P10-2 flake in untouched code
  (`internal/media/storage` `Test_AddToClassificationQueue_rateLimit`,
  `cmd/classify` `TestCommand_Run_no_items_found`); both pass in isolation
  with unchanged flags and are recorded, not fixed, per the phase spec's
  D-P10-2 clause. (Phase 5, executed 2026-08-05, worker session 5.)

- **D-8 — The R3-01 fix stays inside `stageNeverEntered`, per the review's
  prescription.** The two maps become `Object.create(null)` and the
  `hasOwnProperty` guard drops: `for…in` over a null-prototype object yields
  only own keys, so no prototype member can collide and an absent id reads
  `undefined` (falsy). `sc.actors` is deliberately untouched — a cast id is
  an own property there (it shadows any prototype member), and the
  validator already drops beats that reference non-cast ids, so the same
  collision class cannot reach the actor lookup through a validated story.
  The harness fixture covers both R3-01 variants in one story (a
  never-entered `constructor` and a `hasOwnProperty` cast id) and fails on
  pre-fix code with exactly the two new assertions. (Phase 1 rework,
  executed 2026-08-05, worker session 6, imago.)

- **Review 3 (code, 2026-08-05).** Commands re-run from the repo root (suite
  with `GOTMPDIR=/home/imago/.cache/go-tmp`): `go run
mvdan.cc/gofumpt@latest -l .` clean; `go run
honnef.co/go/tools/cmd/staticcheck@latest ./...` clean; `go vet ./...`
  clean; `go fix -diff ./...` no changes; `go test ./... -race -cover
-count=3 -timeout=30s` **green on run 1** (theatre 91.2 %, media 83.0 %,
  cmd/serve 75.6 %, storage 85.6 %); `node
cmd/serve/frontend_test/intro.test.js` 26 assertions pass; `go run
github.com/mibk/dupl@latest -t 80 .` 27 clone groups byte-identical to the
  `git archive HEAD` baseline, none in the touched files. The two D-P10-2
  flake episodes from the phase-5 log were not reproduced — this review's
  full suite was green on the first attempt; the documented class and the
  isolation re-runs are consistent with the phase-5 record and are not
  re-opened. Pre-fix regression claims independently reproduced against
  scratch copies for phases 1 and 4 (removing `stageNeverEntered` fails the
  three guard/exit-first assertions; removing the `buildFeedbackControl`
  call fails the eight feedback assertions; all other assertions pass, exit
  1). Verified good: D-5 single-writer (grep-confirmed — `SaveLibrary` omits
  the audience doc and `AppendAudience` is the only production writer),
  R2-01 serialization (company mutex held across load-prepend-save;
  `loadDocLocked`/`saveDocLocked`/`readJSON`/`writeFileAtomic` take no lock —
  no deadlock), the handler's 405/404/501/400/204 matrix with the forwarded
  triple and zero `Prepare` calls, `storyIDRe` ≡ model `idRe` ≡
  `artifactIDRe`, the route mount under `/gallery`, the delegated
  click/keydown containment, and `stageNeverEntered`'s synchronous build-time
  staging on all normal ids. Verdict: phases 2, 3, 4, 5 ship; **phase 1 is
  reopened on R3-01 (Medium)** — see the feedback index and the phase file.

- **Review 5 (holistic, 2026-08-05) — worklog re-verified, all five phases
  still ship.** Re-ran the full gate from the repo root: `go run
mvdan.cc/gofumpt@latest -l .` clean; `go run
honnef.co/go/tools/cmd/staticcheck@latest ./...` clean; `go vet ./...`
  clean; `go fix -diff ./...` no changes; `go build ./...` ok; full
  `GOTMPDIR=/home/imago/.cache/go-tmp go test ./... -race -cover -count=3
 -timeout=30s` **green on run 1** (theatre 91.2 %, media 83.0 %,
  cmd/serve 75.6 %, storage 85.6 % — matching the phase-5 / review-3 /
  review-4 records); node harness **28 assertions pass**; dupl 27 clone
  groups byte-identical to the `git archive HEAD` baseline. Pre-fix
  regression claims independently reproduced against scratch copies
  (removing `stageNeverEntered` fails exactly the staging assertions;
  reverting the null-prototype maps to plain objects fails exactly the two
  prototype-name assertions, 26 pass; removing the `buildFeedbackControl`
  call fails exactly the eight feedback assertions). Verified good: D-5
  single-writer, R2-01 serialization, the handler matrix, `storyIDRe` ≡
  model `idRe` ≡ `artifactIDRe`, the `/gallery` route mount, the delegated
  click/keydown containment, the playwright prompt pin, the audience doc's
  trim/append/excerpt behaviour. Findings: **R5-01 (Low, phase 2)** — the
  audience excerpt renders a thumbs-down note as `[+-1]` (`docs.go:655`,
  format `[+%d]`; the sign-aware `[%+d]` renders `[+1]`/`[-1]`); cosmetic,
  LLM-facing only, non-blocking, recorded with a checkbox in the phase file.
  R3-02 (Low, phase 4) remains accepted as recorded (the reduced-motion
  assertion-strength nit; behaviour correct — `logoOnly` returns before
  `storyEnd` exists). Verdict: the worklog ships complete; no phase reopens
  on this round.

- **Review 6 (code, 2026-08-06) — re-verified the complete worklog; all
  five phases still ship.** Re-ran the full gate from the repo root with the
  same flags and `GOTMPDIR` as the phase-5 record: gofumpt `-l` clean,
  staticcheck clean, `go vet ./...` clean, `go fix -diff ./...` no changes,
  the full `-race -cover -count=3 -timeout=30s` suite **green on runs 2–3**
  (run 1 flaked `cmd/classify` — the documented D-P10-2 family, same
  package the phase-5 record documents; passed in isolation with identical
  flags; the exact test name was not captured, the recorded tail showed
  only flag-parse noise from a passing test; runs 2–3 green — theatre
  91.2 %, media 83.0 %, cmd/serve 75.6 %, storage 85.6–85.8 %, matching
  the phase-5 / review-3/4/5 records), node harness **28 assertions pass**,
  dupl 27 clone groups byte-identical to a fresh `git archive HEAD` scratch
  run (none in the touched files). Verified good: the stageNeverEntered
  invariants (synchronous build-time staging, zero-beat hidden, exit-first
  staged, unknown-character skip, R3-01 fixture), the playwright prompt pin,
  D-5 single-writer (SaveLibrary omits the audience doc; the stale-nil test
  proves a note survives a library save), R2-01 serialization, the handler
  matrix with the forwarded triple and zero `Prepare` calls, `storyIDRe` ≡
  `model.idRe` ≡ `artifactIDRe`, the `/gallery` route mount, the delegated
  click/keydown containment, the audience doc's trim/append/excerpt
  behaviour, and the AGENTS.md contract updates. Findings:
  - **R6-01 (Low, phase 1)** — D-8's sc.actors rationale is false for the
    one id that never becomes an own property: `__proto__` (validator-legal
    under `^[a-z0-9_]{1,24}$`). The cast loop's `sc.actors[a.id] = a`
    invokes the inherited `__proto__` setter and replaces the map's
    prototype with the actor object. Reproduced against the real player:
    the `__proto__` cat is staged at its cast mark and the show plays
    through its beats — lookups resolve through the `__proto__` getter to
    the very actor stored as the prototype, and the validator guarantees
    beats/targets reference only real ids, so no reachable path breaks
    today. Correct behaviour, inaccurate rationale: the corrupted prototype
    is a trap for any future iteration over `sc.actors`. Checkboxed:
    harden the three `sc` maps to `Object.create(null)` or correct the
    D-8 note. Non-blocking.
  - **R5-01 (Low, phase 2)** — re-verified still open: docs.go:655 still
    renders a thumbs-down as `[+-1]` (`[+%d]` verb). Cosmetic, LLM-facing,
    non-blocking; the checkbox in the phase file is the fix.
  - **R3-02 (Low, phase 4)** — re-verified still accepted as recorded;
    behaviour correct, assertion-strength nit only.
  Verdict: the worklog ships complete; no phase reopens on this round.

## Session Journal

### Phase 1 rework — R3-01 null-prototype maps (executed 2026-08-05, worker session 6, imago)

Closed the reopened phase-1 finding. `stageNeverEntered` now builds its two
maps with `Object.create(null)` and drops the `hasOwnProperty` guard, exactly
per the review's prescription (D-8); harness fixture 4 adds a story whose
cast ids are `constructor` and `hasOwnProperty` with never-enter beats.
Details in [phase-1-splash-render-fix.md](./phase-1-splash-render-fix.md).

**Material implementation decisions (this session).**

- D-8 — the fix is confined to `stageNeverEntered`; `sc.actors` stays a
  plain object (own cast-id properties shadow prototype members; the
  validator drops non-cast beat actors).
- The fixture runs both R3-01 variants in one story: the synchronous
  assertion requires both cats staged at their cast marks ('80px'/'400px'),
  which fails on pre-fix code with exactly the two new assertions (the
  crash variant aborts the build, so neither cat is staged and pip never
  enters), and the mid-show assertion proves beats still schedule after the
  staging pass.

**Tests (before: all green; after: all green).**

```
node cmd/serve/frontend_test/intro.test.js   # before fix: 26 ok; with fixture on pre-fix code: 26 ok + 2 FAIL, exit 1; after fix: 28 ok
```

Pre-fix verification: the new fixture fails exactly the two new assertions
on the pre-fix player (26 existing assertions pass unchanged, exit 1) — the
regression test fails on pre-fix code, as the review demands. Full QA gate
over the whole worklog: see phase 5 and the review-3 re-run below.

### Phase 4 — Splash Feedback Control (executed 2026-08-05, worker session 4, imago)

Implemented per the plan, including review findings R2-02 (no `pointer-events`
rule on the overlay or the control — the control is appended to the overlay,
clickable by default, and the delegated `stopPropagation` is the only click
containment) and R2-03 (no control under reduced motion: `playStory` returns
early through `logoOnly()`, so the `storyEnd` callback never runs). Details in
[phase-4-splash-feedback-control.md](./phase-4-splash-feedback-control.md).

**Material implementation decisions (this session).**

- D-6 — keydown containment is a delegated root listener, not input-only:
  it also covers the TV remote's OK on a focused thumb (a keydown that would
  otherwise trip the document's keydown dismissal). One listener, a superset
  of the spec.
- The thumbs are unicode escapes (`\uD83D\uDC4D`/`\uD83D\uDC4E`) so the
  source stays ASCII; SVG is the riskier choice on the old Blink builds
  (the intro-stage CSS comment warns about SVG transforms), and the spec
  permits either.
- The "never extends the splash schedule" AC is pinned by recording every
  timer delay in the harness and asserting the hard cap (13500) is still the
  longest timer with the control present.

**Tests (before: all green; after: all green).**

```
node cmd/serve/frontend_test/intro.test.js   # before: 13 ok; after: 26 ok
go build ./...                               # ok
go vet ./internal/agents/... ./internal/media/    # clean
go test ./internal/agents/theatre/ ./internal/media/ -count=1   # ok
```

Pre-fix verification: with the `buildFeedbackControl(story.id)` call removed
from the `storyEnd` callback, the harness fails exactly the eight feedback
assertions (exit 1) and the existing bird/guard/exit assertions pass
unchanged — the regression test fails on pre-fix code, as the AC demands.

### Phase 3 — Feedback Endpoint (executed 2026-08-05, worker session 3, imago)

Implemented per the plan, including review finding R2-01: the facade owns one
persistent `Company` (`New` sets `t.company = Open(cacheDir)`; `loadLibrary`
reads through it), and `Feedback` delegates to the compound
`Company.AppendAudience`, so two concurrent posts serialize on the company's
mutex — no cross-domain facade lock. Details in
[phase-3-feedback-endpoint.md](./phase-3-feedback-endpoint.md).

**Material implementation decisions (this session).**

- The handler mirrors `recomendHandler`'s strict JSON convention
  (`io.LimitReader` + `DisallowUnknownFields`) — one JSON convention per
  file, not a second one invented for feedback.
- `Feedback` trims the story id before the regex; comment truncation stays
  in `trimAudience` only, never duplicated at the facade.
- The 501 test row reuses `recordingTeller` (Teller, not Feedbacker); its
  buffered `deadline` channel means an erroneous `Prepare` call cannot
  deadlock the table test.

**Tests (before: all green; after: all green).**

```
go test ./internal/agents/theatre/ ./internal/media/ -count=1   # before and after: ok
go build ./...                                                   # ok
go vet ./internal/agents/... ./internal/media/                   # clean
staticcheck ./internal/agents/... ./internal/media/              # clean
go test ./... -race -count=1 -timeout=300s                       # all packages ok
node cmd/serve/frontend_test/intro.test.js                       # 13 ok (frontend untouched)
```

### Phase 2 — Audience Doc (executed 2026-08-05, worker session 2)

Implemented per the plan; no deviations from the specification or decisions
D-3/D-5 and review finding R2-01.

**Changes.**

- `internal/agents/theatre/docs.go` — `AudienceNote`/`AudienceDoc` types;
  `Library.Audience`; `trimAudience` (id pattern, ±1 rating, comment
  truncate-not-reject, date trim, duplicate collapse, cap);
  `LoadAudience`/`SaveAudience`; the compound `AppendAudience` — the doc's
  single write path, load-prepend-trim-save under the company's one mutex;
  `LoadLibrary` reads the audience doc, `SaveLibrary` deliberately does not
  (D-5); `AudienceDoc.context()` headed excerpt.
- `internal/agents/theatre/constants.go` — `audienceCap = 40`,
  `audienceCommentMax = 240`, `audienceExcerpt = 8` (D-3).
- `internal/agents/theatre/company.go` — `audienceFileName = "audience.json"`.
- `internal/agents/theatre/runner.go` — `withDocsContext` appends the
  audience excerpt to the director's and dramaturg's prompts; the other roles
  never read it.
- `internal/agents/theatre/docs_test.go` — round-trip incl. the
  SaveLibrary-untouched assertion; corrupt `audience.json` degrades to empty
  with the error logged; hostile-content trim gates; cap newest-first;
  `AppendAudience` prepend + cap + two concurrent appends lose no note;
  context-excerpt cases for director/dramaturg (have) and
  playwright/scenographer/wardrobe (not-have).

**Material implementation decisions.**

- `loadDoc`/`saveDoc` split into mutex-taking wrappers plus `loadDocLocked`/
  `saveDocLocked` so `AppendAudience` can hold the company's single mutex
  across its read-modify-write without double-locking (R2-01). Existing
  accessor behaviour is unchanged.
- The trim gate keeps the doc's file order (newest first, as
  `AppendAudience` maintains); the cap test fixture is therefore built
  newest-first, mirroring the premise-cap test's convention.

**Tests (before: all green; after: all green).**

```
go test ./internal/agents/theatre/          # before and after: ok
go test ./internal/agents/theatre/ -race -count=1   # ok
go vet ./...                                        # clean
staticcheck ./internal/agents/theatre/              # clean
gofumpt -l docs.go constants.go company.go runner.go docs_test.go  # no output
```

Full suite after the change: `go test ./...` — all packages ok.

### Phase 1 — Splash Render Fix (executed 2026-08-05, worker session 1)

Implemented per the plan; no deviations from the specification or decisions
D-1/R2-04.

**Changes.**

- `cmd/serve/frontend/intro.js` — `stageNeverEntered(sc, beats)` helper in the
  PLAYER section; called from `playStory` after the cast loop, before beats
  are scheduled (synchronous, so it cannot race a t=0 beat). `makeActor`'s
  reveal comment now names the second staging path.
- `internal/agents/theatre/roles.go` — `playwrightPrompt` gains the enter
  rule (every cast member must enter; a never-entered character stands at its
  cast mark). No Go runtime logic changed.
- `internal/agents/theatre/roles_test.go` — pin added to
  `TestRolePrompts_ScopeTextPinned` (needle mid-sentence, case-safe).
- `cmd/serve/frontend_test/intro.test.js` — harness refactored from one
  fixture to three via `runStory(storyJSON)` (playStory is a singleton, so
  each fixture gets fresh stubs). Fixtures: existing bird/chirp (plus
  zero-beat-hidden assertion), the production guard shape, and an exit-first
  actor + unknown-character skip.

**Material implementation decisions.**

- Staging happens in the build path, not in a timer: the guard fixture
  asserts it synchronously right after `runStory` returns, before any beat
  timer can fire.
- The zero-beat hidden case is asserted on the existing fixture's cat (ina),
  which has no beats — no fourth fixture needed for that AC.
- The unknown-character case (error-coverage row 4) rides the exit-first
  fixture as a `dragon` cast member; the staging pass must skip it without a
  panic.

**Tests (before: all green; after: all green).**

```
go test ./internal/agents/theatre/          # before and after: ok
node cmd/serve/frontend_test/intro.test.js  # before: 6 ok; after: 13 ok
```

Pre-fix verification: with `stageNeverEntered` temporarily removed, the
harness fails exactly the three guard/exit-first assertions and the
bird/chirp assertions pass unchanged (regression test fails on pre-fix code,
as the AC demands).

**Full suite after the change.**

```
go test ./internal/agents/theatre/ -race -count=1   # ok
go vet ./internal/agents/theatre/                    # clean
go test ./...                                        # all packages ok
go run mvdan.cc/gofumpt@latest -w -l internal/agents/theatre/roles.go internal/agents/theatre/roles_test.go  # no output, clean
```

### Phase 5 — Quality Gate (executed 2026-08-05, worker session 5, imago)

Ran the full QA gate over phases 1–4; all seven commands pass and are
recorded with exact outputs in
[phase-5-quality-gate.md](./phase-5-quality-gate.md). The full suite
`go test ./... -race -cover -count=3 -timeout=30s` went green on the fourth
run; the two earlier non-green runs each tripped one documented D-P10-2
family flake in untouched code — `internal/media/storage`
`Test_AddToClassificationQueue_rateLimit` and `cmd/classify`
`TestCommand_Run_no_items_found` — both green in isolation with unchanged
flags, recorded per the phase spec's D-P10-2 clause. First run hit the
`/tmp` tmpfs quota (parallel `-race` link outputs), so the suite runs use
`GOTMPDIR=/home/imago/.cache/go-tmp`. Coverage: theatre 91.2 %, media 83.0 %,
cmd/serve 75.6 % — above the 70 % floor, theatre at the 90 % preferred mark.
dupl: 27 clone groups byte-identical to the HEAD baseline (`git archive`
scratch run) — no new groups. Node harness: 26 assertions pass. AGENTS.md
updated per R2-05 and the changed-contract clause: the package map now lists
`Feedbacker`, counts seven durable docs, names `Feedback` on the facade, and
the key insights gained a feedback bullet (splash control →
`POST /gallery/intro/feedback` → `agents.Feedbacker` → `audience.json`,
director/dramaturg excerpt, no cooldown bypass). No production code or tests
changed in this phase.

## Feedback Index

- **Review 1 (design, 2026-08-05) — no phase started.** Reworked the plan
  text. Fixed: the prompt-nudge rationale ("never entered is never seen"
  becomes false once the player fix lands — it is "misses its entrance"); the
  garbled cooldown sentence; the dead control timeout (the splash hard cap
  already bounds the control; a fixed 6–8 s timer would fire only for short
  stories or never for long ones); the missing input-focus click stop (a
  click into the text field would trip the document `click → dismissIntro`
  listener); the "other five docs" miscount (six); and the dependency claim
  (phase 1 is independent of phases 2–4). Added decision D-5: the audience
  doc is single-writer — `SaveLibrary` never persists it, so distillation
  cannot clobber a fresh note with a stale in-memory copy, and
  `Theatre.Feedback` serializes its load-modify-save on the facade's
  `writeMu`.

- **Review 2 (design, 2026-08-05) — no phase started.** Verified the
  post-Review-1 plan against the code at the cited seams: intro.js
  `playStory`/`makeActor`/`logoOnly`, style.css `.intro-overlay`/`.intro-stage`,
  story.go `Validate`, docs.go six-doc `SaveLibrary`, runner.go
  `withDocsContext`, theatre.go facade, serve_setup.go `/gallery` mount,
  artifacts.go `artifactIDRe`, the node harness. No QA gates run — no code
  changed; the plan text is the change. Verdict: sound and ready to implement
  once the findings below are folded in.
  - **R2-01 (Medium, phases 2–3)** — serializing the audience append on the
    facade's `writeMu` couples two persistence domains and makes every reader
    carry the "a fresh `Company` per call does not serialize" gotcha. Simpler:
    the facade keeps one persistent `Company` and `Feedback` calls a compound
    `Company.AppendAudience`; the company's own single mutex then holds across
    the load-modify-save, matching the invariant documented on `Company`.
    Plan text updated (D-5, phase 2, phase 3).
  - **R2-02 (High, phase 4)** — the pointer-events parenthetical contradicts
    the CSS: `.intro-overlay` has no `pointer-events` rule (a click on it
    bubbles to the document dismiss listener — that is today's outside-click
    dismissal); only `.intro-stage` is `pointer-events: none`
    (style.css:1002). A literal implementation would add a rule that changes
    dismissal semantics (clicks fall through to gallery links beneath).
    Corrected: the control is appended to the overlay, clickable by default,
    needs no `pointer-events` rule.
  - **R2-03 (Medium, phase 4)** — the reduced-motion error row claims the
    control "still shows", but `playStory` returns early through `logoOnly()`
    (intro.js:924) under reduced motion: no story, no `storyEnd`, overlay
    dismissed ~1.3 s after load. Corrected: no control under reduced motion;
    the row now splits low-perf (control shows) from reduced-motion (no
    control), with a harness assertion.
  - **R2-04 (Low, phase 1)** — "no … any Go runtime behaviour" half-
    contradicted "the only Go-side change is the playwright prompt string":
    the prompt is Go code that deliberately changes LLM output. Rephrased: no
    Go runtime logic changes; the prompt is data.
  - **R2-05 (Low, phase 5)** — the quality-gate AC only said "AGENTS.md is
    updated", which is exactly where this plan's staleness will hide: the
    package map's "six durable docs" (AGENTS.md:65) and the library bullet's
    "six durable company docs" (AGENTS.md:167) both enumerate the company
    library, and both are wrong once `audience.json` exists. AC sharpened to
    name both lines.

- **Review 3 (code, 2026-08-05) — phases 1–5 re-reviewed, phase 1 reopened.**
  Independent re-run of the full gate plus a code trace. All seven QA
  commands pass (gofumpt `-l` clean, staticcheck clean, vet clean, `go fix
-diff` no changes, full `-race -cover -count=3 -timeout=30s` suite **green
  on run 1** with `GOTMPDIR=/home/imago/.cache/go-tmp` — theatre 91.2 %,
  media 83.0 %, cmd/serve 75.6 %, matching the phase-5 record — node harness
  26 assertions, dupl 27 groups byte-identical to the `git archive HEAD`
  baseline, none in the touched files). Both pre-fix regression claims
  reproduce exactly against scratch copies. R2-01…R2-05 all verified in the
  code. Findings:
  - **R3-01 (Medium, phase 1 → Reopened)** — `stageNeverEntered`'s plain
    object maps break for `Object.prototype`-member cast ids (validator-legal,
    LLM-reachable): id `constructor` is silently left hidden (the phase's own
    bug persists); id `hasOwnProperty` throws mid-build and the show silently
    never plays (swallowed by `begin()`'s catch). Fix: null-prototype maps +
    harness fixtures. Details and reproduction in
    [phase-1-splash-render-fix.md](./phase-1-splash-render-fix.md).
  - **R3-02 (Low, phase 4)** — the reduced-motion harness assertion is
    synchronous-only and would pass a storyEnd regression; move it past
    `storyEnd`. Non-blocking.

- **Review 4 (holistic, 2026-08-05) — phase 1 closed, worklog complete.**
  After the R3-01 rework (worker session 6), re-ran the full gate from the
  repo root: gofumpt `-l` clean; staticcheck clean; `go vet ./...` clean;
  `go fix -diff ./...` no changes; `go build ./...` ok; full
  `GOTMPDIR=/home/imago/.cache/go-tmp go test ./... -race -cover -count=3
-timeout=30s` **green on run 1** (theatre 91.2 %, media 83.0 %,
  cmd/serve 75.6 %, storage 85.8 % — matching the phase-5 and review-3
  records, no D-P10-2 flake this run); node harness **28 assertions** pass
  (26 + the two new prototype-name checks); dupl 27 clone groups, same
  count as the review-3 baseline (the rework touches only JS + worklog
  docs, so no Go clones can shift). The pre-fix regression claim for the
  rework reproduces exactly: with the fixture added against the pre-fix
  player, the harness fails precisely the two new assertions (26 pass,
  exit 1). Holistic verdict: the worklog ships complete — all five phases
  done, phase 1's reopened R3-01 closed. R3-02 (Low, phase 4) remains
  accepted as recorded by review 3: it is a harness-assertion-strength nit
  on the reduced-motion path, explicitly non-blocking, and the reduced-
  motion path never builds the control (logoOnly returns before `storyEnd`
  exists), so the shipped behaviour is correct; the assertion could be
  strengthened later without touching production code.

- **Review 5 (holistic, 2026-08-05) — re-verified the complete worklog; all
  five phases still ship.** Re-ran the full gate from the repo root with the
  same flags and `GOTMPDIR` as the phase-5 record: gofumpt `-l` clean,
  staticcheck clean, `go vet ./...` clean, `go fix -diff ./...` no changes,
  `go build ./...` ok, full `-race -cover -count=3 -timeout=30s` suite
  **green on run 1** (theatre 91.2 %, media 83.0 %, cmd/serve 75.6 %,
  storage 85.6 % — matching the phase-5 / review-3 / review-4 records), node
  harness **28 assertions pass**, dupl 27 clone groups byte-identical to the
  `git archive HEAD` baseline. Pre-fix regression claims independently
  reproduced against scratch copies: removing `stageNeverEntered` fails
  exactly the staging assertions (guards staged, prototype-name staged,
  exit-first staged, guards stay staged, story still plays); reverting the
  null-prototype maps to plain objects fails exactly the two prototype-name
  assertions (26 pass, exit 1); removing the `buildFeedbackControl` call
  fails exactly the eight feedback assertions. Verified good: D-5
  single-writer (grep-confirmed), R2-01 serialization (no deadlock), the
  handler's 405/404/501/400/204/500 matrix with the forwarded triple and
  zero `Prepare` calls, `storyIDRe` ≡ model `idRe` ≡ `artifactIDRe`, the
  `/gallery` route mount, the delegated click/keydown containment, the
  playwright prompt pin, and the audience doc's trim/append/excerpt
  behaviour. Findings:
  - **R5-01 (Low, phase 2)** — `AudienceDoc.context()` renders a
    thumbs-down note as `[+-1]` (docs.go:655, format `[+%d]`); the
    sign-aware verb `[%+d]` renders `[+1]`/`[-1]`. Cosmetic only (LLM-facing
    text; the rating remains parseable), non-blocking — recorded with a
    checkbox in phase-2-audience-feedback-doc.md.
  - **R3-02 (Low, phase 4)** — re-verified: remains accepted as recorded;
    the reduced-motion path never builds the control and the assertion
    could be strengthened later without touching production code.

- **Review 6 (code, 2026-08-06) — all five phases re-verified; still ship,
  no reopen.** Full gate re-run (gofumpt/staticcheck/vet/fix clean, the
  `-race -cover -count=3 -timeout=30s` suite green on runs 2–3 — run 1
  flaked the documented D-P10-2 `cmd/classify` class, passed in isolation —
  node harness 28 assertions, dupl 27 groups = HEAD baseline). Findings:
  - **R6-01 (Low, phase 1)** — D-8's sc.actors rationale is false for
    `__proto__`: the id is validator-legal and the assignment silently
    replaces the map's prototype (verified: the show still plays, so this is
    a rationale/robustness gap, not a reachable defect). Checkboxed:
    harden the three `sc` maps to `Object.create(null)` or correct the
    D-8 note. See [phase-1-splash-render-fix.md](./phase-1-splash-render-fix.md).
  - **R5-01 (Low, phase 2)** — re-verified still open (`[+%d]` at
    docs.go:655 renders a thumbs-down as `[+-1]`).
  - **R3-02 (Low, phase 4)** — re-verified still accepted as recorded.

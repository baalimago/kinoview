# Phase 14 — Review-7 Fixes (addendum)

**Status:** ✅ Complete | [README](./README.md)

## Goal

Close the findings of review 7 (2026-08-03). The review re-ran the gates and
found two High correctness defects that reopened phases 4 and 6: R7-01 (the
exhaustion path ships any readable draft, validated or not) and R7-02 (submit
ignores story-persistence failure). This is an addendum phase: both fixes are
small and cross the phases the review annotated — 4 (director orchestrator,
`finish` resolution, `submit_story`) and 6 (the submit-persistence boundary
guarding distillation) — so they are consolidated here instead of reopening
two phase files. The board rows point at this phase.

## Work items (one checkbox per finding; close in order)

- [x] **R7-01 — exhaustion ships an unvalidated draft (High).**
      `internal/agents/theatre/director.go` `finish` selected the
      readable-working-file branch on `werr == nil` alone, so any playable
      draft — status `draft`, never blessed by `validate_story` — was
      persisted and shipped as "the last validated draft" when the director
      hit its call or wall-clock limit before validating. The phase-4
      contract promised the composer floor (or an earlier validated
      snapshot) in exactly that case.
      Fix: an explicit `Working.Validated` flag. `validate_story` is the only
      writer of the blessing; every writer that rewrites the draft
      (`write_draft`, `write_scene`, `fallbackDraft`, `fallbackScene`) clears
      it, so the flag means "this exact working content passed the gate".
      `finish`'s exhaustion branch requires `w.Validated`; a readable but
      unvalidated draft emits a warning note and falls through to the
      composer floor with the specific error "the draft was never
      validated". → **Fixed**, with regression tests
      `TestTheatre_DraftOnlyExhaustionFallsToComposer` (draft-only
      exhaustion) and `TestTheatre_ValidatedThenRewrittenDraftDoesNotShip`
      (a rewrite after validation clears the blessing); both fail on the
      pre-fix code. The existing exhaustion test
      (`TestTheatre_BudgetExhaustionShipsLastValidatedDraft`) and the
      wall-clock test (`TestTheatre_WallClockRefusesSpawnsPastDeadline`)
      pass unchanged — both validate before exhausting.

- [x] **R7-02 — submit ignores story persistence failure (High).**
      `submitStory` called `Theatre.saveStory` without an error result while
      `theatre.go` logged marshal/mkdir/temp/rename failures and returned
      nothing, then marked `working.json` submitted and ran distillation —
      paperwork claimed success while `intro_story.json` was absent or
      stale.
      Fix: `saveStory` returns its error, unlogged (the house "saves return
      their errors unlogged — the caller must act on them" pattern, company.go
      `logLoadFailure`). The fire-and-forget callers — `Next`, `Prepare`,
      `Warm` and `finish`'s exhaustion branch — log it and keep serving from
      memory (the splash must not depend on disk health); `submit_story`
      aborts: the story must be durably on disk before the submitted
      transition or distillation, and the director receives
      "submit refused: story not persisted: …". → **Fixed**, with the
      failure-injection regression test
      `TestTheatre_SubmitAbortsWhenStoryNotPersisted` (`intro_story.json` is
      a directory, so the atomic rename fails while the company paperwork
      stays writable): the submit returns the refusal, the working file is
      not marked submitted and no library doc is distilled. The existing
      `TestTheatre_SaveStoryWriteFailureLogged` became
      `TestTheatre_SaveStoryWriteFailureReturnsError`, asserting the
      returned error instead of the internal log line.

## Implementation notes

Executed by imago, 2026-08-03 session (phase 14 of the playwright-company
worklog).

| File | Change |
|---|---|
| `working.go` | `Working` gains `Validated bool` (`json:"validated,omitempty"`) — the explicit blessing, documented as the R7-01 gate |
| `director.go` | `finish`: the exhaustion branch requires `w.Validated`; a readable-but-unvalidated draft emits a warning note and falls through to the composer floor ("the draft was never validated"); the submitted branch is unchanged. `validateStory` sets `w.Validated = true`. `submitStory` aborts on `saveStory` error before the submitted transition and distillation |
| `theatre.go` | `saveStory` returns `error` (marshal/mkdir/temp/write/close/rename causes wrapped, no internal logging); `Next`/`Prepare`/`Warm` log the error and continue |
| `roles.go` | `write_draft` and `write_scene` clear `w.Validated` — a rewrite loses the blessing |
| `fallback.go` | `fallbackDraft` and `fallbackScene` clear `w.Validated` — the deterministic floors are writers too |
| `theatre_test.go` | Two R7-01 regression tests, one R7-02 failure-injection test, `TestTheatre_SaveStoryWriteFailureLogged` reworked to assert the returned error |
| `AGENTS.md` | Theatre insight updated: the exhaustion gate is the explicit `Validated` flag, and `saveStory`'s error crosses the submit boundary |
| worklog | Phase 4 and 6 rows closed via phase 14; strategy items 20/21 and the feedback index rows updated |

## Material decisions (recorded for chronology)

- **D-P14-1 — the validated blessing is an explicit flag, not a status
  check.** The finding suggested `w.Status == "validated"`, but the status
  label cannot carry the meaning: `pin_identity` re-labels a validated file
  to `pinned`, and a director that validates then re-dresses lands on
  `dressed` — both would wrongly refuse (or, with a permissive status set,
  wrongly admit) the exhaustion gate. A dedicated `Validated` flag set only
  by `validate_story` and cleared by every draft-rewriting writer is exact:
  it means "this exact working content passed the gate". The flag rides the
  working file like `Brief` and `Dressed`, inside the same trust model (the
  stage manager owns the file).
- **D-P14-2 — `saveStory` returns its error unlogged; callers decide.** The
  house pattern for saves is "return errors unlogged, the caller must act"
  (`logLoadFailure` in company.go documents it). `Next`/`Prepare`/`Warm`/
  `finish`-exhaustion log and keep serving from memory — the splash must not
  depend on disk health — while `submit_story` aborts, because a submission
  that was not persisted must not claim success. Logging stayed inside
  `saveStory` would double-report every failure (the log plus the tool
  message); returning the error keeps one report per path.

## Validation (exact commands and results)

| Command | Result |
|---|---|
| `go test ./internal/agents/theatre/ -count=1 -timeout=120s` (before changes) | pass — baseline green |
| `go test ./internal/agents/theatre/ -count=1 -timeout=120s` (after changes) | pass — 180 top-level tests (198 with subtests), including the three new regression tests and every pre-existing exhaustion/wall-clock/composer-floor test unchanged |
| `go test ./internal/agents/theatre/ -run 'TestTheatre_(DraftOnlyExhaustionFallsToComposer\|ValidatedThenRewrittenDraftDoesNotShip\|SubmitAbortsWhenStoryNotPersisted\|BudgetExhaustionShipsLastValidatedDraft\|FixtureProductionRunsFlow\|SubmitTwiceRefused\|SubmitRefusesInvalidDraft\|WallClockRefusesSpawnsPastDeadline\|WorkingFileUnreadableShipsComposer\|SaveStoryWriteFailureReturnsError)' -v` | 10/10 pass |
| `go test ./... -count=1 -timeout=300s` | pass — full suite |
| `go test ./internal/agents/theatre/... -race -count=1 -timeout=300s` | pass — no races |
| `go test ./... -race -count=1 -timeout=300s` | pass — full race suite |
| `go run mvdan.cc/gofumpt@latest -l .` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/` | 0 clone groups |

## Acceptance check

Both findings are closed: `finish` ships a submitted story, a validated
draft, or the composer floor — never an unvalidated draft
(`TestTheatre_DraftOnlyExhaustionFallsToComposer`,
`TestTheatre_ValidatedThenRewrittenDraftDoesNotShip`); `submit_story`
persists the story before the submitted transition and distillation, and
aborts both on persistence failure with the refusal returned to the director
(`TestTheatre_SubmitAbortsWhenStoryNotPersisted`). The pre-existing
guarantees hold unchanged: the last-validated-draft path, the wall-clock
gate, the composer floor and the fire-and-forget save paths all pass their
original tests, and the full QA gate is green including `-race`.

## Review findings

None filed for this phase.

**Review 8 (2026-08-03) verified good:** the R7-01 gate was traced through
every branch — `validate_story` is the only writer of `Working.Validated`
and all four draft-rewriting writers clear it (`write_draft` roles.go:264,
`write_scene` roles.go:312, `fallbackDraft` fallback.go:125,
`fallbackScene` fallback.go:162); `finish` ships a submitted story, a
validated draft, or the composer floor with the "the draft was never
validated" cause, never an unvalidated draft. The R7-02 boundary was
traced through the failure branch: `saveStory` returns its error unlogged
(marshal/mkdir/temp/write/close/rename causes wrapped), `submit_story`
aborts before the submitted transition and distillation, and the
fire-and-forget callers (`Next`/`Prepare`/`Warm`/finish-exhaustion) log and
keep serving from memory. All three regression tests plus the pre-existing
exhaustion/wall-clock/composer-floor tests re-run green under the round-8
gates.

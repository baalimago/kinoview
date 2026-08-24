# Phase 0 — Deprecate the old theatre

**Status:** ✅ Complete (session 2026-08-23)
[← README](./README.md)

## Goal

Remove the fixed-vocabulary theatre end to end so that no implementer of the
troupe ever reads it. The build and `make qa` stay green with no intro splash:
the frontend mounts an empty `<div id="troupe">` and serves nothing until the
troupe submits a play.

This is a pure deletion phase. Nothing troupe-shaped lands here.

## Removal list (grounded in the current tree)

Go:

```text
internal/agents/theatre/           # the whole package: director, roles, composer
                                   #   (floor/fallback/staging/muse), runner, broker,
                                   #   stage, feed, dialog, collab, artifacts, company,
                                   #   atomic, ledger, transcript, working, schema,
                                   #   constants, tools/, testdata, all _test.go
internal/model/story.go            # model.Story + Scene/Cast/Prop/Beat/Cell +
                                   #   the closed Valid* vocabularies + Validate
internal/agents/interfaces.go      # drop agents.Teller + agents.Feedbacker (keep the rest)
internal/media/index_handlers.go   # introStoryHandler, introSessionEndHandler,
                                   #   introFeedbackHandler, storyIDRe,
                                   #   introFeedbackRequest, prepareNextStory
internal/media/index.go            # theatre + feedback fields, WithTheatre +
                                   #   WithFeedbacker options, the three /intro/* routes
internal/agents/slivingdoc/feedback.go  # FeedbackRecorder + feedback.jsonl +
                                   #   feedbackRecord (decision 21 retires it)
cmd/serve/serve.go                 # -theatre, -theatreCooldown, -theatreMaxCalls,
                                   #   -theatreWallClock, -theatreGlobalCalls flags + fields
cmd/serve/serve_setup.go           # bard construction + bard.Warm + media.WithTheatre +
                                   #   feedbackRecorder wiring + media.WithFeedbacker
cmd/debug/production.go            # theatre production dialog renderer (and its registration)
```

Frontend:

```text
cmd/serve/frontend/intro.js        # the whole splash player
cmd/serve/frontend_test/intro.test.js  # its headless Node test
cmd/serve/frontend/index.html      # intro overlay markup + intro.js <script> →
                                   #   an empty <div id="troupe"></div>
cmd/serve/frontend/style.css       # every .intro-* rule
cmd/serve/frontend/index.js        # window.__introMarkLoaded/__introMarkFailed,
                                   #   skip-intro wiring
```

The `slivingdoc.Notebook` seam itself stays: it is the materialise → edit →
commit unit the troupe (and later the new feedback handler) rides on. Only the
`feedback.jsonl`-shaped recorder is retired here; the new per-note `feedback/`
envelope arrives in Phase 9.

## Implementation notes

1. Delete the theatre package first so any remaining reference is a compile
   error, then follow the compiler until the tree is green.
2. Remove the two interfaces from `internal/agents/interfaces.go`; update their
   only implementers (the theatre facade, `slivingdoc.FeedbackRecorder`) and
   the test stubs that mirror them (`recordingTeller`, `feedbackRecorder` in
   `internal/media/index_handlers_test.go`).
3. Replace the intro overlay markup in `index.html` with an empty
   `<div id="troupe">`. Nothing mounts into it until Phase 9.
4. Update `cmd/serve/serve_setup_test.go`, `cmd/serve/serve_test.go` and the
   indexer tests to drop theatre/feedback expectations. The `withTheatre`/`withFeedback`
   pins become `withTroupe` placeholders later — for now, remove them.
5. Remove the `debug production` command entirely; a troupe-equivalent debug
   renderer is out of scope for this phase (the play files under `plays/` are
   already plain JSON, inspectable with `cat`).

## Tests

- Existing `internal/agents/theatre/*_test.go`, `index_handlers_test.go` intro
  cases, and `frontend_test/intro.test.js` are deleted with their subject.
- The QA gate is the regression test: no troupe code exists yet, so green means
  "nothing references the removed symbols".

## Acceptance

- [x] `go build ./...` passes with no reference to `theatre`, `model.Story`,
      `Teller`, `Feedbacker`, `intro.js` or `/intro/`.
- [x] `make qa` (gofumpt, staticcheck, vet, `go test ./... -race -cover -count=3
      -timeout=30s`, fix, dupl) is green.
- [x] Serving the frontend shows an empty `<div id="troupe">` and no splash.

## Session report (2026-08-23)

Removed end to end:

```text
internal/agents/theatre/            (whole package)
internal/model/story.go             + story_test.go
internal/agents/interfaces.go       Teller + Feedbacker dropped
internal/media/index.go             theatre/feedback fields + WithTheatre/WithFeedbacker + /intro/* routes
internal/media/index_handlers.go    intro handlers, prepareNextStory, storyIDRe, introFeedbackRequest
internal/media/index_handlers_test.go
internal/agents/slivingdoc/feedback.go (FeedbackRecorder + feedback.jsonl + feedbackRecord)
cmd/serve/serve.go                  -theatre* flags + fields + defaults
cmd/serve/serve_setup.go            bard construction + feedbackRecorder + WithTheatre/WithFeedbacker
cmd/serve/serve_setup_test.go       theatre budget flag tests
cmd/debug/production.go             + production_test.go + debug registration
cmd/serve/frontend/intro.js         + frontend_test/intro.test.js (dir removed)
cmd/serve/frontend/index.html       intro overlay → empty <div id="troupe">
cmd/serve/frontend/style.css        splash rules (930–1387, 1404–2456) removed
cmd/serve/frontend/index.js         __introMarkLoaded/__introMarkFailed calls + comment
```

Kept: the `slivingdoc.Notebook` seam (`notebook.go` AppendJSONL) — the
materialise → edit → commit unit the troupe rides on. Its tests now exercise
`AppendJSONL` generically (the feedback envelope arrives in phase 9).

Pre-existing breakage fixed (required for a green build): the clai
v1.10.23-rc1 upgrade (commit 951cf8a) removed `agent.WithOutputTo`; the
classifier's `SetOutput` now wires `agent.WithLogger(slog text handler)`.
The theatre runner's call site vanished with the package.

Verification (exact commands and results):

```bash
# Baseline (pre-change): go build ./... FAILED — pre-existing clai v1.10.23-rc1
# breakage (agent.WithOutputTo undefined in classifier + theatre runner),
# introduced by commit 951cf8a.

go build ./...                          # green after the phase
node --check cmd/serve/frontend/index.js # green

# Full QA gate (make qa): green
#   go test ./... -race -count=3 -cover -timeout=30s  → all packages ok
#     (media 85.1%, storage 85.8%, model 96.6%, slivingdoc 84.5%,
#      classifier 83.7%, serve 74.8%; no skips, no test edits to the gate)
#   staticcheck ./...  → clean
#   gofumpt -w -l .     → no diffs
#   go vet ./...        → clean
#   go fix ./...        → clean
#   dupl -t 80 .        → only the pre-existing accepted clone groups
#                        (test fixtures / table loops, per the duplication policy)
```

Serving check (structural): `index.html` carries an empty `<div id="troupe">`,
no `intro.js` script tag, no intro overlay markup; `style.css` has no `.intro-*`
splash rules (the player's `.skip-intro` episode button is unrelated and kept).

## Notes for later phases

- The empty `<div id="troupe">` is the engine's mount point (Phase 2 self-mounts
  into it; Phase 9 wires it into production).
- The `slivingdoc.Notebook` seam is deliberately preserved — Phase 9's unified
  feedback handler needs the pull → write-one-file → commit unit.

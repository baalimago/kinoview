# Phase 9 — Serve-side wiring + play API + cutover

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

Expose the troupe to the browser and retire the last of the old splash. Serve
the play API, record unified feedback, adapt the facade, wire the engine into
the production frontend, and cut the intro overlay over.

## Shape (planned)

```text
internal/agents/troupe/         # facade already built (Phase 7); this phase wires it
internal/media/index.go         # /api/v1/troupe routes
internal/media/troupe_handlers.go  # play API + feedback handlers
cmd/serve/serve.go              # -troupeModel, -troupeTokenStoploss flags
cmd/serve/serve_setup.go        # construct + wire the troupe facade
cmd/serve/frontend/engine.js    # embedded; self-mounts into <div id="troupe">
cmd/serve/frontend/index.html   # load engine.js into the troupe div
cmd/serve/frontend/index.js     # fetch /api/v1/troupe/play/resolved
```

## Behaviour to implement

- **Play API** (decision 20), all under `/api/v1/troupe`:
  - `GET /play` — paginated over `plays/index.json` with keyset cursor
    `limit`/`order`/`status`/`author` filters.
  - `GET /play/resolved` — the newest submitted play, read from disk (matched
    before `GET /play/{id}`; `resolved` is a reserved path segment, never a play
    id).
  - `GET /play/{id}` — one play by datetime id.
  - `POST /feedback` — audience feedback (below).
- **Unified feedback.** One directory (`feedback/`), one file per note named
  `<playId>_<type>_<utc>.json`, uniform body `{ playId, type, ts, data }`. The
  client sends `{ playId, type, data }`; the server stamps `ts` and derives the
  filename — the client never sends `ts`. The filename's `<type>` and `<utc>`
  match the body's `type` and `ts` (compact form). Types: `rating` (±1, optional
  comment), `dismissal` (`atMs`), `completion` (`durationMs`), `replay`
  (`count`), `continuity` (`history`), `criticism` (server-side, Phase 8).
  Retires the old `feedback.jsonl`.
- **Adapt the facade.** Cooldown + single-flight + `Warm` (already built, Phase
  7): `Prepare` triggers at most one generation, `Warm` materialises the
  notebook, and the served play is always the newest submitted play from disk —
  there is no in-memory `current` story.
- **Wire the engine.** Embed `engine.js`, self-mount it into
  `<div id="troupe">`, and fetch the resolved play from the API.
- **Two flags only** (decision 19): `-troupeModel` and `-troupeTokenStoploss`.
  Remove the last intro-splash remnants (already gone in Phase 0).

## Implementation notes

1. **Gating.** The troupe is gated on slivingdoc — a missing notebook means the
   troupe does not start and the play API returns 404. There is no seed and no
   offline floor; an empty stage is the signal to investigate.
2. The feedback write and commit remain one unit: a commit failure surfaces as a
   500, never a silent drop.
3. `GET /api/v1/troupe/play/resolved` reads the newest `plays/index.json` entry
   and serves the corresponding resolved play file; old plays stay readable via
   `GET /play/{id}`.
4. The facade's caller never wraps `Prepare` in a smaller timeout — the troupe's
   own gates are the budget authority (the same rule the old theatre enforced).

## Tests

- Play API: `resolved` returns the newest play; `{id}` returns one; `play` pages
  with keyset cursor and honours `limit`/`order`/`status`/`author`; `resolved` is
  matched before `{id}`.
- Feedback: valid note → 204 + one file; missing/malformed body → 400; commit
  failure → 500; disabled notebook → 404/501 as appropriate.
- Facade: cooldown + single-flight hold; the served play is read from disk, not
  memory.
- Frontend: engine mounts and renders the resolved play from a stubbed endpoint.

## Acceptance

- [x] `make qa` is green.
- [x] A submitted play renders in the browser via `/api/v1/troupe/play/resolved`.
- [x] No `/intro/*` route, no `intro.js`, no `feedback.jsonl` remain.
- [x] The troupe runs only when the slivingdoc notebook is enabled; otherwise the
      play API returns 404.

## Session report (2026-08-23)

Picked up phase 9 from the handover state (README status: "Phase 9 (serve-side
wiring + play API + cutover) is next"; phases 0–8 already in the tree, all
green).

**Built the wiring end to end:**

- `internal/agents/troupe/playlib.go` — the read-only `PlayLibrary` (Newest /
  Get / keyset-paginated Page) over the submitted-play history on disk.
- `internal/agents/troupe/feedback.go` — the audience `FeedbackWriter`: the
  closed five note types, per-type strict data validation, server-stamped ts,
  `<playId>_<type>_<utc>.json` naming, append-only atomic write and the
  `WithFeedbackCommit` seam; the shared `feedbackFilename` helper now backs the
  critic's filename too.
- `internal/agents/troupe/troupe.go` + `critic.go` — `WithGenerationCritic`
  (the facade runs the critic after each generation with a stamped `g_<UTC>`
  id and the outcome) and `WithCriticismCommit`; `role.go` gained the live
  `NewWorktreeRoleSource`; `slivingdoc/notebook.go` gained `Commit`, reused by
  `AppendJSONL`.
- `internal/media/troupe_handlers.go` + `index.go` — the four `/api/v1/troupe`
  handlers, the `TroupeHandler()` mux (nil when disabled), the
  `WithTroupe*` options and the `runTroupeLoop` (Warm → first generation after
  the concierge startup delay → Prepare every cooldown).
- `cmd/serve/serve.go` + `serve_setup.go` — the two flags
  (`-troupeModel`, `-troupeTokenStoploss`) and the full construction: spawner
  (live role source), submitter, director, critic, facade, feedback writer and
  play library over the shared worktree, with the slivingdoc commit seam; the
  API is mounted only when the troupe is enabled.
- `cmd/serve/frontend/` — `engine.js` loads before `index.js`; `index.js`
  fetches `/api/v1/troupe/play/resolved` and mounts the engine into `#troupe`;
  an empty stage (404) renders nothing.

**Tests (all green):** `go test ./... -race -count=3 -timeout=30s` (23
packages ok), `go run mvdan.cc/gofumpt@latest -l .` (clean), `go vet ./...`
(clean), staticcheck (clean), `go fix ./...` (clean), dupl (only the
pre-existing test clone groups; none in the new code), `node
cmd/serve/frontend_test/engine.test.js` (16/16, +2 bootstrap tests). New
tests: `playlib_test.go`, `feedback_test.go`, `troupe_test.go` critic wiring,
`critic_test.go` commit seam, `role_test.go` live source,
`troupe_handlers_test.go`, `serve_setup_test.go` flags + 404 gating.

## Decision log (session 2026-08-23, clai worker)

- **D-9-1 — The play API reads through a read-only `PlayLibrary` in the troupe
  package.** `playlib.go` owns the worktree reads (`Newest`, `Get`, `Page` with
  the keyset cursor, `limit`/`order`/`status`/`author`); the media handlers are
  thin HTTP over it, so pagination is testable without a server.
- **D-9-2 — Audience feedback is a troupe-package `FeedbackWriter`, not a
  slivingdoc recorder.** The uniform envelope, per-type data validation, the
  server-stamped ts, the `<playId>_<type>_<utc>.json` filename and the
  append-only rule live beside the critic's `CriticismWriter` (the shared
  `feedbackFilename` helper); the commit half is the `WithFeedbackCommit` /
  `WithCriticismCommit` seam, wired by serve to the slivingdoc `Notebook.Commit`
  — write+commit stay one unit, and a commit failure surfaces (500, never a
  silent drop).
- **D-9-3 — The role source is live, never a setup-time snapshot.** The phase-4
  snapshot source would freeze roles at Setup and refuse every role the
  director authors mid-generation; `NewWorktreeRoleSource` reads
  `roles/<id>.json` from the materialised worktree at spawn time.
- **D-9-4 — The facade runs the critic after each generation.**
  `WithGenerationCritic` + a stamped `g_<UTC>` generation id: the critic
  reviews the submitted play or the honest empty stage, outside the generation
  budget, and its note is committed through the same seam.
- **D-9-5 — Generations tick on the hardcoded cooldown.** The indexer's
  `runTroupeLoop` mirrors the concierge loop: `Warm` first, the first
  generation after the concierge's startup delay (the troupe has no timing
  flags — decision 19), then `Prepare` every cooldown; the facade's own gates
  are the authority. No last-run persistence: a restart is a fresh generation,
  matching the old theatre's startup behaviour.
- **D-9-6 — The troupe is gated on the notebook AND `-troupeModel`.** Both must
  be present; otherwise the facade is nil, `TroupeHandler` returns nil, serve
  mounts nothing, and `/api/v1/troupe/*` answers 404.

## Notes for later phases

- This is the cutover: from here the troupe is the only splash path, and the
  grammar/engine stay frozen while the repertoire evolves in the notebook.
- The frontend mounts the newest play but posts no feedback yet; the audience
  note types (`rating`/`dismissal`/`completion`/`replay`/`continuity`) are the
  API contract a future player UI posts against.

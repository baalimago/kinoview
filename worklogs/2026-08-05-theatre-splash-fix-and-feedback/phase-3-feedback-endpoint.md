# Phase 3 — Feedback Endpoint: agents.Feedbacker + Theatre.Feedback + HTTP

**Status:** ✅ Done | [README](./README.md)

## Goal

Expose audience feedback over HTTP: a new narrow agent contract
(`agents.Feedbacker`), a `Theatre.Feedback` implementation that appends to the
audience doc, and a `POST /gallery/intro/feedback` handler on the indexer.

## Specification

**Agent contract** (in `internal/agents/interfaces.go`, decision D-2). The
`Teller` contract is untouched; a new additive interface carries feedback:

```go
// Feedbacker records what the audience thought of a show, so the next
// production can improve. The theatre implements it; the indexer type-asserts
// it in the feedback handler.
type Feedbacker interface {
	// Feedback stores one audience note about a story. rating is +1 (thumbs
	// up) or -1 (thumbs down); comment is optional and capped by the
	// implementation.
	Feedback(ctx context.Context, storyID string, rating int, comment string) error
}
```

**Theatre implementation** (`internal/agents/theatre/theatre.go`). A method
on `*Theatre` that appends one note through the facade's persistent company:
`New` keeps `t.company = Open(cacheDir)` (reused by `loadLibrary`), and
`Feedback` calls `t.company.AppendAudience(note)`. The company's single mutex
holds across the load-modify-save, so two concurrent feedback posts cannot
lose a note; decision D-5 keeps the doc single-writer, so distillation never
competes for it (a fresh `Company` per call would not serialize across calls
— the company must be the facade's own):

- rating outside `+1`/`-1` → error (the handler already validates; the facade
  re-checks because it is the trust boundary, like `submit_story`)
- comment truncated to `audienceCommentMax` (never rejected — a long comment is
  clipped, not lost)
- story id validated against `artifactIDRe` → error when invalid
- date stamped `YYYY-MM-DD`
- `var _ agents.Feedbacker = (*Theatre)(nil)` compile-time proof, next to the
  existing `agents.Teller` proof

**HTTP handler** (`internal/media/index_handlers.go`). New
`introFeedbackHandler`:

- `POST` only → `405` otherwise
- theatre nil → `404` (same as the story handlers)
- `i.theatre.(agents.Feedbacker)` fails → `501` with a clear message (defensive;
  the serve wiring always supplies the theatre)
- body: `{"storyId": "...", "rating": 1, "comment": "..."}` — decoded strictly;
  malformed JSON → `400`
- rating must be `1` or `-1` → `400` otherwise
- storyId must be non-empty and match `^[a-z0-9_]{1,24}$` → `400` otherwise
- comment optional, length capped by the facade
- success → `204 No Content`; the handler never triggers `prepareNextStory`
  (decision Q3 — no cooldown bypass)

**Route** (`internal/media/index.go`): register
`mux.HandleFunc("/intro/feedback", i.introFeedbackHandler())` next to the
existing intro routes. The frontend posts to `/gallery/intro/feedback`.

**Affected paths:** `internal/agents/interfaces.go`, `theatre.go`,
`index_handlers.go`, `index.go`, plus tests in `theatre_test.go` and
`index_test.go`.

## Integration contract

| Input / trigger | Collaborator / fake | Externally observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| `POST /gallery/intro/feedback` `{"storyId":"stry_abc12345","rating":1,"comment":"more dog"}` | real handler, fake `Feedbacker` recording the call | `204`, fake received `(stry_abc12345, 1, "more dog")` | — | no `Prepare` trigger, no cooldown reset |
| `GET /gallery/intro/feedback` | real handler | `405` | — | — |
| `POST` with `rating: 0` | real handler | `400` | — | — |
| `POST` with malformed JSON | real handler | `400` | — | — |
| `POST` with bad storyId `"../etc"` | real handler | `400` | — | — |
| `POST` with theatre nil | indexer without `WithTheatre` | `404` | — | — |
| `Feedback` with rating 5 (direct call) | `Theatre` on a temp cache dir | error returned, no doc write | — | — |
| `Feedback` happy path | `Theatre` on a temp cache dir | `audience.json` gains one note, newest first | doc persisted atomically | — |

## Acceptance criteria

- [ ] Handler table test covers: 204 happy path, 405 on GET, 400 on bad
      rating, 400 on malformed JSON, 400 on bad storyId, 404 on nil theatre,
      501 on a teller that is not a `Feedbacker`.
- [ ] Happy-path test proves the fake `Feedbacker` received exactly
      `(storyID, rating, comment)` and that no `Prepare` was triggered.
- [ ] `Theatre.Feedback` unit tests: happy path writes the doc; rating 0/5
      rejected with error and no write; bad story id rejected; long comment
      truncated; `audience.json` reloads with the note newest-first.
- [ ] `var _ agents.Feedbacker = (*Theatre)(nil)` compiles.
- [ ] Full theatre + media test suites green.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| rating outside ±1 | `400` (handler) / error (facade), no write | handler + facade tests |
| storyId invalid | `400`, no write | handler test |
| malformed body | `400` | handler test |
| wrong method | `405` | handler test |
| theatre nil / not a Feedbacker | `404` / `501` | handler tests |
| doc write fails (read-only dir) | facade returns the error, handler `500` | facade + handler tests |

## Implementation notes

*(filled by the executing agent)*

Implemented per the plan, including review finding R2-01: the facade now owns
one persistent `Company` (`t.company = Open(cacheDir)` in `New`, reused by
`loadLibrary`), and `Feedback` delegates to the compound
`Company.AppendAudience` — the serialization lives with the doc, not in a
cross-domain facade lock.

**Changes.**

- `internal/agents/interfaces.go` — new additive `Feedbacker` contract
  (decision D-2); `Teller` untouched.
- `internal/agents/theatre/theatre.go` — `company *Company` field set in
  `New`; `loadLibrary` reads through `t.company` instead of a throwaway
  `Open(t.cacheDir)`; `Feedback` method (rating re-check → story id
  re-check → `AppendAudience` with a `YYYY-MM-DD` stamp);
  `var _ agents.Feedbacker = (*Theatre)(nil)` next to the Teller proof.
- `internal/media/index_handlers.go` — `introFeedbackHandler`: POST-only
  (405 + `Allow`), nil theatre → 404, non-`Feedbacker` → 501, strict
  `DisallowUnknownFields` decode → 400, rating ∉ {+1, −1} → 400, story id
  failing `storyIDRe` → 400, facade error → 500, success → 204. Never calls
  `prepareNextStory` (decision Q3). `storyIDRe` mirrors `model.Story`'s id
  pattern, the same way the theatre's `artifactIDRe` does.
- `internal/media/index.go` — route `mux.HandleFunc("/intro/feedback", …)`
  next to the existing intro routes.
- `internal/agents/theatre/theatre_test.go` — `Feedback` unit tests: append
  newest-first + reload; bad ratings (0/5/−2) rejected with no write; bad
  story ids rejected with no write; long comment truncated to
  `audienceCommentMax` runes; write failure (cache dir is a file, the
  SaveStory-test shape) returns the error.
- `internal/media/index_handlers_test.go` — `feedbackRecorder` fake (Teller
  + Feedbacker); status-code table (204/405/400×5/404/501); happy-path test
  pins the forwarded triple and `prepareCalls == 0`; write-failure → 500.

**Material implementation decisions.**

- The handler mirrors `recomendHandler`'s strictness (`io.LimitReader` +
  `DisallowUnknownFields`) rather than inventing a second JSON convention in
  the same file; an empty body is a 400 via the decode error.
- `Feedback` trims the story id before the regex (the handler validates the
  raw value; the facade is the trust boundary and re-checks). The comment is
  left to the doc trim — truncation lives in one place
  (`trimAudience`), never duplicated.
- The 501 row reuses the existing `recordingTeller` as the
  Teller-without-Feedbacker fixture; its `deadline` channel is buffered, so
  an erroneous `Prepare` call could not deadlock the test.

**Tests (before: all green; after: all green).**

```
go test ./internal/agents/theatre/ ./internal/media/ -count=1   # before and after: ok
go build ./...                                                   # ok
go vet ./internal/agents/... ./internal/media/                   # clean
staticcheck ./internal/agents/... ./internal/media/              # clean
go test ./... -race -count=1 -timeout=300s                       # all packages ok
node cmd/serve/frontend_test/intro.test.js                       # 13 ok (frontend untouched)
```

## Review findings (review 2, 2026-08-05)

- **R2-01 (Medium).** `Feedback` no longer wraps `LoadAudience` +
  `SaveAudience` on a fresh `Company` under the facade's `writeMu`. The
  facade now owns one persistent `Company` (set in `New`), and `Feedback`
  delegates to the compound `Company.AppendAudience` (phase 2) — the
  serialization lives with the doc, not in a cross-domain facade lock.
  `var _ agents.Feedbacker = (*Theatre)(nil)` and every other bullet in this
  phase are unchanged.

## Review findings (review 6, 2026-08-06)

No findings. Verified good: the handler's status matrix
(204/405/404/501/400×6/500) with strict `DisallowUnknownFields` decode; the
happy path forwards exactly `(storyID, rating, comment)` and pins
`prepareCalls == 0` (decision Q3 — no cooldown bypass); `storyIDRe` ≡
`model.idRe` ≡ `artifactIDRe` (all `^[a-z0-9_]{1,24}$`); the route mounts
under `/gallery` via the `StripPrefix` in serve_setup.go:268; the facade
re-checks rating and story id, stamps `YYYY-MM-DD`, truncation lives only in
`trimAudience`; `var _ agents.Feedbacker = (*Theatre)(nil)` compiles; a
write failure surfaces as a 500 and the facade returns the error.

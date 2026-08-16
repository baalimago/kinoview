# Phase 6 — Feedback endpoint → slivingdoc `feedback.jsonl`

## Decision (Q6, resolved)

Keep the feedback endpoint. Each audience note is appended as one JSON line
to `feedback.jsonl` in the shared slivingdoc worktree and committed. No
markdown prose, no `audience.md`, no trimming. Option (b) — dropping the
endpoint — is closed.

## Goal

The thumbs control keeps posting to `POST /gallery/intro/feedback`. The
handler appends one JSONL record to `<workspace>/feedback.jsonl` and commits
through slivingdoc, so the note is durable, merged like every other note and
readable by the next generation's roles.

## Record shape

One line per note, JSON-encoded, append-only:

```jsonl
{"storyId":"stry_abc12345","rating":1,"comment":"more dog","ts":"2026-08-16T16:07:35Z"}
{"storyId":"stry_abc12345","rating":-1,"comment":"","ts":"2026-08-16T16:08:01Z"}
```

`rating` is `+1` (thumbs up) or `-1` (thumbs down). `comment` is the raw
comment and may be empty. `ts` is the receive time in RFC 3339 UTC. Whatever
the audience sent is what lands on the line — no field is rewritten or
dropped.

## Changes

The `agents.Feedbacker` interface stays. Its implementation moves off the
theatre facade (whose `audience.json` is removed in Phase 4) onto a small
slivingdoc-backed recorder. `internal/agents/slivingdoc` gains a non-MCP seam
for the handler — agents use the MCP tools, the handler uses this direct
seam:

```go
// Notebook appends to a file in the shared worktree and commits through the
// slivingdoc binary. AppendJSONL encodes v as one JSON line and appends it.
type Notebook struct { /* workspace, commit seam */ }
func (n *Notebook) AppendJSONL(name string, v any) error
```

A `FeedbackRecorder` implements `agents.Feedbacker` over that notebook:

```go
type FeedbackRecorder struct { notebook *Notebook }
func (r *FeedbackRecorder) Feedback(ctx context.Context, storyID string, rating int, comment string) error
```

`Feedback` builds the record `{storyId, rating, comment, ts}` and calls
`AppendJSONL("feedback.jsonl", rec)`. Append and commit are one unit: a commit
that fails returns an error so the handler answers 500 rather than silently
losing the note. The append never runs a `notes_pull` first — the worktree is
the shared copy and a pull would clobber an uncommitted line.

The handler path stays `POST /gallery/intro/feedback`. The indexer holds an
`agents.Feedbacker` field (the recorder, nil when slivingdoc is disabled)
instead of type-asserting on the theatre. A nil recorder answers `501`
"feedback not configured"; the happy path answers `204`.

When slivingdoc is disabled the recorder is nil and the handler answers `501`
with a warning — feedback is not recorded locally, matching the strategy's
"everything degrades gracefully" rule (no notebook, no audience notes).

## Tests

- `TestFeedbackRecorder_AppendsJSONLLine` — one post yields one valid JSONL
  line whose decoded fields match `(storyId, rating, comment, ts)`.
- `TestFeedbackRecorder_CommitErrorReturnsError` — a failing commit
  propagates.
- `TestNotebook_AppendJSONLDoesNotPull` — appending never clobbers an
  existing line.
- Handler table stays: 204 happy path, 400 malformed/rating/id, 405 method,
  501 nil recorder.

## Acceptance

- `go test ./internal/agents/slivingdoc/... ./internal/media/... -race -count=3`
  passes.

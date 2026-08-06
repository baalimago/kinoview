# Phase 2 — Audience Doc: Durable Feedback in the Company Library

**Status:** ✅ Done | [README](./README.md)

## Goal

Add the company's seventh durable document, `audience.json` — audience
feedback notes (story id, thumbs rating, optional comment, date) — with the
same atomic write, load validation, cap-and-trim and context-excerpt
machinery as the six existing docs.

## Specification

**Types** (in `internal/agents/theatre/docs.go`):

```go
// AudienceNote is one piece of audience feedback on a story.
type AudienceNote struct {
	StoryID string `json:"storyId"`
	Rating  int    `json:"rating"`            // +1 thumbs up, -1 thumbs down
	Comment string `json:"comment,omitempty"`
	Date    string `json:"date,omitempty"`
}

// AudienceDoc is the audience's memory: what viewers thought of recent
// productions, so the next generation can improve.
type AudienceDoc []AudienceNote
```

**Caps** (in `constants.go`, decision D-3):

- `audienceCap = 40` — newest first, oldest dropped on trim
- `audienceCommentMax = 240` — runes, same as `EntryMaxBody`
- `audienceExcerpt = 8` — notes shown in a working context

**Trim gate** `trimAudience` (load and save run the same gate, like every
other doc): story id must match `artifactIDRe`; rating must be exactly
`+1`/`-1` (anything else drops the note); comment truncated to
`audienceCommentMax`; date trimmed to `YYYY-MM-DD`; duplicates (same story +
rating + comment) collapse; cap drops the oldest.

**Company plumbing** (mirror the other docs):

- `audienceFileName = "audience.json"` in `company.go`
- `LoadAudience` / `SaveAudience` accessors in `docs.go`, plus the compound
  `AppendAudience(note)` — the single feedback write path: load, prepend,
  trim, save under the company's mutex (decision D-5)
- `Library` gains `Audience AudienceDoc`; `LoadLibrary` includes it (context
  injection and corrupt-degrade coverage). `SaveLibrary` deliberately does NOT
  persist it — the audience doc is single-writer: only `Theatre.Feedback`
  (phase 3) ever writes `audience.json`, so distillation can never overwrite a
  fresh note with a stale in-memory copy (decision D-5).
- `AudienceDoc.context()` renders the excerpt as a headed list, e.g.
  `Audience feedback from recent shows:` with `[+1] "comment" (story, date)`
  lines — same shape as the director lessons context

**Context injection** (decision Q2 / Option A). In `Runner.withDocsContext`
(`runner.go`), the director and the dramaturg each append
`lib.Audience.context()` to their prompt — the director to steer the
production, the dramaturg to steer the brief. The other roles do not read it
(the playwright is told through the director's notes). The bulletin still
reaches everyone.

**Affected paths:** `docs.go`, `constants.go`, `company.go`, `runner.go`,
`docs_test.go`, `runner_test.go` (context assertions). No HTTP, no player, no
model changes in this phase.

## Integration contract

| Input / trigger | Collaborator / fake | Externally observable result | Required side effects | Prohibited side effects |
|---|---|---|---|---|
| `SaveAudience` with 45 notes | Company on a temp dir | file has exactly 40, newest first | atomic write; oldest dropped | no crash on cap |
| `AppendAudience` past the cap | Company on a temp dir | file has exactly 40, newest note first | oldest dropped; two concurrent appends lose no note | — |
| `LoadAudience` on corrupt JSON | Company on a temp dir | empty doc + logged error | other docs unaffected | no startup failure |
| `LoadAudience` on a note with rating 0 / story id `../x` | Company | note dropped by trim | — | — |
| Director or dramaturg invocation | runner with a library holding audience notes | prompt contains the audience excerpt under its own heading | playwright/scenographer/wardrobe prompts do NOT contain it | — |

## Acceptance criteria

- [ ] `audience.json` round-trips: write → load → identical semantics.
- [ ] Trim drops invalid ratings, bad story ids, over-long comments (truncate,
      not reject), and duplicates; cap keeps the newest 40.
- [ ] `LoadLibrary` includes `Audience`; a corrupt `audience.json` degrades to
      empty and the other six docs still load; `SaveLibrary` leaves
      `audience.json` untouched.
- [ ] `AppendAudience` prepends, trims to the cap, and two concurrent appends
      lose no note (the load-modify-save holds the company's mutex).
- [ ] Director and dramaturg prompts include the audience excerpt; playwright,
      scenographer and wardrobe prompts do not.
- [ ] Existing docs tests and runner context tests still pass unchanged.

## Error coverage

| Failure | Expected outcome | Test |
|---|---|---|
| `audience.json` corrupt | empty doc, error logged, server starts | corrupt-file unit test |
| Comment > 240 runes | truncated on write and on load | trim unit test |
| Rating outside ±1 (hand-edited file) | note dropped | trim unit test |
| Story id fails `artifactIDRe` | note dropped | trim unit test |
| Duplicate note | collapsed | trim unit test |
| Doc over cap | oldest dropped, newest 40 kept | cap unit test |

## Implementation notes

Executed 2026-08-05, worker session 2, per the specification and decisions
D-3/D-5, R2-01. No deviations.

- `docs.go` — `AudienceNote`/`AudienceDoc` types; `Library.Audience`;
  `trimAudience` (id pattern, ±1 rating, comment truncate-not-reject, date
  trim, duplicate collapse, cap); `LoadAudience`/`SaveAudience`;
  `AppendAudience` (load-prepend-trim-save under the company mutex);
  `LoadLibrary` reads it, `SaveLibrary` deliberately does not (D-5);
  `AudienceDoc.context()` renders `[+1] "comment" (story, date)` lines under
  `Audience feedback from recent shows:`.
- `loadDoc`/`saveDoc` split into locked wrappers + `loadDocLocked`/
  `saveDocLocked` so `AppendAudience` holds the company's single mutex across
  its read-modify-write (R2-01); the existing accessors are unchanged in
  behaviour.
- `constants.go` — `audienceCap = 40`, `audienceCommentMax = 240`,
  `audienceExcerpt = 8` (D-3).
- `company.go` — `audienceFileName = "audience.json"`.
- `runner.go` — `withDocsContext` appends `lib.Audience.context()` to the
  director and the dramaturg; the other roles never read it.
- `docs_test.go` — round-trip incl. SaveLibrary-untouched (D-5); corrupt
  `audience.json` degrades to empty with the error logged; hostile-content
  gate (bad id, bad rating, truncation, dedupe); cap kept newest-first;
  `AppendAudience` prepend + cap + concurrent appends lose no note (two
  goroutines, race-clean); context excerpts per role incl. the wardrobe
  negative.

**Tests (before: green; after: green).**

```
go test ./internal/agents/theatre/            # before and after: ok
go test ./internal/agents/theatre/ -race -count=1   # ok
go vet ./...                                        # clean
staticcheck ./internal/agents/theatre/              # clean
gofumpt -l <changed files>                          # no output, clean
go test ./...                                       # all packages ok
```

## Review findings (review 2, 2026-08-05)

- **R2-01 (Medium).** The audience write path now specifies a compound
  `Company.AppendAudience` (load, prepend, trim, save under the company's
  mutex) instead of the facade's `writeMu` wrapping separate `LoadAudience` +
  `SaveAudience` calls on a fresh `Company`. The fresh-Company approach would
  not serialize across calls — the guarantee had to live on the facade and
  couple two persistence domains. With `AppendAudience`, the company's
  documented "one mutex serialises every read and write" invariant holds for
  the doc itself. The facade holds one persistent `Company` (created in `New`,
  reused by `loadLibrary`), so `Feedback` (phase 3) needs no facade lock.
  Added: an `AppendAudience` integration row and acceptance criterion.

## Review findings (review 5, 2026-08-05)

- **R5-01 (Low).** `AudienceDoc.context()` renders a thumbs-down note as
  `[+-1]` because the format string is `"  - [+%d] "` (docs.go:655). With
  rating `-1` the verb produces `[+-1]`, which reads as "plus minus one" in
  the director's and dramaturg's prompts; the intended rendering (per the
  phase spec's `[+1] "comment" (story, date)` example) is `[+1]` for a
  thumbs-up and `[-1]` for a thumbs-down — i.e. the sign-aware verb `%+d`.
  Cosmetic only (LLM-facing text; the rating is still parseable), severity
  Low, non-blocking.
  - [ ] Change the format verb from `[+%d]` to `[%+d]` and pin the `[-1]`
        rendering in `TestDocs_ContextExcerptsPerRole` (the excerpt test
        today only covers a `+1` note).

**Verified good (review 5).** The doc round-trips through its own write path
(`SaveAudience`), `SaveLibrary` never persists it (D-5), corrupt
`audience.json` degrades to empty with the error logged, the trim gates
reject bad ids/ratings, truncate long comments, collapse duplicates and cap
to the newest 40; `AppendAudience` prepends under the company's single mutex
and two concurrent appends lose no note; the excerpt reaches the director
and the dramaturg only. `loadDocLocked`/`saveDocLocked` take no lock, so
`AppendAudience`'s compound read-modify-write cannot double-lock (R2-01).

## Review findings (review 6, 2026-08-06)

- **R5-01 re-verified — still open.** docs.go:655 still formats the rating
  with `[+%d]`, so a thumbs-down note renders as `[+-1]` in the director's
  and dramaturg's prompts; the checkbox above is the fix. Cosmetic,
  LLM-facing, non-blocking — re-recorded as open, not fixed.

**Verified good (review 6).** The doc round-trips through `SaveAudience`;
`SaveLibrary` never persists it (the stale-nil test asserts the note
survives a library save); corrupt `audience.json` degrades to empty with
the error logged and the other docs unaffected; the trim gates reject bad
ids and ratings, truncate long comments (240 runes), collapse duplicates
and cap to the newest 40; `AppendAudience` prepends under the company's
single mutex and two concurrent appends lose no note (race-clean); the
excerpt reaches the director and the dramaturg only (wardrobe negative
pinned); `loadDocLocked`/`saveDocLocked` take no lock, so the compound
append cannot double-lock (R2-01).

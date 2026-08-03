# Phase 6 — Persistence and the Self-Developing Library

**Status:** ✅ Complete | [README](./README.md)

## Goal

Make the company remember: per-role persistent documents that grow across
generations, a durable cross-role bulletin, the deterministic character registry that
pins identity, and distillation of each generation's board into the library at
submit.

## Implementation notes

Executed by imago, 2026-08-02 session (phase 6 of the playwright-company worklog).

**Delivered** — the six durable company docs, the durable registry, submit-time
distillation and per-role context injection:

| File | Contents |
|---|---|
| `docs.go` (new) | The six doc types — `PremisesDoc` (themes/shapes, cap 40), `RepertoireDoc` (summaries cap 30 + canon facts cap 40), `SetsDoc` (backdrop + cells + props recipes with usage counts, cap 50), `RegistryDoc` (fixed, cap 16), `DirectorDoc` (lessons, cap 30), `BulletinDoc` (notices, cap 40) — plus the trim/repair functions (one per doc: vocabularies checked, lengths capped, deduped, trimmed oldest-first), the generic `loadDoc`/`saveDoc` pair (missing = empty, corrupt = logged + empty, load and save run the same gate), the six `Company` accessor pairs, `Library` + `LoadLibrary`/`SaveLibrary` (each doc independent, joined errors), the per-doc context excerpts (a role reads the most recent few entries, never the whole history), `setRecipeKey` (backdrop + sorted cell/prop layout = the recipe's identity) and `dateStamp`. |
| `registry.go` (rewritten) | The costumer's book is now durable: entries are `CharacterEntry{ID, Species, Coat, Variants, Notes}`; `newRegistry` seeds the permanent cast's canonical defaults (ina→cat/ginger + `catVariants`, freija→dog/tan + `dogVariants`, mouse1→mouse/field + `mouseVariants` — the player's palettes, mirrored until phase 9); `PinAndApply` stamps coat+species for registered ids and leaves guests alone; `Canonize` is the only place identities are born — explicit director approval at submit, validated (id pattern, on-stage, known species, coat ∈ species variants, registry cap), refusals returned as messages; `Doc`/`LoadDoc` round-trip through registry.json (permanent defaults never overridden by a file); `Variants` feeds the wardrobe context. |
| `distill.go` (new) | Submit-time distillation: `(p *production) distill()` folds one generation's paperwork into the library — premises from the brief, repertoire from the working file (draft report when present, else the draft itself), sets from the dressed scene (gated on the scenographer's board deliverable), director lessons from the submit call's notes, bulletin from the board's `decision`/role-`note` entries (never the stage's), registry always. Missing artifact → that doc untouched. `premiseFrom`, `repertoireFrom`, `setsFrom`, `bumpSetRecipe` (same dress again = count bump), `lessonsFrom`/`splitLessons`, `bulletinFrom`, `parseCanonizations` (JSON array or a refusal the director sees). |
| `director.go` (changed) | `submitStory(notes, characters string)` — the final gate now carries the director's final word: critique lessons and newly approved characters, distilled AFTER the story is persisted (docs never precede the story). A distillation failure is logged and never fails the submit. The production carries `lessons`; the director prompt documents the two optional inputs; `pinIdentity` now reports canonical pins. |
| `tools/director.go` (changed) | `submit_story` gains the optional `notes` and `characters` inputs (spec + call), so the model can hand over lessons and canonizations at the final gate. |
| `runner.go` (changed) | `withDocsContext(prompt, role)` appends the library excerpts: the bulletin reaches every role, premises the dramaturg, repertoire the playwright, sets the scenographer, lessons the director. `withRegistryContext` now renders the wardrobe variants. |
| `fallback.go` (changed) | The dramaturg's floor fills the brief's no-repeat list from the premises doc (`premisesNoRepeat`) — the floor avoids repeating history even with the LLM down. |
| `theatre.go` (changed) | `loadLibrary` at startup: the registry doc seeds the costumer's book, so canonized characters survive the restart; a corrupt doc degrades to empty, never a crash. |
| `stage.go` (changed) | `Submit`/`Fail`/`Close` now drain the feed goroutine before returning — no feed goroutine outlives its generation, and the transcript is flushed before the caller moves on (the phase-6 contract). This also removes a cross-test data race (a leftover feed printing into ancli while the next test swaps ancli's global output mode). |
| `company.go`, `constants.go` (changed) | The six doc file names (`premises.json` … `bulletin.json`) and the doc caps + context excerpt caps. |

**Material decisions (recorded for chronology):**

- **D-P6-1 — the registry seeds canonical coats, replacing the phase-5
  "first coat seen" pin.** The spec's table is the ground truth: ina is ginger
  from generation one, whatever the draft says. `PinAndApply` stamps
  registered ids and leaves unregistered guests as-is (the error table's
  "guest coat stands"); a guest becomes a character only through explicit
  director approval at submit (`Canonize`, validated against the draft cast —
  a canonized character is a character the audience actually saw).
  (`TestRegistry_PinStableAcrossSeedsAndMisstatedCoats`,
  `TestRegistry_UnregisteredIdLeftAsIs`)
- **D-P6-2 — the docs are six files, one per doc, with one shared gate.**
  Load and save run the same repair function per doc (a doc that could not be
  read back is never written); missing = empty, corrupt = logged + empty
  (never a crash); every doc is trimmed to its cap on write, oldest first.
  The registry is the exception to trimming: fixed and small, a full book
  refuses new characters rather than dropping canonized ones.
- **D-P6-3 — distillation is deterministic extraction from the board + the
  working file.** The agents write the board and the artifacts; distillation
  copies. Premises come from the brief only (the premise is the dramaturg's
  reading of the theme, not the theme alone); repertoire from the working
  file (the draft report's author-owned shape when present, else the draft);
  sets from the dressed scene (the applied dress is the authoritative state —
  the board's scene-report copy is truncated at the board cap), gated on the
  scenographer's deliverable so an undressed default never reads as a recipe;
  lessons from the submit call's notes; bulletin from board `decision`/role
  `note` entries, newest first, stage notes excluded as per-generation noise;
  registry always (it is the book, not a log).
- **D-P6-4 — the director's final word rides the submit call.** `submit_story`
  gains two optional inputs: `notes` (critique lessons, one per line) and
  `characters` (a JSON array of canonizations). Both are distilled AFTER the
  story is persisted — the integration contract's "docs never precede the
  story" is structural, not advisory. A distillation failure is logged and
  never fails the submit; the next submit writes again.
- **D-P6-5 — canonization is validated against the draft cast.** A new
  character must have appeared in the working draft, name a known species and
  a coat the player can draw (a species variant), and pass the id pattern;
  the permanent cast is never re-approved. Refusals return to the director as
  messages.
- **D-P6-6 — the registry doc round-trips at startup.** `Theatre.loadLibrary`
  reads registry.json and merges it into the seeded book; the permanent
  cast's canonical defaults are never overridden by a file (a hostile
  registry.json cannot move ina off ginger).
- **D-P6-7 — contexts read excerpts, not histories.** The docs grow across
  generations; a prompt shows the most recent few entries per doc (bulletin 8,
  premises 8, facts 8, summaries 4, sets 6, lessons 6), so the library never
  grows a prompt without bound.
- **D-P6-8 — the feed drains before the stage ends.** `Submit`/`Fail`/`Close`
  await the feed goroutine, so no goroutine outlives its generation and the
  transcript is flushed before the caller moves on. This fixed a real
  cross-test race: a leftover feed printing into ancli while the next test
  swapped ancli's global output mode (found under `-race -count=3`).
- **D-P6-9 — the dramaturg floor reads the premises ledger.** The fallback
  brief's no-repeat list comes from the premises doc, so the floor avoids
  repeating history even when the LLM is down (phase 5's "waits for the
  premises ledger" is now real).

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` | pass — baseline green before the phase |
| `go test ./internal/agents/theatre/...` (before changes) | pass — phase 1–5 baseline |
| `go test ./internal/agents/theatre/...` | pass — 138 top-level test functions (124 pre-existing + 14 new: 5 docs, 4 registry, 4 distill, 1 fallback no-repeat, 2 theatre generation/canonize) |
| `go test ./...` | pass — full suite |
| `go test ./internal/agents/theatre/... -race -count=3` | pass — repeated runs clean (a pre-existing feed-goroutine race surfaced and was fixed by D-P6-8; a `TempDir` cleanup flake in the corrupt-doc test was fixed by warming synchronously) |
| `go test ./... -race -cover -count=3 -timeout=30s` | pass |
| `go test ./internal/agents/theatre/ -cover` | 90.6% (phase 5: 89.5%); the new docs/distill/registry surface is 82–100% per function |
| `go test ./internal/agents/theatre/tools/ -cover` | 93.1% |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre/` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/` | 0 clone groups |

**Acceptance check** — all criteria met: the registry defaults match the
table and `pin_identity` is stable across 400 seeds and across drafts that
omit or misstate coats (`TestRegistry_CanonicalDefaultsMatchTable`,
`TestRegistry_PinStableAcrossSeedsAndMisstatedCoats`); two sequential
generations carry generation 1's canon facts into generation 2's playwright
context, generation 1's premise into the dramaturg's no-repeat list and
generation 1's lesson into the director's context
(`TestTheatre_SecondGenerationReadsFirstLibrary`); distillation produces all
six docs from a fixture generation with correct counts and no LLM
involvement (`TestDistill_ProducesAllSixDocs`, `TestDistill_SameSetBumpedNotDuplicated`);
a corrupt doc on disk loads as an empty doc with an error log and the server
starts (`TestDocs_CorruptFileDegradesToEmpty`); a doc over its cap is trimmed
oldest-first on the next write (`TestDocs_TrimmedToCapOldestFirst`); the
existing storyteller/theatre tests (cooldown, persistence, warm, atomicity)
pass unchanged. Error coverage: registry file corrupt → empty registry,
canonical defaults used, error logged (`TestRegistry_LoadDocRepairsHostileFile` +
the corrupt-load path); a missing artifact (no brief, no scene report) skips
that doc and writes the others (`TestDistill_MissingArtifactSkipsDoc`); a doc
write failure mid-submit leaves the story persisted, the failure logged and
the generation complete (`TestDistill_WriteFailureAfterStoryPersisted` for
registry and bulletin); `pin_identity` on an unregistered id leaves it as-is
(`TestRegistry_UnregisteredIdLeftAsIs`); a bulletin write failure still
completes the generation. Integration contract: `submit_story` canonizes an
approved character into the registry and refuses one not in the draft
(`TestTheatre_SubmitCanonizesApprovedCharacter`); the docs never precede the
story (distill runs after `saveStory` in `submitStory`); the LLM never writes
the docs directly — only the board and the artifacts.

**Docs** — AGENTS.md package map gained docs.go/distill.go and the updated
registry line; a new key insight ("The theatre's library is self-developing").
The phase README marks phase 6 complete.

## Review findings

### Review 3 — 2026-08-02 (holistic review; worker: imago)

**R3-02 — a generation that overflows the board loses its premise at distill,
silently (Low — fix tracked in phase 13).** `internal/agents/theatre/distill.go`
`premiseFrom` walks `board.Entries` backwards for the last "brief" entry, but
`Board.Append` caps the board at `BoardMaxEntries` (60, constants.go) and
`distill` reads the board as it stands at submit. A chatty generation posting
60+ entries after the brief trims the brief off the board, so `premiseFrom`
returns "no brief" and the premises doc is not updated for that generation —
no warning, no ledger note, the dramaturg's next no-repeat list silently
misses a theme. The same trim silently skips `setsFrom`'s scenographer
deliverable scan and `bulletinFrom`'s older decisions. Failure scenario:
any generation whose director posts more than 60 board entries between the
brief and the submit loses its premise. Fix (checkbox): carry the brief (and
the scenographer deliverable) out of band — record them in the working file
or a distillable field at write time — or emit a warning note when a distill
input was trimmed off the board.

**R3-04 — the registry load gate trusts hand-edited variant lists (Low — fix
tracked in phase 13).** `internal/agents/theatre/docs.go:220` (`trimRegistry`)
keeps `e.Variants` from the file (pattern-checked, capped at 8) without
checking them against the species palette, while `Canonize` (registry.go)
restricts variants to `speciesVariants`. A hand-edited or stale registry.json
can therefore surface out-of-palette coats in every working context
(`withRegistryContext`: "— wardrobe: pink") and in the wardrobe's answers; a
playwright may then write that coat, which passes `model.Story.Validate`
(coats are checked against the id pattern only, story.go:294-297) and is
silently replaced by a random palette coat in the player (intro.js
`def.coats[spec.coat] || def.coats[pick(coatNames)]`). Failure scenario: an
operator edits registry.json to add "pink" to ina's variants; the next
generation's playwright writes coat "pink", the story validates, and the
player substitutes an arbitrary palette coat. Fix (checkbox): run the load
gate through the same palette check as `Canonize` (drop variants not in
`speciesVariants`), so load and canonize agree on what a coat may be.

Verified good for this phase (review 3): the six docs are each
atomic-written, validated on load and trimmed to their caps oldest-first; a
corrupt doc degrades to the empty one and the server starts (the acceptance
criterion holds on the corrupt-load path); the registry is the only place
identities are born (`Canonize` gates on valid species, palette coat, on-stage
id and the registry cap, in that order); the integration contract holds —
`submitStory` persists the story before `distill` runs, a distillation failure
is logged and never fails the submit, and `SaveLibrary` writes every doc
even when one fails; the dramaturg floor's no-repeat list reads the premises
doc on the fallback path.

### Review 5 — 2026-08-02 (holistic review; worker: imago)

**R5-01 — the pre-draft brief window is not provably closed, so the premise
can still be dropped at distill (Low; does not reopen).** The phase-13 R3-02
closure argument claimed that “at most ~57 entries can be posted before the
playwright's draft write (director 50-call cap minus the dramaturg spawn,
plus the playwright's 8)”, so the brief (entry 1) always reaches the working
file via `boardBrief()`. The accounting undercounts the board traffic before
the playwright's `writeDraft`: a director `consult` posts question + answer
(2 entries from the broker) and the consulted role can post up to its own
budget (4 more), so one director call can add up to 6 entries; the dramaturg
itself can post up to 8 entries (its 8-call budget includes `post_to_board`)
before or beside the brief; the playwright can post 7 notes before
`write_draft`. Reproduced with a scratch test (removed after the repro): a
budget-respecting generation — dramaturg posts 7 notes + brief (8 calls),
then 33 consults under the 200 global cap (each question + answer + 4
consulted posts) — trims the board to 60 and drops the brief before the
playwright's draft write; `w.Brief` comes out empty and `premiseFrom`
returns no premise, the exact R3-02 failure mode. Fix (checkbox): capture
the brief at brief-post time (`writeBrief`/`fallbackBrief` write it into the
working file the moment it is posted), so the capture happens where the
board is guaranteed to still hold the brief; the `boardBrief()` scan stays a
fallback for older files, never the primary copy.

Verified good for this phase (review 5): `premiseFrom` reads `w.Brief`
first and `setsFrom` checks `w.Dressed`; the out-of-band working fields
round-trip through `normalize`; a board overflow after the draft write still
distills both (the `TestDistill_PremiseAndSetsSurviveBoardOverflow`
regression); the registry load gate still shares the species palette with
`Canonize`; the docs still trim to their caps oldest-first.

## Review findings (review 7, 2026-08-03)

**R7-02 — submit ignores story persistence failure (High; closed in phase 14).**
The phase contract requires the story to be persisted before the submitted
working state and library distillation. However, `submitStory` in
`internal/agents/theatre/director.go:338-370` calls `Theatre.saveStory` without
checking an error, and `theatre.go:346-380` only logs atomic-write failures.

Failure scenario: the story file cannot be created or replaced while company
paperwork remains writable. The submit call succeeds, `working.json` is marked
submitted, and docs may be distilled even though the story is absent or stale.

Fix (closed): `saveStory` returns the atomic story-write error and `submit_story`
leaves the working state and distillation untouched when persistence fails;
failure-injection test `TestTheatre_SubmitAbortsWhenStoryNotPersisted` proves
submit does not claim success (see [phase 14](./phase-14-review-fixes.md)). **[x]**

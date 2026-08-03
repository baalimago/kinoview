# Phase 5 — Roles and Prompts

**Status:** ✅ Complete | [README](./README.md)

## Goal

Author the four production roles — dramaturg, playwright, scenographer, wardrobe
consultant — as mini-agents with scope prompts, artifact schemas, deterministic
fallbacks, and soft-continuity canon-fact injection.

## Implementation notes

Executed by imago, 2026-08-02 session (phase 5 of the playwright-company worklog).

**Delivered** — the four production roles in full:

| File | Contents |
|---|---|
| `artifacts.go` (new) | The three artifact schemas — `BriefArtifact`, `DraftReport`/`Act`, `SceneReport`/`CellPlacement`/`PropPlacement` — plus `parseArtifact` (extract-first-balanced-JSON), `normalizeBrief`, `normalizeDraftReport`, `normalizeSceneReport` and the caps (`MaxMoodLen`…`MaxReasonLen`). Same strictness as `model.Story.Validate`: unknown values dropped, ids pattern-checked against `artifactIDRe` (a mirror of the model's private pattern — phase 9 consolidates), lengths capped. A deliverable that is not a JSON artifact passes through untouched — the free-text path stays the legacy quick form. |
| `registry.go` (new) | The costumer's book (decision D7): `Registry` seeded with the permanent cast (ina/freija/mouse1, coatless — a coat marks a real pin), `PinAndApply` (first look seen becomes the pin; pinned looks are applied back), `Lookup`, `Known`, `IDs`, `Size`. Thread-safe; phase 6 makes it durable. |
| `fallback.go` (new) | The deterministic floors (decision D11): `fallbackFor` (the seam — an injected `WithFallback` wins; otherwise the internal dispatcher), `roleFallback(role, task, depth)` routing per role, and the six floors: `fallbackBrief` (board theme + registry lineup, posted like write_brief), `fallbackDraft` (composer draft saved into the working file + a valid draft report), `fallbackScene` (composer `DressDraft` dressed into the working file + a valid scene report posted), `fallbackAdvice` (registry lookup + backdrop lane note), and the three consulted-role in-place answers. |
| `roles.go` (rewritten) | The four role prompts rewritten with the three scope sections — **You decide: … You ask: … You stop: …** — with compile-time constness guards (`const _ = len(prompt)`); the writer wrappers (`writeBrief`, `writeDraft(story, report)`, `writeScene(backdrop, report)`) validating artifacts at the wrapper boundary; `applyCellPlacements` (merge by row:col, fresh ids for new slots so the playwright's setCell beats stay addressable), `applyPropPlacements` (cross-checked against the draft's props) and `nextCellID`. |
| `runner.go` (changed) | `rnd` (seeds the fallbacks), `registry` + `WithRegistry`, `fallbackFor` dispatch, `runOnce` carries the invocation depth, the fallback failure is recorded in the ledger, `withRegistryContext` appends the costumer's book to every prompt, and the playwright floor: a loop that ends without a playable draft is answered by the composer draft with a warning note (the error table's "fallback draft offered to the director"). `defaultFallback` (the phase-3 placeholder) is gone. |
| `working.go` (changed) | `Working.Report *DraftReport` (the playwright's author-owned act structure, stored beside the draft, normalized on load like the rest of the file), `Summary.Canon` and the act count superseded by the report's acts when present (D-P1-6); canon facts are now deduped on the gate. |
| `context.go` (changed) | `AssembleContext` renders the canon facts under the working summary — the soft-continuity injection seam (phase 6 seeds them from the repertoire doc). |
| `theatre.go` / `director.go` (changed) | `Theatre.registry` is now the seeded `*Registry` (was a bare map); `openProduction` wires it into the runner; `pinIdentity` delegates to `Registry.PinAndApply`. |
| `storyteller/composer.go`, `storyteller/staging.go` (changed) | `DressDraft` — the deterministic scenographer floor, exported for the theatre: the draft's backdrop kept when valid, pieces laid into the columns nobody occupies. The dresser lists are extracted (`indoorDressers`/`outdoorDressers`) so the floor and the composer share one recipe; `planFromCast` recovers a staging plan from a story's cast. Reused, not rewritten. |

**Material decisions (recorded for chronology):**

- **D-P5-1 — the fallback is a dispatcher, not a placeholder.** Phase 3's
  `defaultFallback` (error out, caller answers with the composer) is replaced
  by per-role floors that answer with an artifact of the role's own schema and
  do the side effects the role's tools would have done: the dramaturg posts
  the brief, the playwright saves the composer draft into the working file,
  the scenographer dresses the draft. The injected seam (`WithFallback`,
  tests) still wins when present, so the phase-3 fixtures keep their shape.
- **D-P5-2 — the fallback dispatcher carries the invocation depth.** A
  consulted role must never rewrite the director's draft: at a consult depth
  (≥ 1) the playwright and scenographer fallbacks answer in place (the
  working file's shape, the set as dressed) instead of running their
  production side effects; the dramaturg answers from the board's brief.
  `fallbackFor(role, task, depth)` keeps the public `WithFallback` signature
  untouched. The playwright's primary floor goes one step further: a draft
  already in the working file under THIS generation's id is the playwright's
  own work, so a failed revision reports it instead of clobbering it with a
  composer scene; a stale file from an earlier generation (a different id) is
  overwritten like any other missing draft.
  (`TestFallback_PlaywrightFallbackPreservesThisGenerationsDraft`)
- **D-P5-3 — artifacts are validated at the wrapper boundary, leniently.**
  The brief, draft report and scene report are parsed and normalised inside
  the writer tools (before anything enters the board or the working file);
  unknown values are dropped, ids pattern-checked, lengths capped. A
  deliverable that is not a JSON artifact passes through as-is — the free-text
  path keeps every phase-3/4 fixture green. The draft itself stays
  hard-failing (`model.Story.Validate` at the working-file gate).
- **D-P5-4 — the playwright's rescue is structural.** A playwright loop that
  ends without a playable draft in the working file — LLM succeeded but never
  wrote one — is answered by the composer draft with a warning note on the
  transcript, so the director always has a working file to build on and a
  missing artifact can never crash the production (the error table's
  "fallback draft offered to the director"). Gated on depth 0.
- **D-P5-5 — canon facts round-trip through the working file.** The canon
  injection seam is the working file's `Canon` (rendered into every prompt by
  `AssembleContext`; phase 6 seeds it from the repertoire doc at generation
  start). The playwright's draft report lists the facts it kept; `write_draft`
  appends them to the working file, truncated and deduped by the file's own
  gate. The fallback draft keeps the facts as they are — the floor riffs by
  keeping.
- **D-P5-6 — the draft report is stored, and its acts supersede the derived
  count.** `Working.Report` carries the playwright's author-owned act
  structure (D-P1-6's "supersedes this" is now real), normalised on load and
  dropped when empty. `Summary.Acts` uses the report when it delivered acts.
- **D-P5-7 — the registry is seeded with the permanent cast, coatless.** A
  coat is the marker of a real pin: a seeded-but-unpinned character is pinned
  afresh every generation until it finally shows a coat. This keeps
  `pin_identity`'s "first look seen becomes the pin" semantics intact while
  giving lineups and wardrobe answers a ground truth from generation one.
- **D-P5-8 — the scenographer's scene report lands on the board.** The
  integration contract's "scene report posted" is real: `write_scene` (and
  the fallback) post the report (or a compact backdrop line on the free-text
  path) to the board as a `deliverable`; a board write failure is logged, not
  fatal — the working file is the authoritative artifact.
- **D-P5-9 — cells merge by slot, keeping the playwright's setCell beats
  addressable.** A scenographer cell at a (row, col) the draft already has
  updates that cell's piece in place; a new slot gets a fresh id. The
  scenographer's authority over the set and the playwright's scene beats both
  survive the dress.
- **D-P5-10 — the floor is deterministic and seedable.** Each runner owns a
  `math/rand` source; tests inject a fixed seed, which is what makes the
  400-seed sweep a real sweep. `storyteller.SceneNames` supplies the brief's
  shape vocabulary; the backdrop sets stay the composer's (phase 7 widens
  them).

**Validation (exact commands and results):**

| Command | Result |
|---|---|
| `go build ./...` (before changes) | pass — baseline green |
| `go test ./internal/agents/theatre/... ./internal/agents/storyteller/...` (before changes) | pass — phase 1–4 baseline |
| `go test ./internal/agents/theatre/ -v` | 124 top-level test functions pass (103 pre-existing + 21 new: 5 artifacts, 8 fallback, 2 roles, 4 runner phase-5, 1 working, 1 muse-panic; `TestRunner_RunClaiFailsWithoutModel` updated for the now-real fallback) |
| `go test ./internal/agents/theatre/tools/ -v` | pass — tool specs unchanged |
| `go test ./internal/agents/storyteller/ -v` | pass — composer tests unchanged, plus 2 new `DressDraft` tests |
| `go test ./...` | pass — full suite |
| `go test ./... -race -count=1 -timeout=300s` | pass — no races |
| `go test ./internal/agents/theatre/ -cover` | 89.5% (phase 4: 90.0%; the new artifact/fallback/registry surface is 88–100% per function) |
| `go test ./internal/agents/storyteller/ -cover` | 83.4% (DressDraft included) |
| `go run mvdan.cc/gofumpt@latest -l internal/agents/theatre/ internal/agents/storyteller/` | clean |
| `go vet ./...` / `go fix ./...` | pass |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| `go run github.com/mibk/dupl@latest -t 80 internal/agents/theatre/` (and `storyteller/` separately) | 0 clone groups within each package (the cross-package storyteller↔theatre clones are the phase-4 facade mirror and `extractJSON`, both documented as acceptable) |
| grep `fmt.Print*`/`os.Stdout` in `internal/agents/theatre/` | no matches — the company stays ancli-only |

**Acceptance check** — all criteria met: each role's fallback produces an
artifact that passes its schema validation across 400 seeds
(`TestFallback_AllRolesProduceValidArtifactsAcrossSeeds` — brief posted and
registry-clean, draft report with a readable working file behind it, scene
report with a valid backdrop dressing the file, registry-grounded wardrobe
answer); the playwright fallback drafts keep the composer's invariants across
200 seeds (`TestFallback_PlaywrightDraftKeepsComposerInvariants`, and the
existing composer tests pass unchanged); canon facts round-trip — injected
into the playwright's context, kept in the report, appended to the working
file (`TestRunner_CanonFactsRoundTrip`); a wardrobe Q&A about a known
character against a known backdrop returns a registry-grounded answer in
fallback mode (`TestFallback_WardrobeRegistryGroundedAnswer`); the role
prompts are constants — compile-time `const _ = len(prompt)` guards — and
each contains all three sections (`TestRolePrompts_ThreeSections`); the
scenographer fallback respects the staging rules (clear pieces never share a
column with a performer — `TestDressDraft_KeepsBackdropAndRespectsStaging`,
and `staging_test.go` passes). Error coverage: a panicking muse degrades to
the empty theme (`TestTheatre_MusePanicGuarded`, and the 400-seed sweep
asserts the empty-theme brief); a playwright that fails to produce a playable
draft gets the composer draft with a warning note
(`TestRunner_PlaywrightNoDraftFallsBack`, `TestRunner_WriterErrorPaths`); a
canon fact past the length cap is truncated on write
(`TestRunner_CanonFactTruncatedToCap`); a scene report naming a prop the
draft lacks drops the unknown prop (`TestRunner_SceneValidatedAgainstDraft`);
an unknown character consulted gets the "no registry entry" refusal
(`TestFallback_WardrobeUnknownCharacterNoEntry`). Integration contract: the
brief is posted to the board, the scene report lands on the board, the
wardrobe's answer is posted by the broker, consulted roles never mutate the
draft (`TestFallback_ConsultedRolesAnswerInPlace`), and the fallback failure
is noted in the ledger (`RecordFailure` in `runOnce`).

**Docs** — AGENTS.md package map gained artifacts/fallback/registry and the
roles-go description; a new key insight ("The theatre's roles are scoped
artifacts with deterministic floors"). The phase README marks phase 5
complete.

## Review findings

*(filled by reviewers)*

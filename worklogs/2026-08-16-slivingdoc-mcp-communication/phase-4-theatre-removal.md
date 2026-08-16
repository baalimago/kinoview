# Phase 4 — Theatre removal: board, durable docs, registry, distillation

## Goal

Remove the structured board, the seven durable company docs, the character
registry and the deterministic distillation. The theatre keeps only the
single-writer working file, the ledger, the transcript and the deterministic
composer floor. This phase is a pure deletion plus the removal of references;
it must leave the tests green before Phase 5 adds the slivingdoc board.

## Remove files

```text
internal/agents/theatre/board.go
internal/agents/theatre/board_test.go
internal/agents/theatre/docs.go
internal/agents/theatre/docs_test.go
internal/agents/theatre/registry.go
internal/agents/theatre/registry_test.go
internal/agents/theatre/distill.go
internal/agents/theatre/distill_test.go
```

## Remove symbols and their uses

Board:

- `Board`, `Entry`, `appendBoardEntry`, `LoadBoard`, `SaveBoard`,
  `BoardMaxEntries`, `BoardExcerptMax`, `ValidBoardKinds`, `EntryMaxBody`.

Docs and library:

- `Premise`, `PremisesDoc`, `RepertoireDoc`, `SetsDoc`, `RegistryDoc`,
  `DirectorDoc`, `BulletinDoc`, `AudienceDoc`, `Library`, all `Load*` /
  `Save*` accessors, `AppendAudience`, `LoadLibrary`, `SaveLibrary`, all
  `trim*` and `context()` methods, and the doc file-name constants.

Registry:

- `Registry`, `RegistryDoc`, `Canonize`, `PinAndApply`, `Known`, `Lookup`,
  `Variants`, `IDs`, `Size`.

Distillation:

- `distill`, `p.distill`, `p.lessons`, and the `splitLessons` /
  `parseCanonizations` helpers.

## Adjust the production flow

In `director.go`:

- `openProduction` no longer seeds a board (`SaveBoard`), no longer loads the
  registry (`WithRegistry`), and no longer calls `ResetWorking` against the
  board (working reset stays).
- `directorTools` drops `post_to_board` and `pin_identity`. The director keeps
  `dramaturg_brief`, `draft_story`, `dress_set`, `read_story`,
  `validate_story`, `consult` and `submit_story`.
- `submitStory` no longer distils lessons or canonizes characters. It still
  persists the story, marks the working file submitted and refuses a second
  submit. The `notes` and `characters` inputs are removed from the tool.

In `roles.go`:

- `roleTools` drops the shared `post_to_board` / `read_board` pair. Roles keep
  `consult` (except wardrobe) and their deliverable writer.
- `postToBoard`, `boardBrief`, `readBoardExcerpt` are removed.
- `writeBrief` no longer posts to the board — it returns the brief text
  directly (the dramaturg's deliverable is the text itself now).
- `writeScene` no longer posts a scene report to the board; the working file
  stays the only record.
- `validateBrief`, `parseArtifact`/`marshalArtifact` stay only if still used
  by the working-file writers; otherwise remove with their tests.

In `runner.go`:

- `withRegistryContext` and `withDocsContext` are removed; `Run` no longer
  calls them.
- The `WithRegistry` option and the `registry` field are removed.

In `company.go` / `working.go`:

- `boardPath`, `ResetWorking` board references, `registryDocPath`, the doc
  path helpers, and any `Company` field still pointing at the removed files
  are deleted. `Company` keeps only the working, ledger and transcript paths.

In `tools/`:

- Remove `postToBoardTool`, `readBoardTool` and `writeBriefTool` if the brief
  no longer has a writer (the dramaturg's brief is now free text). Keep the
  deliverable writers that touch the working file (`write_draft`,
  `write_scene`, `append_canon`, `advise`) and `consult`.

## The composer floor stays

`floor.go` and `fallback.go` draw from `model.ValidCharacters` and
`speciesVariants` directly. They do not read the registry, so the
deterministic composer is unaffected.

## Tests

Delete the removed files' tests. Update `theatre_test.go`, `roles_test.go`,
`director_test.go`, `runner_test.go`, `company_test.go` and `tools_test.go`
to the new surface. The acceptance is the full theatre package green with
`-race -count=3`.

## Acceptance

- The theatre package compiles with no reference to board, docs, registry or
  distillation.
- `go test ./internal/agents/theatre/... -race -count=3` passes.

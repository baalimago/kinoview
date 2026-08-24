# Phase 7 — Director + swarm

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

The mastermind and its swarm. The director is a fixed, sovereign role that
reads viewer feedback, decomposes the work into smaller concerns, spawns a
concurrent swarm of sub-agents (roles are notes, shaped by the director),
assembles the play, and submits it. Single-writer play; exhaustion without a
submit ships nothing (no seed).

## Shape (as built)

```text
internal/agents/troupe/director.go    # fixed director prompt + tool set + run loop
internal/agents/troupe/director_test.go
internal/agents/troupe/swarm.go       # the Swarm seam: spawn sub-agents by role note + gather
internal/agents/troupe/swarm_test.go
internal/agents/troupe/troupe.go      # the facade (Prepare/Warm/single-flight + hardcoded cooldown)
internal/agents/troupe/troupe_test.go
internal/agents/troupe/registry.go    # spawn_role's build now closes over the Swarm seam
internal/agents/troupe/spawn.go       # spawnRoleTool binds to Swarm, not *Spawner
internal/agents/slivingdoc/slivingdoc.go # Pull extracted from Seed (the facade's Warm seam)
```

## Behaviour (as built)

- **Fixed director prompt + tool set.** The director is a fixed Go role, not a
  note. Its prompt is short-imperative like every other role note and names
  the submit step; the tool descriptions carry the argument lists and the
  workspace layout (implementation note 3). Its tool set is the closed
  registry plus `submit_play`: the file tools as exact clai globs, the
  notebook tools only when the notebook is on, `spawn_role` bound to the
  swarm seam, and `submit_play` wrapped in the outcome recorder. The prompt
  is stamped with the current UTC at generation time — the play id is a
  `story_<UTC>` datetime the director must author, and the closed registry
  carries no clock tool (D-7-3).
- **Spawn sub-agents by role note.** `swarm.go` defines the `Swarm` seam —
  `Spawn(ctx, roleID, task) (string, error)` — implemented by the `Spawner`
  (phase 4) under the phase-5 budget, so the director's `spawn_role` tool
  runs every sub-agent through the same runner and same bounds, and a
  generation test injects a fake swarm and runs the machinery without an LLM.
  The registry's `spawn_role` build signature narrowed from `*Spawner` to
  `Swarm`; the Spawner satisfies the seam unchanged (D-7-1).
- **Assemble.** The director reads what the swarm left with the file tools
  and writes the play with references intact (`"model": "cat@1"`), then
  submits through `submit_play` — the phase-6 gate. The scripted-generation
  test exercises the assemble → submit path end to end against a real
  submitter over a real worktree.
- **Single-writer play.** The director alone submits: `submit_play` is in the
  director's tool set and in no role note (already pinned in phase 6); the
  recording wrapper marks the generation submitted only when the inner gate
  succeeds.
- **Exhaustion without a submit ships nothing.** A generation whose swarm
  produces nothing, or whose director never submits, persists nothing: no new
  play file, no index entry — no seed, no composer floor (pinned by tests).
- **One generation = one `Director.Run`** (implementation note 1): the run
  admits against the generation budget at depth 0 (the termination authority
  refuses a generation outright once a guard is spent), the agent loop
  accounts every budgeted model call into the same budget, and the run
  reports the outcome (`Outcome{Submitted, PlayID}`) — the facade and the
  play API read the disk, never an in-memory story.

## Implementation notes (as executed)

1. **The facade is built in this phase** (phase 9 adapts it): `troupe.go`
   ships `Prepare` — single-flight + the hardcoded cooldown (decision 13,
   D-7-5) — and `Warm`, which materialises the notebook through the
   `WithMaterialise` seam and authors nothing. The served play is always the
   newest submitted play read from disk; there is no in-memory `current`
   story and no seed.
2. **The director runs as one bounded agent of the generation.** `Run` admits
   at depth 0 (so its spawns are depth 1), builds the clai agent with the
   fixed prompt, the tool set, `WithMaxToolCalls(directorToolCalls)` — the
   same hardcoded global maximum that bounds the generation's budgeted calls
   (D-7-2) — and `WithUsageRecorder(budget)`, so the stage's bounds hold
   regardless of swarm size. The agent seam (`WithRunDirector`) is the
   old-theatre `runLLM` pattern: production runs clai's Setup+Run; tests
   script the tool objects directly.
3. **The play id's UTC comes from the prompt stamp, not a new tool.** The
   registry stays frozen (the README's tool surface is unchanged); the
   director's prompt carries `NOW <YYYYMMDDTHHMMSSZ>` and instructs the
   director to author `plays/story_<UTC>.json` at or after that instant —
   the gate refuses an id already on disk, keeping the datetime ids
   honest (D-7-3).
4. **The swarm is an interface, the spawner is the swarm.** `swarm.go` is
   deliberately small: the seam + the compile-time proof. The director's
   `spawn_role` closes over the seam, so a generation test swaps in a fake
   swarm and the tool still gathers the sub-agent's final message (the
   "gather" of the shape plan).
5. **The submit outcome is recorded by a wrapper, not by disk reads.** The
   recording `submit_play` wrapper marks the generation on inner success;
   the agent loop is single-threaded, so the fields need no lock (the same
   rule the old theatre's director used). `Outcome` resets per `Run`, so
   repeated generations re-arm (pinned).
6. **`slivingdoc.Pull` was extracted from `Seed`.** `Warm`'s materialisation
   is the same pull every agent's `mcp_slivingdoc_notes_pull` runs — no
   bulletin seeding, no commit — so the stage starts from the repertoire the
   last generation left, never from a seed. `Seed` now composes `Pull` +
   bulletin + commit; its observable behaviour and error text are unchanged
   (existing tests pass verbatim).

## Tests

`swarm_test.go`, `director_test.go` and `troupe_test.go` (all real-filesystem
integration over seeded temp worktrees, per AGENTS.md), plus the two new
`slivingdoc` pull tests:

- **Swarm seam**: `*Spawner` satisfies `Swarm` (compile-time); the
  `spawn_role` tool over a fake swarm gathers the sub-agent's final message,
  records the commission, and surfaces a swarm failure to the spawning agent.
- **Generation submits end to end** (the phase headline): a scripted director
  over a mocked swarm spawns one sub-agent, assembles the play into the
  worktree, submits through the real `submit_play` — the resolved play lands
  on disk and `plays/index.json` carries its entry; the budget returns to
  rest (depth 0, reservation released).
- **Exhaustion ships nothing**: swarm-produces-nothing and director-never-
  submits both end with no new play file and no index — the fixture worktree
  stays byte-identical.
- **Budget refusals**: a generation against a spent call max, stoploss or
  depth cap is refused before the agent loop, with the exact guard error.
- **Agent error**: a failing agent loop fails the generation and releases the
  admission.
- **Outcome re-arm**: two runs over one worktree submit two plays, each run
  reporting its own; both stay on disk.
- **Tool set**: file globs always, notebook globs only with the notebook,
  `spawn_role` + `submit_play` on top; prompt carries the workflow, the NOW
  stamp in the play-id format, and the NOTES partial when the notebook is on.
- **Facade**: single-flight (a concurrent `Prepare` is refused while a
  generation is in flight — exactly one runs), cooldown (refused inside,
  runs after), generation-submits and generation-error paths, `Warm`
  materialises without authoring (materialise called once, no generation
  run), and a failed materialisation is logged, never fatal.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] A generation that submits produces one resolved play on disk; one that
      exhausts produces nothing.

## Decision log (session 2026-08-23, clai worker)

- **D-7-1 — The swarm is a seam, and the spawner is the swarm.** `swarm.go`
  defines `Swarm` (`Spawn(ctx, roleID, task) (string, error)`); the
  `Spawner` implements it. The registry's `spawn_role` build closes over the
  seam instead of `*Spawner`, so the director's tool binds to its own swarm
  and a generation test injects a fake. The phase-4 runner and its bounds are
  untouched; the change narrows the tool's dependency, nothing else.
- **D-7-2 — The director is one bounded agent of the generation.** Its
  per-agent tool-call cap is the same hardcoded global maximum
  (`directorToolCalls = maxGenerationCalls`), it admits at depth 0 before the
  loop, and its agent accounts every budgeted model call into the generation
  budget — the stage bounds the whole swarm, the director's own spend
  included. The agent seam (`WithRunDirector`) is the old-theatre `runLLM`
  pattern: production builds the real clai agent, tests script the tools.
- **D-7-3 — The play id's UTC comes from a prompt stamp, not a new tool.**
  The director must author `story_<UTC>` datetime ids, and the closed
  registry (README tool surface) carries no clock. Rather than widen the
  registry, the director's prompt is stamped `NOW <YYYYMMDDTHHMMSSZ>` at
  generation time and directs authoring at or after that instant; the gate's
  duplicate-id refusal keeps the datetime ids honest.
- **D-7-4 — The submit outcome is recorded by a wrapper over the gate.**
  The director's `submit_play` wraps the phase-6 gate: inner success marks
  the generation and records the id. `Outcome` resets per `Run`, so each
  generation re-arms; the facade and API never parse agent output and never
  hold a story in memory.
- **D-7-5 — The cooldown is a hardcoded constant, test-overridable.**
  `generationCooldown = 6h` (decision 13 — never a flag), matching the
  concierge's cadence on the same server. `WithGenerationCooldown` is a test
  seam only; production always runs under the constant.
- **D-7-6 — `slivingdoc.Pull` extracted from `Seed`.** Warm's
  materialisation is the pure pull — no bulletin, no commit, no authoring —
  composed by `Seed` with the bulletin/commit steps. Observable behaviour is
  unchanged; the seam keeps the facade decoupled from the CLI.

## Notes for later phases

- Phase 8's critic runs after this generation and comments for the next.
- Phase 9 wires the facade into serve: `-troupeModel` and
  `-troupeTokenStoploss` through `WithDirectorModel`/`WithGenerationBudget`
  and `WithStoploss`, `WithMaterialise(slivingdoc.Pull)` for `Warm`, and the
  play API reads the newest submitted play from disk.

## Session report (2026-08-23)

Picked up phase 7 from the handover state (README status: "Phase 7 (director
+ swarm) is next"; phases 0–6 already in the tree, all green).

**Built the four planned files plus the two phase-4 touch-ups:**

- `swarm.go` — the `Swarm` seam (spawn by role note + gather) and the
  compile-time proof that the `Spawner` implements it.
- `director.go` — the fixed director role: the prompt (workflow + NOW stamp
  + NOTES partial), the tool set (registry + recording `submit_play`), the
  bounded agent loop (`runAgent`), the outcome recording, and the
  `WithRunDirector` test seam. `DirectorOption`s are prefixed
  `WithDirector*`/`WithGeneration*` to avoid colliding with the phase-4
  `SpawnerOption`s in one package.
- `troupe.go` — the facade: `Prepare` (hardcoded cooldown + single-flight),
  `Warm` (materialise, no seed, no composer), both under the `With*` option
  seam.
- `registry.go`/`spawn.go` — the `spawn_role` build narrowed to the `Swarm`
  seam; the tool now binds to `Swarm`, and the phase-4 tests were updated in
  place (`newSpawnRoleTool(s)` instead of the old method).
- `slivingdoc.go` — `Pull` extracted from `Seed`, so `Warm` materialises
  without authoring; `Seed` composes `Pull` + bulletin + commit unchanged.

**Verification (exact commands and results):**

```bash
go build ./...                       # OK
go vet ./internal/agents/troupe/... ./internal/agents/slivingdoc/...  # OK
go test ./internal/agents/troupe/... ./internal/agents/slivingdoc/...  # ok (both)
go test ./internal/agents/troupe/... -race -count=3                     # ok
go test ./...                        # all packages green
```

The acceptance criteria hold: the phase headline test (mocked swarm →
assemble → real submit end to end) lands one resolved play on disk with its
index entry, and both exhaustion variants leave the fixture worktree
byte-identical.

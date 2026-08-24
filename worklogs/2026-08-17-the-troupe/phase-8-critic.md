# Phase 8 — Critic

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

The one fixed reviewer. After a generation it reads viewer feedback, the
submitted play, and the notes the swarm left, and emits evidence-cited, spicy
comments. It has opinions, never authority: it never drives, never blocks, never
edits.

## Shape (as built)

```text
internal/agents/troupe/critic.go       # fixed critic prompt + run + the append-only gate
internal/agents/troupe/critic_test.go
```

## Behaviour (as built)

- **Fixed advisory role in Go.** Alongside the director, the critic is the
  second fixed pole (director = generative/sovereign, critic =
  reflective/advisory). `Critic.Run(ctx, generationID, outcome)` runs one
  bounded clai agent — the `WithRunCritic` seam is the old-theatre `runLLM`
  pattern, production builds the real agent, tests script the tool objects —
  with the fixed prompt (short-imperative like every other fixed role),
  stamped with the generation id and the generation's outcome.
- **Reads** viewer feedback (`feedback/`), the submitted play (`plays/`), and
  the notes the swarm left — through the read-only file tools `cat`,
  `rows_between`, `ls`, `rg` (the read subset of the closed registry, still
  resolved through the registry so the name→tool table stays the single
  source of truth).
- **Writes** one evidence-cited `criticism` note into `feedback/` through the
  critic-only `write_criticism` gate: the uniform feedback envelope
  (`playId`/`type`/`ts`/`data`) with `type: "criticism"` and
  `data: { generationId, cites, body }` (decision 21). The writer validates
  before it persists — `cites` must be real notebook note paths (a missing
  file, a path escaping the notebook, a non-note path, or the `plays/`
  bookkeeping index are all refused with exact errors), the body non-empty
  and length-capped, the generation id non-empty — stamps `ts` server-side
  and derives the filename (`<playId>_criticism_<utc>.json`), so the
  filename and the body never drift. A note is append-only: an existing
  filename is refused, never overwritten.
- **Never vetoes, never edits.** The "never edits" rule is enforced by the
  tool set, not by the prompt: the critic's instrument set is the read-only
  file tools plus `write_criticism` — no `write_file`, no `apply_patch`, no
  `mkdir`, no `spawn_role`, no `submit_play`, no notebook tools. Its only
  write path is one appended note; a worktree diff across a full run shows
  exactly one added file and every other file byte-identical (pinned).
- **Empty-stage honesty.** If a generation ends by exhaustion with no
  submitted play, the critic still runs — it is not part of the generation
  budget, so a spent stoploss or call max cannot silence it — and comments
  on why nothing shipped, citing whatever notes exist. The note's `playId`
  is empty and the filename is `criticism_<utc>.json`; the play id is pinned
  by the stage from the generation's outcome at tool construction, so the
  critic structurally cannot claim a play that was not submitted (pinned by
  the pinned-tool test and the writer's play-on-disk check).

## Implementation notes (as executed)

1. **The critic runs after the generation, not inside it; there is no review
   gate.** The director's `Run` is untouched. The critic carries its own
   hardcoded per-agent tool-call cap (`criticToolCalls = maxGenerationCalls`,
   the same bound as the director's loop) and **no usage recorder**: it does
   not admit against the generation budget, because an exhausted generation
   (stoploss or call max spent) must still be reviewed. Termination comes
   from the stage, never from the critic.
2. **The criticism note is a `feedback/` note with `type: "criticism"` and
   `data: { generationId, cites, body }`.** `CriticismNote`/`CriticismData`
   are the typed envelope; the writer stamps `ts` server-side (the critic
   never does) and derives the filename from it. The commit half of the
   one-file-per-note write+commit unit is phase 9's serve wiring; this phase
   ships the write (the note lands durably, atomically, via the phase-6
   `writeFileAtomic`).
3. **The critic's output is advisory text, not a gate.** A review that writes
   nothing — the critic found no evidence, or chose not to comment — fails
   no one: `Run` returns nil once the agent loop finishes. The empty-stage
   prompt pushes the critic to comment; the gate refuses a note without
   evidence, so nothing is ever fabricated.
4. **The play id is pinned, not authored.** The `write_criticism` tool is
   constructed per run with the generation's outcome play id (empty when
   nothing shipped) — the model never passes a play id, so the note cannot
   mislabel the generation it reviews, and the writer's play-on-disk check
   refuses a play the worktree does not hold.
5. **Cites are real note paths, validated.** A cite must be a worktree-
   relative path to an existing `.json` note in a notebook directory
   (`feedback/` canonical; `models/`, `clips/`, `voices/`, `sounds/`,
   `gags/`, `roles/`, `plays/` cover the empty stage, where the critic cites
   whatever notes exist). `plays/index.json` is bookkeeping, never a note,
   and is excluded. The acceptance criterion — every criticism note carries
   non-empty, real cites — is enforced by the gate, not requested.
6. **A clock seam, never a flag.** `WithCriticismClock` freezes the writer's
   clock in tests so the append-only refusal (two notes in one second derive
   one filename) is deterministic; production always stamps `time.Now`.

## Tests

`critic_test.go`, all real-filesystem integration over seeded temp worktrees
(per AGENTS.md), with the conformance fixture worktree as the play/repertoire
and a seeded audience feedback note as the evidence trail:

- **The phase headline**: a critic run over a submitted play + feedback trail
  writes exactly one criticism note — uniform envelope, `type: criticism`,
  server-stamped `ts`, `generationId` matching, `cites` the real feedback
  note path, filename `<playId>_criticism_<utc>.json` (derivation pinned
  separately, including the empty-stage `criticism_<utc>.json`).
- **Empty-stage honesty**: an exhausted generation (outcome not submitted)
  still produces a criticism note with empty `playId`, a filename carrying no
  play id, and cites whatever notes exist — no play fabricated (the pinned
  tool's playID is empty; the writer refuses a play not on disk).
- **Never edits**: a full review's worktree diff shows exactly one added file
  (the appended note) and every other file byte-identical — the critic never
  edits the play or the repertoire, only appends.
- **Tool set**: read-only globs (`cat`, `rows_between`, `ls`, `rg`) present;
  `write_file`, `apply_patch`, `mkdir` and the notebook tools absent;
  `write_criticism` on top, pinned to the outcome's play; `write_criticism`
  is NOT in the general registry (critic-only, like `submit_play` is
  director-only).
- **Writer validation**: no cites, a fabricated (missing) cite, a cite
  escaping the notebook, a non-note path, the `plays/index.json` bookkeeping
  index, a non-.json cite, an empty body, an empty generation, a malformed
  play id and a play not on disk — each returns its exact error and writes
  nothing.
- **Append-only**: a second note deriving an existing filename is refused
  (frozen clock), and the refusal rewrites nothing.
- **Tool surface**: `write_criticism` requires non-empty `generation` and
  `body`, an array `cites`; the happy path writes and confirms; the spec
  names the tool and requires `generation`/`cites`/`body` (with `items` on
  the array — clai's `InputSchema.IsOk` requires it).
- **Prompt**: the workflow naming the gate, the generation id stamped, and
  the outcome line — the submitted play id, or `PLAY none` naming no play.
- **Run gates**: an empty generation id is refused before the agent loop; a
  failing agent loop fails the review with the wrapped error.
- **Required options**: a critic without a writer, a model or a config dir is
  refused.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] Every criticism note carries non-empty, real `cites` paths (enforced by
      the gate, pinned by the validation and headline tests).

## Decision log (session 2026-08-23, clai worker)

- **D-8-1 — The critic is outside the generation budget, with its own cap.**
  The critic runs after the generation — never inside it, no review gate —
  and admits against nothing: an exhausted generation (stoploss or call max
  spent) must still be reviewed, so the critic would be silenced by the very
  budget that bounds the generation it reviews. Its per-agent tool-call cap
  is the same hardcoded global maximum as the director's loop
  (`criticToolCalls = maxGenerationCalls`); there is no usage recorder.
- **D-8-2 — The play id is pinned by the outcome, not authored by the
  model.** `write_criticism` is constructed per run with the generation's
  outcome play id (empty when nothing shipped), so the critic structurally
  cannot claim a play that was not submitted — the empty-stage honesty is
  enforced by the tool shape, and the writer's play-on-disk check backs it
  up. The model never passes a play id.
- **D-8-3 — Cites are real note paths, validated by the gate.** A cite must
  be a worktree-relative path to an existing `.json` note in a notebook
  directory (`feedback/` canonical; the other note directories cover the
  empty stage, where the critic cites whatever notes exist — decision 12's
  "feedback note paths" describes the typical case, the gate's rule is
  "real notes"). `plays/index.json` is bookkeeping, never a note, and is
  excluded. Missing, fabricated, escaping or bookkeeping cites are refused
  with exact errors; the acceptance "every criticism note carries
  non-empty, real cites" is enforced, not requested.
- **D-8-4 — The tool set is the "never edits" enforcement, not the prompt.**
  The critic gets the read-only registry file tools plus `write_criticism` —
  no `write_file`, no `apply_patch`, no `mkdir`, no `spawn_role`, no
  `submit_play`, no notebook tools (the notebook is already materialised by
  the facade when the critic runs; pull/commit are not the critic's job). A
  worktree diff across a full review shows exactly one added file.
- **D-8-5 — The note's ts is stamped server-side; the filename derives from
  it.** The critic never stamps time. `CriticismWriter.Write` stamps
  `ts` (RFC3339) and derives `<playId>_criticism_<utc>.json` (compact form)
  from it, so the filename and the body never drift; the empty-stage note
  drops the play id lead (`criticism_<utc>.json`). Append-only: an existing
  filename is refused. The commit half of the write+commit unit is phase 9's
  wiring.
- **D-8-6 — The writer is a separate gate with a clock seam.** Following the
  director/submitter split, `CriticismWriter` is the append-only gate over
  the worktree, testable in isolation; `WithCriticismClock` is a test-only
  seam (like `WithGenerationCooldown`) so the append-only refusal is
  deterministic. Production always stamps `time.Now`.

## Notes for later phases

- Phase 9 wires the critic to run after each generation: `Prepare` runs the
  director, then the critic with the generation's id and outcome
  (`WithGenerationCritic`), and serves the notes through the unified
  feedback directory. The commit half of the write+commit unit lands there
  too. The facade passes `-troupeModel` through `WithCriticModel` (the two
  fixed roles share one model flag, decision 19).
- The empty-stage criticism filename (`criticism_<utc>.json`, no play id
  lead) is the only playId-less feedback note; the feedback API and the
  director's `ls feedback/<playId>_*` pull both treat it as a whole-dir
  note.

## Session report (2026-08-23)

Picked up phase 8 from the handover state (README status: "Phase 8 (critic)
is next"; phases 0–7 already in the tree, all green).

**Built the two planned files:**

- `critic.go` — the fixed advisory role: `Critic` (options `WithCriticism*`,
  avoiding collisions with the director's `WithDirector*` in one package),
  the fixed prompt (workflow + `GENERATION <id>` / `PLAY <id>` or `PLAY
  none` stamp), the read-only tool set resolved through the closed registry
  plus the per-outcome `write_criticism` gate, the bounded agent loop
  (`runAgent`, no usage recorder), and `CriticismWriter` — the append-only
  gate that validates cites/body/generation/play, stamps ts, derives the
  filename and persists atomically via the phase-6 `writeFileAtomic`.
- `critic_test.go` — 14 tests over seeded temp worktrees: the headline
  submitted-play review, the empty-stage review, the never-edits worktree
  diff, the tool-set pin (read-only + pinned gate + not-in-general-registry),
  the writer's ten-case validation table, append-only with a frozen clock,
  the filename derivation, the tool surface, the prompt, and the run gates.

**Verification (exact commands and results):**

```bash
# Baseline (pre-change):
go build ./...                                       # green
go test ./internal/agents/troupe -race -count=1      # ok

# After the change:
go build ./...                                       # green
go vet ./...                                         # clean
go run mvdan.cc/gofumpt@latest -l .                  # no output (clean)
go run honnef.co/go/tools/cmd/staticcheck@latest ./internal/agents/troupe/...  # clean
go run github.com/mibk/dupl@latest -t 80 internal/agents/troupe   # 0 clone groups
go test ./internal/agents/troupe -race -count=3      # ok (acceptance)
go test ./internal/agents/troupe -race -cover -count=1  # ok, 87.3% coverage
go test ./... -race -cover -count=3 -timeout=30s     # ok, all packages
```

The acceptance criteria hold: the headline test lands one criticism note with
real cites on disk, the empty-stage variant lands one with no play id, and
the never-edits diff shows exactly the one appended note.

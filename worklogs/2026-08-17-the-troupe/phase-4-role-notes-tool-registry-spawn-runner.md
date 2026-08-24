# Phase 4 — Role notes + tool registry + spawn runner

**Status:** ✅ Complete (session 2026-08-23, clai worker)
[← README](./README.md)

## Goal

Make roles notes, not Go code. A role is a file in `roles/`; the stage reads
and executes it by name. Every agent gets exactly the tools its role note
selects from a closed registry — a role can select, never define.

## Shape (as built)

```text
internal/agents/troupe/role.go      # role-note reader + validator + RoleSource
internal/agents/troupe/registry.go  # the closed name→tool mapping
internal/agents/troupe/spawn.go     # the recursive spawn_role runner
internal/agents/troupe/role_test.go
internal/agents/troupe/registry_test.go
internal/agents/troupe/spawn_test.go
```

Role note envelope, a flat note — not a grammar asset, never resolved into a
play:

```json
{
  "id": "clown",
  "prompt": "You are the clown. You decide: … You stop: …",
  "tools": ["cat", "write_file", "spawn_role"],
  "budget": 8
}
```

## Behaviour (as built)

- **ParseRole validates before running.** The filename is the identity
  authority (`roles/<id>.json`); the role id shares the asset id charset
  (`^[a-z0-9_-]{1,64}$`) because it becomes a filename. `prompt` is required
  and capped at 8000 runes. Every `tools` entry must be in the closed
  registry — a note naming anything outside it is refused with an exact
  error. `budget` is clamped: absent or negative becomes the default 8,
  oversized is capped at 64. The envelope decodes strictly, like every note:
  an unknown field is drift.
- **The closed registry is the single name→tool mapping.** `toolRegistry` is
  one table: the filesystem read/write tools (`cat`, `rows_between`, `ls`,
  `rg`, `write_file`, `apply_patch`, `mkdir`) as exact clai globs, the
  slivingdoc tools (`mcp_slivingdoc_notes_pull`, `mcp_slivingdoc_notes_commit`)
  enumerated the same way, and `spawn_role` as a dynamic tool built per
  spawner. Exact-name globs are enforcement: a tool clai adds to its own
  registry later matches no glob here and stays unreachable.
- **The spawn runner is recursive and uniform.** `Spawner.Spawn` reads the
  role note, materialises exactly its selected tools, builds a bounded clai
  agent (the role's budget as the tool-call cap) and runs it. `spawn_role`
  is just another registry tool calling the same `Spawn` — the director's own
  spawns run through the same runner; there is no privileged execution path.
- **One substrate.** Roles read and write the notebook through the shared
  slivingdoc pull/commit boundary and the file tools: when the notebook is
  enabled the agent gets the callsign, the exact MCP globs and the byte-
  identical `NOTES` prompt partial. A role selecting an MCP tool with the
  notebook disabled is refused at spawn with an exact error — a role note is
  stable content, never silently degraded.
- **The runner performs no I/O of its own.** It reads role notes through a
  `RoleSource` seam; the shipped implementation reads `roles/<id>.json` from
  a resolver `Snapshot` — the shape the serve wiring hands the troupe when it
  materialises the notebook.

## Implementation notes (as executed)

1. **Budget is a per-spawn tool-call cap** (`agent.WithMaxToolCalls`), clamped
   into [8, 64]. 0 never means unlimited: a spawned agent is always bounded.
2. **The registry test pins the closed set** — the exact ten canonical names.
   Adding a tool is a human-gated change to the table, and the test fails
   until the table and the test agree.
3. **The file-tool globs resolve against clai's own tools** (pinned by a test
   comparing each glob to the tool's `Specification().Name`), so a registry
   name is provably the tool it names, and the notebook globs are covered by
   the shared `mcp_slivingdoc*` glob — the troupe table and the slivingdoc
   globs stay in sync.
4. **Phase 5 hangs the termination authority on this runner.** The spawner is
   shaped for it: `Spawn` is the single choke point where the depth cap, the
   global call max and the token stoploss will reserve and refuse.
5. **STAGE.md** gains a "Roles are notes" section mirroring the envelope and
   the closed registry, so the human-readable note stays honest about what
   the stage executes.

## Tests

- **Role reader** (`role_test.go`): the valid note parses with the filename
  as identity authority; budget clamping (absent → 8, negative → 8, oversized
  → 64, in-range untouched); refusals with exact errors — unregistered tool,
  no tools, duplicate tool, empty prompt, prompt over the cap, id/filename
  mismatch, wrong directory, bad id charset, unknown field; the snapshot
  `RoleSource` reads and validates `roles/<id>.json` notes.
- **Registry** (`registry_test.go`): the closed set is exactly the ten
  canonical names, one entry per name, each with exactly one materialisation
  (glob or build); every file-tool glob resolves to the clai tool it names;
  the notebook tools are enumerated, gated and covered by the shared
  `mcp_slivingdoc*` glob; `spawn_role` is a dynamic per-spawner tool with the
  role/task input contract.
- **Spawn runner** (`spawn_test.go`): a spawned role gets exactly its
  selected tools (file tools as globs, `spawn_role` as a dynamic tool, mixed
  sets in selection order); an MCP tool without the notebook is refused with
  an exact error; the prompt carries the role definition plus the commission
  and names the workspace through the shared NOTES partial when the notebook
  is on; the workspace resolves from the explicit option or the callsign
  args; `spawn_role`'s input validation (role and task required, non-empty
  strings); the missing-role spawn refusal flows back exact. Recursion is
  bounded by Phase 5's depth cap, per the plan.

## Acceptance

- [x] `go test ./internal/agents/troupe -race -count=3` passes.
- [x] A role note selecting an out-of-registry tool is refused with an exact
      error (`tools: "website_text" is not in the closed registry`).

## Decision log (session 2026-08-23, clai worker)

- **D-4-1 — Role notes are a flat envelope, not the asset envelope.** A role
  is `{id, prompt, tools, budget}` in `roles/<id>.json`; roles are executed
  by the spawn runner, never resolved into plays. The envelope decodes
  strictly (unknown field = error), and the filename is the identity
  authority like every note.
- **D-4-2 — Budget = per-spawn tool-call cap, clamped into [8, 64].** Absent
  or negative becomes the default 8; oversized is capped. 0 never means
  unlimited — a spawned agent is always bounded. The termination authority
  (Phase 5) bounds the generation globally on top of this.
- **D-4-3 — The registry maps each name to exactly one materialisation.**
  File and notebook tools are exact clai globs (enumerated, never wildcards,
  so a tool clai adds later stays unreachable); `spawn_role` is a dynamic
  tool built per spawner so recursion always runs through the same runner.
- **D-4-4 — MCP tools without the notebook are refused at spawn, not
  silently dropped.** A role note is stable content; running an agent missing
  its selected tools would be a silent failure. The exact refusal names the
  tool and the cause.
- **D-4-5 — The spawner reads role notes through a `RoleSource` seam and
  performs no I/O of its own.** The shipped implementation reads a resolver
  `Snapshot`; the serve wiring hands the materialised worktree through the
  same seam in Phase 9.
- **D-4-6 — The spawned prompt is role prompt + commission + the shared
  `NOTES` partial** (byte-identical across every agent per AGENTS.md), with
  the workspace named from the explicit option or read back from the callsign
  args.
- **D-4-7 — A duplicate tool selection is an error, not a dedupe.** Exact
  errors return to the director to fix, consistent with the strictness
  everywhere else on the stage.

## Notes for later phases

- Phase 5 enforces the depth cap and token stoploss on this runner.
- Phase 7's director spawns sub-agents through this runner by role note.

## Session report (2026-08-23)

Picked up phase 4 from the handover state (README status: "Phase 4 (role
notes + tool registry + spawn runner) is next"; phases 0–3 already in the
tree).

**Built the three files exactly as planned:**

- `role.go` — the flat role envelope, `ParseRole` (filename identity,
  strict decode, id/prompt/tools checks, budget clamp) and the `RoleSource`
  seam with the snapshot-backed implementation.
- `registry.go` — the closed `toolRegistry` table (seven file-tool globs, two
  notebook-gated MCP globs, one dynamic `spawn_role` constructor) with
  `registryHas`/`lookupTool`.
- `spawn.go` — `Spawner` (functional options, required role source/model/
  config dir), `Spawn` (read → toolSet → bounded agent → run), `toolSet`,
  `prompt` with the shared NOTES partial, and the recursive `spawnRoleTool`.
- `grammar.go` package doc and `STAGE.md` updated to ship the phase's
  surface; the worklog README status advanced to phase 5.

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
go test ./internal/agents/troupe -race -cover -count=1  # ok, 87.8% coverage
go test ./... -race -count=3 -timeout=30s           # ok, all packages
```

# 2026-08-16: slivingdoc MCP communication + self-hosted S3

**Status:** ✅ Complete — all 8 phases done (sessions 1–8) | [Phase list](#phase-status)

## Summary

Kinoview's agents become a communicating company over a shared, inspectable
notebook. The notebook is **slivingdoc** — a git-backed UTF-8 text notebook
that syncs through an S3-compatible bucket and merges concurrent edits with
visible conflict markers. Two agent surfaces get the slivingdoc MCP callsign:

- **concierge** — reads the shared notebook and posts its findings.
- **theatre** — the director and its subagents read and write the production
  board as free-form notes in the notebook, replacing the old structured
  `board.json`.

The S3 backend is **SeaweedFS**, shipped as a static binary and supervised as
a child process by kinoview. No Docker, no systemd, no separate operator
step: kinoview starts `weed`, waits for it, and stops it on shutdown.

The theatre's old durable-memory machinery — the structured board, the seven
company docs, the registry and the deterministic distillation — is removed.
It was a beta and did not earn its complexity. The company's only memory is
what the agents write into slivingdoc themselves.

## Strategy

1. **SeaweedFS is a supervised child, not a dependency.** SeaweedFS is a CLI
   application with a global flag framework and a very large module graph
   (cloud SDKs, three databases, Kafka, Elasticsearch; RocksDB behind a
   `rocksdb` build tag). Importing it as a Go library would bloat kinoview's
   `go.mod` and reach into unstable internals. Instead kinoview ships the
   official static `linux_arm` binary and owns its lifecycle.
2. **slivingdoc is reached through clai's MCP callsign.** The pattern is the
   sakfraga `harvest_agent.go` pattern: a `models.McpServer` with
   `Name: "slivingdoc"` (the callsign), passed via `agent.WithMcpServers`,
   plus the `mcp_slivingdoc*` tool glob. clai spawns the MCP server per agent
   `Setup`; the tools surface as `mcp_slivingdoc_notes_pull` and
   `mcp_slivingdoc_notes_commit`.
3. **`npx slivingdoc`, the npm package.** The MCP server command is `npx`
   with `-y slivingdoc serve` (the package auto-installs headlessly; the
   `-npxCommand` flag overrides the npx path). `--endpoint` and `--path-style`
   point at the local SeaweedFS child.
4. **Agents get file-edit tools so they can make notes.** `cat`,
   `rows_between`, `ls`, `rg`, `write_file` and `apply_patch` are added to
   concierge and theatre, alongside the MCP glob. The loop is
   `notes_pull` → read/edit files → `notes_commit`.
5. **The theatre board becomes free-form prose.** `post_to_board` and
   `read_board` are removed. Roles write their brief, questions, findings and
   scene notes directly into the notebook and read each other's notes back.
   The single-writer working file, ledger and transcript stay local — they
   are the deliverable and the observability, not the conversation.
6. **Durable memory is dropped, not rebuilt.** The `premises`, `repertoire`,
   `sets`, `registry`, `director`, `bulletin` and `audience` docs, the
   deterministic distillation and the registry are removed. The composer
   floor is unaffected: it draws from `model.ValidCharacters` directly, never
   from the registry.
7. **Everything degrades gracefully.** If `weed` or the `slivingdoc` binary
   is missing, kinoview logs a warning and runs without the notebook — the
   agents fall back to their old single-shot behaviour, and the theatre's
   deterministic composer still ships the splash.

## Target architecture

```text
kinoview (one process, rpie)
├── SeaweedFS supervisor  ── spawns/stops  weed server -s3  (127.0.0.1:<s3Port>)
│                                            └── bucket "slivingdoc"
├── shared worktree       ── <cache>/slivingdoc/   (agents pull/commit here)
│
├── concierge (clai agent)
│     WithMcpServers[slivingdoc] + WithToolGlobs[mcp_slivingdoc*, file tools]
│
└── theatre (director + subagents, clai agents)
      WithMcpServers[slivingdoc] + WithToolGlobs[mcp_slivingdoc*, file tools]
      (board = free-form notes in the shared worktree; working/ledger/transcript local)
```

Data flow for one agent note write:

```text
agent
  → mcp_slivingdoc_notes_pull  (materialise notebook into the shared worktree)
  → cat / rows_between / rg     (read)
  → write_file / apply_patch    (edit)
  → mcp_slivingdoc_notes_commit (publish; slivingdoc merges concurrent changes)
```

## Prompt updates

Phases 3 and 5 change agent prompts. The changes follow the `prompting` skill
from the shared skills repo: short imperative lines, one idea per line, no
prose essays, and strict separation of concerns — the prompt states intent and
method, the tool descriptions state how to use a tool, and code carries
budgets, schemas and paths.

The notebook loop is a single shared `NOTES` partial, byte-identical across
every agent, with only the workspace path substituted by the constructor:

```text
NOTES
Pull the shared notebook into <workspace> before you start.
Read what others wrote with the file tools.
Write what you learn for the next agent.
Commit with mcp_slivingdoc_notes_commit with path <workspace> when done.
```

**Concierge (Phase 3)** appends the `NOTES` partial to its system prompt. A
zero slivingdoc server omits the partial and the concierge behaves exactly as
today.

**Theatre (Phase 5)** appends the same `NOTES` partial to the director and
every role prompt, and drops every reference to the removed machinery:
`post_to_board`, `read_board`, `pin_identity`, `registry`, `bulletin`,
`lessons`, earlier productions and set recipes. The director works from the
notebook and submits once the piece is good.

The prompt names only the commit step; `notes_pull` and `notes_commit`
argument lists and the workspace layout live in the MCP tool descriptions, not
the prompt. No file name (`bulletin.md` or otherwise) is hardcoded — agents
discover the layout with the file tools.

## Phase Status

| Phase                                          | Status              | Summary                                                                                                        |
| ---------------------------------------------- | ------------------- | -------------------------------------------------------------------------------------------------------------- |
| [Phase 1](phase-1-seaweedfs-supervisor.md)     | ✅ Done (session 1) | SeaweedFS supervisor: spawn, health-check, stop `weed server -s3`; IAM + bucket setup                          |
| [Phase 2](phase-2-slivingdoc-callsign.md)      | ✅ Done (session 2) | slivingdoc callsign helper: `models.McpServer` builder + shared tool globs + shared worktree                   |
| [Phase 3](phase-3-concierge-wiring.md)         | ✅ Done (session 3) | Concierge: MCP + file tools + prompt teaching pull/read/post/commit                                            |
| Phase 4                                        | ✅ Done (session 4) | Theatre removal: board, seven docs, registry, distillation and their prompt paths                              |
| [Phase 5](phase-5-theatre-slivingdoc-board.md) | ✅ Done (session 5) | Theatre: MCP + file tools + rewritten director/role prompts for the free-form board                            |
| Phase 6                                        | ✅ Done (session 6) | Feedback endpoint → `feedback.jsonl` in the notebook (decision Q6, resolved — see Decisions)                   |
| Phase 7                                        | ✅ Done (session 7) | Config surface: `-s3*` and `-slivingdoc*` flags, graceful degradation, deploy notes                            |
| Phase 8                                        | ✅ Done (session 8) | Quality gate + AGENTS.md update: full QA suite green, readiness window fixed for warm restarts, live smoke run |

Execution order: 1 and 4 are independent and may run in parallel. The real
chain is 2 → 3 and 2 → 5 and (2 + 4) → 6; 7 spans 1 and 2; 8 gates all.

## Decisions

- **Q1 — S3 server = SeaweedFS, supervised child.** Garage is out (official
  docs: no conditional writes). MinIO is out (no 32-bit ARM release; rpie is
  `armv7l`). SeaweedFS supports `If-Match`/`If-None-Match` → 412 on
  `PutObject` (verified in `s3api_object_handlers_put.go`), ships `linux_arm`,
  and supports path-style, multipart and checksum headers.
- **Q2 — slivingdoc reach = clai MCP plumbing.** The sakfraga callsign
  pattern: `agent.WithMcpServers` + `mcp_slivingdoc*` tool glob, running the
  slivingdoc npm package via `npx` (no native binary is shipped).
- **Q3 — Scope = concierge + theatre, read+write.** Classifier, recommender
  and butler get nothing in the first cut: the classifier's 10 concurrent
  clones would spawn 10 MCP servers for no communication value, and
  recommender/butler use the non-agentic querier that does not honour
  `WithMcpServers`.
- **Q4 — Theatre board = free-form markdown (option A).** Remove
  `post_to_board`/`read_board`; roles use `notes_pull` + file tools +
  `notes_commit` directly. Deliverable writers stay (working file).
- **Q5 — Durable docs dropped entirely (option c).** No cross-generation
  memory except what agents write into slivingdoc. Registry, distillation and
  the seven docs are removed.
- **Q6 — Feedback endpoint: keep, append JSONL.** The splash thumbs control
  posts to `feedback.jsonl` in the notebook: one JSON line per note
  (`{storyId, rating, comment, ts}`), appended and committed through
  slivingdoc. No markdown prose, no trimming, no `audience.md`.
- **D-1 — SeaweedFS flags.** `weed server -dir=<s3Dir> -s3 -filer -ip=127.0.0.1
-ip.bind=127.0.0.1 -master.port=<port> -volume.port=<port>
-filer.port=<port> -s3.port=<s3Port> -s3.config=<iamJSON>`; exact IAM JSON
  schema verified against the shipped SeaweedFS version in Phase 1.
- **D-2 — Shared worktree.** `<cache>/slivingdoc/` is the single visible
  workspace every agent materialises into. Seed it with a `bulletin.md` for
  cross-agent notices; agents may create their own files.
- **D-3 — The MCP server's `--private-root`** defaults per process and is
  guarded by slivingdoc's file lock; kinoview passes a dedicated
  `--private-root` under the cache to avoid clashing with any other slivingdoc
  use on the host.
- **D-4 — Endpoint derivation.** When `-slivingdocEndpoint` is empty and the
  SeaweedFS supervisor is running, derive `http://127.0.0.1:<s3Port>` and set
  `--path-style`. When `weed` is absent, the whole slivingdoc feature is
  disabled with a warning.

## Files touched (expected)

New:

```text
internal/s3embed/                # SeaweedFS supervisor (Phase 1)
internal/agents/slivingdoc/      # callsign + globs + worktree helper (Phase 2)
                                 #   notebook.go + feedback.go: handler-side seam (Phase 6)
```

Removed (Phase 4):

```text
internal/agents/theatre/board.go          (and board_test.go)
internal/agents/theatre/docs.go           (and docs_test.go)
internal/agents/theatre/registry.go       (and registry_test.go)
internal/agents/theatre/distill.go        (and distill_test.go)
```

Modified:

```text
cmd/serve/serve.go             # flags (Phase 7)
cmd/serve/serve_setup.go       # supervisor + slivingdoc wiring (Phases 1, 2, 3, 5, 7)
internal/agents/concierge/     # MCP + tools + prompt (Phase 3)
internal/agents/theatre/       # removal + MCP + prompts (Phases 4, 5)
internal/agents/theatre/tools/ # drop post_to_board/read_board (Phase 4)
internal/media/index.go        # feedback handler route (Phase 6)
AGENTS.md                      # architecture + conventions (Phase 8)
```

## Severity taxonomy

- **Critical**: OOM, data loss, process crash
- **High**: breaks a feature or creates incorrect behaviour
- **Medium**: degrades observability or performance
- **Low**: cosmetic

All findings above Low reopen the phase.

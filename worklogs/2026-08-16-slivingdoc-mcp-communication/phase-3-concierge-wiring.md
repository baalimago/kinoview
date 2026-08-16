# Phase 3 — Concierge slivingdoc wiring

**Status:** ✅ Done (session 3)
[← README](./README.md)

## Goal

Give the concierge agent the slivingdoc callsign and the file-edit tools, and
teach it — via its system prompt — to pull the notebook, read the shared
notes, record its own findings and commit them.

## Changes in `internal/agents/concierge/concierge.go`

The `New` constructor currently builds the clai agent with
`agent.WithTools(llmTools)`. It gains:

```go
agent.WithMcpServers([]models.McpServer{slivingdoc.Server(...)}),
agent.WithToolGlobs(slivingdoc.ToolGlobs()...),
```

The existing built-in file tools appended to `llmTools` (`clai_tools.Cat`,
`clai_tools.RowsBetween`) are removed from that list — they now arrive through
the globs, so there is one source of truth for the file toolset. The other
built-ins (`WebsiteText`, `Date`, `FFProbe`) may stay as `WithTools` entries or
move to the same glob set; keep them as they are to minimise churn.

The concierge needs the slivingdoc server values passed in. Add options:

```go
concierge.WithSlivingdocServer(models.McpServer)
```

or, simpler, build the server in `serve_setup.go` and pass it through a single
option. The constructor stores it and applies it only when non-zero (a zero
server means "notebook disabled", so composer-only and unit fixtures keep
working).

## Prompt

Append the shared `NOTES` partial (the `prompting` skill's notebook section)
to `baseSystemPrompt`, byte-identical across every agent, with only the
workspace path substituted:

```text
NOTES
Pull the shared notebook into <workspace> before you start.
Read what others wrote with the file tools.
Write what you learn for the next agent.
Commit with mcp_slivingdoc_notes_commit with path <workspace> when done.
```

The exact workspace path is substituted into the prompt by the constructor
(the same value the MCP server uses), so the model is never asked to guess it.
The pull and commit argument lists stay in the MCP tool descriptions; the
prompt does not restate them, and no file name (`bulletin.md` or otherwise)
is hardcoded.

## Behaviour when disabled

When `slivingdocServer` is zero, the constructor does not add
`WithMcpServers` or `WithToolGlobs`, and the prompt addendum is omitted. The
concierge behaves exactly as today.

## Tests

- `TestConcierge_ToolGlobsIncludeSlivingdoc` asserts the globs are present
  when a server is configured.
- `TestConcierge_NoServerOmitsSlivingdoc` asserts the globs and prompt section
  are absent when the server is zero.
- `TestConcierge_PromptNamesWorkspace` pins the workspace path substitution.

## Changes (session 3)

### `internal/agents/slivingdoc/`

- `NotesPartial(workspace)` — the shared `NOTES` contract, byte-identical
  across every agent with only the workspace path substituted. Lives in the
  notebook package so phase 5's theatre consumes the same literal; the
  concierge prompt joins it with a blank line.
- `WorkspaceRoot(server)` — reads `--workspace-root` back from the callsign
  args, so a prompt can name the exact path the MCP child materialises into.

### `internal/agents/concierge/concierge.go`

- New options `WithSlivingdocServer(models.McpServer)` and
  `WithSlivingdocWorkspace(string)`. A zero server means "notebook disabled".
- `New` gains `agent.WithMcpServers` and `agent.WithToolGlobs(slivingdoc.ToolGlobs()...)`
  only when the server is configured; the `NOTES` partial is appended to the
  prompt the same way. `clai_tools.Cat` / `clai_tools.RowsBetween` leave the
  explicit tool list only when the globs carry them, so the no-server path
  keeps its subtitle-validation workflow byte-for-byte unchanged.
- The wiring decisions are small receiver methods (`notebookEnabled`,
  `notebookWorkspace`, `notebookGlobs`, `buildPrompt`), so the phase's tests
  pin them without reaching into clai's unexported agent fields.

### `cmd/serve/serve_setup.go`

- `slivingdocWorkspaceRoot` hoisted next to `storePath`/`subsPath` and passed
  to the concierge constructor together with the callsign (`c.slivingdocServer`,
  zero when the notebook is disabled).

## Verification (session 3)

```bash
go test ./internal/agents/concierge -race -count=3   # ok (16 tests)
go test ./internal/agents/slivingdoc -race -count=3   # ok (12 tests)
go test ./cmd/serve/... ./internal/agents/... -race -count=1  # ok
go test ./... -race -count=3 -timeout=30s            # QA gate ok
```

Smoke run (`weed` + `slivingdoc` beside the binary, `-concierge gpt-5.2
-conciergeStartupDelay 0`, empty media dir):

```text
SeaweedFS S3 ready at http://127.0.0.1:8333 (bucket "slivingdoc")
slivingdoc notebook ready at /tmp/kinoview-smoke/cache/slivingdoc
concierge setup OK
Running concierge
Call: 'mcp_slivingdoc_notes_pull', inputs: [ 'path': '/tmp/kinoview-smoke/cache/slivingdoc' ]
Call: 'ls' … 'cat' bulletin.md … media_stats …
Call: 'write_file', inputs: [ 'file_path': '…/bulletin.md', 'append': 'true' ]
  Successfully wrote 186 bytes to …/bulletin.md
Call: 'mcp_slivingdoc_notes_commit', inputs: [ 'message': '2026-08-16: …', 'path': '…' ]
fresh pull into a second worktree → bulletin.md +8 (the concierge note)
```

The concierge pulled the notebook, read the bulletin, wrote a dated note and
committed it; a fresh materialisation into a separate worktree returned the
committed note, proving the write survived the S3 round trip.

## Notes for later phases

- Phase 5 consumes `slivingdoc.NotesPartial` and `slivingdoc.WorkspaceRoot`
  for the theatre prompts; the runner should carry the same
  `WithSlivingdocServer` + workspace pair.
- The smoke run re-confirmed a warm SeaweedFS data dir can take longer than
  the supervisor's 15 s readiness window to bring the S3 gateway up (cold
  starts are ~2 s). Phase 7 should expose the window or the probe as a flag.

## Acceptance

- [x] `go test ./internal/agents/concierge -race -count=3` passes.
- [x] A debug run with a live slivingdoc server shows the concierge pulling
      and committing a note file.

# Phase 5 — Theatre slivingdoc board + file tools

**Status:** ✅ Done (session 5)
[← README](./README.md)

## Goal

Wire the slivingdoc callsign and file tools into the theatre runner, and
rewrite the director and role prompts so the production board is free-form
prose in the shared notebook. Roles pull, read, write and commit their own
notes; nothing is structured or distilled.

## Runner wiring

In `runner.go`, `runClai` builds the clai agent. It gains the same two options
as the concierge:

```go
agent.WithMcpServers([]models.McpServer{server}),
agent.WithToolGlobs(slivingdoc.ToolGlobs()...),
```

The `server` is carried on the `Runner` via a new option:

```go
func WithSlivingdocServer(s models.McpServer) RunnerOption
```

A zero server means "notebook disabled": `runClai` omits the MCP options and
the prompt text below is omitted, so composer-only mode and unit fixtures keep
working. The shared workspace path is also carried on the runner so it can be
substituted into prompts.

## Prompts

The working-context standard already injects the generation and theme. The
prompt changes replace the removed board/registry/audience references with a
notebook contract. Every role and the director receive the same shared
`NOTES` partial (the `prompting` skill's notebook section), byte-identical
with only the workspace path substituted:

```text
NOTES
Pull the shared notebook into <workspace> before you start.
Read what others wrote with the file tools.
Write what you learn for the next agent.
Commit with mcp_slivingdoc_notes_commit with path <workspace> when done.
```

The director prompt drops `post_to_board`, `pin_identity`, the bulletin,
lessons, audience notes, earlier productions and the registry — and instead
directs the director to work from the notebook and to submit once the piece
is good.

The role prompts drop the registry, earlier-productions and set-recipes
references. The dramaturg's `write_brief` is gone; its deliverable is the
brief text, which it also writes into the notebook. Notes stay short and
dated; the pull and commit argument lists and the workspace layout live in
the MCP tool descriptions, not the prompt.

## Budgets and telemetry

The MCP and file tools are registered by clai through the tool globs, so they
are not wrapped by the runner's `countingTool` and do not appear in the
ledger's per-tool call counts. The generation's hard bounds still hold:
clai's `WithMaxToolCalls(p.budget)` caps every tool execution, MCP included,
and the stage's wall-clock and global-call gates still refuse new work. Note
the telemetry gap in the ledger and accept it for this cut.

## Tests

- `TestRunner_RunClai_WithSlivingdoc` asserts `WithMcpServers` and
  `WithToolGlobs` are applied when a server is configured.
- `TestRunner_NoServer_OmitsSlivingdoc` asserts the options and prompt text are
  absent when the server is zero.
- `TestRolePrompt_DropsRegistryAndBoardReferences` pins that no role prompt
  mentions `post_to_board`, `read_board`, `registry`, `pin_identity`,
  `bulletin` or `lessons`.

## Acceptance

- `go test ./internal/agents/theatre/... -race -count=3` passes.
- A debug production with a live slivingdoc server shows the director and
  roles pulling, writing and committing note files.

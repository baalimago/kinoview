# Phase 2 — slivingdoc MCP callsign helper

**Status:** ✅ Done (session 2)
[← README](./README.md)

## Goal

One package builds the slivingdoc `models.McpServer` (the callsign) and the
tool globs every agent shares. It mirrors the sakfraga
`harvest_agent.go` pattern with kinoview-specific adaptations: the native
binary, the SeaweedFS endpoint, and path-style addressing.

## Package

New `internal/agents/slivingdoc`.

```text
internal/agents/slivingdoc/slivingdoc.go
internal/agents/slivingdoc/slivingdoc_test.go
```

## API

```go
// Server builds the slivingdoc MCP server. command is the native slivingdoc
// binary path; endpoint is the SeaweedFS S3 endpoint (empty = real AWS, no
// --path-style). workspaceRoot is the single shared worktree.
func Server(command, bucket, region, endpoint, workspaceRoot, privateRoot string) models.McpServer

// ToolGlobs returns the shared tool globs: the slivingdoc callsign plus the
// file tools agents use to read and write notes.
func ToolGlobs() []string
```

`Server` produces:

```go
models.McpServer{
	Name:           "slivingdoc",
	Command:        command,
	Args: []string{"serve",
		"--bucket", bucket,
		"--region", region,
		"--endpoint", endpoint, "--path-style",   // only when endpoint != ""
		"--workspace-root", workspaceRoot,
		"--private-root", privateRoot,
	},
	TimeoutSeconds: 300,
}
```

`ToolGlobs` returns:

```text
mcp_slivingdoc*
cat
rows_between
ls
rg
write_file
apply_patch
mkdir
```

The wildcard `mcp_slivingdoc*` matches both `mcp_slivingdoc_notes_pull` and
`mcp_slivingdoc_notes_commit`. The file tools are clai built-in names, so
enumeration is enforcement: a tool clai adds to its registry later matches no
exact glob and stays unreachable.

## Wiring in serve_setup.go

The serve command resolves:

- `command` — `-slivingdocCommand`, default the native binary path
  (`/home/imago/go/bin/slivingdoc`, or `slivingdoc` on PATH).
- `bucket`, `region` — `-slivingdocBucket` / `-slivingdocRegion`.
- `endpoint` — `-slivingdocEndpoint`, or derived from the SeaweedFS supervisor
  (`http://127.0.0.1:<s3Port>`) when the supervisor is running.
- `workspaceRoot` — `-slivingdocWorkspace`, default `<cache>/slivingdoc`.
- `privateRoot` — `<cache>/slivingdoc-private`.

The serve command also seeds the worktree with a `bulletin.md` file at setup,
so agents always have a conventional cross-agent surface to append to.

## Credentials

The slivingdoc binary reads AWS credentials from its environment. kinoview
writes a small env file (for example `<configDir>/slivingdoc.env`) with the
same `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` the SeaweedFS supervisor
generated (Phase 1), plus `AWS_REGION`. The `McpServer` carries this through
its `EnvFile` field, so clai's MCP client injects it into the child process.

## Tests

- `TestServer_Args` pins the argument list for the endpoint (path-style on)
  and the no-endpoint (path-style off) cases.
- `TestServer_Timeout` pins `TimeoutSeconds == 300`.
- `TestToolGlobs` pins the exact glob list, including the `mcp_slivingdoc*`
  wildcard.

## Acceptance

- [x] `go test ./internal/agents/slivingdoc -race -count=3` passes (10 tests).
- [x] Smoke run: `kinoview serve` with `weed` next to the binary and
      `slivingdoc` on PATH logs "SeaweedFS S3 ready …" then "slivingdoc
      notebook ready at <cache>/slivingdoc"; `bulletin.md` is seeded and
      committed (notebook generation 1); a fresh pull returns the identical
      bulletin; the MCP server built by `Server()` starts and serves
      (`serving … bucket=slivingdoc workspaceRoot=…`).

## Notes for later phases

- Phase 3 consumes `slivingdoc.Server(...)`, `slivingdoc.ToolGlobs()` and
  the workspace path for the concierge; Phase 5 does the same for the
  theatre.
- Phase 6 extends the package with the `Notebook`/`FeedbackRecorder` seam;
  `loadEnvFile` and the `runCLI` seam are reusable there.
- Phase 7 adds the `-slivingdoc*` flags; `resolveSlivingdocCommand` gains
  the flag override and the hardcoded `"us-east-1"` region in
  `serve_setup.go` becomes `-slivingdocRegion`.
- The smoke run re-confirmed the boilerplate's flag parser stops at the
  first positional arg (flags must precede the media path); the slivingdoc
  CLI is the opposite — flags follow the subcommand
  (`slivingdoc pull --workspace-root … <path>`).

- A debug run shows the registered tools as `mcp_slivingdoc_notes_pull` and
  `mcp_slivingdoc_notes_commit` for a wired agent — deferred to the Phase 3
  and Phase 5 acceptance, where an agent is actually wired.

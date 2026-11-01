# The notebook runs slivingdoc v1.3.1 and serves with --login never

## Abstract

kinoview now requires the slivingdoc release binary v1.3.1. Version 1.3.0
added a sign-in on first use to `serve`: without a credential, serve starts,
and the first tool call answers with an approval page instead of an error.
An MCP child of the kinoview server has no person to open that page, so the
callsign args now carry `--login never`. The library dependency clai stays at
v1.13.0, the latest release.

## What changed upstream

| Release | Change | Effect on kinoview |
| ------- | ------ | ------------------ |
| v1.3.0  | `serve` signs in on first use; new `--login auto\|never` | `--login never` is passed; nothing else |
| v1.3.1  | `serve` gives a login without a space a default space | none |

The flags kinoview passes are unchanged across v1.2.0 → v1.3.1: `serve` with
`--bucket`, `--region`, `--endpoint`, `--path-style`, `--workspace-root` and
`--private-root`; `pull` and `commit` with the two roots. `pull` and `commit`
already refuse to start without a credential, so only `serve` needed the flag.

## What changed in kinoview

- `internal/agents/slivingdoc/slivingdoc.go`: `Server` appends
  `--login never` to the serve args.
- `internal/agents/slivingdoc/slivingdoc_test.go`: both arg tables expect it.
- `AGENTS.md`: the notebook note names v1.3.1 and the non-interactive
  contract.

## clai

`go.mod` pins clai v1.13.0. `go list -m github.com/baalimago/clai@latest`
answers v1.13.0, and the release list ends at v1.13.0, so no bump was needed.
The local checkout at `~/Projects/public/clai` is 14 commits ahead of the tag
(audio tools, model aliases, profile truncation); those are unreleased and
kinoview pins releases.

## Proof

Against the compose SeaweedFS gateway (`docker compose up -d seaweedfs`,
bucket `slivingdoc`), with the `.env` credentials:

```bash
/tmp/sl131 pull /tmp/w131 --bucket slivingdoc \
  --workspace-root /tmp/w131 --private-root /tmp/p131r
# OK  generation 1  /tmp/w131 — 1 file, 3 lines, bulletin.md

printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}' \
  | /tmp/sl131 serve --workspace-root /tmp/ws131 --private-root /tmp/ps131 \
      --bucket slivingdoc --region us-east-1 --endpoint http://127.0.0.1:8333 \
      --path-style --login never
# serverInfo version 1.3.1, tool names notes_pull and notes_commit unchanged
```

The server wiring was then checked end to end: `kinoview serve
-slivingdocCommand /tmp/sl131` logged `slivingdoc notebook ready at
~/.cache/kinoview/slivingdoc`, which means the seed pull and the bulletin
commit both succeeded on v1.3.1. `go test ./internal/agents/slivingdoc/
./cmd/serve/ ./internal/agents/concierge/ ./internal/agents/troupe/ -race`
passes.

## Deploying to rpie

Replace the binary on PATH with the armv7l asset; the notebook then reports
1.3.1 and the next serve restart uses `--login never`:

```bash
curl -sL -o ~/go/bin/slivingdoc \
  https://github.com/baalimago/slivingdoc/releases/download/v1.3.1/slivingdoc-v1.3.1-linux-arm
chmod +x ~/go/bin/slivingdoc
~/go/bin/slivingdoc version   # slivingdoc 1.3.1
```

Pull the kinoview binary again after the bump: an older kinoview passes no
`--login never`, and v1.3.1 `serve` then answers a first tool call with an
approval page when the S3 credentials do not resolve.
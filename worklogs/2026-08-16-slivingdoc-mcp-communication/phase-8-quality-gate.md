# Phase 8 — Quality gate + AGENTS.md

## Goal

Close the worklog: full QA suite green, no dead code, documentation updated.

## Gate

| Tool | Command | Result |
| ---- | ------- | ------ |
| Format | `go run mvdan.cc/gofumpt@latest -w -l .` | clean |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| Lint | `go vet ./...` | clean |
| Fix | `go fix ./...` | clean |
| Dupl | `go run github.com/mibk/dupl@latest -t 80 .` | no new clones (27 pre-existing groups, all outside this worklog's files) |
| Test | `go test ./... -race -cover -count=3 -timeout=30s` | pass (all packages, exit 0) |

The full `-race -cover -count=3` suite must pass unedited. Coverage must stay
at or above 70%; the removed theatre files must not be replaced with
false-positive tests.

## Finding fixed this phase

The live smoke run exposed a warm-restart readiness bug: a SeaweedFS data dir
with existing raft state takes ~25 s to come up (leader election + filer
startup) while a fresh dir takes ~2 s. The supervisor's 15 s readiness window
therefore failed every server restart, silently disabling the notebook. The
window is now 60 s (child-death detection still fails fast on every poll) and
`TestSupervisor_Restart` pins the warm path. See
[session-8/decisions.md](./session-8/decisions.md).

## Documentation

Update `AGENTS.md`:

- The package map: add `internal/s3embed`, `internal/agents/slivingdoc`;
  remove the theatre's board/docs/registry/distill files.
- The data-flow section: replace "store is the single source of truth" with
  the slivingdoc notebook as the communication layer, and describe the
  supervised SeaweedFS child.
- The theatre description: the company now communicates through slivingdoc;
  durable docs and distillation are gone.
- The CLI flags table: add the `-s3*` and `-slivingdoc*` flags.
- The QA validation section: unchanged.

## Smoke test (manual, documented)

Run live this session (SeaweedFS 4.41 `linux_amd64` downloaded to /tmp for
the run; the rpie deployment ships `linux_arm`):

1. `kinoview serve -port 0 -configDir <tmp>/cfg -cacheDir <tmp>/cache
   -s3ServerDir <tmp>/s3 -s3ServerPort 18333 -s3MasterPort 19333
   -s3VolumePort 18080 -s3FilerPort 18888 -slivingdocWorkspace <tmp>/worktree
   <tmp>/media` starts `weed`, creates the bucket, and logs the wiring:
   "SeaweedFS S3 ready at http://127.0.0.1:18333 (bucket \"slivingdoc\")"
   and "slivingdoc notebook ready".
2. `POST /gallery/intro/feedback` (204) appends `feedback.jsonl` to the
   worktree and commits it (generation 2) — the handler-side seam round trip.
3. `slivingdoc pull <tmp>/host-pull` on the host (credentials sourced from
   the supervisor's `credentials.env`) shows the committed notes and the git
   history: `OK generation 2` with `bulletin.md +3` and `feedback.jsonl +1`.
4. Ctrl-C (SIGINT) → "initiating webserver graceful shutdown" → "shutdown
   complete", exit 0; no orphaned weed process (verified via `ps`/`ss`).

The concierge/theatre agent pull-write-commit loop (step 2 of the plan) was
exercised through the same callsign + worktree machinery in the phase 3/5
smoke runs; this session's live run covered the seed, the feedback seam and
the warm restart.

## Acceptance

- All QA gates pass.
- `AGENTS.md` reflects the new architecture.
- The reviewer signs off, including the resolved Q6 feedback decision.

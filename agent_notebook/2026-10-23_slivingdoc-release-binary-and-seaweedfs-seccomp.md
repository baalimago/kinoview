# The notebook needs the slivingdoc release binary, and SeaweedFS needs seccomp off

## Symptom

The troupe ("scene builder") was deployed but `/api/v1/troupe/*` answered 404 on
rpie. The chain is a gate: the play API mounts only when the troupe is wired, the
troupe only when `-troupe` is set **and** the slivingdoc notebook came up, and the
notebook only when the seed pull succeeds against the S3 gateway. Any broken link
logs one warning at startup and leaves the API unmounted.

```
troupe 404  ← troupe disabled  ← notebook off  ← seed pull failed  ← S3 gateway down
```

## SeaweedFS on 32-bit ARM: dockerd seccomp, not SeaweedFS

The gateway exited with `futexwakeup ... returned -1` and `SIGSEGV` at `0x1006`.
Go 1.26 calls `futex_time64` on 32-bit ARM. dockerd 26.1.4 ships a seccomp profile
that lists that syscall, but the host `libseccomp2` (2.3.3, Raspbian buster) cannot
resolve the name, so the rule is dropped and the call returns `EPERM`. Go's runtime
treats a failed `futexwakeup` as fatal by design.

Proof: the same image crashes with the default profile and runs with
`--security-opt seccomp=unconfined` (`version 30GB 4.48 ... linux arm`).

`docker-compose.yml` therefore sets `security_opt: [seccomp=unconfined]` on the
SeaweedFS service. Drop it once dockerd's profile resolves `futex_time64`.

Also note: SeaweedFS 4.44 and older panic on 32-bit ARM in the volume `generation`
alignment (upstream PR #11028). The fix shipped in **4.45**, so the compose file
pins `chrislusf/seaweedfs:4.48`.

## slivingdoc: the release binary, no npm

The notebook used to default to `npx -y slivingdoc`. rpie has **no Node.js at all**
(`node`, `npx` and `npm` are all missing; Raspbian 10 buster, armv7l), and the npm
package requires Node >= 22, so that default could never start the notebook there.
The installed binary was an `0.1.0-dev` build from before the notebook existed.

The npm package is only a launcher anyway: `bin/slivingdoc.mjs` downloads the
native binary from the GitHub release, verifies it against `SHA256SUMS`, caches it
and execs it. Both routes end at the same release asset; the binary route just
drops the Node requirement.

The integration targets that release asset (v1.2.0 or later; `linux-arm` covers
rpie's armv7l):

```
curl -sL -o ~/go/bin/slivingdoc \
  https://github.com/baalimago/slivingdoc/releases/download/v1.2.0/slivingdoc-v1.2.0-linux-arm
chmod +x ~/go/bin/slivingdoc
~/go/bin/slivingdoc version   # slivingdoc 1.2.0
```

`serve` resolves the command from `-slivingdocCommand` when set, else `slivingdoc`
on PATH. The CLI surface kinoview uses is unchanged in v1.2.0: the `serve`, `pull`
and `commit` subcommands with `--bucket`, `--region`, `--endpoint`, `--path-style`,
`--workspace-root` and `--private-root`; the env file variables
(`SLIVINGDOC_BUCKET`, `AWS_REGION`, `AWS_ENDPOINT_URL_S3`, `SLIVINGDOC_PATH_STYLE`)
are also unchanged. Only the npx wrapper is gone.

`internal/agents/slivingdoc.Config` carries the binary, the S3 target and both
roots, so the MCP callsign, the startup seed, the troupe's pull and the feedback
and criticism writers cannot drift apart.

There is no kinoview Dockerfile: kinoview runs on rpie as a bare process in a tmux
session. The only container in the repo is the SeaweedFS sidecar.

## Local end-to-end check (no rpie needed)

```
docker compose up -d
docker compose exec -T seaweedfs sh -c 'echo "s3.bucket.create -name slivingdoc" | weed shell'
set -a; . ./.env; set +a
go build -o /tmp/kinoview . && /tmp/kinoview serve \
  -slivingdocCommand /tmp/slivingdoc-v1.2.0-linux-amd64 -troupe gpt-5 /tmp/media
```

Expect `slivingdoc notebook ready at ...`, `troupe ready over the shared notebook
at ...`, and `GET /api/v1/troupe/play` → 200. The bucket gains the seeded
`bulletin.md` on the first run.

Pitfalls found while testing:

- `serve` parses flags **before** the media path. Flags placed after the path are
  dropped, so the defaults silently apply.
- `-slivingdocWorkspace` is defaulted from the cache dir before `-cacheDir` is
  parsed, so overriding `-cacheDir` alone does not move the notebook worktree.
- dockerd may run with a private `/tmp`, so bind-mounting a test binary from `/tmp`
  gives a surprise directory. Mount from a directory the daemon shares.

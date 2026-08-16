# Phase 1 — SeaweedFS supervised child

**Status:** ✅ Done (session 1)
[← README](./README.md)

## Goal

Run SeaweedFS as a child process owned by kinoview, bound to loopback, with a
single S3 gateway and one bucket. No operator step: kinoview spawns it, waits
for readiness, and stops it on shutdown.

## Package

`internal/s3embed` (name reviewed against `seaweed`; `s3embed` chosen — the
package supervises an embedded S3-compatible gateway, not the whole SeaweedFS
product). Files:

```text
internal/s3embed/supervisor.go   # Supervisor lifecycle + options
internal/s3embed/credentials.go  # IAM config, credentials env, key generation, output tail
internal/s3embed/sign_v4.go      # minimal SigV4 signer (no AWS SDK dependency)
internal/s3embed/supervisor_test.go
```

## Shape (as built)

```go
type Supervisor struct { ... }

func New(opts ...Option) *Supervisor
func (s *Supervisor) Start(ctx context.Context) error   // spawn + wait ready + create bucket
func (s *Supervisor) Stop(ctx context.Context) error    // SIGTERM + bounded window + SIGKILL
func (s *Supervisor) Endpoint() string                  // http://127.0.0.1:<s3Port>
func (s *Supervisor) Bucket() string                    // the created bucket
func (s *Supervisor) EnvPath() string                   // credentials env file (Phase 2 sources it)
```

Options: `WithBinary`, `WithDataDir`, `WithS3Port`, `WithMasterPort`,
`WithVolumePort`, `WithFilerPort`, `WithBucket`, `WithAccessKey`/`WithSecretKey`.
Defaults: `8333` / `9333` / `8080` / `8888`, bucket `slivingdoc`, data dir
`<user-config-dir>/kinoview/s3`.

## Verified against the shipped SeaweedFS 4.41

- **IAM schema** (`weed/s3api/auth_credentials_static_config_test.go`,
  `weed/command/s3.go`): `{"identities":[{"name","credentials":[{"accessKey","secretKey"}],"actions"}]}`.
  Bucket creation (`PutBucketHandler`) requires `Admin`; `ListBucketsHandler`
  requires `List`. The identity carries `["Admin","Read","Write","List","Tagging"]`.
- **Flag names** (`weed/command/server.go`): `-dir`, `-ip`, `-ip.bind`,
  `-master.port`, `-volume.port`, `-filer`, `-filer.port`, `-s3`, `-s3.port`,
  `-s3.config=<iam.json path>`. `-s3` implies `-filer`.
- **Stop timing**: SIGTERM triggers a graceful shutdown whose filer gRPC stop
  has a hardcoded 10 s timeout (`filer.go:483`); observed clean exit ~11 s.
  `-volume.preStopSeconds=0` removes the volume's extra pre-stop heartbeat
  delay. The supervisor's bounded SIGTERM window is 15 s, then SIGKILL with a
  5 s final wait.
- **Loopback hygiene**: `-master.telemetry=false` (no telemetry home calls) and
  `-s3.port.iceberg=0` (no extra listener that could collide with host ports).

## Implementation notes

### Start

1. Resolve the weed binary: explicit path → next to the current executable →
   PATH. Nothing resolves → sentinel `ErrBinaryNotFound` (serve logs a warning
   and runs without the notebook). An explicit-but-missing path fails with a
   message naming the path.
2. Prepare credentials: explicit options win, then the persisted
   `credentials.env`, then a generated pair. Generated keys are random base32
   (shell-safe for env + command line), persisted to `credentials.env` so
   restarts reuse the same keys.
3. Write `<dataDir>/iam.json` (SeaweedFS `-s3.config`) and
   `<dataDir>/credentials.env` (the slivingdoc MCP server config sources it in
   Phase 2): `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`,
   `AWS_ENDPOINT_URL_S3`, `SLIVINGDOC_BUCKET`, `SLIVINGDOC_PATH_STYLE`.
4. Spawn `weed server` with the D-1 flag set, own process group (`Setpgid`),
   stdout/stderr captured into a 200-line tail for diagnostics.
5. Wait for readiness: signed `ListBuckets` (GET /) every 500 ms up to 60 s.
   The window must cover a warm restart: a data dir with existing raft state
   takes ~25 s for the master to elect a leader and the filer to come up
   (fresh dirs are ~2 s); the 15 s window of the original build failed warm
   restarts, fixed in phase 8. The child-death check still fails fast, so
   the window only bounds the wedged-but-alive case.
   Any HTTP answer counts; a dead child or a full window fails with the
   captured log tail. The readiness/bucket HTTP client carries a 2 s timeout
   so a wedged gateway can never hang Start forever. SigV4 signing is a
   ~80-line in-package signer — no AWS SDK dependency (the strategy bans
   SeaweedFS as a library; the SDK is not needed for two request shapes).
6. Create the bucket: signed `PUT /<bucket>`; `409` (already owned) counts as
   success, so restarts are idempotent.

### Stop

SIGTERM, wait up to 15 s, escalate to SIGKILL, wait up to 5 s. The caller's
ctx only aborts the final SIGKILL wait — the graceful window is always
honoured, so a shutdown path with a cancelled context still stops the child
cleanly.

### Serve wiring

`cmd/serve/serve_setup.go` constructs the supervisor with
`WithDataDir(path.Join(*c.configDir, "s3"))` and starts it during Setup; any
start failure logs a warning and the server runs without the notebook.
`cmd/serve/serve.go` stops the supervisor on **every** exit path (normal and
error) with `context.Background()` — a supervised child must never be
orphaned. The serve tests pin `PATH=""` (`withoutWeed`) so a weed binary on a
developer's PATH can never spawn a real child into the test environment.

## Tests

| Test | Kind | Asserts |
| --- | --- | --- |
| `TestSupervisor_Endpoint` | unit | endpoint derivation from the S3 port |
| `TestSupervisor_Defaults` | unit | option defaults (ports, bucket, data dir) |
| `TestSupervisor_EnvPath` | unit | env file path beside the data dir |
| `TestSupervisor_MissingBinary` | unit | `ErrBinaryNotFound` without spawning; explicit missing path names itself |
| `TestCredentials_PersistAcrossRestarts` | unit | explicit keys win; else persisted env file; else generated |
| `TestCredentials_EnvFile` | unit | env file contract (AWS_* + SLIVINGDOC_*) |
| `TestCredentials_IAMConfig` | unit | IAM JSON schema + action set |
| `TestSignV4` / `TestCanonicalQuery` | unit | SigV4 request shape, %20 query encoding |
| `TestSupervisor_StartStop` | integration | real weed spawn, readiness, clean stop |
| `TestSupervisor_BucketCreated` | integration | signed HEAD on the bucket answers 200 after Start |

The integration tests spawn the real weed binary from `S3EMBED_TEST_BIN` and
skip in `-short` mode or when the env var is unset — the QA gate
(`go test ./... -race -count=3 -timeout=30s`) must never depend on an external
fixture. On this machine the fixture is SeaweedFS 4.41 (`linux_amd64`,
downloaded for development; the rpie deployment ships `linux_arm` per Phase 7).

## Acceptance

- [x] `go test ./internal/s3embed -race -count=3` passes (78.4 s with the
  fixture; 0.005 s without).
- [x] Full QA gate `go test ./... -race -count=3 -timeout=30s` passes
  (s3embed integration tests skip without `S3EMBED_TEST_BIN`).
- [x] Smoke run: `kinoview serve -configDir <tmp>/cfg -cacheDir <tmp>/cache
  -port 0 <tmp>/media` with `weed` next to the built binary —
  "SeaweedFS S3 ready at http://127.0.0.1:8333 (bucket \"slivingdoc\")";
  `slivingdoc pull` → `OK generation 0`; a note file committed via
  `slivingdoc commit` → `OK generation 1` (`hello.md +1`); SIGINT → graceful
  shutdown, weed child reaped (no orphan, verified via `ps`).

## Notes for later phases

- Phase 2 consumes `Supervisor.EnvPath()`, `Endpoint()` and `Bucket()` for the
  slivingdoc MCP server config.
- Phase 7 adds the `-s3*` flag surface; the supervisor's resolution order
  already supports an explicit binary path.

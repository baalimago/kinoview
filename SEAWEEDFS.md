# SeaweedFS — the slivingdoc S3 backend

A standalone SeaweedFS (single `server` process) that backs the shared agent
notebook. kinoview no longer spawns or supervises SeaweedFS: it connects to the
S3 gateway this stack publishes, with the credentials in `.env`.

## Why we build the image instead of pulling it

The official `chrislusf/seaweedfs` image panics on 32-bit ARM (rpie is
`armv7l`):

```
panic: unaligned 64-bit atomic operation
internal/runtime/atomic.Store64(...)
github.com/seaweedfs/seaweedfs/weed/cluster.(*LiveLock).doLock.func1(...)
```

`weed/cluster.LiveLock.generation` is a 64-bit field accessed with
`atomic.StoreInt64`/`atomic.LoadInt64`, but it is the *last* field of the struct,
so on 32-bit ARM (where `int64` is 4-byte aligned) it lands on a non-8-byte
address and every lock acquisition panics. The S3 gateway's bucket-size-metrics
loop takes the `s3.leader` lock ~10 s after startup, so the container
crash-loops before kinoview ever connects. The bug is present upstream in every
4.x release.

`docker/Dockerfile` builds `weed` from source with `docker/seaweedfs-arm32-align.patch`
applied (moves `generation` to the first field, which Go guarantees 8-byte
aligned). Because the build compiles natively for each host architecture, the
same stack works on rpie (armv7) and amd64 without a registry.

## Bring it up

Copy `.env.example` to `.env` and set the credentials, then:

```bash
docker compose up -d --build
```

Data lives in the `seaweedfs-data` named volume and survives `down`/restarts.
Only the S3 gateway (`8333`) is published, and it binds `0.0.0.0` so a
workstation on the LAN can inspect the bucket without port forwarding. kinoview
itself always dials `http://127.0.0.1:8333`; the `0.0.0.0` bind is for checkout
and debug only.

## Credentials

`.env` declares one identity; the container's entrypoint writes it into
SeaweedFS's static-identity IAM config at startup. kinoview and any checkout
client must use the same pair, supplied as the standard AWS env vars:

```bash
export AWS_ACCESS_KEY_ID=placeholder-access-key
export AWS_SECRET_ACCESS_KEY=placeholder-secret-key
```

kinoview reads exactly those two variables (plus `-slivingdocEndpoint`, default
`http://127.0.0.1:8333`) and writes the rest of the credentials contract itself.
To rotate the credentials, edit `.env`, then `docker compose restart` so the
container regenerates its IAM config from the new values, and change the two env
vars kinoview reads to match.

The entrypoint starts the gateway with `-s3.iam=false`, which disables the
embedded IAM *API* (kinoview does not use it) and avoids the "no signing key
found for STS service" error the IAM manager would otherwise log. Static SigV4
auth from `-s3.config` is unaffected.

## Checkout the notebook from a workstation

```bash
export AWS_ACCESS_KEY_ID=placeholder-access-key
export AWS_SECRET_ACCESS_KEY=placeholder-secret-key
npx -y slivingdoc pull \
  --workspace-root /tmp/notebook \
  --private-root /tmp/notebook-private \
  --endpoint http://<host>:8333 --path-style \
  /tmp/notebook
```

Replace `<host>` with the machine running this stack (`rpie` on the home LAN).
The `slivingdoc` bucket is created on first use, so no setup step is needed
beyond `docker compose up -d --build`.

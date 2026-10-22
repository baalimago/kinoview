# SeaweedFS — the slivingdoc S3 backend

A standalone SeaweedFS (single `server` process) that backs the shared agent
notebook. kinoview no longer spawns or supervises SeaweedFS: it connects to the
S3 gateway this stack publishes, with the credentials in `.env`.

## Why we build the image instead of pulling it

We build from source for the base image, not for the code. The official
`chrislusf/seaweedfs` image is Alpine based, and rpie is `armv7l` on a Raspberry
Pi vendor kernel that returns garbage from the 32-bit ARM `clock_gettime64`
syscall. musl 1.2 relies on that syscall for time, so TLS inside Alpine
containers fails on rpie and `apk add` / `git clone` do not work there. Debian
(glibc) uses the legacy clock and works. `docker/Dockerfile` therefore builds
`weed` from a pinned upstream release on `debian:bookworm-slim`, natively for
the host architecture, so rpie (armv7) and amd64 use the same stack without a
registry.

We used to also carry `docker/seaweedfs-arm32-align.patch`, because
`weed/cluster.LiveLock.generation` was the *last* field of the struct: on 32-bit
ARM (`int64` is 4-byte aligned) it landed on a non-8-byte address and every lock
acquisition panicked with `unaligned 64-bit atomic operation`. That fix is
upstream since **4.45** ([PR #11028](https://github.com/seaweedfs/seaweedfs/pull/11028)),
so the patch is gone and the Dockerfile pins 4.48. Do not pin below 4.45 on
armv7.

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

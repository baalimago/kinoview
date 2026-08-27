# SeaweedFS — the slivingdoc S3 backend

A standalone SeaweedFS (single `server` process) that backs the shared agent
notebook. kinoview no longer spawns or supervises SeaweedFS: it connects to the
S3 gateway this stack publishes, with the credentials in `.env`.

## Bring it up

Copy `.env.example` to `.env` and set the credentials, then:

```bash
docker compose up -d
```

Data lives in the `seaweedfs-data` named volume and survives `down`/restarts.
Only the S3 gateway (`8333`) is published, and it binds `0.0.0.0` so a
workstation on the LAN can inspect the bucket without port forwarding. kinoview
itself always dials `http://127.0.0.1:8333`; the `0.0.0.0` bind is for checkout
and debug only.

## Credentials

`.env` declares one identity; the container writes it into SeaweedFS's IAM
config at startup. kinoview and any checkout client must use the same pair,
supplied as the standard AWS env vars:

```bash
export AWS_ACCESS_KEY_ID=placeholder-access-key
export AWS_SECRET_ACCESS_KEY=placeholder-secret-key
```

kinoview reads exactly those two variables (plus `-slivingdocEndpoint`, default
`http://127.0.0.1:8333`) and writes the rest of the credentials contract itself.
To rotate the credentials, edit `.env`, then `docker compose restart` so the
container regenerates its IAM config from the new values, and change the two env
vars kinoview reads to match.

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
beyond `docker compose up`.

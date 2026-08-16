# Phase 7 — Config surface and graceful degradation

**Status:** ✅ Done (session 7)
[← README](./README.md)

## Goal

Expose the SeaweedFS and slivingdoc knobs as serve flags, with defaults that
make a deployed rpie "just work" and a missing dependency degrade to the old
behaviour.

## Flags (added to `cmd/serve/serve.go`)

```text
-s3ServerPath        string   path to the weed binary; empty = auto-discover
                              (next to the kinoview binary, then "weed" on PATH)
-s3ServerPort        int      S3 gateway port (default 8333)
-s3ServerDir         string   SeaweedFS data dir (default <configDir>/s3)
-s3MasterPort        int      (default 9333)
-s3VolumePort        int      (default 8080)
-s3FilerPort         int      (default 8888)

-slivingdocCommand   string   slivingdoc binary (default auto-discover:
                              /home/imago/go/bin/slivingdoc, then "slivingdoc")
-slivingdocBucket    string   (default "slivingdoc")
-slivingdocRegion    string   (default "us-east-1")
-slivingdocEndpoint  string   empty = derive http://127.0.0.1:<s3ServerPort>
-slivingdocWorkspace string   shared worktree (default <cache>/slivingdoc)
-slivingdocDisable   bool     force-disable the notebook even when binaries exist
```

The S3/slvingdoc feature is enabled when both binaries resolve and
`-slivingdocDisable` is false. Any other state logs one warning and continues
without the notebook.

## serve_setup.go flow

```text
resolve weed binary
  ├── missing → warn, slivingdoc disabled
  └── found  → start supervisor, derive endpoint

resolve slivingdoc binary
  ├── missing → warn, slivingdoc disabled
  └── found  → build slivingdoc.Server(...), pass to concierge + theatre

write slivingdoc env file (access key / secret / region)
seed shared worktree with bulletin.md
```

The supervisor's `Stop` is registered on the existing shutdown path so a
normal context cancel always stops `weed` and flushes the store.

## Deploy note (human-only, documented for review)

The `weed` static `linux_arm` binary and the `slivingdoc` binary are shipped
next to `/home/imago/go/bin/kinoview` on rpie. The existing deploy procedure
gains one line:

```text
scp weed rpie:/home/imago/go/bin/weed
scp slivingdoc rpie:/home/imago/go/bin/slivingdoc
```

No systemd, no Docker, no separate supervisor — kinoview owns both children.

## Tests

- `TestResolveWeedBinary_Found` / `TestResolveWeedBinary_NotFound`.
- `TestResolveSlivingdocBinary_Found` / `TestResolveSlivingdocBinary_NotFound`.
- `TestDisabled_NoWiring` asserts the supervisor and MCP options are absent
  when `-slivingdocDisable` is set.
- `TestEndpointDerivedFromSupervisor` pins the `http://127.0.0.1:<port>` form.

## Acceptance

- `go test ./cmd/serve/... -race -count=3` passes.
- `kinoview serve -help` lists every flag above.

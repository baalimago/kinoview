# Subtitle Storage Architecture

## Purpose

This document supplements `architecture/subtitles-v2.md` with storage-specific detail.

It exists because subtitle persistence is the part most likely to become brittle if underspecified.

The design here is intentionally stdlib-only and filesystem-backed.

---

## Storage goals

- no additional database driver dependency,
- deterministic layout,
- strong crash tolerance through atomic writes,
- explicit repository invariants,
- simple recovery by scanning disk,
- clear separation between metadata and file bytes.

---

## Directory layout

Recommended root:

```text
<cache-root>/kinoview/subtitles/
  resources/
    <subtitle-id>.json
  bindings/
    <item-id>.json
  files/
    <item-id>/
      <subtitle-id>.vtt
      <subtitle-id>.orig.srt
  tmp/
```

---

## Metadata records

### Resource record

One file per subtitle resource:

```text
resources/<subtitle-id>.json
```

This file stores subtitle metadata only.

### Binding record

One file per item binding:

```text
bindings/<item-id>.json
```

This file stores the current default subtitle for the item.

---

## Index rebuild on startup

Repository startup should:

1. ensure directories exist,
2. read all resource files,
3. validate and populate in-memory indexes,
4. read all binding files,
5. validate cross-references,
6. continue past invalid records with warnings.

The storage layer should recover from partial bad state, not panic.

---

## Atomic write protocol

All writes should follow this pattern:

1. create temp file in target directory or dedicated temp directory,
2. write full contents,
3. close file,
4. rename to final destination.

The final rename must happen within the same filesystem boundary.

Never stream directly into final metadata files.

---

## In-memory indexes

Recommended indexes:

- `subtitleID -> resource`
- `itemID -> []subtitleID`
- `itemID -> binding`
- `itemID + sourceRef -> subtitleID`
- `itemID + checksum -> []subtitleID`

Do not persist these indexes separately unless proven necessary.
Rebuilding them from canonical metadata files is simpler and safer.

---

## Dedupe rules

MVP dedupe signals:

1. exact source ref match within an item,
2. optional checksum match within an item.

This is enough to prevent common duplicate imports without overcomplicating the system.

---

## Cleanup rules

When item metadata is removed:

- delete item binding,
- delete all subtitle resource records for item,
- delete file directory under `files/<item-id>/`.

Temp files older than a safe threshold may be cleaned during startup.

---

## Why not a generic ad-hoc DB

Because the subtitle domain only needs a narrow set of operations.

The repository should implement those operations directly rather than inventing:

- query parsing,
- generic indexes,
- generic relations.

Keep it small, explicit, and application-specific.

# Subtitle System Architecture v2

## Purpose

This document defines the recommended subtitle architecture for Kinoview after reviewing the current implementation and the earlier `architecture/subtitles.md` draft.

This version is intentionally more concrete. It is designed to be:

- maintainable,
- stdlib-friendly,
- migration-aware,
- deterministic at runtime,
- safe around ffmpeg and downloaded files,
- easy to test end to end.

It assumes the project may prefer to avoid introducing SQL drivers or other heavy persistence dependencies.

The design therefore uses:

- Go standard library,
- filesystem-backed metadata,
- atomic file writes,
- in-memory indexes rebuilt on startup,
- explicit repository invariants.

---

## Executive summary

Kinoview should treat subtitles as first-class associated resources without changing `model.Item`.

The recommended system has these main parts:

1. **Subtitle domain model**
   - persistent subtitle resources,
   - external search candidates,
   - per-item default bindings,
   - playback resolution output.

2. **File-backed subtitle metadata repository**
   - stdlib-only,
   - JSON records on disk,
   - in-memory indexes for fast lookup,
   - atomic writes and deterministic recovery.

3. **Subtitle file store**
   - stores original and canonical subtitle files,
   - prevents path traversal,
   - owns deterministic file layout.

4. **Embedded subtitle importer**
   - wraps the existing `StreamManager` and `SubtitleSelector`,
   - turns embedded stream extraction into first-class subtitle resources.

5. **External subtitle provider and service**
   - search external systems,
   - fetch a selected candidate,
   - validate, normalize, convert, persist.

6. **Subtitle resolver**
   - determines the actual subtitle used for playback,
   - centralizes override/default/fallback logic.

7. **Thin tools and handlers**
   - no business logic,
   - only input validation, service calls, and response rendering.

The canonical serving identity should be **subtitle resource ID**, not stream index.

---

## Current-state assessment

The current codebase already has subtitle-related behavior, but it is not yet a full subtitle domain.

### Existing capabilities

Current implementation already includes:

- `internal/media/stream.Manager`
  - `Find(item)` using `ffprobe`,
  - `ExtractSubtitles(item, streamIndex)` using `ffmpeg`,
  - extracted `.vtt` storage,
  - in-memory ffprobe caching.

- `internal/agents/tools/preload_subtitles.go`
  - item lookup,
  - stream discovery,
  - subtitle stream selection,
  - extraction.

- `internal/media/storage/handlers.go`
  - a stream-info handler,
  - a subtitle-serving handler based on media stream index.

- `model.Suggestion.SubtitleID`
  - which indicates the system already expects subtitle identity,
  - but does not yet back that identity with a proper subtitle resource repository.

### Current limitations

Today subtitles are primarily:

- an extraction/cache concern,
- tied to stream indexes and media containers,
- not repository-backed,
- not resolved through a dedicated playback abstraction.

This document defines the target architecture that evolves the current implementation into a coherent subtitle subsystem.

---

## Non-goals

This architecture does **not** aim to build:

- a generic database engine,
- a generic query layer,
- a full public subtitle management API all at once,
- unconstrained shell tooling for agents.

It is a narrow, application-specific subtitle system.

---

## Core design rules

### 1. `model.Item` stays generic

Do not add subtitle-specific fields to `model.Item`.

Subtitle state belongs in subtitle-specific models keyed by `Item.ID`.

### 2. Subtitle resource ID is the canonical identity

Playback, tools, and future APIs should primarily refer to subtitles by subtitle resource ID.

Stream index is an implementation detail for embedded import, not the domain-level identity.

### 3. Default subtitle is a binding, not a flag duplicated everywhere

The default subtitle for an item must have a single source of truth:

- `SubtitleBinding`

The repository may enrich read models with `IsDefault`, but should not persist both a binding and independent `IsDefault` flags that can diverge.

### 4. Metadata and file bytes are separate concerns

- repository owns subtitle metadata and bindings,
- file store owns subtitle file bytes,
- service layers coordinate the two.

### 5. Operations should be idempotent when feasible

Repeated import/fetch/default-setting operations should converge on one stable result instead of creating duplicated state.

### 6. No shell execution

All ffmpeg/ffprobe interactions must execute binaries directly with explicit args and bounded timeouts.

---

## Recommended package layout

### `internal/model`

Contains the core domain types:

- `SubtitleResource`
- `SubtitleCandidate`
- `SubtitleBinding`
- `ResolvedSubtitle`
- any small typed enums used by those models.

### `internal/media/subtitles`

Own subtitle domain logic. Suggested files or subareas:

- `repository.go`
- `file_store.go`
- `service.go`
- `resolver.go`
- `importer.go`
- `provider.go`
- `converter.go`
- `ids.go`
- `validation.go`
- `atomic.go`

Keep it cohesive. Avoid scattering subtitle logic across handlers and legacy storage code.

### `internal/media/stream`

Retains the low-level embedded stream inspection and extraction capability.

This remains a useful dependency of the new subtitle subsystem, but should not grow into the new subtitle repository/service layer.

### `internal/agents/tools`

Expose thin subtitle and ffmpeg tools:

- `preload_subtitles`
- `subtitle_search`
- `subtitle_fetch`
- `subtitle_associate`
- `subtitle_list`
- optional `ffmpeg` tool

### `internal/media`

Handlers should depend on the subtitle resolver or subtitle repository-facing APIs, not directly reconstruct business logic.

---

## Domain model

### 1. SubtitleResource

Represents a subtitle Kinoview has persisted and can serve.

Suggested structure:

```go
type SubtitleOrigin string

const (
	SubtitleOriginEmbedded   SubtitleOrigin = "embedded"
	SubtitleOriginDownloaded SubtitleOrigin = "downloaded"
	SubtitleOriginManual     SubtitleOrigin = "manual"
	SubtitleOriginGenerated  SubtitleOrigin = "generated"
)

type SubtitleSource string

const (
	SubtitleSourceEmbedded      SubtitleSource = "embedded"
	SubtitleSourceOpenSubtitles SubtitleSource = "opensubtitles"
	SubtitleSourceManualURL     SubtitleSource = "manual_url"
	SubtitleSourceGenerated     SubtitleSource = "generated"
)

type SubtitleFormat string

const (
	SubtitleFormatVTT SubtitleFormat = "vtt"
	SubtitleFormatSRT SubtitleFormat = "srt"
	SubtitleFormatASS SubtitleFormat = "ass"
	SubtitleFormatSSA SubtitleFormat = "ssa"
	SubtitleFormatSUB SubtitleFormat = "sub"
	SubtitleFormatUnknown SubtitleFormat = "unknown"
)

type SubtitleResource struct {
	ID               string
	ItemID           string
	Source           SubtitleSource
	Origin           SubtitleOrigin
	Language         string
	Format           SubtitleFormat
	Label            string
	StorageKey       string
	OriginalStorageKey string
	OriginalFileName string
	MIMEType         string
	ChecksumSHA256   string
	SizeBytes        int64
	Score            float64
	SourceRef        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
```

### Why this model is preferred over `Path` + booleans

- `StorageKey` is less brittle than persisting raw absolute file paths,
- `OriginalStorageKey` allows keeping original files separate from canonical playback files,
- `ChecksumSHA256` supports dedupe and integrity checks,
- `Origin` is clearer than `IsExternal`,
- `SourceRef` allows idempotency for embedded streams or provider references.

### 2. SubtitleCandidate

Represents a search result before Kinoview owns it.

```go
type SubtitleCandidate struct {
	Provider          string
	ProviderCandidateID string
	Language          string
	Label             string
	Format            SubtitleFormat
	Score             float64
	Release           string
	FileName          string
	FetchToken        string
}
```

Important notes:

- candidate IDs are provider-scoped, not internal resource IDs,
- candidates may go stale,
- `FetchToken` may optionally carry an opaque short-lived resolver token.

### 3. SubtitleBinding

Represents the item-level default subtitle.

```go
type SubtitleBinding struct {
	ItemID            string
	DefaultSubtitleID string
	UpdatedAt         time.Time
}
```

This is the single source of truth for defaults.

### 4. ResolvedSubtitle

Represents what playback will actually use.

```go
type ResolvedSubtitle struct {
	SubtitleID string
	ItemID     string
	Path       string
	Format     SubtitleFormat
	Language   string
	Source     SubtitleSource
	Origin     SubtitleOrigin
}
```

This keeps handlers from reconstructing resolution logic ad hoc.

---

## Identity and ID policy

The system must define IDs clearly.

### Internal subtitle resource IDs

Generated by Kinoview.

Requirements:

- globally unique enough for local runtime,
- stable once written,
- not reused,
- not derived from provider ID alone.

Recommended format:

- `sub_<timestamp>_<random>`

This can be implemented using stdlib only with:

- `time.Now().UTC()`,
- random bytes from `crypto/rand`,
- hex encoding.

### Source references

Used for idempotency and dedupe of origins.

Examples:

- embedded stream: `embedded:stream:2`
- provider candidate: `opensubtitles:123456`
- manual URL: `manual_url:https://...`

These are **not** the canonical subtitle ID. They are provenance keys.

---

## Persistence model: stdlib-only metadata repository

The subtitle metadata repository should be a file-backed repository that behaves like a narrow embedded database.

This is the recommended zero-extra-dependency solution.

### Responsibility

Owns:

- subtitle resource metadata,
- item default bindings,
- startup rebuild of in-memory indexes,
- validation of repository invariants.

Does not own:

- subtitle bytes,
- ffmpeg,
- network calls,
- playback policy.

### On-disk layout

Recommended root:

```text
<config-root>/kinoview/subtitles/
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

Optional later additions:

- `quarantine/` for rejected downloads,
- `failed/` for debugging bad input,
- `versions/` if revisions are introduced.

### Why this layout

- `resources/` contains one persisted metadata record per subtitle,
- `bindings/` contains one current default-binding record per item,
- `files/` stores canonical and original subtitle files,
- `tmp/` supports atomic file writes and safe staging.

### In-memory indexes

Build at startup and update on mutation:

```go
type repositoryIndex struct {
	bySubtitleID map[string]model.SubtitleResource
	byItemID     map[string][]string
	bindings     map[string]model.SubtitleBinding
	byChecksum   map[string][]string
	bySourceRef  map[string]string
}
```

Purpose:

- `bySubtitleID`: fastest direct lookup,
- `byItemID`: list subtitles for an item,
- `bindings`: resolve item defaults,
- `byChecksum`: dedupe candidate imports,
- `bySourceRef`: idempotent re-import for embedded or provider-based subtitles.

This is enough. Do not invent a generic query engine.

### Startup rebuild behavior

On startup, repository should:

1. create required directories,
2. load every `resources/*.json`,
3. validate each resource,
4. populate all indexes,
5. load every `bindings/*.json`,
6. validate that bindings reference an existing subtitle belonging to the same item,
7. log and ignore invalid/corrupt metadata entries rather than panic.

Repository startup should favor resilience and observability over crashing on one bad record.

### Repository invariants

Repository must enforce:

- subtitle ID is unique,
- subtitle must belong to exactly one item,
- binding points to an existing subtitle,
- bound subtitle belongs to the same item as binding,
- checksum is optional for dedupe but immutable once stored,
- storage keys are non-empty and repository-safe.

### Persistence strategy

All metadata writes must be atomic.

Approach:

1. marshal JSON,
2. write to temp file in same target directory,
3. close file,
4. rename temp file onto final destination.

Never write directly into final metadata files.

### Concurrency strategy

Initial implementation may use one repository mutex:

- `sync.RWMutex` for index access,
- write lock around mutations and index updates.

This is acceptable for current scale and simpler to reason about than premature fine-grained locking.

Per-item locks can be added later if contention is proven.

---

## Subtitle file store

### Responsibility

Owns subtitle file bytes on disk.

It should:

- persist canonical playback files,
- optionally persist original downloaded/extracted files,
- resolve storage keys to real paths,
- prevent path traversal,
- enforce deterministic naming,
- perform atomic writes.

### File naming policy

Recommended naming:

- canonical file: `<item-id>/<subtitle-id>.vtt`
- original file: `<item-id>/<subtitle-id>.orig.<ext>`

This avoids naming collisions and avoids trusting remote filenames.

### Why store both canonical and original files

Recommended behavior:

- always serve canonical file,
- keep original when fetched or extracted from non-canonical source,
- allow future reconversion/debugging.

If disk usage becomes a concern, keeping original files can be made configurable.

### Atomic writes

All file writes should use temp file + rename.

### Validation before persistence

File store should reject:

- empty storage keys,
- path traversal attempts,
- absolute paths where relative keys are expected,
- writes outside subtitle root.

---

## External subtitle input validation and normalization

This is critical for maintainability and safety.

Downloaded subtitle data is untrusted input.

### Validation rules

At minimum validate:

- maximum byte size,
- non-empty payload,
- supported extension or detectable format,
- acceptable MIME type if available,
- text-based content where expected,
- archive policy.

### Archive policy

External providers often return zip archives.

Recommended MVP:

- support simple single-subtitle zip archives only if needed,
- reject nested archives,
- reject archives with multiple ambiguous subtitle files unless selection logic is explicit,
- reject archive entries with path traversal.

If archive handling is not in MVP, reject archives explicitly and clearly.

### Encoding normalization

Providers may return odd encodings.

Recommended MVP:

- require converter/validator to accept UTF-8 cleanly,
- log and reject malformed encodings until explicit transcoding support is added.

If encoding normalization is later added, keep it in one dedicated validation/normalization step.

### Format policy

Canonical playback format should be:

- **WebVTT**

Other formats may be stored as original input but should be converted before serving where possible.

---

## Embedded subtitle importer

### Responsibility

Wrap the existing embedded subtitle flow and make it repository-backed.

### Dependencies

- `agents.ItemGetter` or direct item lookup capability,
- `agents.StreamManager`,
- `agents.SubtitleSelector`,
- subtitle repository,
- subtitle file store if extraction needs to be re-homed later.

### Flow

1. load item,
2. inspect streams using current `Find(item)`,
3. choose stream using selector,
4. extract subtitle with current `ExtractSubtitles(item, streamIndex)`,
5. compute checksum of extracted file,
6. create or reuse subtitle resource based on source ref/checksum policy,
7. save resource,
8. optionally set binding as default.

### Source reference for embedded import

Recommended `SourceRef` format:

- `embedded:stream:<stream-index>`

This makes repeated import idempotent per item and stream.

### Migration note

The current `preload_subtitles` tool should eventually call this importer instead of only warming the extraction cache.

---

## External subtitle provider model

### Responsibility

Provider implementations communicate with remote systems.

### Suggested interface

```go
type Provider interface {
	Name() string
	Search(ctx context.Context, item model.Item) ([]model.SubtitleCandidate, error)
	Download(ctx context.Context, candidate model.SubtitleCandidate) (DownloadedSubtitle, error)
}

type DownloadedSubtitle struct {
	FileName string
	MIMEType string
	Bytes    []byte
}
```

### Important boundaries

Provider should not:

- write files,
- set defaults,
- decide canonical storage,
- run repository logic.

Provider should only:

- search,
- download,
- return raw data and metadata.

### Candidate staleness

Search results may go stale.

`subtitle_fetch` should therefore accept enough information to redownload or re-resolve safely:

- provider name,
- provider candidate ID,
- optional fetch token.

Do not assume a plain previous URL is always still valid.

---

## Subtitle converter

### Responsibility

Turns non-canonical subtitle input into canonical WebVTT.

### Ownership

Owns:

- format detection help if needed,
- ffmpeg invocation for conversion,
- output creation,
- contextual error wrapping.

Does not own:

- provider search,
- repository save,
- binding selection.

### Conversion rule

Serveable output should be `.vtt`.

If input is already valid WebVTT, conversion may be skipped.

### ffmpeg execution requirements

- execute binary directly,
- no shell,
- timeout always applied,
- stderr captured for diagnosis,
- output path constrained to file store location.

---

## Subtitle service

### Responsibility

Acts as the orchestration layer for subtitle lifecycle operations.

### Suggested interface

```go
type Service interface {
	Search(ctx context.Context, item model.Item) ([]model.SubtitleCandidate, error)
	FetchAndStore(ctx context.Context, item model.Item, candidate model.SubtitleCandidate) (model.SubtitleResource, error)
	ListForItem(ctx context.Context, itemID string) ([]model.SubtitleResource, error)
	AssociateDefault(ctx context.Context, itemID, subtitleID string) error
}
```

### Responsibilities in fetch-and-store flow

1. validate candidate request,
2. download via provider,
3. validate payload,
4. persist original file if configured,
5. convert to canonical VTT if needed,
6. compute checksum,
7. dedupe or reuse existing subtitle if policy allows,
8. save resource metadata,
9. optionally return enriched status to callers.

### Idempotency expectations

For the same item and same source reference, repeated fetch should preferably return the same resource instead of generating duplicates.

For identical checksum under same item, dedupe policy may either:

- reuse existing resource,
- or store distinct provenance records if truly needed.

MVP recommendation:

- dedupe by `ItemID + SourceRef`,
- optionally dedupe by `ItemID + ChecksumSHA256`.

---

## Subtitle resolver

### Responsibility

Centralize all playback subtitle selection logic.

This should be a dedicated component.

### Suggested interface

```go
type Resolver interface {
	ResolveForPlayback(ctx context.Context, item model.Item, explicitSubtitleID string) (model.ResolvedSubtitle, error)
}
```

### Resolution order

1. explicit subtitle override,
2. item default binding,
3. embedded importer fallback if configured,
4. no subtitle.

### Why this must be isolated

Without a resolver, this logic ends up duplicated across:

- handlers,
- tools,
- suggestion code,
- playback endpoints.

That becomes brittle quickly.

---

## Playback and serving model

### Canonical serving contract

Playback should resolve to subtitle resource ID and serve the canonical file behind that resource.

### Recommended direction

Future handlers should move toward:

- `GET /api/items/{itemID}/subtitles`
- `POST /api/items/{itemID}/subtitles/default`
- `GET /api/subtitles/{subtitleID}`
- `POST /api/items/{itemID}/subtitles/import-embedded`

Exact API names can vary, but the identity model should remain resource-centric.

### Transitional compatibility

Current stream-index subtitle serving may remain temporarily for:

- diagnostics,
- admin/debug use,
- backwards compatibility.

But it should not remain the primary playback subtitle mechanism.

---

## Concierge and tools

### Tool philosophy

Tools must remain thin adapters.

They should:

- validate arguments,
- call services,
- render concise responses.

They should not own subtitle business logic.

### Recommended tool surface

- `preload_subtitles`
- `subtitle_search`
- `subtitle_fetch`
- `subtitle_associate`
- `subtitle_list`

### `preload_subtitles` migration

Current behavior only extracts subtitles.

Target behavior should be:

- invoke embedded subtitle importer,
- return created or reused subtitle resource,
- optionally report whether it became default.

---

## ffmpeg tool policy

### Strong recommendation

For MVP, implement **safe mode only**.

If raw mode is ever added, it should be:

- disabled by default,
- admin/debug only,
- feature-gated,
- heavily guarded.

### Safe mode operations

Safe mode may expose narrowly scoped operations such as:

- convert subtitle file to VTT,
- extract a subtitle stream from a known media file.

### Raw mode constraints if later added

- executable always fixed to `ffmpeg`,
- no shell,
- `-nostdin` injected,
- timeout mandatory,
- arg count bounded,
- readable roots allowlisted,
- writable roots allowlisted,
- traversal rejected,
- stdout/stderr truncated in tool response.

Raw mode exists only as an advanced escape hatch.

---

## Migration plan from current implementation

This section is mandatory because the codebase already has subtitle behavior.

### Phase 1: Introduce subtitle domain and repository

Add:

- subtitle models,
- file-backed repository,
- file store,
- service interfaces.

Keep existing:

- `internal/media/stream.Manager`,
- `SubtitleSelector`,
- current extraction logic.

### Phase 2: Add embedded importer

Build importer on top of existing stream manager.

Update `preload_subtitles` to use importer and return subtitle resource information.

### Phase 3: Add resolver and resource-based serving

Introduce:

- subtitle listing,
- default association,
- serving by subtitle resource ID,
- playback resolution through resolver.

Keep old stream-index route as transitional debug/admin path if needed.

### Phase 4: Add external providers and fetch flow

Add:

- provider abstraction,
- search tool,
- fetch tool,
- validation and conversion pipeline.

### Phase 5: Deprecate stream-index-first user flow

At this point, normal subtitle usage should be:

- import/fetch subtitle resource,
- associate default,
- resolve for playback,
- serve canonical file by resource ID.

---

## Lifecycle and cleanup policy

This is required for long-term maintainability.

### When an item is deleted

System should delete:

- subtitle resource metadata for that item,
- binding for that item,
- subtitle files under that item directory.

Deletion may be immediate or batched, but behavior must be deterministic.

### When a subtitle is replaced or reimported

Policy should be explicit:

- either reuse existing resource if idempotent match is found,
- or create a new resource and update binding accordingly.

MVP preference:

- prefer reuse on same `SourceRef` or identical checksum under same item.

### Temporary files

Repository and file store should clean stale temp files on startup when safe.

---

## Observability requirements

To keep the system maintainable in practice, add structured logs around:

- subtitle ID,
- item ID,
- source,
- origin,
- stream index if embedded,
- provider name,
- conversion duration,
- file size,
- dedupe decisions,
- resolver decision path.

Useful counters or metrics later:

- embedded import success/failure,
- provider search latency,
- provider fetch latency,
- subtitle conversion success/failure,
- resolver fallback rate,
- cache hit/miss for embedded imports.

---

## Error handling rules

Every error should add context.

Examples:

- `fmt.Errorf("save subtitle resource %s: %w", resource.ID, err)`
- `fmt.Errorf("set default subtitle for item %s: %w", itemID, err)`
- `fmt.Errorf("convert subtitle %s to vtt: %w", inputPath, err)`
- `fmt.Errorf("download subtitle candidate %s from provider %s: %w", candidate.ProviderCandidateID, candidate.Provider, err)`

Repository should not panic on bad persisted records.
It should log, skip invalid entries, and continue startup where possible.

---

## Test strategy

### Pure unit tests

- ID generation helpers,
- storage key validation,
- source ref generation,
- resolver precedence,
- candidate validation,
- archive/traversal rejection helpers.

### Mock-driven unit tests

- subtitle service,
- embedded importer,
- converter behavior,
- provider implementations,
- tools.

### Temp-dir integration tests

- repository startup rebuild,
- atomic metadata writes,
- file store persistence,
- binding consistency,
- item cleanup.

### Optional real-binary integration tests

Behind environment flags only:

- real ffmpeg conversion,
- real ffprobe probing,
- real provider integration.

These should not be mandatory in default test runs.

---

## Implementation order

Recommended order:

1. add subtitle models in `internal/model`,
2. add file-backed subtitle repository,
3. add subtitle file store,
4. add repository startup rebuild and atomic helpers,
5. add embedded importer over current stream manager,
6. migrate `preload_subtitles` to importer-backed flow,
7. add subtitle resolver,
8. add resource-based subtitle serving endpoints,
9. add subtitle service,
10. add provider abstraction,
11. add first provider implementation,
12. add conversion pipeline,
13. add search/fetch/list/associate tools,
14. optionally add safe ffmpeg tool,
15. only later consider raw ffmpeg mode.

---

## Appendix: recommended repository interface

```go
type SubtitleRepository interface {
	Save(ctx context.Context, resource model.SubtitleResource) (model.SubtitleResource, error)
	GetByID(ctx context.Context, subtitleID string) (model.SubtitleResource, error)
	ListByItemID(ctx context.Context, itemID string) ([]model.SubtitleResource, error)
	GetBySourceRef(ctx context.Context, itemID, sourceRef string) (model.SubtitleResource, error)
	GetByChecksum(ctx context.Context, itemID, checksum string) ([]model.SubtitleResource, error)
	SetDefault(ctx context.Context, itemID, subtitleID string) error
	GetDefault(ctx context.Context, itemID string) (model.SubtitleResource, error)
	DeleteByItemID(ctx context.Context, itemID string) error
}
```

### Notes

- `GetBySourceRef` supports idempotent embedded/provider imports,
- `GetByChecksum` supports dedupe,
- `DeleteByItemID` supports cleanup on item removal.

---

## Final mental model

Think about the system like this:

- `Item` is the movie or episode,
- `SubtitleResource` is a subtitle Kinoview owns,
- `SubtitleBinding` is the item default,
- repository is a small domain-specific embedded metadata database,
- file store owns bytes,
- importer turns embedded streams into resources,
- provider finds and downloads remote candidates,
- service coordinates persistence and conversion,
- resolver decides what playback actually uses,
- tools and handlers stay thin.

That is the recommended subtitle architecture for Kinoview v2.
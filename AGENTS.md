# AGENTS.md — Kinoview

## Architecture

Kinoview is a self-hosted media gallery and classification server. It crawls a local
filesystem directory for media files, indexes them in a local store, and serves them
through a single-page web frontend. Optional LLM-driven agents enrich the library
with metadata, recommendations and curated suggestions.

The core loop: **fsnotify watcher → media index → storage → HTTP handlers**. Agentic
components (classifier, butler, recommender, concierge) are opt-in via
CLI flags and plug into the indexer via interface contracts defined in
`internal/agents/interfaces.go`. The concierge also
communicates through the shared **slivingdoc** notebook — a git-backed UTF-8
text notebook synced through an external SeaweedFS S3 backend (see Data
Flow).

### Package Map

```
cmd/
├── serve/               # Monolithic web-server command: wires watcher, store, agents, HTTP mux
│   ├── serve.go         # Flagset, Run (server lifecycle), Help/Describe, startServeRoutine
│   ├── serve_setup.go   # Setup: instantiates every subsystem via functional options,
│   │                    #   plus the SeaweedFS supervisor and slivingdoc notebook wiring
│   ├── serve_setup_test.go
│   ├── serve_test.go
│   └── frontend/        # //go:embed vanilla JS SPA (gallery, minigallery, events, SSE client)
├── classify/            # Standalone CLI for ad-hoc item classification (debugging)
├── debug/               # Standalone CLI for store inspection and debugging
├── media/               # Standalone CLI for media listing
├── llm/                 # LLM usage analytics CLI
└── (main.go)            # Root command dispatcher: flags → cmd.Run with shutdown.MonitorV2

internal/
├── agents/              # Agent contracts + implementations (LLM-driven, all opt-in)
│   ├── interfaces.go    # Classifier, Recommender, Butler, Concierge,
│   │                    #   ItemGetter, ItemLister, MetadataManager, SuggestionManager,
│   │                    #   StreamManager, SubtitleSelector, ClientContextManager, OutputSetter
│   ├── item_updater.go  # Shared helper: updates item metadata with retry + rate-limit awareness
│   ├── slivingdoc/      # The shared agent notebook: slivingdoc MCP callsign, tool globs,
│   │                    #   NOTES prompt partial, worktree seeding, and the handler-side seam
│   │                    #   (notebook.go AppendJSONL)
│   ├── classifier/      # LLM classifier: inspects media files, writes title/genre/year/plot tags
│   ├── recommender/     # LLM recommender: semantic media discovery from user query
│   ├── butler/          # Butler: proactive suggestion cascades based on viewing patterns
│   │   ├── butler.go    # Orchestrator: fetch subs → semantic index → rank → cascade
│   │   ├── selector.go  # Best-subtitle-stream selection logic
│   │   ├── semantic_indexer.go  # Embedding-based content indexing
│   │   ├── subs_parser.go       # Subtitle file parsing (SRT/VTT)
│   │   └── subtitle_rank.go     # Subtitle quality ranking heuristics
│   ├── concierge/       # Concierge: autonomous periodic agent with tool-based actions
│   │   ├── concierge.go # clai-based agent loop, runs on fixed interval, notebook-aware
│   │   ├── cmd.go       # Tool command registration and routing
│   │   └── docs.go      # System prompt and tool documentation strings
│   ├── troupe/          # The fixed stage: frozen grammar (grammar.go/validate.go),
│   │                    #   STAGE.md (the human-readable grammar note), and the
│   │                    #   resolver/tools/roles of later phases
│   └── tools/           # Concierge tool implementations (all satisfy clai's LLMTool interface)
│       ├── add_suggestion.go / remove_suggestion.go / check_suggestions.go
│       ├── client_context_getter.go / concierge_context_*.go
│       ├── extract_subtitle.go / fetch_subtitles.go / list_subtitle_candidates.go
│       ├── media_get_item.go / media_list.go / media_stats.go
│       └── update_metadata.go
├── media/               # Core media subsystem: index, store, watcher, streaming, thumbnails
│   ├── index.go         # Indexer: central orchestrator — wires all components, owns HTTP handler
│   ├── index_handlers.go            # Gallery HTTP endpoints
│   ├── index_handlers_eventStream.go # SSE event stream for frontend live updates
│   ├── index_handlers_shows.go      # TV show grouping endpoints
│   ├── fingerprint.go   # Perceptual hashing for media deduplication
│   ├── storage/         # JSON-file-backed persistent store
│   │   ├── store.go     # Store: Setup, Start, Store, Snapshot, ListHandlerFunc, etc.
│   │   ├── classification.go  # Classification queue with rate limiting (token bucket)
│   │   ├── handlers.go  # HTTP handlers for store operations
│   │   ├── item_updater.go    # Metadata update with debounced disk writes
│   │   └── stream.go    # Video streaming with Range request support
│   ├── watcher/         # Recursive fsnotify watcher, publishes model.Item on update channel
│   ├── stream/          # Subtitle extraction and management (ffprobe-backed)
│   ├── suggestions/     # Suggestion CRUD manager (persisted to cache dir)
│   ├── thumbnail/       # Video thumbnail generation pipeline
│   ├── clientcontext/   # Per-session user viewing context persistence
│   └── constants/       # Shared media constants
├── model/               # Domain types — free of external dependencies
│   ├── item.go          # Item, Metadata, ShowGrouping, Suggestion
│   ├── media.go         # MediaInfo, Stream (codec/language/subtitle metadata)
│   ├── event.go         # Event types for SSE broadcast
│   └── log.go           # Structured log types for LLM agent introspection
└── loghandler/          # HTTP handler for agent log streaming (SSE)
```

### Data Flow

```
                    ┌─── fsnotify ───┐
                    │  (recursive)   │
                    └───────┬────────┘
                            │ model.Item (new/removed media files)
                            ▼
 ┌──────────────────────────────────────────────────────────┐
 │                      Media Index                         │
 │                                                          │
 │  ┌──────────┐   ┌──────────┐   ┌────────────────────┐    │
 │  │  Store   │◄──│ Watcher  │   │  Agent Subsystem   │    │
 │  │ (JSON)   │   │ channel  │   │                    │    │
 │  │          │──►│          │   │  Classifier (LLM)  │    │
 │  │ Snapshot │   └──────────┘   │  Recommender (LLM) │    │
 │  │ Stream   │                  │  Butler (LLM)      │    │
 │  │ Suggest  │                  │  Concierge (LLM)   │    │
 │  └────┬─────┘                  └─────────┬──────────┘    │
 │       │                                  │               │
 │       │         ┌────────────────────────┘               │
 │       │         │  metadata updates, suggestions,        │
 │       │         │  recommendations                       │
 │       ▼         ▼                                        │
 │  ┌──────────────────────────────────────────────────┐    │
 │  │              HTTP Handler (ServeMux)             │    │
 │  │  /gallery/*   SSE events   WebSocket   SPA static│    │
 │  └────────────────────┬─────────────────────────────┘    │
 └───────────────────────┼──────────────────────────────────┘
                         │
                         ▼
                  ┌──────────────┐
                  │   Frontend   │
                  │  (vanilla    │
                  │   JS SPA)    │
                  └──────────────┘
```

The agent conversation happens in the shared slivingdoc notebook, backed by
a standalone SeaweedFS S3 gateway (a Docker stack, see docker-compose.yml):

```text
┌──────────────┐   S3 (pull / commit)   ┌──────────────────────────┐
│ kinoview     │───────────────────────►│ SeaweedFS S3 gateway     │
│ (serve)      │                        │ http://<host>:8333       │
└──────┬───────┘                        │ bucket "slivingdoc"     │
       │ pull / commit                  └──────────────────────────┘
       ▼
┌─────────────────────────────────────────────────────────────┐
│        shared worktree  <cache>/slivingdoc/                 │
│   bulletin.md · agent notes                                 │
└───────▲─────────────────────────────────────────────────────┘
        │ file tools (cat, rows_between, ls, rg, write_file, apply_patch, mkdir)
┌───────┴────────┐
│  concierge     │
│  (clai agent)  │
└────────────────┘
```

One agent note write is the loop: `mcp_slivingdoc_notes_pull` materialises
the notebook into the shared worktree, the file tools read and edit the
notes, and `mcp_slivingdoc_notes_commit` publishes — slivingdoc merges
concurrent changes with visible conflict markers.

All agentic components are **opt-in** — each has its own `-model` flag. When unset,
that agent is nil and the indexer skips the corresponding feature path. The
shared notebook is opt-in too: `serve` enables it when the AWS credentials
(`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`) resolve, the slivingdoc command
resolves (`npx -y slivingdoc` by default, or a prebuilt binary via
`-slivingdocCommand`) and `-slivingdocDisable` is unset. A missing dependency
logs one warning and the notebook is off — agents fall back to their old
single-shot behaviour. The old intro splash was removed in
worklog 2026-08-17-the-troupe phase 0; the troupe (director + swarm + critic)
replaces it from phase 7 onward.

**Key insights:**

- **The store is the single source of truth; the notebook is the
  communication layer.** Agents read from `Storage.Snapshot()` and write back
  via `UpdateMetadata`/`SuggestionManager.Add` — the store keeps no agent
  conversation. The shared slivingdoc notebook carries that conversation: the
  concierge pulls it into the shared worktree,
  reads and edits the materialised notes with the file tools, and commits with
  `mcp_slivingdoc_notes_commit`. The loop is one shared `NOTES` prompt
  partial, byte-identical across every agent with only the workspace path
  substituted.
- **The notebook's S3 backend is a standalone Docker SeaweedFS.** kinoview
  no longer spawns or supervises SeaweedFS: it connects to the S3 gateway
  published by `docker-compose.yml` with the operator's AWS
  credentials, and writes the credentials env file the slivingdoc CLI and MCP
  child source. Missing credentials or the slivingdoc command disables the
  notebook with one warning: agents fall back to their old single-shot
  behaviour.
- **The classifier uses a cloned-agent model.** Each worker goroutine gets its own
  `Classifier.Clone()`, eliminating shared mutable state and LLM session races.
- **Classification is rate-limited.** A token-bucket limiter (configurable
  rate/burst) gates admission; a startup cooldown delays the first classification.
- **Store writes are debounced.** Metadata updates accumulate in memory and flush
  to disk on a configurable delay, reducing I/O during bulk classification.
- **The butler is cascading.** On session end (pong timeout), it fetches subtitles
  for recent items, builds a semantic index, ranks content, and pre-populates
  suggestions for the user's next visit. Results are cached with configurable TTL.
- **The concierge is autonomous.** It runs on a fixed interval (default 6h), uses
  a clai agent with registered tools, and can add/remove suggestions, update
  metadata, fetch subtitles, and inspect client context — all without user
  interaction. With the slivingdoc callsign configured it also pulls the
  shared notebook, posts its findings and commits them (the shared `NOTES`
  partial).
- **The frontend uses SSE for live updates.** The `index_handlers_eventStream.go`
  broadcasts item changes, suggestions, and logs to connected browsers.
- **clai ≥ v1.10.22-r1 ships the reasoning cap upstream.** A looping model
  can stream reasoning tokens forever; clai ≤ v1.10.21 accumulated them in
  unbounded O(n²) string builders, which OOMed the server on 2026-08-11
  (2.53 GB heap, 476 MiB failed allocation). The fix caps both reasoning
  accumulators (`reasoningContent` in `internal/text/generic/` and
  `reasoningBuf` in `internal/text/`) at 1 MiB, keeping the tail. Kinoview
  pins ≥ v1.10.23-rc1 (race-safe clai, which also replaced the io.Writer
  terminal output `WithOutputTo` with the `WithLogger` slog channel — the
  classifier's per-worker log files are text-handler sinks now); do not
  downgrade below v1.10.22-r1. Two wall-clock guards back the
  cap up: `-conciergeTimeout` (default 10 min) aborts a stuck concierge run,
  and `-classifierTimeout` (default 5 min) aborts a stuck classification
  call (the attempt still counts). Regression proof:
  `REPRO_OOM=1 go test ./internal/agents/butler -run TestRepro_HeapGrowthPerReasoningByte`
  must keep heapSys under 256 MiB and return on the 120 s deadline.

---

## Build/Test/Lint Commands

```bash
# Build
go build -o kinoview .

# Test all packages
go test ./...

# Test single package
go test ./internal/media

# Test with verbose output
go test -v ./...

# Test with race detector and coverage
go test ./... -race -cover -count=3 -timeout=30s

# Lint
go vet ./...

# Format
go run mvdan.cc/gofumpt@latest -w -l .
```

## QA Validation

Before signing off on ANY changes, these must all pass:

| Tool        | Command                                                  |
| ----------- | -------------------------------------------------------- |
| Format      | `go run mvdan.cc/gofumpt@latest -w -l .`                 |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` |
| Lint        | `go vet ./...`                                           |
| Test        | `go test ./... -race -cover -count=3 -timeout=30s`       |
| Fix         | `go fix ./...`                                           |
| Dupl        | `go run github.com/mibk/dupl@latest -t 80 .`             |

The dupl check is a signal, not a verdict — see the Duplication policy
below for deciding which clones are acceptable and which need fixing.

**Important:** `go test ./... -race -count=3 -timeout=30s` MUST pass unedited. The strictness
is intentional to produce a highly testable, efficient system which follows strict inversion of control.
Do not modify the timeout, count, or race. Do not add test skips, false-positive tests or any other cheat.
Instead, start testing early and ensure that test passes for each new modification.

**Important:** 70+% test coverage is a must. 90+% test coverage is preferred.
Run `make qa` to run all at once.

---

## Function Shape

Prefer many small single-purpose functions sequenced by a thin orchestrator over
one function that does several things. Two smells drive most refactors here:

- **`and` in a name is a split point.** `fooAndBar` is two functions wearing one
  name. Name each helper for the single verb it performs and let a caller sequence
  them; the orchestrator reads as the outline of the operation.
- **A growing return tuple wants to be a struct.** When a function returns three or
  more values — or you are tempted to add "just one more" to carry new data —
  introduce a result struct instead. Adding a field to a struct is invisible to
  every call site; adding a return value churns the signature, the interface it
  satisfies, and every mock that implements it.

Populate that struct incrementally, capturing each value at its source. A value set
early (timing, telemetry, the raw upstream result) then survives a later step's
failure and is available on both the success and error paths, so the caller reads
one field regardless of outcome — no per-branch plumbing.

Returning a non-nil result struct alongside a non-nil error is acceptable when
the struct's job is to carry diagnostics across the outcome boundary; keep the
usual "nil result on error" everywhere else.

---

## Conventions

### Imports

- Standard library first, then local packages, lastly third-party if absolutely
  necessary.
- Use blank line separation between groups.

### Naming

- camelCase for variables and unexported functions.
- PascalCase for exported types and functions.
- Interface names end with 'er' (e.g., `Indexer`, `Classifier`, `Recommender`).
- Package names are lowercase, single word when possible.

### Types & Structs

- Use `any` instead of `interface{}`.
- Avoid using `any` to the furtest extent, we use types in typed languages.
- Embed interfaces for composition.
- Use struct tags for JSON serialization.
- Constructor functions use functional options (`WithXxx` pattern).

### Error Handling

- Return errors as last return value.
- Use `fmt.Errorf` for error wrapping with `%w` verb.
- Use `ancli.Errf` for logging errors in CLI context, `ancli.Noticef` and
  `ancli.Okf` for logging other information.
- Propagate errors up the call stack with context: `fmt.Errorf("thing place: %w", err)`.

### Logging

- Always use `"corrID"` as the slog attribute key for correlation IDs.
- Agent output is written to log files (`SetOutput`) not stdout, avoiding interleaved
  log races when multiple LLM workers run concurrently.

### CLI Flags

- Naming: `-camelCase` for flags, with environment variable overrides where
  practical.
- All agent components are opt-in via their own `-model` flag.
- Defaults are documented in the flag help text.

The shared agent notebook (slivingdoc over the standalone SeaweedFS S3 backend,
see docker-compose.yml) is configured by these `serve` flags:

| Flag                   | Default              | Purpose                                                                                            |
| ---------------------- | -------------------- | -------------------------------------------------------------------------------------------------- |
| `-slivingdocCommand`   | `npx -y slivingdoc`  | path to a prebuilt slivingdoc binary; empty runs the npm package through npx (`npx -y slivingdoc`) |
| `-slivingdocBucket`    | `slivingdoc`         | S3 bucket backing the notebook                                                                     |
| `-slivingdocRegion`    | `us-east-1`          | AWS region label (SigV4 signing, env file, MCP `--region`)                                         |
| `-slivingdocEndpoint`  | `http://127.0.0.1:8333` | S3 endpoint for the notebook (the Docker SeaweedFS S3 gateway)                                  |
| `-slivingdocWorkspace` | `<cache>/slivingdoc` | shared worktree every agent materialises the notebook into                                         |
| `-slivingdocDisable`   | false                | force-disable the notebook even when the AWS credentials and slivingdoc command resolve            |

The credentials are not flags: kinoview reads `AWS_ACCESS_KEY_ID` and
`AWS_SECRET_ACCESS_KEY` from its environment (matching `.env`)
and writes the credentials env file the slivingdoc CLI and MCP child source.
The notebook is on when those credentials and the slivingdoc command resolve
and `-slivingdocDisable` is false; any other state logs one warning and the
server runs without it.

### Testing

- Test files co-locate with the package they test (`_test.go` in the same directory).
- Use table-driven tests for parameterized cases.
- Integration tests use real filesystem fixtures (see `internal/media/storage/mock/`).
- The `kinoview` binary at repo root can be used for smoke testing.

---

## Duplication Policy

Duplication is not always a defect. `dupl -t 80` is a signal, not a verdict.
Use these principles to decide whether a reported clone needs fixing.

### Acceptable duplication (do not refactor)

- **Interface + mock mirroring.** A test file that declares a struct with the same
  method signatures as the interface it implements is necessary — the mock is the
  proof that the interface contract is satisfied.
- **Thin wrappers over a shared helper.** Several short functions that differ only
  in a field name or constant and all delegate to a single implementation are the
  abstraction.
- **Test-setup boilerplate.** Store setup, watcher channel wiring, or SSE connection
  preamble in every test is structural, not logical, duplication.
- **Table-driven test loops.** `for _, tt := range tests { t.Run(tt.name, …` is the
  idiom, not a clone.
- **Tool implementations in `internal/agents/tools/`.** Each tool follows the same
  `Name()`, `Description()`, `Run()` contract — the structural similarity is the
  interface, not duplicated logic.

### Actionable duplication (fix these)

- **Two or more functions or tests whose bodies differ only in parameterised values.**
  Merge into a table-driven test or extract a parameterised helper.
- **Production code where the same sequence of operations appears verbatim** with
  different call-site constants. Extract a function.
- **Identical setup + teardown across >3 tests in the same file.** Extract a test
  helper local to that file.

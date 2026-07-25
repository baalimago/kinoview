# AGENTS.md — Kinoview

## Architecture

Kinoview is a self-hosted media gallery and classification server. It crawls a local
filesystem directory for media files, indexes them in a local store, and serves them
through a single-page web frontend. Optional LLM-driven agents enrich the library
with metadata, recommendations, curated suggestions, and an intro splash story.

The core loop: **fsnotify watcher → media index → storage → HTTP handlers**. Agentic
components (classifier, butler, recommender, concierge, storyteller) are opt-in via
CLI flags and plug into the indexer via interface contracts defined in
`internal/agents/interfaces.go`.

### Package Map

```
cmd/
├── serve/               # Monolithic web-server command: wires watcher, store, agents, HTTP mux
│   ├── serve.go         # Flagset, Run (server lifecycle), Help/Describe, startServeRoutine
│   ├── serve_setup.go   # Setup: instantiates every subsystem via functional options
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
│   ├── interfaces.go    # Classifier, Recommender, Butler, Concierge, ItemGetter, ItemLister,
│   │                    #   MetadataManager, SuggestionManager, StreamManager, SubtitleSelector,
│   │                    #   ClientContextManager, OutputSetter
│   ├── item_updater.go  # Shared helper: updates item metadata with retry + rate-limit awareness
│   ├── classifier/      # LLM classifier: inspects media files, writes title/genre/year/plot tags
│   ├── recommender/     # LLM recommender: semantic media discovery from user query
│   ├── butler/          # Butler: proactive suggestion cascades based on viewing patterns
│   │   ├── butler.go    # Orchestrator: fetch subs → semantic index → rank → cascade
│   │   ├── selector.go  # Best-subtitle-stream selection logic
│   │   ├── semantic_indexer.go  # Embedding-based content indexing
│   │   ├── subs_parser.go       # Subtitle file parsing (SRT/VTT)
│   │   └── subtitle_rank.go     # Subtitle quality ranking heuristics
│   ├── concierge/       # Concierge: autonomous periodic agent with tool-based actions
│   │   ├── concierge.go # clai-based agent loop, runs on fixed interval
│   │   ├── cmd.go       # Tool command registration and routing
│   │   └── docs.go      # System prompt and tool documentation strings
│   ├── storyteller/     # Storyteller: generates intro splash story from library stats
│   │   ├── storyteller.go   # LLM-backed story generation with cooldown
│   │   └── composer.go      # Deterministic fallback composer (no LLM required)
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
│   ├── log.go           # Structured log types for LLM agent introspection
│   └── story.go         # Story type for intro splash
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
 │  └────┬─────┘                  │  Storyteller (LLM) │    │
 │       │                        └─────────┬──────────┘    │
 │       │                                  │               │
 │       │         ┌────────────────────────┘               │
 │       │         │  metadata updates, suggestions,        │
 │       │         │  recommendations, stories              │
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

All agentic components are **opt-in** — each has its own `-model` flag. When unset,
that agent is nil and the indexer skips the corresponding feature path. The
storyteller is the exception: it always constructs (with a deterministic composer
fallback) so the intro splash works offline and without an API key.

**Key insights:**

- **The store is the single source of truth.** Agents read from `Storage.Snapshot()`
  and write back via `UpdateMetadata`/`SuggestionManager.Add`. There is no
  event bus — components interact through the store and interface contracts.
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
  interaction.
- **The frontend uses SSE for live updates.** The `index_handlers_eventStream.go`
  broadcasts item changes, suggestions, and logs to connected browsers.

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

# Phase 5 — Quality Gate

**Status:** ✅ Done | [README](./README.md)

## Goal

Run the repository's full QA gate over the whole change (phases 1–4) and
record the exact commands and outcomes.

## Specification

Per AGENTS.md QA Validation and the Makefile `qa` target, with the addition of
the node frontend harness (a documented part of the frontend work):

| Tool | Command |
| ---- | ------- |
| Format | `go run mvdan.cc/gofumpt@latest -w -l .` |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` |
| Lint | `go vet ./...` |
| Fix | `go fix ./...` |
| Tests | `go test ./... -race -cover -count=3 -timeout=30s` |
| Dupl | `go run github.com/mibk/dupl@latest -t 80 .` |
| Frontend harness | `node cmd/serve/frontend_test/intro.test.js` |

The full suite must pass **unedited** — no timeout, count or race changes, no
skips, no false-positive tests. If a marginal-wall-time flake appears (the
documented D-P10-2 family), re-run the affected package in isolation and
record it, exactly as the playwright-company worklog does.

## Acceptance criteria

- [x] All seven commands above pass and are recorded in the implementation
      notes with their exact output summaries.
- [x] Coverage for the touched packages (theatre, media) stays at or above
      the repo's 70 % floor (90 % preferred).
- [x] `dupl` reports no new clone groups beyond the pre-existing accepted
      ones.
- [x] The node harness passes (phases 1 and 4 added assertions).
- [x] AGENTS.md is updated if the phase work changed any documented contract
      or package map entry — the feedback contract, the new route, and the
      audience doc: the package map's "six durable docs" (AGENTS.md:65) and
      the library bullet's "six durable company docs" (AGENTS.md:167) both
      count seven once `audience.json` exists.

## Implementation notes

Executed 2026-08-05, worker session 5, imago. All commands run from the repo
root; the suite runs use `GOTMPDIR=/home/imago/.cache/go-tmp` (see run 1).

| Tool | Command | Result |
| ---- | ------- | ------ |
| Format | `go run mvdan.cc/gofumpt@latest -w -l .` | clean — no output, no file rewritten |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | clean |
| Lint | `go vet ./...` | clean |
| Fix | `go fix ./...` | clean — no changes |
| Tests | `go test ./... -race -cover -count=3 -timeout=30s` | see the run log below |
| Dupl | `go run github.com/mibk/dupl@latest -t 80 .` | 27 clone groups, byte-identical to the HEAD baseline; none touch this worklog's code |
| Frontend harness | `node cmd/serve/frontend_test/intro.test.js` | 26 assertions pass ("all intro player assertions passed") |

### Test-suite run log (same flags every run: `-race -cover -count=3 -timeout=30s`)

| Run | Outcome |
| --- | ------- |
| 1 | Build failures: `disk quota exceeded` on the `/tmp` tmpfs (7.5 G) — parallel `-race` link outputs exhausted it. Re-ran with `GOTMPDIR=/home/imago/.cache/go-tmp` (the build cache already lives on `/home`). No test failed; the packages that built were green. |
| 2 | Every package ok except `internal/media/storage` `Test_AddToClassificationQueue_rateLimit` (classification_test.go:745 "expected at least 2 items from burst, got 0"). A 100 ms wall-clock assertion, load-sensitive; the package is untouched by this worklog and sits in the documented D-P10-2 marginal class (playwright-company phase 10: "pre-existing marginal package ... 28.2–30.6 s wall variance under the gate flags"). Isolation re-run with the same flags: **ok** (11.9 s). |
| 3 | Every package ok except `cmd/classify` `TestCommand_Run_no_items_found` (classify_test.go:1311) — the documented D-P10-2 `cmd/classify` context-timeout class: a 100 ms deadline doubles as the test duration for a 10 ms mock startup sleep, and full-suite load stretches it. Isolation re-run with the same flags: **ok**. |
| 4 | **Green** — all packages ok. |

Per the phase spec's D-P10-2 clause, the two flaked packages were re-run in
isolation with the unchanged flags and recorded, exactly as the
playwright-company worklog does. The green run 4 is the gate result; the
flake family is pre-existing, untouched by phases 1–4, and passes in
isolation on every attempt.

### Coverage, touched packages (run 4, green)

`internal/agents/theatre` **91.2 %** (21.5 / 17.6 / 14.5 s across runs), `internal/media`
**83.0 %**, `cmd/serve` 75.6 % — all above the repo's 70 % floor; theatre also
clears the 90 % preferred mark. Coverage was stable across the flaky runs
(storage 85.6–85.8 %, untouched). No regression vs the phase baselines
(theatre 91.2 %, media 83.0 % in this worklog's phase 3 gate).

### dupl triage

The working tree's 27 clone groups are byte-identical to the HEAD baseline's
(`git archive HEAD` into a scratch dir, same command) — **no new clone
groups**. None of the groups touch `internal/agents/theatre/` (zero clones in
the touched package) or the phase's other files; the playwright-company phase
10 triage applies verbatim: table-driven loops, test-setup boilerplate,
tool-contract mirroring, and the pre-existing `internal/model/item.go` alias
pair.

### AGENTS.md update (AC, R2-05)

- Package map `interfaces.go` — `Feedbacker` added to the contract list.
- Package map `docs.go` — "six durable docs" → "seven durable docs (premises,
  repertoire, sets, registry, director, bulletin, audience)".
- Package map `theatre.go` — facade line now names `Feedbacker` + `Feedback`.
- Key insights, library bullet — the seventh doc, the audience doc, is
  written only by the audience; distillation still produces the six.
- New key-insight bullet — audience feedback is a durable doc, not an event:
  splash control → `POST /gallery/intro/feedback` → `agents.Feedbacker` →
  `audience.json` (single write path), director/dramaturg excerpt, no
  cooldown bypass.

No production code or tests changed in this phase — it is the gate.

## Review findings (review 6, 2026-08-06)

No findings. The full gate re-ran green: gofumpt `-l` clean, staticcheck
clean, `go vet ./...` clean, `go fix -diff ./...` no changes, the full
`-race -cover -count=3 -timeout=30s` suite green on runs 2–3 of this review
(run 1 flaked `cmd/classify` — the documented D-P10-2 family; passed in
isolation with identical flags; the exact test name was not captured), the
node harness 28 assertions, dupl 27 groups byte-identical to the `git
archive HEAD` baseline. Coverage matches the records: theatre 91.2 %,
media 83.0 %, cmd/serve 75.6 %, storage 85.6–85.8 %. AGENTS.md carries the
Feedbacker contract, the seven-doc count and the feedback bullet (both
"six docs" lines the R2-05 AC named are updated).

## Review findings (review 2, 2026-08-05)

- **R2-05 (Low).** The AGENTS.md AC only said "is updated" — precisely where
  this plan's staleness will hide: the package map's "six durable docs"
  (AGENTS.md:65) and the library bullet's "six durable company docs"
  (AGENTS.md:167) both enumerate the company library, and both are wrong once
  `audience.json` exists. AC sharpened to name both lines.

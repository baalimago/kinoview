# Kinoview

[![Go Report Card](https://goreportcard.com/badge/github.com/baalimago/kinoview)](https://goreportcard.com/report/github.com/baalimago/kinoview)
[![wakatime](https://wakatime.com/badge/user/018cc8d2-3fd9-47ef-81dc-e4ad645d5f34/project/c215f59a-0855-4729-a32e-95eef473ada1.svg)](https://wakatime.com/badge/user/018cc8d2-3fd9-47ef-81dc-e4ad645d5f34/project/c215f59a-0855-4729-a32e-95eef473ada1)
[![Simple Go Pipeline - validate](https://github.com/baalimago/kinoview/actions/workflows/validate.yml/badge.svg)](https://github.com/baalimago/kinoview/actions/workflows/validate.yml)

Test coverage: 84.983% 😍👌

Host local movies within your private network with mandatory AI!

<div align="center">
  <img src="img/banner.jpg" alt="Banner">
</div>

## Installation

**Option 1:**

```bash
go install github.com/baalimago/kinoview@latest
```

**Option 2:**

```bash
curl -fsSL https://raw.githubusercontent.com/baalimago/kinoview/main/setup.sh | sh
```

## How to use it?

1. `kinoview s|serve -host 0.0.0.0 <directory-with-media>`
2. `ip a`
3. Browse to `<private-network-address>` on your device
4. Enjoy media!

## Butler Configuration

The butler prepares viewing suggestions on client disconnect. Three flags control
cascade rate-limiting and caching:

```bash
# Minimum interval between butler suggestion cascades (default 30s, 0 disables)
kinoview serve -butlerDebounce 30s

# Grace period after pong timeout before triggering disconnect (default 10s, 0 disables)
kinoview serve -pongGrace 10s

# How long a cached suggestion set is served before re-querying (default 6h, 0 disables)
kinoview serve -butlerCacheTTL 6h
```

## Concierge Configuration

The concierge manages suggestions periodically. Its schedule is configurable:

```bash
# Interval between concierge runs (default 6h, 0 runs once then stops)
kinoview serve -conciergeInterval 6h
```

The last-run timestamp is persisted to `<cacheDir>/concierge_last_run`, so
process restarts within the interval do not trigger additional runs.

## LLM Usage Reporting

`kinoview llm usage` aggregates cost and token data from clai's persisted
conversation files. It parses the conversation directory, attributes each
file to an agent via its system prompt, and prints per-agent (or per-model,
per-day) totals.

```bash
# Default: per-agent aggregation over all time
kinoview llm usage

# Last 7 days, grouped by model
kinoview llm usage --since 168h --by model

# Machine-readable output
kinoview llm usage --json
```

See `kinoview llm usage --help` for all flags.

## Why not Plex or Jellyfish?

I dunno.
That's probably a better tool for the job.
I don't have all the answers, man, I'm not omniscient or something?

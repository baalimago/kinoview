# Troupe perception — the shared Playwright MCP server

The troupe inspects its own render: the director, the critic and any role whose
note selects `mcp_playwright` navigate to the served show, read the outline,
take a screenshot, read the console, and correct the notes. This note is the
operator side: what has to run, and how to prove it runs.

**Related:** [`AGENTS.md`](../AGENTS.md) (the insight list),
[`STAGE.md`](../internal/agents/troupe/STAGE.md) (the registry and the `SHOW`
partial), [`docker/playwright-mcp.Dockerfile`](../docker/playwright-mcp.Dockerfile).

## Two artifacts

| Artifact | Job |
|---|---|
| `kinoview-playwright` container | `@playwright/mcp` 0.0.83 plus one Chromium, streamable HTTP on `127.0.0.1:8931` |
| `<configDir>/clai/mcpServers/playwright.json` | the `url` that registers `mcp_playwright_*` on every spawned agent |

kinoview holds no browser code. `serve` reads the same config the agents load,
uses it only as a switch, and logs whether the tools are reachable. The server
and the browser are one container, the shape sakfraga runs.

## What kinoview expects

Perception is off until all of these hold:

| Switch | Value |
|---|---|
| A Playwright MCP server config exists | `<configDir>/clai/mcpServers/playwright.json` |
| The config can start here | a `command` that resolves, or a **loopback** `url` |
| The served port is known | `-port 0` leaves no stable show URL, so perception stays off |

The clai config dir is `<configDir>/clai` unless `-configDir` already ends in
`clai`. The file name is the server name: `playwright.json` registers
`mcp_playwright_browser_*`, which is what the troupe's closed registry selects.

```json
{"url": "http://127.0.0.1:8931/mcp", "timeout_seconds": 300}
```

With that file in place, startup logs:

```text
troupe perception: the browser tools inspect the show at http://127.0.0.1:80/
```

Without it, startup names the exact path it looked for and the troupe runs as
before.

## One server, one browser, one context per agent

A generation spawns the director, the critic and several roles. A Chromium tree
costs hundreds of megabytes, so there is one browser and every agent gets its
own browser context:

```text
clai agent ──streamable HTTP──► playwright-mcp :8931 ──► Chromium
clai agent ──streamable HTTP──►        │                (context per session)
clai agent ──streamable HTTP──►        │                 show: http://127.0.0.1:80/
```

`--isolated` opens a fresh context for every client, so one role's navigation
cannot move another role's viewport. `browser_close` drops that context; the
browser outlives it.

## The tool surface

The registry in `internal/agents/troupe/registry.go` is the gate, and it names
seven read-only tools:

| Tool | Result |
|---|---|
| `browser_navigate` | loads a URL and waits for the page to settle |
| `browser_wait_for` | sleeps, for an animation or an async render |
| `browser_snapshot` | indented outline of the page |
| `browser_take_screenshot` | the viewport as an image block |
| `browser_console_messages` | console lines, with a severity floor |
| `browser_resize` | viewport in CSS pixels |
| `browser_close` | drops the context |

The server exposes more (click, type, evaluate), but every registry entry is an
exact name, so a tool the table does not name matches nothing and stays
unreachable. Page mutation is deliberately absent: the troupe needs to look at
the show, not to drive it.

## Running the sidecar

```bash
make perception-up     # docker compose up -d --build playwright
```

The compose service carries what the server needs: host networking, so the
loopback endpoint is the same loopback kinoview serves on; `init`, so reparented
browser children are reaped; a `/tmp` tmpfs; a 1200 MB memory limit; and
`seccomp=unconfined`. The last one matters where the default Docker profile
blocks the 64-bit time syscalls: the container clock then reads as a huge year,
and every TLS handshake fails with `certificate verify failed`.

The published image is not used: the image is built from
[`docker/playwright-mcp.Dockerfile`](../docker/playwright-mcp.Dockerfile), which
adds the distro Chromium, because Playwright publishes no browser for every
platform.

### Build on the host that runs it

An image built for another architecture starts, connects, and then breaks at the
first navigation. Cross-architecture emulation does not help: `docker build
--platform` for a foreign architecture runs the image smoke step under QEMU,
where Chromium reports `GPU process isn't usable. Goodbye.` and `ptrace:
Function not implemented`.

Where the build step itself cannot read the clock, Docker needs a builder that
allows an unsandboxed step:

```bash
docker buildx create --name insecure --driver docker-container \
  --buildkitd-flags "--allow-insecure-entitlement security.insecure"
docker buildx build --builder insecure --allow security.insecure --load \
  -t kinoview-playwright .
```

### Known limit: 32-bit ARM

Playwright injects page utilities for its outline and for most `evaluate` calls.
On a 32-bit ARM host (measured on a Raspberry Pi, kernel 5.10):

| Call | Result |
|---|---|
| `page.evaluate("1+1")` | `2` — plain CDP evaluation is fine |
| `page.evaluate(() => document.title)` | `Target crashed` |
| `locator.ariaSnapshot()` | `Target crashed`, kernel logs `Alignment trap` |
| Playwright `launch` + `goto` + `screenshot` | worked, 46 kB JPEG of the gallery |

A navigation and a screenshot are safe there; the outline is the call to watch.
Run one `browser_snapshot` against the show and read the container log. If the
renderer dies, that platform cannot run Playwright's injected scripts, and
perception there is a screenshot-and-console tool only.

### Prove it

```bash
# 1. the server answers MCP
curl -s -X POST http://127.0.0.1:8931/mcp \
  -H 'content-type: application/json' \
  -H 'accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}'

# 2. one navigation and one outline, against the live show
curl -s -X POST http://127.0.0.1:8931/mcp \
  -H 'content-type: application/json' \
  -H 'accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"browser_snapshot","arguments":{}}}'
```

## Local development

```bash
make perception-up
mkdir -p ~/.config/kinoview/clai/mcpServers
printf '%s\n' '{"url":"http://127.0.0.1:8931/mcp","timeout_seconds":300}' \
  > ~/.config/kinoview/clai/mcpServers/playwright.json
go build -o kinoview . && ./kinoview serve -troupe <model> ...
```

The show is then `http://localhost:8080/`. `make perception-logs` follows the
server and the browser, which is the only place a renderer crash shows up.

## Operating notes

| Symptom | Cause | Fix |
|---|---|---|
| `troupe perception off: no Playwright MCP server at …` | the config file is missing | write it, restart |
| `troupe perception off: … the config sets both "command" and "url"` | clai rejects that file | keep exactly one |
| `troupe perception off: … url "…" is not on this host` | the endpoint is not loopback | run the sidecar on the kinoview host |
| `troupe perception off: … command "…" does not resolve` | a command-based config points at a moved binary | use the `url` config above |
| a tool result says the server is unreachable | the container is not running | `docker ps`, then `make perception-up` |
| the renderer dies on `browser_snapshot` | Playwright's injected script on 32-bit ARM (see the limit) | keep to navigate, screenshot and console there |
| `apk add` fails with `certificate verify failed` during the build | the build step cannot read the clock | rebuild with the `--security=insecure` builder |
| `GPU process isn't usable. Goodbye.` in the build or the container log | the host cannot run Chromium: an emulated architecture, or a kernel without `ptrace` | build and run on the native architecture |
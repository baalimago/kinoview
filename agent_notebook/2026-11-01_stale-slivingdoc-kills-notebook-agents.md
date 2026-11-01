# A slivingdoc that pulls can still be too old to serve

## Abstract

On 2026-11-01 the rpie host kept slivingdoc v1.2.0 while kinoview moved to
`serve --login never` (v1.3.0). Setup still logged `slivingdoc notebook ready`
because the seed only runs `pull`, and v1.2.0 supports `pull`. The failure
surfaced later, per agent: the concierge was dropped for the whole process
lifetime, and the first troupe generation died at director setup and still
spent the 6 h cooldown. Notebook readiness is not proof that the MCP child
starts.

## Timeline (2026-11-01, UTC)

| Time | Event |
|------|-------|
| 12:25 | kinoview restarted on rpie with `--login never`; slivingdoc still 1.2.0 |
| 12:25 | `slivingdoc notebook ready`, then `failed to setup concierge: … mcp server 'slivingdoc' failed to start at stage 'initialize'` |
| 12:26 | `troupe: generation startup failed: … director setup`; the cooldown was burned |
| 12:32 | slivingdoc 1.3.1 installed; server restarted 12:33 |
| 12:33 | `troupe perception: the browser tools inspect the show at http://127.0.0.1:80/`; still `attempted to find tool: 'rg'` |
| 12:34 | generation `startup` begins |
| 13:02 | `submitted story_20261101T123456Z` |
| 13:06 | critic note for the new play; ripgrep 15.2.0 installed at 13:12 |

## What the fix pins

- `slivingdoc.CheckVersion` runs `<binary> --version` in `setupNotebook`,
  before the worktree is seeded. A release older than `1.3.0`, or one that
  cannot answer, turns the notebook off with one warning: the server runs,
  the concierge falls back, the play API stays unmounted.
- The perception notice names the one reason perception is off. Previously a
  missing Playwright config logged its own cause, then a second line claimed
  "the port is chosen by the OS" whatever the real cause was.

## Traps found on rpie

| Trap | Symptom | Action |
|------|---------|--------|
| slivingdoc skew | notebook ready, every agent dies at initialize | version gate; check `slivingdoc --version` first when an agent reports an MCP start failure |
| missing `clai/mcpServers/playwright.json` | perception off although the container runs | write the config the container prints on start |
| no `rg` on the host | `attempted to find tool: 'rg'` per agent start | ripgrep 15.2.0 static armv7 → `/usr/local/bin/rg` |
| `deb.griffo.io` has no buster suite | `apt update` errors; nothing installs | rpie is Raspbian buster (EOL); use the static release asset |
| `raspbian.raspberrypi.org` buster | 404; only cached lists remain | do not plan on apt on this host |
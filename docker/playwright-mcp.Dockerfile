# The troupe's eye: @playwright/mcp and one Chromium in a container, the shape
# sakfraga runs (compose.yaml, playwright service). kinoview holds only the MCP
# server config that points at it; docs/PERCEPTION.md is the operator view.
#
# Build on the host that runs it. An image built for another architecture
# starts, connects, and then breaks at the first navigation.
FROM alpine:3.22

RUN apk add --no-cache nodejs npm chromium

# Playwright ships no browser for armv7, so the distro Chromium is the browser.
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1

# Prove the browser runs headless before the image is called done. --no-sandbox
# is required for a rootless Chromium in a container.
RUN chromium-browser --headless --no-sandbox --disable-gpu --dump-dom about:blank >/dev/null 2>&1

# The MCP server is the only thing kinoview asks of this image: it serves the
# browser tools the troupe calls. Pinned, because the tool surface is what the
# roster's globs name.
RUN npm install --global --no-fund --no-audit @playwright/mcp@0.0.83

# A browser needs a writable home for its profile; run unprivileged.
RUN addgroup -g 10002 -S playwright \
	&& adduser -u 10002 -S -G playwright -h /home/playwright playwright
ENV HOME=/home/playwright
USER 10002:10002

# Streamable HTTP on loopback: kinoview and the troupe share the host network,
# so 127.0.0.1:8931 reaches the same host that serves the show. --isolated gives
# every agent session its own browser context, so no run inherits another's page.
EXPOSE 8931
ENTRYPOINT ["playwright-mcp"]
CMD ["--headless", \
	"--isolated", \
	"--no-sandbox", \
	"--executable-path", "/usr/bin/chromium-browser", \
	"--host", "127.0.0.1", \
	"--port", "8931", \
	"--allowed-hosts", "127.0.0.1:8931", \
	"--viewport-size", "1024x768"]
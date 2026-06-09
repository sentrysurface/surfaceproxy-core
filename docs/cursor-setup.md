# Cursor IDE Integration Guide

This guide sets up SurfaceProxy as an MCP tool inside Cursor, giving your AI agent local, token-optimised web browsing — no API keys, no external services.

## Prerequisites

- SurfaceProxy binary installed (see [README.md](../README.md))
- Google Chrome or Chromium installed (for `browser.mode = "auto"`)
- Cursor IDE ≥ 0.40

## Step 1 — Install the binary

```bash
# macOS / Linux (Homebrew — coming in Phase 3)
brew install sentrysurface/tap/surface-proxy

# Or build from source
git clone https://github.com/sentrysurface/surfaceproxy-core
cd surfaceproxy-core
go build -o surface-proxy ./cmd/surface-proxy
sudo mv surface-proxy /usr/local/bin/
```

## Step 2 — Create your config file

```bash
mkdir -p ~/.surface-proxy
```

Create `~/.surface-proxy/config.json`:

```json
{
  "listen_addr": ":8443",
  "mcp_transport": "stdio",
  "browser": {
    "mode": "auto"
  },
  "firewall": {
    "allowlist": [
      "^https?://.*"
    ],
    "blocklist": []
  },
  "pruning": {
    "output_format": "markdown",
    "max_tokens": 8192,
    "strip_tags": ["script", "style", "svg", "noscript"]
  }
}
```

> **Tip:** Set `allowlist` to specific domains you want the agent to browse. Leave it broad (`^https?://.*`) during development.

## Step 3 — Add SurfaceProxy to Cursor's MCP config

Open or create `~/.config/Cursor/mcp.json` (macOS/Linux) or `%APPDATA%\Cursor\mcp.json` (Windows):

```json
{
  "mcpServers": {
    "surface-proxy": {
      "command": "surface-proxy",
      "args": ["mcp-mode", "--config", "~/.surface-proxy/config.json"]
    }
  }
}
```

> **Windows users:** Use the full binary path if `surface-proxy` is not in `%PATH%`:
> ```json
> "command": "C:\\Users\\YourName\\bin\\surface-proxy.exe"
> ```

## Step 4 — Restart Cursor

Restart Cursor completely. You should see **surface-proxy** appear in the MCP servers panel (Settings → MCP).

## Step 5 — Test it

In a Cursor chat, try:

```
Browse https://github.com/sentrysurface and summarise the homepage in 3 bullet points.
```

SurfaceProxy will:
1. Launch a local headless Chrome browser
2. Navigate to the URL
3. Strip all script/style/SVG noise from the HTML
4. Return a compact Markdown summary to your AI assistant

## Available MCP Tools

| Tool | Description | Arguments |
|---|---|---|
| `browse` | Navigate to a URL and return pruned DOM | `{"url": "https://example.com"}` |
| `getDOM` | Get current page state | `{}` |
| `click` | Click an element by CSS selector | `{"selector": "#submit-btn"}` |
| `type` | Type text into an input | `{"selector": "#search", "text": "hello"}` |
| `screenshot` | Capture a PNG screenshot (base64) | `{}` |

## Viewing the Dashboard

While SurfaceProxy is running in daemon mode, open:

```
http://localhost:8080
```

This shows real-time token savings, active sessions, and firewall rule hits.

## Troubleshooting

| Issue | Solution |
|---|---|
| `no Chrome binary found` | Set `browser.mode = "path"` and specify `binary_path` |
| Tool calls not appearing in Cursor | Restart Cursor; check MCP server status in Settings |
| Firewall blocking URLs | Add domain regex to `allowlist` in your config file |
| `connection refused` at `:8443` | Ensure `surface-proxy` binary is in `$PATH` |

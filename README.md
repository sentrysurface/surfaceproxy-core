# SurfaceProxy

**AI Web Proxy — Local, Low-Latency, Token-Efficient.**

SurfaceProxy sits inline between your AI agent and the browser, intercepting Chrome DevTools Protocol traffic to strip DOM noise and return clean, token-optimised Markdown back to your LLM — reducing context size by up to 90%.

```
┌────────────────────────────────────────────────────────────────┐
│                     DEVELOPER MACHINE                          │
│                                                                │
│   ┌────────────────┐  (MCP stdio / WebSocket)                  │
│   │  Cursor / IDE  │ ──────────────────────► ┌──────────────┐ │
│   │  Claude Desktop│ ◄────────────────────── │ SurfaceProxy │ │
│   │  Agent script  │       Pruned Markdown    │  Go Binary   │ │
│   └────────────────┘                         └──────┬───────┘ │
│                                                     │ CDP      │
│                                                     ▼          │
│                                         ┌──────────────────┐  │
│                                         │ Headless Chrome  │  │
│                                         │ (auto-launched)  │  │
│                                         └──────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

## ⚡ Quick Start (60 seconds)

### 1. Build the binary

```bash
git clone https://github.com/sentrysurface/surfaceproxy-core
cd surfaceproxy-core
go build -o surface-proxy ./cmd/surface-proxy
```

### 2. Run the full daemon

```bash
./surface-proxy --config surface-proxy.json
```

The proxy starts on `:8443`, launches a headless Chrome browser automatically, and opens the dashboard at **http://localhost:8080**.

### 3. Connect your IDE

**Cursor** — add to `~/.config/Cursor/mcp.json`:

```json
{
  "mcpServers": {
    "surface-proxy": {
      "command": "./surface-proxy",
      "args": ["mcp-mode", "--config", "surface-proxy.json"]
    }
  }
}
```

**Claude Desktop** — add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "surface-proxy": {
      "command": "./surface-proxy",
      "args": ["mcp-mode", "--config", "surface-proxy.json"]
    }
  }
}
```

### 4. Connect Playwright / Browser-Use

```python
browser = await playwright.chromium.connect(
    wsEndpoint="ws://localhost:8443/v1/session?allowlist=*.github.com"
)
```

## Core Architecture

```
[ Inbound CDP / MCP Traffic ]
           │
  ┌────────┴──────────┐
  ▼                   ▼
Firewall          Pruning Engine
Evaluator         HTML Tokenizer
(regex, <1ms)     sync.Pool bufs
                  Diff Cache
           │
           ▼
  Headless Chrome Runtime
  (ephemeral, auto-managed)
```

### Key Design Properties

| Property | Implementation |
|---|---|
| Zero external dependencies | Single static Go binary |
| Sub-2ms proxy latency | Goroutine-isolated sessions, non-blocking channels |
| Zero-allocation parsing | `sync.Pool` byte buffers, no `fmt.Sprintf` in hot paths |
| Hot-reload config | `fsnotify` watches `surface-proxy.json` at runtime |
| Panic isolation | `SafeGo()` wraps all goroutines with `recover()` barriers |

## Available MCP Tools

| Tool | Description |
|---|---|
| `browse` | Navigate to URL, return pruned Markdown |
| `getDOM` | Current page state with structural diff |
| `click` | Click element by CSS selector |
| `type` | Type text into an input field |
| `screenshot` | Capture PNG screenshot (base64) |

## Documentation

- [Cursor Setup Guide](docs/cursor-setup.md)
- [Claude Desktop Setup Guide](docs/claude-desktop-setup.md)

## Development

```bash
# Generate go.sum and verify build (requires Docker)
.\scripts\generate-gosum.ps1    # Windows
bash scripts/generate-gosum.sh  # macOS / Linux

# Run tests
go test ./...

# Build dev Docker image
docker build -f Dockerfile.dev -t surface-proxy-dev .
docker run -p 8443:8443 -p 8080:8080 surface-proxy-dev
```

## License

Business Source License 1.1 — see [BSL-LICENSE.txt](BSL-LICENSE.txt).


## Feedback & Support

For issues, feedback, and inquiries, please contact support@sentrysurface.io or visit https://sentrysurface.io/contact

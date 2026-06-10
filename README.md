# SurfaceProxy

**Stop paying the HTML context tax. Compress browser DOM trees 60–90% before they hit your LLM.**

SurfaceProxy is a zero-config local proxy that intercepts Chrome DevTools Protocol traffic between your AI agent and the browser. It strips layout noise (scripts, styles, SVG, invisible elements) and returns clean semantic Markdown — so your agent pays for signal, not whitespace.

```
┌────────────────────────────────────────────────────────────────────────┐
│                         DEVELOPER MACHINE                              │
│                                                                        │
│  ┌──────────────────────────────┐        ┌──────────────────────────┐  │
│  │  Your Agent / IDE            │  MCP / │   SurfaceProxy           │  │
│  │                              │ ──────►│   (Go Binary)            │  │
│  │  • browser-use               │        │                          │  │
│  │  • Cursor / Claude Desktop   │ ◄──────│  • Semantic pruning      │  │
│  │  • Playwright                │ Pruned │  • DOM diff cache        │  │
│  │  • Custom agent script       │   MD   │  • Firewall rules        │  │
│  └──────────────────────────────┘        └──────────┬───────────────┘  │
│                                                     │ CDP              │
│                                                     ▼                  │
│                                         ┌──────────────────────────┐  │
│                                         │   Headless Chrome        │  │
│                                         │   (auto-launched)        │  │
│                                         └──────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

## Why SurfaceProxy

Modern web pages routinely contain **2,000–8,000 tokens** of raw HTML noise per navigation step — script tags, inline styles, SVG paths, tracking pixels, hidden elements. Your agent doesn't need any of it.

| Scenario | Raw HTML tokens | After SurfaceProxy | Reduction |
|---|---|---|---|
| GitHub repo page | ~8,400 | ~620 | **93%** |
| Wikipedia article | ~12,000 | ~1,800 | **85%** |
| PyPI search results | ~6,200 | ~480 | **92%** |
| SaaS admin dashboard | ~15,000 | ~2,100 | **86%** |

At Claude 3.5 Sonnet pricing ($3/MTok input), a 5-step browser-use loop reading GitHub, PyPI, and Wikipedia pages costs roughly **$0.18 per run without SurfaceProxy** and **$0.014 with it** — a 13× reduction.

SurfaceProxy is a single static Go binary. No Python runtime, no Node.js, no Docker required. It runs as a local daemon and works with any framework that supports the Chrome DevTools Protocol.

---

## ⚡ 60-Second Quickstart

### 1. Install

```bash
# macOS / Linux — one-liner installer
curl -sSL https://raw.githubusercontent.com/sentrysurface/surfaceproxy-core/main/scripts/install.sh | sh

# Or build from source (requires Go 1.23+)
git clone https://github.com/sentrysurface/surfaceproxy-core
cd surfaceproxy-core
go build -o surface-proxy ./cmd/surface-proxy
```

### 2. Start the daemon

```bash
./surface-proxy
# [INIT] SurfaceProxy dev — Bootstrapping core engine
# [BROWSER] Auto-detected Google Chrome binary: /usr/bin/google-chrome
# [BROWSER] Launched headless Google Chrome (PID 12345) on port 54321
# [PROXY] CDP Proxy listening on ws://localhost:8443
# [UI] Dashboard listening on http://localhost:8080
```

Chrome is launched automatically. No configuration required.

### 3. Connect your agent (pick your framework)

**browser-use (Python)**
```python
from browser_use import Agent, Browser, BrowserConfig
browser = Browser(config=BrowserConfig(
    cdp_url="ws://localhost:8443/v1/session"
))
```

**Playwright (TypeScript)**
```typescript
const browser = await chromium.connectOverCDP("ws://localhost:8443/v1/session");
```

**Cursor / Claude Desktop (MCP — one command)**
```bash
./surface-proxy init --cursor    # Auto-registers in Cursor's mcp.json
./surface-proxy init --vscode    # Auto-registers in VS Code's mcp.json
```

---

## Framework Integrations

### browser-use

browser-use's `BrowserConfig.cdp_url` connects directly to SurfaceProxy's CDP proxy endpoint. Every `get_dom_content()` call your agent makes is automatically intercepted, pruned, and returned as Markdown — no changes to your agent code beyond the config line.

```python
# Before (expensive)
browser = Browser()

# After (up to 90% cheaper — one line change)
browser = Browser(config=BrowserConfig(
    cdp_url="ws://localhost:8443/v1/session"
))
```

Per-session domain filtering:
```python
# Lock this agent run to only the domains it needs
cdp_url="ws://localhost:8443/v1/session?allowlist=*.github.com,*.pypi.org"
```

→ Full examples: [`examples/browser-use/`](examples/browser-use/)

### Playwright / Puppeteer

```typescript
// Playwright
const browser = await chromium.connectOverCDP("ws://localhost:8443/v1/session");

// Puppeteer
const browser = await puppeteer.connect({ browserWSEndpoint: "ws://localhost:8443/v1/session" });
```

→ Full examples: [`examples/playwright/`](examples/playwright/)

### Cursor IDE (MCP)

```bash
surface-proxy init --cursor
# ✓  Registered surface-proxy in Cursor config:
#    /home/user/.config/Cursor/User/mcp.json
#    Restart Cursor to pick up the new MCP server.
```

Then in Cursor chat: `Browse https://docs.python.org and summarise the getting started section.`

→ Full guide: [`docs/cursor-setup.md`](docs/cursor-setup.md)

### Claude Desktop (MCP)

→ Full guide: [`docs/claude-desktop-setup.md`](docs/claude-desktop-setup.md)

---

## Session-Level Firewall

Every `/v1/session` connection accepts query parameters that scope the firewall rules to that session without touching the global config:

| Parameter | Description | Example |
|---|---|---|
| `allowlist` | Comma-separated URL glob patterns | `allowlist=*.gov.au,*.edu` |
| `blocklist` | Block specific domains | `blocklist=*.doubleclick.net` |
| `target` | Override browser endpoint | `target=ws://localhost:9222/...` |

```python
# This agent can only navigate to Australian government and university domains
cdp_url="ws://localhost:8443/v1/session?allowlist=*.gov.au,*.edu.au"
```

---

## MCP Tools (for Cursor / Claude Desktop)

| Tool | What it does | Arguments |
|---|---|---|
| `browse` | Navigate and return pruned Markdown | `{ "url": "https://..." }` |
| `getDOM` | Current page snapshot with structural diff | `{}` |
| `click` | Click element by CSS selector | `{ "selector": "#btn" }` |
| `type` | Type into an input field | `{ "selector": "#q", "text": "hello" }` |
| `screenshot` | Capture PNG (base64) | `{}` |

---

## Live Dashboard

Open **http://localhost:8080** while the daemon is running to see real-time token savings, active sessions, compression ratios, and dollar savings across all connected agents.

---

## Core Architecture

```
[ CDP / MCP Traffic In ]
         │
┌────────┴──────────────────┐
│  Firewall Rule Engine     │  Compiled regex, atomic pointer swap, <1ms eval
│  (allowlist / blocklist)  │
└────────┬──────────────────┘
         │ allowed
┌────────┴──────────────────┐
│  Semantic Pruning Engine  │  Streaming HTML tokenizer, sync.Pool buffers
│  strip: script, style,    │  DOM diff cache (SHA-256 ring buffer)
│  svg, noscript, iframe    │  Output: Markdown or JSON
└────────┬──────────────────┘
         │
┌────────┴──────────────────┐
│  Telemetry Ledger         │  Per-session byte/token accounting
│  (in-memory, thread-safe) │  Dollar savings calculation
└───────────────────────────┘
```

| Property | Implementation |
|---|---|
| Single static binary | No runtime dependencies, CGO_ENABLED=0 |
| Sub-2ms proxy overhead | Goroutine-isolated sessions, non-blocking channels |
| Zero-allocation hot paths | `sync.Pool` byte buffers, streaming tokenizer |
| Hot-reload config | `fsnotify` watches `surface-proxy.json` at runtime |
| Panic isolation | `SafeGo()` wraps every goroutine with `recover()` |
| Cross-platform Chrome detection | PATH → Windows registry → well-known OS paths |

---

## Development

```bash
# Run tests
go test ./...

# Build for current platform
go build -o surface-proxy ./cmd/surface-proxy

# Generate go.sum (requires Docker)
.\scripts\generate-gosum.ps1    # Windows
bash scripts/generate-gosum.sh  # macOS / Linux

# Build Docker dev image
docker-compose -f docker-compose.dev.yml up
```

---

## Documentation

| Guide | Description |
|---|---|
| [Cursor Setup](docs/cursor-setup.md) | Connect Cursor IDE via MCP |
| [Claude Desktop Setup](docs/claude-desktop-setup.md) | Connect Claude Desktop via MCP |
| [Playwright Quickstart](docs/integrations/playwright-quickstart.md) | Playwright & Puppeteer CDP integration |
| [Contributing](.github/CONTRIBUTING.md) | How to contribute |

---

## License

Business Source License 1.1 — see [BSL-LICENSE.txt](BSL-LICENSE.txt).

## Feedback & Support

For issues, feedback, and inquiries, please contact support@sentrysurface.io or visit https://sentrysurface.io/contact

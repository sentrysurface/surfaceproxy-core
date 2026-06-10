# Integration Quickstarts

| Integration | Guide | Examples |
|---|---|---|
| browser-use (Python) | [browser-use-quickstart.md](browser-use-quickstart.md) | [`examples/browser-use/`](../../examples/browser-use/) |
| Playwright (TypeScript) | [playwright-quickstart.md](playwright-quickstart.md) | [`examples/playwright/`](../../examples/playwright/) |
| Puppeteer (JavaScript) | [playwright-quickstart.md](playwright-quickstart.md) | — |

## Framework Compatibility Matrix

| Framework | Connection method | Per-session firewall | Savings API |
|---|---|---|---|
| browser-use | `BrowserConfig(cdp_url=...)` | ✅ `?allowlist=` | ✅ REST `/api/status` |
| Playwright | `chromium.connectOverCDP(url)` | ✅ `?allowlist=` | ✅ REST `/api/status` |
| Puppeteer | `puppeteer.connect({ browserWSEndpoint })` | ✅ `?allowlist=` | ✅ REST `/api/status` |
| Cursor (MCP) | `surface-proxy init --cursor` | ✅ Config-level | ✅ Dashboard |
| Claude Desktop (MCP) | Manual `mcp.json` | ✅ Config-level | ✅ Dashboard |

## Session Endpoint Reference

All frameworks connect to:

```
ws://localhost:8443/v1/session[?allowlist=...&blocklist=...&target=...]
```

| Query param | Type | Example |
|---|---|---|
| `allowlist` | Comma-separated glob patterns | `allowlist=*.github.com,*.pypi.org` |
| `blocklist` | Comma-separated glob patterns | `blocklist=*.ads.com,*.tracking.io` |
| `target` | WebSocket URL | `target=ws://localhost:9222/devtools/browser/abc` |

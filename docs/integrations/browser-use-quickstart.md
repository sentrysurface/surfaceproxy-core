# SurfaceProxy + browser-use Integration

## The One-Line Change

If you are already using [browser-use](https://github.com/browser-use/browser-use), connecting it to SurfaceProxy is a single line change:

```python
# Before — raw browser, full HTML tokens hit your LLM
browser = Browser()

# After — route through SurfaceProxy, 60–90% fewer tokens
browser = Browser(config=BrowserConfig(
    cdp_url="ws://localhost:8443/v1/session"
))
```

That's it. Every `get_dom_content()` call your agent makes is now intercepted, pruned, and returned as semantic Markdown. No other code changes.

---

## Why This Works

browser-use's `BrowserConfig.cdp_url` points the framework at a Chrome DevTools Protocol endpoint. Normally this is a raw browser. When you point it at SurfaceProxy instead:

```
browser-use agent
      │
      │  cdp_url = ws://localhost:8443/v1/session
      ▼
SurfaceProxy (local Go binary)
      │  strips: <script>, <style>, <svg>, hidden elements,
      │          inline event handlers, data-* noise, layout blocks
      │  keeps:  headings, links, text, form fields, semantic structure
      │  format: Markdown (default) or JSON
      ▼
Headless Chrome (auto-managed)
```

The agent receives clean Markdown instead of thousands of tokens of HTML garbage. Token reduction of 85–93% is typical for content-heavy pages.

---

## Prerequisites

### 1. Install SurfaceProxy

```bash
# macOS / Linux
curl -sSL https://raw.githubusercontent.com/sentrysurface/surfaceproxy-core/main/scripts/install.sh | sh

# From source
git clone https://github.com/sentrysurface/surfaceproxy-core
cd surfaceproxy-core
go build -o surface-proxy ./cmd/surface-proxy
```

### 2. Start SurfaceProxy

```bash
surface-proxy
# Auto-detects your system Chrome and starts on ws://localhost:8443
```

### 3. Install Python dependencies

```bash
cd examples/browser-use
pip install -r requirements.txt
```

---

## Running the Examples

### Basic agent

```bash
export ANTHROPIC_API_KEY=sk-ant-...
python basic_agent.py
```

### Benchmark (compare savings)

```bash
# Proxy only — see live savings
python benchmark_agent.py

# Compare before/after in one run
python benchmark_agent.py --compare

# Baseline (no proxy) for manual comparison
python benchmark_agent.py --no-proxy
```

**Sample output:**

```
══════════════════════════════════════════════════════════════
  BENCHMARK RUN: WITH SurfaceProxy
══════════════════════════════════════════════════════════════

  Task completed in 42.3s

  ────────────────────────────────────────────────────────────
  📥  Raw HTML tokens fed to agent:        87,420
  📤  Pruned MD tokens (after proxy):       7,319
  🗜️   Tokens saved this run:              80,101  (91.6%)
  💰  Cost saved this run:              $0.000240
  ────────────────────────────────────────────────────────────
```

---

## Per-Session Domain Firewall

Add an `?allowlist=` parameter to restrict an agent run to specific domains. This is particularly useful for preventing agents from accidentally crawling outside their intended scope:

```python
browser = Browser(config=BrowserConfig(
    # This agent session can only navigate to GitHub and PyPI
    cdp_url="ws://localhost:8443/v1/session?allowlist=*.github.com,*.pypi.org"
))
```

Navigation to any other domain returns a CDP error. The agent receives the error and can handle it gracefully.

Supported parameters:

| Parameter | Example | Effect |
|---|---|---|
| `allowlist` | `allowlist=*.gov.au,*.edu` | Only allow matching domains (glob) |
| `blocklist` | `blocklist=*.ads.com` | Block matching domains |
| `target` | `target=ws://localhost:9222/...` | Override browser endpoint for this session |

---

## Checking Real-Time Savings

Query the SurfaceProxy REST API while your agent is running:

```python
import httpx

data = httpx.get("http://localhost:8080/api/status").json()
print(f"Tokens saved: {data['tokens_reduced']:,}")
print(f"Dollars saved: ${data['dollars_saved']:.6f}")
print(f"Compression: {data['reduction_pct']:.1f}%")
```

Or open **http://localhost:8080** in your browser for the live dashboard.

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SURFACEPROXY_CDP` | `ws://localhost:8443/v1/session` | CDP endpoint |
| `SURFACEPROXY_API` | `http://localhost:8080/api/status` | REST API for savings metrics |
| `ANTHROPIC_API_KEY` | — | Your Anthropic API key |
| `ANTHROPIC_MODEL` | `claude-3-5-sonnet-20241022` | LLM model to use |

---

## Troubleshooting

| Problem | Solution |
|---|---|
| `Connection refused at ws://localhost:8443` | Start `surface-proxy` first |
| `No Chrome binary found` | Install Chrome, or set `browser.mode = "path"` in `surface-proxy.json` |
| `Allowlist blocked navigation` | Add the domain to your `?allowlist=` parameter |
| Agent runs but savings API shows 0 | Check that `surface-proxy` is running in daemon mode (not mcp-mode) |

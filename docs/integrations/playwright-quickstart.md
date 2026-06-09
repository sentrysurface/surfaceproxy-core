# Playwright & Puppeteer Integration Guide

This guide explains how to connect automated headless browser frameworks (like Playwright or Puppeteer) to **SurfaceProxy** to intercept CDP traffic, apply firewall filtering, and get token-optimised semantic DOM representations.

---

## Architecture Overview

Instead of launching Chrome directly or connecting directly to a debug port, your scripts connect to SurfaceProxy's **CDP Proxy Layer**.

```
[ Playwright Script ]
         │
         ▼  (Connect via WebSocket to SurfaceProxy)
[ SurfaceProxy (CDP Proxy) ]  ◄── [ Apply Firewall & Pruning Rules ]
         │
         ▼  (Intercepted WebSocket protocol)
  [ Local Chrome ]
```

---

## Quickstart (Playwright TypeScript)

Install Playwright:
```bash
npm install playwright
```

### TypeScript Example

Connecting to SurfaceProxy is as simple as replacing `launch()` with `connectOverCDP()`.

```typescript
import { chromium } from 'playwright';

async function run() {
  // 1. Connect to SurfaceProxy with optional allowlist configuration
  // This session will only allow navigating to Wikipedia and Python docs
  const wsEndpoint = 'ws://localhost:8443/v1/session?allowlist=*.wikipedia.org,*.python.org';
  
  console.log(`Connecting to SurfaceProxy at ${wsEndpoint}...`);
  const browser = await chromium.connectOverCDP(wsEndpoint);
  
  // 2. Open page and perform actions
  const context = browser.contexts()[0];
  const page = context ? context.pages()[0] || await context.newPage() : await browser.newPage();
  
  console.log('Navigating to Wikipedia...');
  await page.goto('https://en.wikipedia.org/wiki/Main_Page');
  
  // 3. This navigation will be BLOCKED by the per-session firewall because it's not in the allowlist
  try {
    console.log('Attempting to navigate to blocked website (google.com)...');
    await page.goto('https://www.google.com');
  } catch (err) {
    console.log('Navigation correctly blocked:', err.message);
  }

  // Clean up
  await browser.close();
}

run().catch(console.error);
```

---

## Puppeteer Integration

For Puppeteer, use `puppeteer.connect` with the `browserWSEndpoint` option.

```javascript
const puppeteer = require('puppeteer');

async function run() {
  const wsEndpoint = 'ws://localhost:8443/v1/session?allowlist=*.wikipedia.org';
  
  const browser = await puppeteer.connect({
    browserWSEndpoint: wsEndpoint
  });
  
  const page = await browser.newPage();
  await page.goto('https://en.wikipedia.org/wiki/Main_Page');
  
  await browser.close();
}

run().catch(console.error);
```

---

## Per-Session Query Parameters

When connecting to `/v1/session`, you can pass query parameters to configure the session firewall and target browser on the fly:

| Parameter | Type | Description | Example |
|---|---|---|---|
| `allowlist` | Comma-separated globs | Allowed navigation domains (merged with global rules) | `allowlist=*.gov.au,*.edu` |
| `blocklist` | Comma-separated globs | Blocked navigation domains (merged with global rules) | `blocklist=*.doubleclick.net` |
| `target` | URL | Explicit target browser WebSocket debugging URL override | `target=ws://localhost:9222/...` |

### Example with multiple overrides:
```javascript
const wsEndpoint = 'ws://localhost:8443/v1/session' +
  '?allowlist=*.wikipedia.org' +
  '&blocklist=*.ads.com' +
  '&target=ws://localhost:9222/devtools/browser/abc';
```

---

## Fetching Savings Metrics Programmatically

If you want your agent or test scripts to check how many tokens and dollars they've saved, you can query SurfaceProxy's REST API:

```javascript
const resp = await fetch('http://localhost:8080/api/status');
const data = await resp.json();

console.log(`Saved $${data.dollars_saved.toFixed(4)}!`);
console.log(`Total savings: ${data.savings_pct.toFixed(1)}%`);
```

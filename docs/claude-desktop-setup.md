# Claude Desktop Integration Guide

Connect SurfaceProxy to Claude Desktop to give Claude local web browsing without touching the Anthropic API browser tool.

## Prerequisites

- SurfaceProxy binary installed
- Claude Desktop ≥ 0.10
- Google Chrome or Chromium

## Step 1 — Install & configure SurfaceProxy

Follow Steps 1–2 in the [Cursor Setup Guide](./cursor-setup.md) to install the binary and create `~/.surface-proxy/config.json`.

## Step 2 — Edit Claude Desktop's MCP config

The config file location depends on your OS:

| OS | Path |
|---|---|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |

Add the following to `mcpServers`:

```json
{
  "mcpServers": {
    "surface-proxy": {
      "command": "surface-proxy",
      "args": ["mcp-mode", "--config", "/Users/yourname/.surface-proxy/config.json"]
    }
  }
}
```

> **Windows:** Use full path with escaped backslashes:
> ```json
> "command": "C:\\Users\\YourName\\bin\\surface-proxy.exe",
> "args": ["mcp-mode", "--config", "C:\\Users\\YourName\\.surface-proxy\\config.json"]
> ```

## Step 3 — Restart Claude Desktop

Fully quit and relaunch Claude Desktop. The SurfaceProxy tools should now appear as available tools in your conversation.

## Step 4 — Test it

Try asking Claude:

```
Use the browse tool to visit https://news.ycombinator.com and give me the top 5 stories.
```

Claude will call `surface-proxy → browse`, receive a clean Markdown snapshot of the page, and respond with the content — at a fraction of the token cost of a raw HTML pass.

## Protocol Notes

SurfaceProxy implements **MCP 2024-11-05**, the same version Claude Desktop uses. The stdio transport is used by default which means:

- No network ports opened for MCP (only the CDP proxy at `:8443` and UI at `:8080`)
- Claude Desktop spawns and manages the `surface-proxy` subprocess automatically
- The process exits when Claude Desktop closes

## Allowlist Configuration

For Claude Desktop usage, you typically want to restrict browsing to trusted domains. Update your config:

```json
{
  "firewall": {
    "allowlist": [
      "^https?://([a-zA-Z0-9-]+\\.)*news\\.ycombinator\\.com(/.*)?$",
      "^https?://([a-zA-Z0-9-]+\\.)*github\\.com(/.*)?$",
      "^https?://([a-zA-Z0-9-]+\\.)*wikipedia\\.org(/.*)?$"
    ],
    "blocklist": []
  }
}
```

Changes to the config file are **hot-reloaded** — no restart required.

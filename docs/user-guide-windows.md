# SurfaceProxy: Windows User & Integration Guide

Welcome to SurfaceProxy! This guide outlines how to run the background tray daemon, perform one-click integrations with **VS Code** (via Cline/Roo Code) and **Claude Desktop**, verify active isolated sessions, and track your token cost savings.

---

## 1. Installation & Bootstrapping

When you download SurfaceProxy on Windows, you receive two main executables:
1. `surface-proxy.exe` — The command-line engine that handles MCP stdio commands and CDP proxy routing.
2. `surface-proxy-tray.exe` — The GUI system tray application that launches headless Chrome and serves the dashboard.

### Step 1: Run the Tray Daemon in the Background
To start the engine silently in the background and free up your terminal, open PowerShell and run:
```powershell
.\surface-proxy-tray.exe --background
```
* **What happens under the hood**: The tray executable launches itself detached from the shell, creates a system tray icon in your Windows taskbar, spawns a warm headless Chrome instance, and starts the developer control console at `http://localhost:8080`.

### Step 2: Open the Dashboard
Open your web browser and go to:
```
http://localhost:8080
```
You will see the dark-themed glassmorphism developer dashboard displaying live telemetry, active sessions, and engine configuration diagnostics.

---

## 2. One-Click IDE Integration

SurfaceProxy provides automated setup for your AI agent clients directly from the web dashboard.

### Step 1: Register the Integration
1. On the dashboard, navigate to the **Integrations** tab.
2. You will see cards for **Cursor**, **VS Code** (specifically targetting extensions like **Cline** and **Roo Code**), and **Claude Desktop**.
3. Click the **Register** button on the target card.
   * **How Path Resolution Works**:
     * **Claude Desktop**: Automatically checks both the standard `%APPDATA%\Claude` path and the virtualized Windows Store package path (`%LOCALAPPDATA%\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Roaming\Claude`) to locate `claude_desktop_config.json`.
     * **VS Code**: Scans your local settings folders to locate the Cline/Roo Code settings path (`cline_mcp_settings.json`).
     * **Path Translation**: If you run one-click registration from the tray daemon (`surface-proxy-tray.exe`), the system automatically translates the executable target path to the CLI tool `surface-proxy.exe` in the same directory.
4. The dashboard will display a green `Registered` badge.

### Step 2: Restart your IDE
* **Claude Desktop**: Completely exit the app from the taskbar tray, then relaunch it.
* **VS Code**: Restart VS Code to refresh the Cline/Roo Code extensions.

---

## 3. Verifying and Testing the Integration

Let's trigger an agent run and observe how the connection attaches.

### Step 1: Ask the Agent to Browse
In Claude Desktop or your VS Code agent sidebar (Cline/Roo Code), type a prompt that triggers a browse action:
```
Browse python.org and find the latest release version
```

### Step 2: Dynamic Tab Creation & Session Isolation
When the agent executes the browse tool, here is the lifecycle:
1. The IDE spawns a subprocess of `surface-proxy.exe mcp-mode`.
2. This standalone process checks if the main tray daemon is running on port `:8443`.
3. Seeing the active daemon, it skips launching another Chrome instance and dials:
   ```
   ws://localhost:8443/v1/session?new_page=true
   ```
4. The tray daemon's CDP Proxy intercepts `new_page=true`, makes an HTTP call to Chrome's `/json/new` endpoint, and spawns a **brand-new, isolated blank tab**.
5. The proxy routes the agent's browse connection strictly to *that specific tab*.
6. When the agent finishes its browse task and the IDE kills the subprocess, a deferred handler calls `/json/close/{id}` on Chrome to **automatically close the tab**.

> [!NOTE]
> **Why this matters**: If you run multiple agent sessions in parallel (e.g. two open VS Code windows), each session is assigned an isolated Chrome tab. They will never collide, overwrite each other's navigation, or share history/cookies.

---

## 4. Telemetry and Token Saving Metrics

While your agent runs, go to the dashboard and navigate to the **Overview** and **Token Analytics** tabs:

### Active Sessions Table
* You will see your active agent session listed, showing the exact URL the agent is browsing (e.g. `https://www.python.org`).
* The **Active** badge will glow green. Once the agent finished, the status changes to closed.

### Savings Forensics
* **Raw vs Pruned Tokens**: Web pages contain heavy layout scripts, SVG icons, stylesheet blocks, and headers. SurfaceProxy's in-memory DOM engine automatically strips these out, leaving only readable clean Markdown content.
* **Telemetry Ledger**: The bytes and tokens saved are recorded under the connection's session ID and summarized on the dashboard:
  * **Tokens Compressed**: The total tokens stripped from the prompt.
  * **Reduction Percentage**: Usually averages **90% - 93% token reduction**.
  * **Accumulated Dollars Saved**: Calculates real-world money saved based on standard pricing (e.g. Claude 3.5 Sonnet @ $3.00/M input tokens).

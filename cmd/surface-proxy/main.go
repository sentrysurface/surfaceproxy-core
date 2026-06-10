package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sentrysurface/surface-proxy/internal/app"
	"github.com/sentrysurface/surface-proxy/internal/bootstrap"
	"github.com/sentrysurface/surface-proxy/internal/version"
)

func buildInfo() string {
	return version.BuildInfo()
}

func main() {
	// Surface-proxy supports the following top-level invocation modes:
	//
	//   surface-proxy [flags]              — full daemon (CDP proxy + MCP + UI)
	//   surface-proxy mcp-mode [flags]     — MCP-only stdio server (for Cursor / Claude Desktop)
	//   surface-proxy init [flags]         — self-register MCP config in Cursor / VS Code
	//   surface-proxy --version            — print version and exit

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp-mode":
			runMCPMode(os.Args[2:])
			return
		case "init":
			runInit(os.Args[2:])
			return
		}
	}

	runDaemon(os.Args[1:])
}

// ── Daemon mode ──────────────────────────────────────────────────────────────

// runDaemon starts the full SurfaceProxy engine: CDP proxy, MCP server, and dashboard UI.
func runDaemon(args []string) {
	fs := flag.NewFlagSet("surface-proxy", flag.ExitOnError)
	configPath := fs.String("config", "surface-proxy.json", "Path to configuration policy file")
	showVersion := fs.Bool("version", false, "Print version and exit")
	_ = fs.Parse(args)

	if *showVersion {
		fmt.Printf("surface-proxy %s\n", buildInfo())
		os.Exit(0)
	}

	log.Printf("[INIT] SurfaceProxy %s — Bootstrapping core engine using policy: %s", buildInfo(), *configPath)

	a, err := app.NewApp(*configPath, app.ModeFull)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize application: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("[SHUTDOWN] Executing clean application termination routine...")
		cancel()
	}()

	if err := a.Start(ctx); err != nil {
		log.Fatalf("[FATAL] Application exited with error: %v", err)
	}
}

// ── MCP stdio mode ───────────────────────────────────────────────────────────

// runMCPMode starts a minimal MCP stdio server — no dashboard, no CDP proxy listener.
// This is the mode used when Cursor or Claude Desktop spawns the binary as a subprocess.
//
// Example ~/.config/Cursor/mcp.json:
//
//	{
//	  "mcpServers": {
//	    "surface-proxy": {
//	      "command": "surface-proxy",
//	      "args": ["mcp-mode", "--config", "~/.surface-proxy/config.json"]
//	    }
//	  }
//	}
func runMCPMode(args []string) {
	fs := flag.NewFlagSet("surface-proxy mcp-mode", flag.ExitOnError)
	configPath := fs.String("config", "surface-proxy.json", "Path to configuration policy file")
	_ = fs.Parse(args)

	// In mcp-mode, stdout is owned by the JSON-RPC protocol.
	// All log output must go to stderr so it doesn't corrupt the RPC stream.
	log.SetOutput(os.Stderr)
	log.Printf("[MCP] surface-proxy %s — starting in MCP stdio mode, config: %s", buildInfo(), *configPath)

	a, err := app.NewApp(*configPath, app.ModeMCPOnly)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		cancel()
	}()

	if err := a.Start(ctx); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}
}

// ── Init / self-registration mode ────────────────────────────────────────────

// runInit registers surface-proxy as an MCP server in Cursor and/or VS Code.
// It locates the editor's mcp.json, merges in the surface-proxy entry without
// touching other existing server definitions, and writes the result atomically.
//
// Usage:
//
//	surface-proxy init --cursor               # Register with Cursor (default)
//	surface-proxy init --vscode               # Register with VS Code
//	surface-proxy init --cursor --vscode      # Register with both
//	surface-proxy init --cursor --dry-run     # Preview without writing
func runInit(args []string) {
	fs := flag.NewFlagSet("surface-proxy init", flag.ExitOnError)
	cursor := fs.Bool("cursor", false, "Register surface-proxy as an MCP server in Cursor IDE")
	vscode := fs.Bool("vscode", false, "Register surface-proxy as an MCP server in VS Code")
	dryRun := fs.Bool("dry-run", false, "Print what would be written without touching the filesystem")
	_ = fs.Parse(args)

	// Default: register with Cursor if no target is specified
	if !*cursor && !*vscode {
		*cursor = true
	}

	// Resolve the absolute path of this binary so the mcp.json entry always
	// works even if surface-proxy is not in the editor's PATH.
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("[INIT] Could not determine executable path: %v", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		log.Fatalf("[INIT] Could not resolve symlinks on executable path: %v", err)
	}

	if *dryRun {
		fmt.Println("[DRY-RUN] No files will be written.\n")
	}

	var registered int
	if *cursor {
		if err := bootstrap.Register(bootstrap.EditorCursor, execPath, *dryRun); err != nil {
			log.Printf("[INIT] ERROR registering Cursor: %v\n", err)
		} else {
			registered++
		}
	}
	if *vscode {
		if err := bootstrap.Register(bootstrap.EditorVSCode, execPath, *dryRun); err != nil {
			log.Printf("[INIT] ERROR registering VS Code: %v\n", err)
		} else {
			registered++
		}
	}

	if !*dryRun && registered > 0 {
		fmt.Println("\nNext steps:")
		fmt.Println("  1. Restart your editor to pick up the new MCP server.")
		fmt.Printf("  2. Run 'surface-proxy' to start the daemon.\n")
		fmt.Printf("  3. Ask your AI assistant: \"Browse github.com and summarise the homepage.\"\n")
	}
}

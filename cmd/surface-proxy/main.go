package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sentrysurface/surface-proxy/internal/app"
)

// Build metadata — injected via -ldflags at release time.
// Defaults to development values when built without ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func buildInfo() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, buildDate)
}


func main() {
	// Surface-proxy supports two top-level invocation modes:
	//   surface-proxy [flags]             — full daemon (CDP proxy + MCP + UI)
	//   surface-proxy mcp-mode [flags]    — MCP-only stdio server (for Cursor / Claude Desktop)

	if len(os.Args) > 1 && os.Args[1] == "mcp-mode" {
		runMCPMode(os.Args[2:])
		return
	}

	runDaemon(os.Args[1:])
}

// runDaemon starts the full SurfaceProxy engine: CDP proxy, MCP server, and dashboard UI.
func runDaemon(args []string) {
	fs := flag.NewFlagSet("surface-proxy", flag.ExitOnError)
	configPath := fs.String("config", "surface-proxy.json", "Path to configuration policy file")
	showVersion := fs.Bool("version", false, "Print version and exit")
	fs.Parse(args)

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
	fs.Parse(args)

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

package app

import (
	"context"
	"log"

	"github.com/sentrysurface/surface-proxy/internal/browser"
	"github.com/sentrysurface/surface-proxy/internal/cdp"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/mcp"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
	"github.com/sentrysurface/surface-proxy/internal/ui"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

// Mode controls which subsystems are started.
type Mode int

const (
	// ModeFull starts the CDP proxy, MCP server (websocket or stdio), and dashboard UI.
	ModeFull Mode = iota
	// ModeMCPOnly starts only the MCP stdio server and browser launcher — no CDP proxy or UI.
	// This is the mode used when an IDE spawns the binary as an MCP subprocess.
	ModeMCPOnly
)

// App is the root application object that owns all component lifetimes.
type App struct {
	mode       Mode
	configPath string
	loader     *config.Loader
	firewall   *firewall.RuleEngine
	pruner     *pruning.Pruner
	launcher   *browser.Launcher // nil when browser.mode = "external"
	proxy      *cdp.Proxy        // nil in ModeMCPOnly
	mcpServer  *mcp.Server
}

func NewApp(configPath string, mode Mode) (*App, error) {
	loader, err := config.NewLoader(configPath)
	if err != nil {
		return nil, err
	}

	cfg := loader.GetConfig()

	fw, err := firewall.NewRuleEngine(cfg.Firewall)
	if err != nil {
		loader.Close()
		return nil, err
	}

	pr := pruning.NewPruner(cfg.Pruning)

	// Conditionally build the browser launcher
	var launcher *browser.Launcher
	if cfg.Browser.Mode != "external" {
		launcher = browser.NewLauncher(cfg.Browser)
	}

	mcpHandlers := mcp.NewHandlers(cfg, fw, pr)

	var proxy *cdp.Proxy
	if mode == ModeFull {
		// Cast launcher to BrowserURLProvider interface (nil is safe — proxy handles nil gracefully)
		var bup cdp.BrowserURLProvider
		if launcher != nil {
			bup = launcher
		}
		proxy = cdp.NewProxy(cfg, fw, pr, bup)
	}

	mcpServer := mcp.NewServer(cfg, mcpHandlers)

	app := &App{
		mode:       mode,
		configPath: configPath,
		loader:     loader,
		firewall:   fw,
		pruner:     pr,
		launcher:   launcher,
		proxy:      proxy,
		mcpServer:  mcpServer,
	}

	// Hot-reload config on file change
	if err := loader.Watch(func(newCfg *config.Config) {
		log.Println("[CONFIG] Configuration changed — hot-reloading rules...")
		if err := fw.UpdateRules(newCfg.Firewall); err != nil {
			log.Printf("[CONFIG] Failed to reload firewall rules: %v", err)
		} else {
			log.Println("[CONFIG] Firewall rules reloaded.")
		}
		pr.UpdateConfig(newCfg.Pruning)
		log.Println("[CONFIG] Pruner config reloaded.")
	}); err != nil {
		log.Printf("[CONFIG] Warning: config watcher failed to start: %v", err)
	}

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	errChan := make(chan error, 4)

	// Step 1: Start the browser (unless using an external browser endpoint)
	if a.launcher != nil {
		wsURL, err := a.launcher.Start(ctx)
		if err != nil {
			log.Printf("[APP] Browser launcher failed: %v", err)
			// Non-fatal in full mode if there's a static TargetBrowserURL fallback
			if a.loader.GetConfig().TargetBrowserURL == "" {
				return err
			}
			log.Printf("[APP] Falling back to static TargetBrowserURL: %s", a.loader.GetConfig().TargetBrowserURL)
		} else {
			// Push the live URL to the MCP handlers so they use the managed browser
			a.mcpServer.Handlers().UpdateBrowserURL(wsURL)
		}
	}

	// Step 2: Start the CDP proxy (full mode only)
	if a.proxy != nil {
		util.SafeGo(func() {
			if err := a.proxy.ListenAndServe(ctx); err != nil {
				errChan <- err
			}
		})
	}

	// Step 3: Start the MCP server
	util.SafeGo(func() {
		if err := a.mcpServer.Start(ctx); err != nil {
			errChan <- err
		}
	})

	// Step 4: Start the dashboard UI (full mode only)
	if a.mode == ModeFull {
		util.SafeGo(func() {
			uiServer := ui.NewServer(a.loader.GetConfig())
			if err := uiServer.Start(ctx); err != nil {
				errChan <- err
			}
		})
	}

	select {
	case <-ctx.Done():
		a.Close()
		return nil
	case err := <-errChan:
		a.Close()
		return err
	}
}

func (a *App) Close() {
	if a.launcher != nil {
		a.launcher.Stop()
	}
	if a.loader != nil {
		a.loader.Close()
	}
}

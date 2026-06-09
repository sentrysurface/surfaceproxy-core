package app

import (
	"context"
	"log"
	"os"

	"github.com/sentrysurface/surface-proxy/internal/browser"
	"github.com/sentrysurface/surface-proxy/internal/cdp"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/mcp"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
	"github.com/sentrysurface/surface-proxy/internal/telemetry"
	"github.com/sentrysurface/surface-proxy/internal/ui"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

// Mode controls which subsystems are started.
type Mode int

const (
	ModeFull    Mode = iota // CDP proxy + MCP server + dashboard UI
	ModeMCPOnly             // MCP stdio only (for IDE subprocess invocation)
)

// App is the root application object that owns all component lifetimes.
type App struct {
	mode      Mode
	loader    *config.Loader
	firewall  *firewall.RuleEngine
	pruner    *pruning.Pruner
	ledger    *telemetry.Ledger
	launcher  *browser.Launcher
	proxy     *cdp.Proxy
	mcpServer *mcp.Server
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

	ledger := telemetry.NewLedger()
	pr := pruning.NewPruner(cfg.Pruning)
	pr.SetTelemetry(ledger)

	var launcher *browser.Launcher
	if cfg.Browser.Mode != "external" {
		launcher = browser.NewLauncher(cfg.Browser)
	}

	mcpHandlers := mcp.NewHandlers(cfg, fw, pr, ledger)

	var proxy *cdp.Proxy
	if mode == ModeFull {
		var bup cdp.BrowserURLProvider
		if launcher != nil {
			bup = launcher
		}
		proxy = cdp.NewProxy(cfg, fw, pr, bup)
	}

	mcpServer := mcp.NewServer(cfg, mcpHandlers)

	app := &App{
		mode:      mode,
		loader:    loader,
		firewall:  fw,
		pruner:    pr,
		ledger:    ledger,
		launcher:  launcher,
		proxy:     proxy,
		mcpServer: mcpServer,
	}

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

	// Step 1: Start the browser
	if a.launcher != nil {
		wsURL, err := a.launcher.Start(ctx)
		if err != nil {
			log.Printf("[APP] Browser launcher failed: %v", err)
			if a.loader.GetConfig().TargetBrowserURL == "" {
				return err
			}
			log.Printf("[APP] Falling back to static TargetBrowserURL: %s", a.loader.GetConfig().TargetBrowserURL)
		} else {
			a.mcpServer.Handlers().UpdateBrowserURL(wsURL)
		}
	}

	// Step 2: CDP proxy (full mode only)
	if a.proxy != nil {
		util.SafeGo(func() {
			if err := a.proxy.ListenAndServe(ctx); err != nil {
				errChan <- err
			}
		})
	}

	// Step 3: MCP server
	util.SafeGo(func() {
		if err := a.mcpServer.Start(ctx); err != nil {
			errChan <- err
		}
	})

	// Step 4: Dashboard UI (full mode only)
	if a.mode == ModeFull {
		util.SafeGo(func() {
			uiServer := ui.NewServer(a.loader.GetConfig(), a.ledger)
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
	// Print global telemetry summary on exit
	if a.ledger != nil {
		stats := a.ledger.GlobalStats()
		if stats.TotalPruneOps > 0 {
			telemetry.PrintGlobalSummary(stats, telemetry.DefaultPricing, os.Stderr)
		}
	}

	if a.launcher != nil {
		a.launcher.Stop()
	}
	if a.loader != nil {
		a.loader.Close()
	}
}

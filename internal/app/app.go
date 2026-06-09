package app

import (
	"context"
	"log"

	"github.com/sentrysurface/surface-proxy/internal/cdp"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/mcp"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
	"github.com/sentrysurface/surface-proxy/internal/ui"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

type App struct {
	configPath string
	loader     *config.Loader
	firewall   *firewall.RuleEngine
	pruner     *pruning.Pruner
	proxy      *cdp.Proxy
	mcpServer  *mcp.Server
}

func NewApp(configPath string) (*App, error) {
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

	proxy := cdp.NewProxy(cfg, fw, pr)

	mcpHandlers := mcp.NewHandlers(cfg, fw, pr)
	mcpServer := mcp.NewServer(cfg, mcpHandlers)

	app := &App{
		configPath: configPath,
		loader:     loader,
		firewall:   fw,
		pruner:     pr,
		proxy:      proxy,
		mcpServer:  mcpServer,
	}

	err = loader.Watch(func(newCfg *config.Config) {
		log.Println("[CONFIG] Configuration file changed, hot-reloading rules...")
		if err := fw.UpdateRules(newCfg.Firewall); err != nil {
			log.Printf("[CONFIG] Failed to hot-reload firewall rules: %v", err)
		} else {
			log.Println("[CONFIG] Firewall rules reloaded successfully.")
		}
		pr.UpdateConfig(newCfg.Pruning)
		log.Println("[CONFIG] Pruner rules reloaded successfully.")
	})
	if err != nil {
		log.Printf("[CONFIG] Failed to watch config file: %v", err)
	}

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	errChan := make(chan error, 3)

	util.SafeGo(func() {
		if err := a.proxy.ListenAndServe(ctx); err != nil {
			errChan <- err
		}
	})

	util.SafeGo(func() {
		if err := a.mcpServer.Start(ctx); err != nil {
			errChan <- err
		}
	})

	util.SafeGo(func() {
		uiServer := ui.NewServer(a.loader.GetConfig())
		if err := uiServer.Start(ctx); err != nil {
			errChan <- err
		}
	})

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
	if a.loader != nil {
		a.loader.Close()
	}
}

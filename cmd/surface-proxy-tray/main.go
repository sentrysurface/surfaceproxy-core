//go:build (darwin || windows) && !headless

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sentrysurface/surface-proxy/internal/app"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/tray"
	"github.com/sentrysurface/surface-proxy/internal/version"
)

func main() {
	fs := flag.NewFlagSet("surface-proxy-tray", flag.ExitOnError)
	configPath := fs.String("config", "surface-proxy.json", "Path to configuration policy file")
	fs.Parse(os.Args[1:])

	log.Printf("[TRAY] SurfaceProxy %s — starting system tray daemon", version.Version)

	resolvedPath := config.ResolvePath(*configPath)
	a, err := app.NewApp(resolvedPath, app.ModeFull)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize application: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		cancel()
	}()

	// Start all core services in the background
	go func() {
		if err := a.Start(ctx); err != nil {
			log.Printf("[TRAY] App error: %v", err)
			cancel()
		}
	}()

	// Run the tray on the main goroutine (required by most OS tray APIs)
	tray.Run(tray.Options{
		Version:      version.Version,
		DashboardURL: "http://localhost:8080",
		Ledger:       a.Ledger(),
		OnQuit:       cancel,
	})
}

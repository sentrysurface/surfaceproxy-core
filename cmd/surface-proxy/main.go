package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sentrysurface/surface-proxy/internal/app"
)

func main() {
	configPath := flag.String("config", "surface-proxy.json", "Path to local configuration policy")
	flag.Parse()

	log.Printf("[INIT] Bootstrapping SurfaceProxy Core Engine using policy: %s", *configPath)

	a, err := app.NewApp(*configPath)
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

//go:build (darwin || windows) && !headless

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/sentrysurface/surface-proxy/internal/app"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/tray"
	"github.com/sentrysurface/surface-proxy/internal/version"
)

func main() {
	fs := flag.NewFlagSet("surface-proxy-tray", flag.ExitOnError)
	configPath := fs.String("config", "surface-proxy.json", "Path to configuration policy file")
	background := fs.Bool("background", false, "Detach from the current terminal and run in the background")
	fs.Parse(os.Args[1:])

	// If --background is requested, re-launch ourselves detached and exit this process.
	// This lets PowerShell / cmd.exe terminals return to their prompt immediately.
	if *background {
		if err := relaunchDetached(); err != nil {
			log.Fatalf("[TRAY] Failed to launch in background: %v", err)
		}
		os.Exit(0)
	}

	resolvedPath := config.ResolvePath(*configPath)
	config.SetupLogging(resolvedPath)

	log.Printf("[TRAY] SurfaceProxy %s — starting system tray daemon", version.Version)

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

// relaunchDetached starts a new instance of this binary without the --background flag,
// fully detached from the parent terminal.
func relaunchDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Build args, stripping --background from the list
	var args []string
	for _, a := range os.Args[1:] {
		if a == "--background" || a == "-background" {
			continue
		}
		args = append(args, a)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: 0x00000008, // DETACHED_PROCESS
		}
	default:
		// On macOS / Linux, set process group to detach
		cmd = exec.Command(exe, args...)
		cmd.SysProcAttr = newSysProcAttr()
	}

	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

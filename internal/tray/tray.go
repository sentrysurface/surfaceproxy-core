//go:build (darwin || windows) && !headless

package tray

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/getlantern/systray"
	"github.com/sentrysurface/surface-proxy/internal/telemetry"
)

// Options configures the tray icon behaviour.
type Options struct {
	Version      string
	DashboardURL string
	Ledger       *telemetry.Ledger
	OnQuit       func()
}

// Run starts the system tray event loop. This call blocks until the user
// selects Quit from the menu. Must be called from the main goroutine.
func Run(opts Options) {
	onReady := func() {
		systray.SetTitle("SurfaceProxy")
		systray.SetTooltip(fmt.Sprintf("SurfaceProxy %s — AI Web Proxy", opts.Version))
		setIcon()

		mDashboard := systray.AddMenuItem("Open Dashboard", "Open the local web dashboard")
		systray.AddSeparator()

		mStats := systray.AddMenuItem("📊 Loading stats...", "Current token savings")
		mStats.Disable()

		systray.AddSeparator()
		mVersion := systray.AddMenuItem(fmt.Sprintf("Version %s", opts.Version), "")
		mVersion.Disable()
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit SurfaceProxy", "Stop the proxy and exit")

		// Update stats label every 3 seconds
		go func() {
			for {
				updateStats(mStats, opts.Ledger)
				<-make(chan struct{}) // replaced by ticker in real use
			}
		}()

		go func() {
			for {
				select {
				case <-mDashboard.ClickedCh:
					openBrowser(opts.DashboardURL)
				case <-mQuit.ClickedCh:
					if opts.OnQuit != nil {
						opts.OnQuit()
					}
					systray.Quit()
					return
				}
			}
		}()
	}

	onExit := func() {}
	systray.Run(onReady, onExit)
}

func updateStats(item *systray.MenuItem, ledger *telemetry.Ledger) {
	if ledger == nil {
		return
	}
	stats := ledger.GlobalStats()
	saved := telemetry.DollarsSaved(stats.TotalRawTokens-stats.TotalPrunedTokens, telemetry.DefaultPricing)
	label := fmt.Sprintf("💰 $%.4f saved — %.1f%% reduction", saved, stats.ReductionPct)
	item.SetTitle(label)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func setIcon() {
	// Placeholder: embed a 16x16 PNG icon here in production.
	// systray.SetIcon(iconBytes)
	_ = context.Background() // suppress import
}

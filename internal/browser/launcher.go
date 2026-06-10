package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

// defaultCDPPort is the standard Chrome DevTools Protocol debug port.
// SurfaceProxy checks this port first in auto mode to reuse an already-running browser.
const defaultCDPPort = 9222

// Launcher manages the lifecycle of an ephemeral headless Chrome subprocess.
type Launcher struct {
	cfg     config.BrowserConfig
	cmd     *exec.Cmd
	wsURL   string
	mu      sync.RWMutex
	running bool
	// reused is true when we connected to an existing Chrome rather than launching one.
	// In this case, Stop() will not kill the process.
	reused bool
}

func NewLauncher(cfg config.BrowserConfig) *Launcher {
	return &Launcher{cfg: cfg}
}

// WSURL returns the active WebSocket debugger endpoint URL.
// Returns empty string if the browser is not running.
func (l *Launcher) WSURL() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.wsURL
}

// IsRunning returns true if the managed browser subprocess is alive.
func (l *Launcher) IsRunning() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.running
}

// Start launches a headless Chrome subprocess and blocks until the debugger endpoint
// is reachable. Returns the WebSocket debugger URL.
//
// If a Chrome instance is already listening on the standard CDP port (9222) and mode
// is "auto", Start will reuse it instead of launching a new process — ideal for
// developers who already have a debugging Chrome session open.
func (l *Launcher) Start(ctx context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return l.wsURL, nil
	}

	// ── Step 1: Try to reuse an already-running Chrome ───────────────────────
	// When mode is "auto" and no explicit debug port is configured, check if a
	// Chrome instance is already listening on the standard port 9222. This avoids
	// spinning up a second browser when the developer already has one running.
	if (l.cfg.Mode == "auto" || l.cfg.Mode == "") && l.cfg.DebugPort == 0 {
		if wsURL, err := waitForBrowser(ctx, fmt.Sprintf("http://127.0.0.1:%d", defaultCDPPort), 350*time.Millisecond); err == nil {
			l.wsURL = wsURL
			l.running = true
			l.reused = true
			log.Printf("[BROWSER] Reusing existing Chrome debugger at port %d: %s", defaultCDPPort, wsURL)
			return wsURL, nil
		}
	}

	// ── Step 2: Resolve binary path ───────────────────────────────────────────
	binaryPath, binaryName, err := l.resolveBinary()
	if err != nil {
		return "", err
	}

	// ── Step 3: Choose a debug port ───────────────────────────────────────────
	port := l.cfg.DebugPort
	if port == 0 {
		port, err = freePort()
		if err != nil {
			return "", fmt.Errorf("failed to find a free port for Chrome debugger: %w", err)
		}
	}

	// ── Step 4: Build launch arguments ───────────────────────────────────────
	args := []string{
		"--headless=new",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--disable-software-rasterizer",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
		"--metrics-recording-only",
		"--mute-audio",
		"--hide-scrollbars",
	}

	// Inject an isolated profile directory so SurfaceProxy doesn't conflict
	// with the developer's personal Chrome session. Only added if the caller
	// hasn't already specified a --user-data-dir in cfg.Args.
	if !hasFlag(l.cfg.Args, "--user-data-dir") {
		profileDir := filepath.Join(os.TempDir(), "surface-proxy-chrome-profile")
		args = append(args, fmt.Sprintf("--user-data-dir=%s", profileDir))
	}

	args = append(args, l.cfg.Args...)

	// ── Step 5: Launch the subprocess ────────────────────────────────────────
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	// Discard Chrome's own log output; our own log messages stay clean
	cmd.Stderr = io.Discard
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to launch %s at %s: %w", binaryName, binaryPath, err)
	}

	l.cmd = cmd
	l.running = true
	l.reused = false

	log.Printf("[BROWSER] Launched headless %s (PID %d) on port %d using: %s",
		binaryName, cmd.Process.Pid, port, binaryPath)

	// Monitor process exit in the background
	go func() {
		_ = cmd.Wait()
		l.mu.Lock()
		pid := cmd.Process.Pid
		l.running = false
		l.wsURL = ""
		l.mu.Unlock()
		log.Printf("[BROWSER] %s process exited (PID %d)", binaryName, pid)
	}()

	// ── Step 6: Wait for the debugger endpoint ────────────────────────────────
	debuggerURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	wsURL, err := waitForBrowser(ctx, debuggerURL, 15*time.Second)
	if err != nil {
		l.shutdownLocked()
		return "", fmt.Errorf("%s started but debugger not reachable within 15s: %w", binaryName, err)
	}

	l.wsURL = wsURL
	log.Printf("[BROWSER] Debugger endpoint ready: %s", wsURL)
	return wsURL, nil
}

// Stop gracefully terminates the managed Chrome subprocess.
// If the browser was reused (not launched by SurfaceProxy), Stop is a no-op.
func (l *Launcher) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shutdownLocked()
}

func (l *Launcher) shutdownLocked() {
	if !l.reused && l.cmd != nil && l.cmd.Process != nil {
		_ = l.cmd.Process.Kill()
	}
	l.running = false
	l.wsURL = ""
}

// resolveBinary finds the Chrome binary path based on the configured mode.
// Returns (path, displayName, error).
func (l *Launcher) resolveBinary() (string, string, error) {
	switch l.cfg.Mode {
	case "path":
		if l.cfg.BinaryPath == "" {
			return "", "", fmt.Errorf("browser.mode is \"path\" but browser.binary_path is empty")
		}
		if _, err := os.Stat(l.cfg.BinaryPath); err != nil {
			return "", "", fmt.Errorf("browser binary not found at %s: %w", l.cfg.BinaryPath, err)
		}
		return l.cfg.BinaryPath, "Chrome", nil

	case "auto", "":
		path, name, found := FindChromeBinary()
		if !found {
			return "", "", fmt.Errorf(
				"no Chrome, Chromium, Edge, or Brave binary was found on this system.\n" +
					"Ensure Chrome is installed, or set browser.mode = \"path\" with " +
					"browser.binary_path in surface-proxy.json",
			)
		}
		log.Printf("[BROWSER] Auto-detected %s binary: %s", name, path)
		return path, name, nil

	case "external":
		return "", "", fmt.Errorf("browser.mode is \"external\" — connect via target_browser_url, not the launcher")

	default:
		return "", "", fmt.Errorf("unknown browser.mode %q — valid values: auto, path, external", l.cfg.Mode)
	}
}

// waitForBrowser polls the Chrome DevTools HTTP endpoint until it responds or timeout elapses.
// Returns the WebSocket URL for the browser target.
func waitForBrowser(ctx context.Context, debuggerHTTP string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		resp, err := client.Get(debuggerHTTP + "/json/version")
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var info struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.WebSocketDebuggerURL != "" {
				return info.WebSocketDebuggerURL, nil
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	return "", fmt.Errorf("timed out after %s waiting for Chrome debugger at %s", timeout, debuggerHTTP)
}

// freePort asks the OS for an available TCP port by binding to :0 then releasing it.
func freePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// hasFlag returns true if any element of args starts with the given flag name.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, flag) {
			return true
		}
	}
	return false
}

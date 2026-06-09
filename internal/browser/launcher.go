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
	"sync"
	"time"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

// Launcher manages the lifecycle of an ephemeral headless Chrome subprocess.
type Launcher struct {
	cfg     config.BrowserConfig
	cmd     *exec.Cmd
	wsURL   string
	mu      sync.RWMutex
	running bool
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

// Start launches the headless Chrome subprocess and blocks until the debugger
// endpoint is reachable. Returns the WebSocket debugger URL.
func (l *Launcher) Start(ctx context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return l.wsURL, nil
	}

	// Resolve the binary path based on mode
	binaryPath, err := l.resolveBinary()
	if err != nil {
		return "", err
	}

	// Choose a debug port — either configured or pick a random free port
	port := l.cfg.DebugPort
	if port == 0 {
		port, err = freePort()
		if err != nil {
			return "", fmt.Errorf("failed to find a free port for Chrome debugger: %w", err)
		}
	}

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
	args = append(args, l.cfg.Args...)

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	// Discard Chrome's stderr to keep our own log output clean
	cmd.Stderr = io.Discard
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to launch Chrome at %s: %w", binaryPath, err)
	}

	l.cmd = cmd
	l.running = true

	log.Printf("[BROWSER] Launched headless Chrome (PID %d) on port %d", cmd.Process.Pid, port)

	// Monitor process exit
	go func() {
		_ = cmd.Wait()
		l.mu.Lock()
		l.running = false
		l.wsURL = ""
		l.mu.Unlock()
		log.Printf("[BROWSER] Chrome process exited (PID %d)", cmd.Process.Pid)
	}()

	// Poll the /json/version endpoint until Chrome is ready
	debuggerURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	wsURL, err := waitForBrowser(ctx, debuggerURL, 10*time.Second)
	if err != nil {
		l.shutdownLocked()
		return "", fmt.Errorf("Chrome started but debugger not reachable: %w", err)
	}

	l.wsURL = wsURL
	log.Printf("[BROWSER] Debugger endpoint ready: %s", wsURL)
	return wsURL, nil
}

// Stop gracefully terminates the managed Chrome subprocess.
func (l *Launcher) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shutdownLocked()
}

func (l *Launcher) shutdownLocked() {
	if l.cmd != nil && l.cmd.Process != nil {
		_ = l.cmd.Process.Kill()
	}
	l.running = false
	l.wsURL = ""
}

// resolveBinary finds the Chrome binary path based on the configured mode.
func (l *Launcher) resolveBinary() (string, error) {
	switch l.cfg.Mode {
	case "path":
		if l.cfg.BinaryPath == "" {
			return "", fmt.Errorf("browser.mode is \"path\" but browser.binary_path is empty")
		}
		if _, err := os.Stat(l.cfg.BinaryPath); err != nil {
			return "", fmt.Errorf("browser binary not found at %s: %w", l.cfg.BinaryPath, err)
		}
		return l.cfg.BinaryPath, nil
	case "auto", "":
		path, found := FindChromeBinary()
		if !found {
			return "", fmt.Errorf(
				"browser.mode is \"auto\" but no Chrome/Chromium binary was found on this system.\n" +
					"Install Chrome or set browser.mode = \"path\" with browser.binary_path in surface-proxy.json",
			)
		}
		log.Printf("[BROWSER] Auto-detected Chrome binary: %s", path)
		return path, nil
	case "external":
		return "", fmt.Errorf("browser.mode is \"external\" — use target_browser_url directly, not the launcher")
	default:
		return "", fmt.Errorf("unknown browser.mode %q — valid values: auto, path, external", l.cfg.Mode)
	}
}

// waitForBrowser polls the Chrome DevTools HTTP endpoint until it responds or timeout.
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

// freePort asks the OS for an available TCP port.
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

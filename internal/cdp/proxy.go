package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
	"github.com/sentrysurface/surface-proxy/internal/telemetry"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

// BrowserURLProvider is satisfied by browser.Launcher and allows the proxy
// to obtain the live WebSocket debugger URL without importing the browser package
// (avoiding a circular dependency if browser ever needs cdp types).
type BrowserURLProvider interface {
	WSURL() string
	IsRunning() bool
}

type Proxy struct {
	cfg         *config.Config
	evaluator   firewall.Evaluator
	pruner      *pruning.Pruner
	diffEngine  *pruning.DiffEngine
	ledger      *telemetry.Ledger
	browserURLs BrowserURLProvider // nil when mode = "external"
	upgrader    websocket.Upgrader
	sessions    sync.Map // sessionID -> *Session
}

func NewProxy(cfg *config.Config, ev firewall.Evaluator, pr *pruning.Pruner, ledger *telemetry.Ledger, bup BrowserURLProvider) *Proxy {
	return &Proxy{
		cfg:        cfg,
		evaluator:  ev,
		pruner:     pr,
		diffEngine: pruning.NewDiffEngine(),
		ledger:     ledger,
		browserURLs: bup,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (p *Proxy) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()

	// Standard CDP DevTools paths (for Playwright, Puppeteer etc. connecting directly)
	mux.HandleFunc("/devtools/browser/", p.handleBrowserConnection)
	mux.HandleFunc("/devtools/page/", p.handlePageConnection)

	// SurfaceProxy v1 session endpoint — supports per-session query param overrides
	// e.g. ws://localhost:8443/v1/session?allowlist=*.gov.au
	mux.HandleFunc("/v1/session", p.handleV1Session)

	// Fallback root — mirrors the browser's own CDP root
	mux.HandleFunc("/", p.handleBrowserConnection)

	server := &http.Server{
		Addr:    p.cfg.ListenAddr,
		Handler: mux,
	}

	util.SafeGo(func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	})

	log.Printf("[PROXY] CDP Proxy listening on ws://%s", p.cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// handleV1Session handles the /v1/session endpoint which supports per-connection
// firewall and browser target overrides via URL query parameters.
func (p *Proxy) handleV1Session(w http.ResponseWriter, r *http.Request) {
	sc, err := ParseSessionConfig(r.URL.Query(), p.cfg, p.evaluator)
	if err != nil {
		http.Error(w, "invalid session parameters: "+err.Error(), http.StatusBadRequest)
		return
	}

	targetURL := sc.BrowserWSURL
	if targetURL == "" {
		targetURL = p.resolvedBrowserURL()
	}
	if targetURL == "" {
		http.Error(w, "no browser endpoint available — Chrome may still be launching", http.StatusServiceUnavailable)
		return
	}

	baseBrowserURL := targetURL
	var createdTargetID string
	if sc.NewPage {
		targetID, newTargetURL, err := createNewPage(baseBrowserURL)
		if err != nil {
			log.Printf("[PROXY] Failed to create new page target for session: %v", err)
			http.Error(w, "failed to create isolated page target: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[PROXY] Created isolated page target %s for session", targetID)
		createdTargetID = targetID
		targetURL = newTargetURL
	}

	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[PROXY] /v1/session upgrade failed: %v", err)
		if createdTargetID != "" {
			_ = closePage(baseBrowserURL, createdTargetID)
		}
		return
	}

	sessionID := util.GenerateID()
	log.Printf("[PROXY] /v1/session opened: %s → %s", sessionID, targetURL)

	if p.ledger != nil {
		p.ledger.OpenSession(sessionID, targetURL)
	}

	session, err := NewSession(sessionID, conn, targetURL, sc.FirewallOverride, p.pruner, p.diffEngine, p.ledger)
	if err != nil {
		log.Printf("[PROXY] Failed to bootstrap v1 session %s: %v", sessionID, err)
		conn.Close()
		if p.ledger != nil {
			p.ledger.CloseSession(sessionID)
		}
		if createdTargetID != "" {
			_ = closePage(baseBrowserURL, createdTargetID)
		}
		return
	}

	p.sessions.Store(sessionID, session)
	defer func() {
		p.sessions.Delete(sessionID)
		if p.ledger != nil {
			p.ledger.CloseSession(sessionID)
		}
		if createdTargetID != "" {
			log.Printf("[PROXY] Closing isolated page target %s", createdTargetID)
			_ = closePage(baseBrowserURL, createdTargetID)
		}
	}()
	session.Start(r.Context())
}

func (p *Proxy) handleBrowserConnection(w http.ResponseWriter, r *http.Request) {
	targetURL := p.resolvedBrowserURL()
	if targetURL == "" {
		http.Error(w, "browser not ready", http.StatusServiceUnavailable)
		return
	}

	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[PROXY] Failed to upgrade agent connection: %v", err)
		return
	}

	sessionID := util.GenerateID()
	log.Printf("[PROXY] Scaffolding new session: %s", sessionID)

	if p.ledger != nil {
		p.ledger.OpenSession(sessionID, targetURL)
	}

	session, err := NewSession(sessionID, conn, targetURL, p.evaluator, p.pruner, p.diffEngine, p.ledger)
	if err != nil {
		log.Printf("[PROXY] Failed to bootstrap session: %v", err)
		conn.Close()
		if p.ledger != nil {
			p.ledger.CloseSession(sessionID)
		}
		return
	}

	p.sessions.Store(sessionID, session)
	defer func() {
		p.sessions.Delete(sessionID)
		if p.ledger != nil {
			p.ledger.CloseSession(sessionID)
		}
	}()
	session.Start(r.Context())
}

func (p *Proxy) handlePageConnection(w http.ResponseWriter, r *http.Request) {
	base := p.resolvedBrowserURL()
	if base == "" {
		http.Error(w, "browser not ready", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Path
	targetURL := base
	if !strings.HasSuffix(targetURL, "/") && !strings.HasPrefix(path, "/") {
		targetURL += "/"
	}
	targetURL += path

	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[PROXY] Failed to upgrade agent connection: %v", err)
		return
	}

	sessionID := util.GenerateID()
	log.Printf("[PROXY] Scaffolding new page session: %s for path %s", sessionID, path)

	if p.ledger != nil {
		p.ledger.OpenSession(sessionID, targetURL)
	}

	session, err := NewSession(sessionID, conn, targetURL, p.evaluator, p.pruner, p.diffEngine, p.ledger)
	if err != nil {
		log.Printf("[PROXY] Failed to bootstrap page session: %v", err)
		conn.Close()
		if p.ledger != nil {
			p.ledger.CloseSession(sessionID)
		}
		return
	}

	p.sessions.Store(sessionID, session)
	defer func() {
		p.sessions.Delete(sessionID)
		if p.ledger != nil {
			p.ledger.CloseSession(sessionID)
		}
	}()
	session.Start(r.Context())
}

// resolvedBrowserURL returns the active browser WebSocket URL.
// Prefers the launcher's live URL when available; falls back to static config.
func (p *Proxy) resolvedBrowserURL() string {
	if p.browserURLs != nil && p.browserURLs.IsRunning() {
		if url := p.browserURLs.WSURL(); url != "" {
			return url
		}
	}
	return p.cfg.TargetBrowserURL
}

// ── Target Control Helpers ───────────────────────────────────────────────────

func wsToHTTP(wsURL string) string {
	if wsURL == "" {
		return ""
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		return ""
	}
	u.Scheme = "http"
	if strings.HasPrefix(wsURL, "wss://") {
		u.Scheme = "https"
	}
	u.Path = ""
	u.RawQuery = ""
	return u.String()
}

func createNewPage(browserWSURL string) (string, string, error) {
	httpAddr := wsToHTTP(browserWSURL)
	if httpAddr == "" {
		return "", "", fmt.Errorf("invalid browser WS URL: %s", browserWSURL)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(httpAddr + "/json/new")
	if err != nil {
		return "", "", fmt.Errorf("failed to create new page target: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("create new page target returned status %d", resp.StatusCode)
	}

	var target struct {
		ID                   string `json:"id"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		return "", "", fmt.Errorf("failed to decode new page target response: %w", err)
	}

	return target.ID, target.WebSocketDebuggerURL, nil
}

func closePage(browserWSURL, targetID string) error {
	httpAddr := wsToHTTP(browserWSURL)
	if httpAddr == "" || targetID == "" {
		return nil
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(httpAddr + "/json/close/" + targetID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

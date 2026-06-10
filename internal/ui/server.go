package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/telemetry"
	"github.com/sentrysurface/surface-proxy/internal/util"
	"github.com/sentrysurface/surface-proxy/web"
)

type Server struct {
	loader   *config.Loader
	firewall *firewall.RuleEngine
	ledger   *telemetry.Ledger
	upgrader websocket.Upgrader
}

func NewServer(loader *config.Loader, fw *firewall.RuleEngine, ledger *telemetry.Ledger) *Server {
	return &Server{
		loader:   loader,
		firewall: fw,
		ledger:   ledger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	subFS, err := fs.Sub(web.Assets, ".")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	// ── Telemetry ──────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/stream", s.handleStream)

	// ── Config ────────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/config", s.handleConfig)

	// ── Firewall Rules ────────────────────────────────────────────────────────
	mux.HandleFunc("/api/firewall/allowlist", s.handleAllowlist)
	mux.HandleFunc("/api/firewall/blocklist", s.handleBlocklist)
	mux.HandleFunc("/api/firewall/log", s.handleFirewallLog)

	// ── Logs ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/logs", s.handleLogs)

	// ── Integrations ──────────────────────────────────────────────────────────
	mux.HandleFunc("/api/integrations", s.handleIntegrations)

	addr := ":8080"
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	util.SafeGo(func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	})

	log.Printf("[UI] Dashboard listening on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ── WebSocket stream ──────────────────────────────────────────────────────────

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		payload := s.buildStatusPayload()
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

// ── /api/status ───────────────────────────────────────────────────────────────

type statusPayload struct {
	Status            string  `json:"status"`
	ProxyAddr         string  `json:"proxy_addr"`
	BrowserURL        string  `json:"browser_url"`
	MCPTransport      string  `json:"mcp_transport"`
	AllowlistCount    int     `json:"allowlist_count"`
	BlocklistCount    int     `json:"blocklist_count"`
	TotalSessions     int64   `json:"total_sessions"`
	ActiveSessions    int64   `json:"active_sessions"`
	TotalRawBytes     int64   `json:"total_raw_bytes"`
	TotalPrunedBytes  int64   `json:"total_pruned_bytes"`
	TotalRawTokens    int64   `json:"total_raw_tokens"`
	TotalPrunedTokens int64   `json:"total_pruned_tokens"`
	TokensReduced     int64   `json:"tokens_reduced"`
	ReductionPct      float64 `json:"reduction_pct"`
	TotalPruneOps     int64   `json:"total_prune_ops"`
	DollarsSaved      float64 `json:"dollars_saved"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.buildStatusPayload())
}

func (s *Server) buildStatusPayload() statusPayload {
	cfg := s.loader.GetConfig()
	al, bl := s.firewall.GetRules()
	p := statusPayload{
		Status:         "running",
		ProxyAddr:      cfg.ListenAddr,
		BrowserURL:     cfg.TargetBrowserURL,
		MCPTransport:   cfg.MCPTransport,
		AllowlistCount: len(al),
		BlocklistCount: len(bl),
	}
	if s.ledger != nil {
		stats := s.ledger.GlobalStats()
		p.TotalSessions = stats.TotalSessions
		p.ActiveSessions = stats.ActiveSessions
		p.TotalRawBytes = stats.TotalRawBytes
		p.TotalPrunedBytes = stats.TotalPrunedBytes
		p.TotalRawTokens = stats.TotalRawTokens
		p.TotalPrunedTokens = stats.TotalPrunedTokens
		p.TokensReduced = stats.TotalRawTokens - stats.TotalPrunedTokens
		p.ReductionPct = stats.ReductionPct
		p.TotalPruneOps = stats.TotalPruneOps
		p.DollarsSaved = telemetry.DollarsSaved(p.TokensReduced, telemetry.DefaultPricing)
	}
	return p
}

// ── /api/sessions ─────────────────────────────────────────────────────────────

type sessionPayload struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	DurationSecs float64 `json:"duration_secs"`
	RawTokens    int64   `json:"raw_tokens"`
	PrunedTokens int64   `json:"pruned_tokens"`
	ReductionPct float64 `json:"reduction_pct"`
	Active       bool    `json:"active"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.buildSessionsPayload())
}

func (s *Server) buildSessionsPayload() []sessionPayload {
	if s.ledger == nil {
		return nil
	}
	records := s.ledger.AllSessions()
	out := make([]sessionPayload, 0, len(records))
	for _, r := range records {
		out = append(out, sessionPayload{
			ID:           r.ID,
			URL:          r.URL,
			DurationSecs: r.DurationSeconds(),
			RawTokens:    r.RawTokens,
			PrunedTokens: r.PrunedTokens,
			ReductionPct: r.ReductionPct(),
			Active:       r.EndedAt == nil,
		})
	}
	return out
}

// ── /api/config ───────────────────────────────────────────────────────────────

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	cfg := s.loader.GetConfig()
	response := struct {
		*config.Config
		ConfigPath string `json:"config_path"`
		LogPath    string `json:"log_path"`
	}{
		Config:     cfg,
		ConfigPath: s.loader.Path(),
		LogPath:    filepath.Join(filepath.Dir(s.loader.Path()), "surface-proxy.log"),
	}
	_ = json.NewEncoder(w).Encode(response)
}

// ── /api/firewall/allowlist & blocklist ──────────────────────────────────────

type ruleRequest struct {
	Pattern string `json:"pattern"`
}

func (s *Server) handleAllowlist(w http.ResponseWriter, r *http.Request) {
	s.handleRuleEndpoint(w, r, true)
}

func (s *Server) handleBlocklist(w http.ResponseWriter, r *http.Request) {
	s.handleRuleEndpoint(w, r, false)
}

func (s *Server) handleRuleEndpoint(w http.ResponseWriter, r *http.Request, isAllow bool) {
	w.Header().Set("Content-Type", "application/json")
	al, bl := s.firewall.GetRules()

	switch r.Method {
	case http.MethodGet:
		if isAllow {
			_ = json.NewEncoder(w).Encode(al)
		} else {
			_ = json.NewEncoder(w).Encode(bl)
		}

	case http.MethodPost:
		var req ruleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, `{"error":"invalid pattern"}`, http.StatusBadRequest)
			return
		}
		cfg := s.loader.GetConfig()
		if isAllow {
			cfg.Firewall.Allowlist = append(cfg.Firewall.Allowlist, req.Pattern)
		} else {
			cfg.Firewall.Blocklist = append(cfg.Firewall.Blocklist, req.Pattern)
		}
		if err := s.applyAndPersist(cfg); err != nil {
			http.Error(w, `{"error":"failed to apply rules"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})

	case http.MethodDelete:
		var req ruleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, `{"error":"invalid pattern"}`, http.StatusBadRequest)
			return
		}
		cfg := s.loader.GetConfig()
		if isAllow {
			cfg.Firewall.Allowlist = removeString(cfg.Firewall.Allowlist, req.Pattern)
		} else {
			cfg.Firewall.Blocklist = removeString(cfg.Firewall.Blocklist, req.Pattern)
		}
		if err := s.applyAndPersist(cfg); err != nil {
			http.Error(w, `{"error":"failed to apply rules"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// applyAndPersist applies the updated config to the firewall engine AND writes it back to disk.
func (s *Server) applyAndPersist(cfg *config.Config) error {
	if err := s.firewall.UpdateRules(cfg.Firewall); err != nil {
		return err
	}
	return s.loader.WriteConfig(cfg)
}

func removeString(slice []string, target string) []string {
	out := slice[:0]
	for _, s := range slice {
		if s != target {
			out = append(out, s)
		}
	}
	return out
}

// ── /api/firewall/log ─────────────────────────────────────────────────────────

func (s *Server) handleFirewallLog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	events := s.firewall.RecentEvents(50)
	_ = json.NewEncoder(w).Encode(events)
}

// ── /api/logs ─────────────────────────────────────────────────────────────────

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	lines := s.readLogTail(100)
	_ = json.NewEncoder(w).Encode(lines)
}

func (s *Server) readLogTail(n int) []string {
	logPath := filepath.Join(filepath.Dir(s.loader.Path()), "surface-proxy.log")
	f, err := os.Open(logPath)
	if err != nil {
		return []string{}
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ── /api/integrations ─────────────────────────────────────────────────────────

type integrationStatus struct {
	Name       string `json:"name"`
	Registered bool   `json:"registered"`
	ConfigPath string `json:"config_path"`
}

func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	results := []integrationStatus{
		s.checkIntegration("Cursor", cursorMCPPath()),
		s.checkIntegration("VS Code", vscodeMCPPath()),
	}
	_ = json.NewEncoder(w).Encode(results)
}

func (s *Server) checkIntegration(name, path string) integrationStatus {
	if path == "" {
		return integrationStatus{Name: name, Registered: false}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return integrationStatus{Name: name, Registered: false, ConfigPath: path}
	}
	registered := strings.Contains(string(data), "surface-proxy")
	return integrationStatus{Name: name, Registered: registered, ConfigPath: path}
}

func cursorMCPPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch {
	case fileExists(filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "mcp.json")):
		return filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "mcp.json")
	case fileExists(filepath.Join(home, "Library", "Application Support", "Cursor", "User", "mcp.json")):
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "mcp.json")
	default:
		return filepath.Join(home, ".config", "Cursor", "User", "mcp.json")
	}
}

func vscodeMCPPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch {
	case fileExists(filepath.Join(home, "AppData", "Roaming", "Code", "User", "mcp.json")):
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "mcp.json")
	case fileExists(filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")):
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	default:
		return filepath.Join(home, ".config", "Code", "User", "mcp.json")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

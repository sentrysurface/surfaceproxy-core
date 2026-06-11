package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	mux.HandleFunc("/api/firewall/upload", s.handleUpload)

	// ── Logs ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/logs", s.handleLogs)

	// ── Integrations ──────────────────────────────────────────────────────────
	mux.HandleFunc("/api/integrations", s.handleIntegrations)
	mux.HandleFunc("/api/integrations/register", s.handleRegisterIntegration)

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

type ruleItem struct {
	Pattern string `json:"pattern"`
	Enabled bool   `json:"enabled"`
}

type ruleRequest struct {
	Pattern string `json:"pattern"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type updateRuleRequest struct {
	OldPattern string `json:"old_pattern"`
	OldEnabled bool   `json:"old_enabled"`
	NewPattern string `json:"new_pattern"`
	NewEnabled bool   `json:"new_enabled"`
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
		items := make([]ruleItem, 0, len(al))
		rules := al
		if !isAllow {
			rules = bl
		}
		for _, raw := range rules {
			enabled := true
			pattern := raw
			if strings.HasPrefix(raw, "_disabled_:") {
				enabled = false
				pattern = strings.TrimPrefix(raw, "_disabled_:")
			}
			items = append(items, ruleItem{Pattern: pattern, Enabled: enabled})
		}
		_ = json.NewEncoder(w).Encode(items)

	case http.MethodPost:
		var req ruleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, `{"error":"invalid pattern"}`, http.StatusBadRequest)
			return
		}
		rawPattern := req.Pattern
		if req.Enabled != nil && !*req.Enabled {
			rawPattern = "_disabled_:" + req.Pattern
		}
		cfg := s.loader.GetConfig()
		if isAllow {
			cfg.Firewall.Allowlist = append(cfg.Firewall.Allowlist, rawPattern)
		} else {
			cfg.Firewall.Blocklist = append(cfg.Firewall.Blocklist, rawPattern)
		}
		if err := s.applyAndPersist(cfg); err != nil {
			http.Error(w, `{"error":"failed to apply rules"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})

	case http.MethodPut:
		var req updateRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OldPattern == "" || req.NewPattern == "" {
			http.Error(w, `{"error":"invalid update request"}`, http.StatusBadRequest)
			return
		}
		if _, err := regexp.Compile(req.NewPattern); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid regular expression: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		cfg := s.loader.GetConfig()
		var found bool
		if isAllow {
			cfg.Firewall.Allowlist, found = replaceRule(cfg.Firewall.Allowlist, req.OldPattern, req.OldEnabled, req.NewPattern, req.NewEnabled)
		} else {
			cfg.Firewall.Blocklist, found = replaceRule(cfg.Firewall.Blocklist, req.OldPattern, req.OldEnabled, req.NewPattern, req.NewEnabled)
		}
		if !found {
			http.Error(w, `{"error":"rule not found"}`, http.StatusNotFound)
			return
		}
		if err := s.applyAndPersist(cfg); err != nil {
			http.Error(w, `{"error":"failed to apply rules"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

	case http.MethodDelete:
		var req ruleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, `{"error":"invalid pattern"}`, http.StatusBadRequest)
			return
		}
		cfg := s.loader.GetConfig()
		if isAllow {
			cfg.Firewall.Allowlist = removeRule(cfg.Firewall.Allowlist, req.Pattern)
		} else {
			cfg.Firewall.Blocklist = removeRule(cfg.Firewall.Blocklist, req.Pattern)
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

func removeRule(slice []string, pattern string) []string {
	out := slice[:0]
	disabledPattern := "_disabled_:" + pattern
	for _, s := range slice {
		if s != pattern && s != disabledPattern {
			out = append(out, s)
		}
	}
	return out
}

func replaceRule(slice []string, oldPattern string, oldEnabled bool, newPattern string, newEnabled bool) ([]string, bool) {
	oldRaw := oldPattern
	if !oldEnabled {
		oldRaw = "_disabled_:" + oldPattern
	}
	newRaw := newPattern
	if !newEnabled {
		newRaw = "_disabled_:" + newPattern
	}
	found := false
	out := make([]string, 0, len(slice))
	for _, s := range slice {
		if s == oldRaw && !found {
			out = append(out, newRaw)
			found = true
		} else {
			out = append(out, s)
		}
	}
	return out, found
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		http.Error(w, `{"error":"failed to parse form"}`, http.StatusBadRequest)
		return
	}
	ruleType := r.FormValue("type")
	if ruleType != "allowlist" && ruleType != "blocklist" {
		http.Error(w, `{"error":"invalid type parameter"}`, http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"missing file"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	var newRules []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if _, errCompile := regexp.Compile(line); errCompile != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid regex pattern %q: %s"}`, line, errCompile.Error()), http.StatusBadRequest)
			return
		}
		newRules = append(newRules, line)
	}
	if errScan := scanner.Err(); errScan != nil {
		http.Error(w, `{"error":"failed to read file"}`, http.StatusInternalServerError)
		return
	}
	if len(newRules) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"count": 0, "status": "no rules found in file"})
		return
	}
	cfg := s.loader.GetConfig()
	if ruleType == "allowlist" {
		cfg.Firewall.Allowlist = append(cfg.Firewall.Allowlist, newRules...)
	} else {
		cfg.Firewall.Blocklist = append(cfg.Firewall.Blocklist, newRules...)
	}
	if errApply := s.applyAndPersist(cfg); errApply != nil {
		http.Error(w, `{"error":"failed to apply and save rules"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"count":  len(newRules),
		"status": fmt.Sprintf("successfully imported %d rules", len(newRules)),
	})
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
		s.checkIntegration("Claude Desktop", claudeDesktopMCPPath()),
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
	appData := os.Getenv("APPDATA")
	if appData != "" {
		path := filepath.Join(appData, "Cursor", "User", "mcp.json")
		if fileExists(path) {
			return path
		}
	}
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
	appData := os.Getenv("APPDATA")
	if appData != "" {
		// Check Roo Code settings path first
		rooPath := filepath.Join(appData, "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "roo_mcp_settings.json")
		if fileExists(rooPath) {
			return rooPath
		}
		// Check Cline settings path next
		clinePath := filepath.Join(appData, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
		if fileExists(clinePath) {
			return clinePath
		}
		path := filepath.Join(appData, "Code", "User", "mcp.json")
		if fileExists(path) {
			return path
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch {
	case fileExists(filepath.Join(home, "AppData", "Roaming", "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "roo_mcp_settings.json")):
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "roo_mcp_settings.json")
	case fileExists(filepath.Join(home, "AppData", "Roaming", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")):
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	case fileExists(filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "roo_mcp_settings.json")):
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "roo_mcp_settings.json")
	case fileExists(filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")):
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	case fileExists(filepath.Join(home, "AppData", "Roaming", "Code", "User", "mcp.json")):
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "mcp.json")
	case fileExists(filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")):
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	default:
		rooLinux := filepath.Join(home, ".config", "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "roo_mcp_settings.json")
		if fileExists(rooLinux) {
			return rooLinux
		}
		clineLinux := filepath.Join(home, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
		if fileExists(clineLinux) {
			return clineLinux
		}
		return filepath.Join(home, ".config", "Code", "User", "mcp.json")
	}
}

func claudeDesktopMCPPath() string {
	// 1. Check Windows MSIX package path first (Local AppData packages)
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		msixPath := filepath.Join(localAppData, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude", "claude_desktop_config.json")
		if fileExists(msixPath) || fileExists(filepath.Dir(msixPath)) {
			return msixPath
		}
	}

	appData := os.Getenv("APPDATA")
	if appData != "" {
		path := filepath.Join(appData, "Claude", "claude_desktop_config.json")
		if fileExists(path) || fileExists(filepath.Dir(path)) {
			return path
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// 2. Check alternative Windows Store MSIX path with user home
	msixHomePath := filepath.Join(home, "AppData", "Local", "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude", "claude_desktop_config.json")
	if fileExists(msixHomePath) || fileExists(filepath.Dir(msixHomePath)) {
		return msixHomePath
	}

	switch {
	case fileExists(filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")):
		return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	case fileExists(filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")):
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	default:
		// Fallback to standard User Profile path if Roaming folder exists
		roamingPath := filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
		if fileExists(filepath.Join(home, "AppData", "Roaming")) {
			return roamingPath
		}
		// Mac / Linux defaults
		if fileExists(filepath.Join(home, "Library", "Application Support")) {
			return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
		}
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	}
}


func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type registerRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRegisterIntegration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"error":"invalid integration name"}`, http.StatusBadRequest)
		return
	}

	var path string
	switch req.Name {
	case "Cursor":
		path = cursorMCPPath()
	case "VS Code":
		path = vscodeMCPPath()
	case "Claude Desktop":
		path = claudeDesktopMCPPath()
	default:
		http.Error(w, `{"error":"unknown integration"}`, http.StatusBadRequest)
		return
	}

	if path == "" {
		http.Error(w, `{"error":"integration config path not resolved"}`, http.StatusInternalServerError)
		return
	}

	if err := s.registerMCP(path); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to register: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

func (s *Server) registerMCP(path string) error {
	exePath, err := os.Executable()
	if err != nil {
		exePath = "surface-proxy"
	}

	// If the running executable is the tray daemon, rewrite the path to the CLI binary
	exePath = strings.Replace(exePath, "surface-proxy-tray", "surface-proxy", 1)

	// Read existing config if it exists
	var data map[string]interface{}
	content, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(content, &data)
	}

	if data == nil {
		data = make(map[string]interface{})
	}

	mcpServersRaw, ok := data["mcpServers"]
	var mcpServers map[string]interface{}
	if ok {
		mcpServers, _ = mcpServersRaw.(map[string]interface{})
	}
	if mcpServers == nil {
		mcpServers = make(map[string]interface{})
		data["mcpServers"] = mcpServers
	}

	mcpServers["surface-proxy"] = map[string]interface{}{
		"command": exePath,
		"args":    []string{"mcp-mode", "--config", s.loader.Path()},
	}

	// Create directory if not exists
	if errDir := os.MkdirAll(filepath.Dir(path), 0755); errDir != nil {
		return errDir
	}

	newContent, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, newContent, 0644)
}

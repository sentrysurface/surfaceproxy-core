package ui

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/telemetry"
	"github.com/sentrysurface/surface-proxy/internal/util"
	"github.com/sentrysurface/surface-proxy/web"
)

type Server struct {
	cfg    *config.Config
	ledger *telemetry.Ledger
	upgrader websocket.Upgrader
}

func NewServer(cfg *config.Config, ledger *telemetry.Ledger) *Server {
	return &Server{
		cfg:    cfg,
		ledger: ledger,
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

	// REST: current aggregate stats snapshot
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := s.buildStatusPayload()
		_ = json.NewEncoder(w).Encode(stats)
	})

	// REST: active session list
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sessions := s.buildSessionsPayload()
		_ = json.NewEncoder(w).Encode(sessions)
	})

	// WebSocket: push live telemetry updates every second
	mux.HandleFunc("/api/stream", s.handleStream)

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

// handleStream upgrades to WebSocket and pushes telemetry every second.
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

// statusPayload is the JSON shape pushed to the dashboard.
type statusPayload struct {
	Status           string  `json:"status"`
	ProxyAddr        string  `json:"proxy_addr"`
	BrowserURL       string  `json:"browser_url"`
	MCPTransport     string  `json:"mcp_transport"`
	AllowlistCount   int     `json:"allowlist_count"`
	BlocklistCount   int     `json:"blocklist_count"`
	TotalSessions    int64   `json:"total_sessions"`
	ActiveSessions   int64   `json:"active_sessions"`
	TotalRawBytes    int64   `json:"total_raw_bytes"`
	TotalPrunedBytes int64   `json:"total_pruned_bytes"`
	TotalRawTokens   int64   `json:"total_raw_tokens"`
	TotalPrunedTokens int64  `json:"total_pruned_tokens"`
	TokensReduced    int64   `json:"tokens_reduced"`
	ReductionPct     float64 `json:"reduction_pct"`
	TotalPruneOps    int64   `json:"total_prune_ops"`
	DollarsSaved     float64 `json:"dollars_saved"`
}

func (s *Server) buildStatusPayload() statusPayload {
	p := statusPayload{
		Status:         "running",
		ProxyAddr:      s.cfg.ListenAddr,
		BrowserURL:     s.cfg.TargetBrowserURL,
		MCPTransport:   s.cfg.MCPTransport,
		AllowlistCount: len(s.cfg.Firewall.Allowlist),
		BlocklistCount: len(s.cfg.Firewall.Blocklist),
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

type sessionPayload struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	DurationSecs float64 `json:"duration_secs"`
	RawTokens    int64   `json:"raw_tokens"`
	PrunedTokens int64   `json:"pruned_tokens"`
	ReductionPct float64 `json:"reduction_pct"`
	Active       bool    `json:"active"`
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

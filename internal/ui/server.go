package ui

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/util"
	"github.com/sentrysurface/surface-proxy/web"
)

type Server struct {
	cfg *config.Config
}

func NewServer(cfg *config.Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Start(ctx context.Context) error {
	subFS, err := fs.Sub(web.Assets, ".")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	// API Status Endpoint for telemetry
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := map[string]interface{}{
			"status":            "running",
			"proxy_addr":        s.cfg.ListenAddr,
			"browser_url":       s.cfg.TargetBrowserURL,
			"mcp_transport":     s.cfg.MCPTransport,
			"allowlist_pattern": len(s.cfg.Firewall.Allowlist),
			"blocklist_pattern": len(s.cfg.Firewall.Blocklist),
		}
		json.NewEncoder(w).Encode(status)
	})

	// Run dashboard on default port :8080
	addr := ":8080"
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	util.SafeGo(func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	})

	log.Printf("[UI] Dashboard server listening on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

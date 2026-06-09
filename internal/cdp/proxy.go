package cdp

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

type Proxy struct {
	cfg        *config.Config
	evaluator  firewall.Evaluator
	pruner     *pruning.Pruner
	diffEngine *pruning.DiffEngine
	upgrader   websocket.Upgrader
	sessions   sync.Map // sessionID -> *Session
}

func NewProxy(cfg *config.Config, ev firewall.Evaluator, pr *pruning.Pruner) *Proxy {
	return &Proxy{
		cfg:        cfg,
		evaluator:  ev,
		pruner:     pr,
		diffEngine: pruning.NewDiffEngine(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (p *Proxy) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/browser/", p.handleBrowserConnection)
	mux.HandleFunc("/devtools/page/", p.handlePageConnection)
	mux.HandleFunc("/", p.handleBrowserConnection)

	server := &http.Server{
		Addr:    p.cfg.ListenAddr,
		Handler: mux,
	}

	util.SafeGo(func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	})

	log.Printf("[PROXY] CDP Proxy listening on ws://%s", p.cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (p *Proxy) handleBrowserConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[PROXY] Failed to upgrade agent connection: %v", err)
		return
	}

	sessionID := util.GenerateID()
	log.Printf("[PROXY] Scaffolding new session: %s", sessionID)

	session, err := NewSession(sessionID, conn, p.cfg.TargetBrowserURL, p.evaluator, p.pruner, p.diffEngine)
	if err != nil {
		log.Printf("[PROXY] Failed to bootstrap session: %v", err)
		conn.Close()
		return
	}

	p.sessions.Store(sessionID, session)
	defer p.sessions.Delete(sessionID)

	session.Start(r.Context())
}

func (p *Proxy) handlePageConnection(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	targetURL := p.cfg.TargetBrowserURL
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

	session, err := NewSession(sessionID, conn, targetURL, p.evaluator, p.pruner, p.diffEngine)
	if err != nil {
		log.Printf("[PROXY] Failed to bootstrap page session: %v", err)
		conn.Close()
		return
	}

	p.sessions.Store(sessionID, session)
	defer p.sessions.Delete(sessionID)

	session.Start(r.Context())
}

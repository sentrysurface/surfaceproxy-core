package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/util"
	"github.com/sentrysurface/surface-proxy/internal/version"
)

// Server implements the MCP 2024-11-05 server protocol over stdio or WebSocket.
type Server struct {
	cfg          *config.Config
	handlers     *Handlers
	initialized  bool
	upgrader     websocket.Upgrader
}

func NewServer(cfg *config.Config, handlers *Handlers) *Server {
	return &Server{
		cfg:      cfg,
		handlers: handlers,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Handlers returns the underlying Handlers instance, allowing callers
// to update the browser URL after the launcher starts Chrome.
func (s *Server) Handlers() *Handlers {
	return s.handlers
}

func (s *Server) Start(ctx context.Context) error {
	if s.cfg.MCPTransport == "websocket" {
		return s.startWebSocket(ctx)
	}
	return s.startStdio(ctx)
}

// ── stdio transport ──────────────────────────────────────────────────────────

func (s *Server) startStdio(ctx context.Context) error {
	log.Println("[MCP] Starting stdio JSON-RPC server (MCP 2024-11-05)")
	// Redirect log to stderr — stdout is exclusively for JSON-RPC responses
	log.SetOutput(os.Stderr)

	reader := bufio.NewReader(os.Stdin)
	errChan := make(chan error, 1)

	util.SafeGo(func() {
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err != io.EOF {
					errChan <- err
				}
				return
			}
			if len(line) <= 1 {
				continue
			}
			s.handleMessage(line, func(data []byte) {
				os.Stdout.Write(data)
				os.Stdout.Write([]byte("\n"))
			})
		}
	})

	select {
	case <-ctx.Done():
		return nil
	case err := <-errChan:
		return err
	}
}

// ── WebSocket transport ──────────────────────────────────────────────────────

func (s *Server) startWebSocket(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWSConnection)

	server := &http.Server{
		Addr:    s.cfg.MCPListenAddr,
		Handler: mux,
	}

	util.SafeGo(func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	})

	log.Printf("[MCP] MCP WebSocket server listening on ws://%s", s.cfg.MCPListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleWSConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[MCP] Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	// Each WebSocket connection gets its own server state (initialized flag, etc.)
	sessionServer := &Server{
		cfg:      s.cfg,
		handlers: s.handlers,
		upgrader: s.upgrader,
	}

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		sessionServer.handleMessage(message, func(data []byte) {
			conn.WriteMessage(websocket.TextMessage, data)
		})
	}
}

// ── MCP message dispatch ─────────────────────────────────────────────────────

func (s *Server) handleMessage(rawData []byte, write func([]byte)) {
	var req Request
	if err := json.Unmarshal(rawData, &req); err != nil {
		s.writeResponse(write, NewErrorResponse(nil, ErrParse, "Parse error: "+err.Error()))
		return
	}

	if req.JSONRPC != "2.0" || req.Method == "" {
		s.writeResponse(write, NewErrorResponse(req.ID, ErrInvalidRequest, "Invalid Request"))
		return
	}

	// Notifications (no ID) — handle and do not respond
	if req.ID == nil {
		s.handleNotification(req.Method, req.Params)
		return
	}

	// Requests — must respond
	resp := s.dispatch(req)
	if resp != nil {
		s.writeResponse(write, resp)
	}
}

func (s *Server) handleNotification(method string, _ json.RawMessage) {
	switch method {
	case "notifications/initialized":
		log.Println("[MCP] Client sent initialized notification — ready for tool calls")
	default:
		log.Printf("[MCP] Unhandled notification: %s", method)
	}
}

func (s *Server) dispatch(req Request) *Response {
	switch req.Method {

	// ── Lifecycle ─────────────────────────────────────────────────────────
	case "initialize":
		return s.handleInitialize(req)

	case "ping":
		resp, _ := NewResultResponse(req.ID, map[string]interface{}{})
		return resp

	// ── Tool discovery ────────────────────────────────────────────────────
	case "tools/list":
		return s.handleToolsList(req)

	// ── Tool execution ────────────────────────────────────────────────────
	case "tools/call":
		return s.handleToolsCall(req)

	default:
		return NewErrorResponse(req.ID, ErrMethodNotFound, "Method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(req Request) *Response {
	var params InitializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrInvalidParams, "invalid initialize params")
		}
	}

	log.Printf("[MCP] initialize — client: %s %s, protocol: %s",
		params.ClientInfo.Name, params.ClientInfo.Version, params.ProtocolVersion)

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCaps{
			Tools: &ToolsCap{ListChanged: false},
		},
		ServerInfo: AppInfo{
			Name:    "SurfaceProxy",
			Version: version.Version,
		},
		Instructions: "SurfaceProxy is a local AI web-browsing proxy. Use 'browse' to navigate to a URL and receive a token-optimised Markdown snapshot. Use 'click' and 'type' to interact with page elements. Use 'getDOM' to retrieve the current page state with structural diffing.",
	}

	s.initialized = true

	resp, err := NewResultResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	return resp
}

func (s *Server) handleToolsList(req Request) *Response {
	result := ToolsListResult{Tools: ToolManifest()}
	resp, err := NewResultResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	return resp
}

func (s *Server) handleToolsCall(req Request) *Response {
	if !s.initialized {
		return NewErrorResponse(req.ID, ErrInvalidRequest, "Server not initialized — send initialize first")
	}

	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, "invalid tools/call params: "+err.Error())
	}

	log.Printf("[MCP] tools/call — tool: %s", params.Name)

	var callResult ToolCallResult

	switch params.Name {
	case "browse":
		callResult = s.handlers.HandleBrowse(params.Arguments)
	case "getDOM":
		callResult = s.handlers.HandleGetDOM(params.Arguments)
	case "click":
		callResult = s.handlers.HandleClick(params.Arguments)
	case "type":
		callResult = s.handlers.HandleType(params.Arguments)
	case "screenshot":
		callResult = s.handlers.HandleScreenshot(params.Arguments)
	default:
		callResult = ErrorContent("unknown tool: " + params.Name)
	}

	resp, err := NewResultResponse(req.ID, callResult)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}
	return resp
}

func (s *Server) writeResponse(write func([]byte), resp *Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[MCP] Failed to marshal response: %v", err)
		return
	}
	write(data)
}

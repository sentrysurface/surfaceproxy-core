package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

type Server struct {
	cfg      *config.Config
	handlers *Handlers
	upgrader websocket.Upgrader
	mu       sync.Mutex
	closed   bool
}

func NewServer(cfg *config.Config, handlers *Handlers) *Server {
	return &Server{
		cfg:      cfg,
		handlers: handlers,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	if s.cfg.MCPTransport == "websocket" {
		return s.startWebSocket(ctx)
	}
	return s.startStdio(ctx)
}

func (s *Server) startStdio(ctx context.Context) error {
	log.Println("[MCP] Starting stdio JSON-RPC server")
	reader := bufio.NewReader(os.Stdin)

	// Since stdout is used for JSON-RPC messages, redirect default log output to stderr
	log.SetOutput(os.Stderr)

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

			resp := s.dispatch(line)
			if resp != nil {
				respData, err := json.Marshal(resp)
				if err == nil {
					os.Stdout.Write(respData)
					os.Stdout.Write([]byte("\n"))
				}
			}
		}
	})

	select {
	case <-ctx.Done():
		return nil
	case err := <-errChan:
		return err
	}
}

func (s *Server) startWebSocket(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleConnection)

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

func (s *Server) handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[MCP] Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		resp := s.dispatch(message)
		if resp != nil {
			respData, err := json.Marshal(resp)
			if err == nil {
				if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
					break
				}
			}
		}
	}
}

func (s *Server) dispatch(rawData []byte) *Response {
	var req Request
	if err := json.Unmarshal(rawData, &req); err != nil {
		return NewErrorResponse(nil, ErrParse, "Parse error: "+err.Error())
	}

	if req.JSONRPC != "2.0" || req.Method == "" {
		return NewErrorResponse(req.ID, ErrInvalidRequest, "Invalid Request")
	}

	var result interface{}
	var err error

	switch req.Method {
	case "browse":
		result, err = s.handlers.HandleBrowse(req.Params)
	case "click":
		result, err = s.handlers.HandleClick(req.Params)
	case "screenshot":
		result, err = s.handlers.HandleScreenshot(req.Params)
	case "getDOM":
		result, err = s.handlers.HandleGetDOM(req.Params)
	default:
		return NewErrorResponse(req.ID, ErrMethodNotFound, "Method not found: "+req.Method)
	}

	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, err.Error())
	}

	resp, err := NewResultResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, "Failed to serialize response: "+err.Error())
	}

	return resp
}

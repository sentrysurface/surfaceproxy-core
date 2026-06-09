package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
	"github.com/sentrysurface/surface-proxy/internal/util"
)

type Session struct {
	ID          string
	agentConn   *websocket.Conn
	browserConn *websocket.Conn
	evaluator   firewall.Evaluator
	pruner      *pruning.Pruner
	diffEngine  *pruning.DiffEngine
	mu          sync.Mutex
	closed      bool
}

func NewSession(id string, agent *websocket.Conn, browserURL string, ev firewall.Evaluator, pr *pruning.Pruner, de *pruning.DiffEngine) (*Session, error) {
	// Evaluate browser URL initially if it is specified
	if browserURL != "" {
		allowed, reason, err := ev.EvaluateURL(browserURL)
		if err != nil || !allowed {
			return nil, errors.New("initial target browser URL blocked by firewall: " + reason)
		}
	}

	browser, _, err := websocket.DefaultDialer.Dial(browserURL, nil)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:          id,
		agentConn:   agent,
		browserConn: browser,
		evaluator:   ev,
		pruner:      pr,
		diffEngine:  de,
	}, nil
}

func (s *Session) Start(ctx context.Context) {
	errChan := make(chan error, 2)

	// Pump from Agent to Browser
	util.SafeGo(func() {
		defer s.Close()
		for {
			_, data, err := s.agentConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			// Intercept and parse frame
			msg, err := ParseFrame(data)
			if err == nil && msg != nil {
				// Intercept Page.navigate method to enforce firewall
				if msg.Method == "Page.navigate" {
					var params struct {
						URL string `json:"url"`
					}
					if json.Unmarshal(msg.Params, &params) == nil && params.URL != "" {
						allowed, reason, err := s.evaluator.EvaluateURL(params.URL)
						if err != nil || !allowed {
							log.Printf("[FIREWALL] Blocked Page.navigate to %s. Reason: %s", params.URL, reason)
							// Return a mocked CDP error response to the agent
							errResp := CDPMessage{
								ID: msg.ID,
								Error: &CDPError{
									Code:    -32000,
									Message: "Blocked by SurfaceProxy Firewall: " + reason,
								},
							}
							respData, _ := json.Marshal(errResp)
							s.agentConn.WriteMessage(websocket.TextMessage, respData)
							continue
						}
					}
				}
			}

			// Forward to browser
			if err := s.browserConn.WriteMessage(websocket.TextMessage, data); err != nil {
				errChan <- err
				return
			}
		}
	})

	// Pump from Browser to Agent
	util.SafeGo(func() {
		defer s.Close()
		for {
			msgType, data, err := s.browserConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			// Intercept response frames (e.g. DOM retrieval response if we want to prune it dynamically)
			msg, err := ParseFrame(data)
			if err == nil && msg != nil {
				// If we intercept a response containing outerHTML, we could prune it here.
				// However, standard CDP proxying mostly transparently passes frames, and
				// the MCP layer/Automation clients query the DOM explicitly via DOM.getOuterHTML.
				// Let's do a basic interception of DOM.getOuterHTML result if returned.
				if msg.ID != 0 && len(msg.Result) > 0 {
					var resultObj map[string]interface{}
					if json.Unmarshal(msg.Result, &resultObj) == nil {
						if htmlVal, ok := resultObj["outerHTML"].(string); ok {
							prunedHTML, err := s.pruner.Prune([]byte(htmlVal))
							if err == nil {
								// Compute structural diff
								diffData, changed := s.diffEngine.ComputeDiff(s.ID, prunedHTML)
								if changed {
									resultObj["outerHTML"] = string(diffData)
								} else {
									resultObj["outerHTML"] = string(prunedHTML)
								}
								newResult, _ := json.Marshal(resultObj)
								msg.Result = newResult
								data, _ = json.Marshal(msg)
							}
						}
					}
				}
			}

			if err := s.agentConn.WriteMessage(msgType, data); err != nil {
				errChan <- err
				return
			}
		}
	})

	select {
	case <-ctx.Done():
		s.Close()
	case <-errChan:
		s.Close()
	}
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.agentConn != nil {
		s.agentConn.Close()
	}
	if s.browserConn != nil {
		s.browserConn.Close()
	}
	s.diffEngine.Clear(s.ID)
	log.Printf("[SESSION] Closed session %s", s.ID)
}

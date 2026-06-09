package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
	"github.com/sentrysurface/surface-proxy/internal/pruning"
)

type Handlers struct {
	cfg        *config.Config
	evaluator  firewall.Evaluator
	pruner     *pruning.Pruner
	diffEngine *pruning.DiffEngine
	mu         sync.Mutex
	wsConn     *websocket.Conn
	nextID     int64
	pending    map[int64]chan []byte
}

func NewHandlers(cfg *config.Config, ev firewall.Evaluator, pr *pruning.Pruner) *Handlers {
	return &Handlers{
		cfg:        cfg,
		evaluator:  ev,
		pruner:     pr,
		diffEngine: pruning.NewDiffEngine(),
		pending:    make(map[int64]chan []byte),
	}
}

func (h *Handlers) connectBrowser() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.wsConn != nil {
		return nil
	}

	conn, _, err := websocket.DefaultDialer.Dial(h.cfg.TargetBrowserURL, nil)
	if err != nil {
		return fmt.Errorf("failed to dial target browser at %s: %w", h.cfg.TargetBrowserURL, err)
	}
	h.wsConn = conn

	go func() {
		defer func() {
			h.mu.Lock()
			if h.wsConn != nil {
				h.wsConn.Close()
				h.wsConn = nil
			}
			h.mu.Unlock()
		}()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(data, &msg); err == nil && msg.ID != 0 {
				h.mu.Lock()
				ch, ok := h.pending[msg.ID]
				h.mu.Unlock()
				if ok {
					select {
					case ch <- data:
					default:
					}
				}
			}
		}
	}()

	return nil
}

func (h *Handlers) sendCDPCommand(method string, params interface{}) (json.RawMessage, error) {
	if err := h.connectBrowser(); err != nil {
		return nil, err
	}

	h.mu.Lock()
	h.nextID++
	id := h.nextID
	ch := make(chan []byte, 1)
	h.pending[id] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	var cdpReq struct {
		ID     int64       `json:"id"`
		Method string      `json:"method"`
		Params interface{} `json:"params,omitempty"`
	}
	cdpReq.ID = id
	cdpReq.Method = method
	cdpReq.Params = params

	data, err := json.Marshal(cdpReq)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	conn := h.wsConn
	h.mu.Unlock()

	if conn == nil {
		return nil, errors.New("browser connection lost")
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		var cdpResp struct {
			Result json.RawMessage `json:"result,omitempty"`
			Error  *struct {
				Code    int64  `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(resp, &cdpResp); err != nil {
			return nil, err
		}
		if cdpResp.Error != nil {
			return nil, fmt.Errorf("CDP error: %s (code %d)", cdpResp.Error.Message, cdpResp.Error.Code)
		}
		return cdpResp.Result, nil
	case <-time.After(10 * time.Second):
		return nil, errors.New("timeout waiting for browser response")
	}
}

func (h *Handlers) HandleBrowse(params json.RawMessage) (interface{}, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if p.URL == "" {
		return nil, errors.New("url parameter is required")
	}

	allowed, reason, err := h.evaluator.EvaluateURL(p.URL)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return map[string]interface{}{
			"status": "blocked",
			"reason": reason,
		}, nil
	}

	_, err = h.sendCDPCommand("Page.navigate", map[string]string{"url": p.URL})
	if err != nil {
		return nil, err
	}

	time.Sleep(1500 * time.Millisecond)

	dom, err := h.getCurrentPrunedDOM(p.URL)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "success",
		"url":    p.URL,
		"dom":    dom,
	}, nil
}

func (h *Handlers) HandleClick(params json.RawMessage) (interface{}, error) {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if p.Selector == "" {
		return nil, errors.New("selector parameter is required")
	}

	js := fmt.Sprintf(`document.querySelector("%s").click()`, p.Selector)
	_, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression": js,
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "success",
	}, nil
}

func (h *Handlers) HandleScreenshot(params json.RawMessage) (interface{}, error) {
	result, err := h.sendCDPCommand("Page.captureScreenshot", map[string]interface{}{
		"format": "png",
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":     "success",
		"screenshot": res.Data,
	}, nil
}

func (h *Handlers) HandleGetDOM(params json.RawMessage) (interface{}, error) {
	res, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression": "window.location.href",
	})
	if err != nil {
		return nil, err
	}

	var valRes struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	url := "current_page"
	if json.Unmarshal(res, &valRes) == nil && valRes.Result.Value != "" {
		url = valRes.Result.Value
	}

	dom, err := h.getCurrentPrunedDOM(url)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "success",
		"url":    url,
		"dom":    dom,
	}, nil
}

func (h *Handlers) getCurrentPrunedDOM(pageKey string) (string, error) {
	res, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression": "document.documentElement.outerHTML",
	})
	if err != nil {
		return "", err
	}

	var valRes struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &valRes); err != nil {
		return "", err
	}

	pruned, err := h.pruner.Prune([]byte(valRes.Result.Value))
	if err != nil {
		return "", err
	}

	diff, changed := h.diffEngine.ComputeDiff(pageKey, pruned)
	if changed {
		return string(diff), nil
	}
	return string(pruned), nil
}

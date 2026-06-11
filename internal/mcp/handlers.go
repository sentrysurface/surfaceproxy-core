package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
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

// Handlers implements all MCP tool call handlers and manages the connection
// to the headless browser via CDP WebSocket.
type Handlers struct {
	cfg             *config.Config
	evaluator       firewall.Evaluator
	pruner          *pruning.Pruner
	diffEngine      *pruning.DiffEngine
	ledger          *telemetry.Ledger
	mu              sync.Mutex
	wsConn          *websocket.Conn
	nextID          int64
	pending         map[int64]chan []byte
	sessionID       string
	createdTargetID string // Keeps track of any dynamically created isolated Chrome tab
}

func NewHandlers(cfg *config.Config, ev firewall.Evaluator, pr *pruning.Pruner, ledger *telemetry.Ledger) *Handlers {
	sessionID := util.GenerateID()
	return &Handlers{
		cfg:        cfg,
		evaluator:  ev,
		pruner:     pr,
		diffEngine: pruning.NewDiffEngine(),
		ledger:     ledger,
		pending:    make(map[int64]chan []byte),
		sessionID:  sessionID,
	}
}

// UpdateBrowserURL updates the target browser URL (called when the launcher starts a new browser).
func (h *Handlers) UpdateBrowserURL(wsURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsConn != nil {
		h.wsConn.Close()
		h.wsConn = nil
	}
	h.cfg.TargetBrowserURL = wsURL
}

// OpenSession registers a new MCP session in the telemetry ledger.
func (h *Handlers) OpenSession(url string) {
	if h.ledger != nil {
		h.ledger.OpenSession(h.sessionID, url)
	}
}

// CloseSession closes the telemetry session and prints the ROI summary.
func (h *Handlers) CloseSession() {
	h.mu.Lock()
	if h.wsConn != nil {
		h.wsConn.Close()
		h.wsConn = nil
	}
	targetID := h.createdTargetID
	browserURL := h.cfg.TargetBrowserURL
	h.createdTargetID = ""
	h.mu.Unlock()

	if targetID != "" && browserURL != "" {
		log.Printf("[MCP] Closing isolated page target %s", targetID)
		_ = closePage(browserURL, targetID)
	}

	if h.ledger == nil {
		return
	}
	record, ok := h.ledger.CloseSession(h.sessionID)
	if ok && record.PruneCount > 0 {
		telemetry.PrintSessionSummary(record, telemetry.DefaultPricing, os.Stderr)
	}
}

func (h *Handlers) connectBrowser() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.wsConn != nil {
		return nil
	}

	targetURL := h.cfg.TargetBrowserURL

	// If connecting directly to a browser-level debugging endpoint, dynamically spin up a new tab
	if strings.Contains(targetURL, "/devtools/browser/") {
		targetID, newTargetURL, err := createNewPage(targetURL)
		if err != nil {
			return fmt.Errorf("failed to create isolated page target: %w", err)
		}
		log.Printf("[MCP] Created isolated page target %s", targetID)
		h.createdTargetID = targetID
		targetURL = newTargetURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		if h.createdTargetID != "" {
			_ = closePage(h.cfg.TargetBrowserURL, h.createdTargetID)
			h.createdTargetID = ""
		}
		return fmt.Errorf("failed to dial target browser at %s: %w", targetURL, err)
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
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.ID != 0 {
				h.mu.Lock()
				ch, ok := h.pending[msg.ID]
				h.mu.Unlock()
				if ok {
					select {
					case ch <- data:
					default:
					}
				}
			} else if msg.Method != "" {
				dispatchEvent(msg.Method)
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
	case <-time.After(15 * time.Second):
		return nil, errors.New("timeout waiting for browser response")
	}
}

// waitForPageLoad enables Page events and waits for Page.loadEventFired
// before proceeding, replacing the brittle time.Sleep approach.
func (h *Handlers) waitForPageLoad(timeoutSecs int) error {
	// Enable Page domain events
	if _, err := h.sendCDPCommand("Page.enable", nil); err != nil {
		return fmt.Errorf("Page.enable failed: %w", err)
	}

	// Register a one-shot pending channel keyed on a sentinel ID that will
	// intercept the next Page.loadEventFired event from the reader goroutine.
	// The reader dispatches by msg.ID — for events, ID is 0 and method is set.
	// We use a dedicated event waiter instead.
	done := make(chan struct{}, 1)
	timeout := time.Duration(timeoutSecs) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Register event listener before navigating
	eventCh := h.registerEventListener("Page.loadEventFired")
	defer h.unregisterEventListener("Page.loadEventFired", eventCh)

	select {
	case <-eventCh:
		return nil
	case <-timer.C:
		return fmt.Errorf("page load timed out after %ds", timeoutSecs)
	case <-done:
		return nil
	}
}

// Event listener management
var (
	eventMu       sync.Mutex
	eventListeners = make(map[string][]chan struct{})
)

// dispatchEvent is called by the connection reader goroutine for CDP events (id=0).
// This is a package-level function since events come from the shared connection.
func dispatchEvent(method string) {
	eventMu.Lock()
	defer eventMu.Unlock()
	for _, ch := range eventListeners[method] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *Handlers) registerEventListener(method string) chan struct{} {
	ch := make(chan struct{}, 1)
	eventMu.Lock()
	eventListeners[method] = append(eventListeners[method], ch)
	eventMu.Unlock()
	return ch
}

func (h *Handlers) unregisterEventListener(method string, target chan struct{}) {
	eventMu.Lock()
	defer eventMu.Unlock()
	list := eventListeners[method]
	for i, ch := range list {
		if ch == target {
			eventListeners[method] = append(list[:i], list[i+1:]...)
			return
		}
	}
}

// ── Tool definitions for tools/list ─────────────────────────────────────────

// ToolManifest returns the full list of MCP tools exposed by SurfaceProxy.
func ToolManifest() []Tool {
	return []Tool{
		{
			Name:        "browse",
			Description: "Navigate the headless browser to a URL and return a semantically pruned, token-optimised Markdown representation of the page content.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"url": {Type: "string", Description: "The fully-qualified URL to navigate to (e.g. https://example.com)."},
				},
				Required: []string{"url"},
			},
		},
		{
			Name:        "getDOM",
			Description: "Return a semantically pruned Markdown snapshot of the current browser page without navigating. Uses structural diffing to only return changed nodes on subsequent calls.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "click",
			Description: "Click a DOM element identified by a CSS selector on the current page.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"selector": {Type: "string", Description: "A CSS selector string identifying the element to click (e.g. #submit-button, .nav-link)."},
				},
				Required: []string{"selector"},
			},
		},
		{
			Name:        "type",
			Description: "Type text into a focused input element identified by a CSS selector.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"selector": {Type: "string", Description: "CSS selector of the input element."},
					"text":     {Type: "string", Description: "The text to type into the input."},
				},
				Required: []string{"selector", "text"},
			},
		},
		{
			Name:        "screenshot",
			Description: "Capture a PNG screenshot of the current browser viewport and return it as a base64-encoded string.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
	}
}

// ── Tool call handlers ───────────────────────────────────────────────────────

func (h *Handlers) HandleBrowse(args json.RawMessage) ToolCallResult {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.URL == "" {
		return ErrorContent("browse requires a 'url' argument")
	}

	allowed, reason, err := h.evaluator.EvaluateURL(p.URL)
	if err != nil {
		return ErrorContent("firewall evaluation error: " + err.Error())
	}
	if !allowed {
		return ErrorContent(fmt.Sprintf("URL blocked by firewall: %s — Reason: %s", p.URL, reason))
	}

	if _, err := h.sendCDPCommand("Page.navigate", map[string]string{"url": p.URL}); err != nil {
		return ErrorContent("navigation failed: " + err.Error())
	}

	// Wait for real page load event instead of sleeping
	// Non-fatal: continue with whatever content is available if it times out
	_ = h.waitForPageLoad(20)

	dom, err := h.getCurrentPrunedDOM(p.URL)
	if err != nil {
		return ErrorContent("DOM retrieval failed: " + err.Error())
	}

	return TextContent(dom)
}

func (h *Handlers) HandleGetDOM(_ json.RawMessage) ToolCallResult {
	res, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression": "window.location.href",
	})
	if err != nil {
		return ErrorContent("failed to get current URL: " + err.Error())
	}

	var valRes struct {
		Result struct{ Value string `json:"value"` } `json:"result"`
	}
	url := "current_page"
	if json.Unmarshal(res, &valRes) == nil && valRes.Result.Value != "" {
		url = valRes.Result.Value
	}

	dom, err := h.getCurrentPrunedDOM(url)
	if err != nil {
		return ErrorContent("DOM retrieval failed: " + err.Error())
	}
	return TextContent(dom)
}

func (h *Handlers) HandleClick(args json.RawMessage) ToolCallResult {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Selector == "" {
		return ErrorContent("click requires a 'selector' argument")
	}

	js := fmt.Sprintf(`(function(){var el=document.querySelector(%q);if(!el)throw new Error('element not found');el.click();return true;})()`, p.Selector)
	if _, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}); err != nil {
		return ErrorContent("click failed: " + err.Error())
	}
	return TextContent("clicked: " + p.Selector)
}

func (h *Handlers) HandleType(args json.RawMessage) ToolCallResult {
	var p struct {
		Selector string `json:"selector"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Selector == "" {
		return ErrorContent("type requires 'selector' and 'text' arguments")
	}

	js := fmt.Sprintf(`(function(){var el=document.querySelector(%q);if(!el)throw new Error('element not found');el.focus();el.value=%q;el.dispatchEvent(new Event('input',{bubbles:true}));return true;})()`, p.Selector, p.Text)
	if _, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}); err != nil {
		return ErrorContent("type failed: " + err.Error())
	}
	return TextContent(fmt.Sprintf("typed into %s: %q", p.Selector, p.Text))
}

func (h *Handlers) HandleScreenshot(_ json.RawMessage) ToolCallResult {
	result, err := h.sendCDPCommand("Page.captureScreenshot", map[string]interface{}{"format": "png"})
	if err != nil {
		return ErrorContent("screenshot failed: " + err.Error())
	}

	var res struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return ErrorContent("failed to decode screenshot: " + err.Error())
	}
	return TextContent("data:image/png;base64," + res.Data)
}

func (h *Handlers) getCurrentPrunedDOM(pageKey string) (string, error) {
	res, err := h.sendCDPCommand("Runtime.evaluate", map[string]interface{}{
		"expression": "document.documentElement.outerHTML",
	})
	if err != nil {
		return "", err
	}

	var valRes struct {
		Result struct{ Value string `json:"value"` } `json:"result"`
	}
	if err := json.Unmarshal(res, &valRes); err != nil {
		return "", err
	}

	// Use PruneWithSession so telemetry is recorded against the active session
	pruned, err := h.pruner.PruneWithSession([]byte(valRes.Result.Value), h.sessionID)
	if err != nil {
		return "", err
	}

	// Update the session's current URL in the ledger
	if h.ledger != nil {
		h.ledger.UpdateSessionURL(h.sessionID, pageKey)
	}

	diff, _ := h.diffEngine.ComputeDiff(pageKey, pruned)
	return string(diff), nil
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

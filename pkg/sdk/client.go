package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	addr    string
	ws      *websocket.Conn
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan []byte
}

func NewClient(addr string) (*Client, error) {
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SurfaceProxy MCP at %s: %w", addr, err)
	}

	c := &Client{
		addr:    addr,
		ws:      conn,
		pending: make(map[int64]chan []byte),
	}

	go func() {
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var resp struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(data, &resp); err == nil && resp.ID != 0 {
				c.mu.Lock()
				ch, ok := c.pending[resp.ID]
				c.mu.Unlock()
				if ok {
					select {
					case ch <- data:
					default:
					}
				}
			}
		}
	}()

	return c, nil
}

func (c *Client) call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan []byte, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	var req struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int64       `json:"id"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
	}
	req.JSONRPC = "2.0"
	req.ID = id
	req.Method = method
	req.Params = params

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	err = c.ws.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	select {
	case resp := <-ch:
		var rpcResp struct {
			Result json.RawMessage `json:"result,omitempty"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(resp, &rpcResp); err != nil {
			return nil, err
		}
		if rpcResp.Error != nil {
			return nil, fmt.Errorf("MCP error: %s (code %d)", rpcResp.Error.Message, rpcResp.Error.Code)
		}
		return rpcResp.Result, nil
	case <-time.After(15 * time.Second):
		return nil, errors.New("timeout waiting for MCP response")
	}
}

func (c *Client) Browse(url string) (string, error) {
	params := map[string]string{"url": url}
	res, err := c.call("browse", params)
	if err != nil {
		return "", err
	}

	var val struct {
		Status string `json:"status"`
		Reason string `json:"reason,omitempty"`
		DOM    string `json:"dom,omitempty"`
	}
	if err := json.Unmarshal(res, &val); err != nil {
		return "", err
	}

	if val.Status == "blocked" {
		return "", fmt.Errorf("blocked by firewall: %s", val.Reason)
	}

	return val.DOM, nil
}

func (c *Client) Click(selector string) error {
	params := map[string]string{"selector": selector}
	_, err := c.call("click", params)
	return err
}

func (c *Client) GetDOM() (string, error) {
	res, err := c.call("getDOM", nil)
	if err != nil {
		return "", err
	}

	var val struct {
		DOM string `json:"dom"`
	}
	if err := json.Unmarshal(res, &val); err != nil {
		return "", err
	}
	return val.DOM, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil {
		return c.ws.Close()
	}
	return nil
}

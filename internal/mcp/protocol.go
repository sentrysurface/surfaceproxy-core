// Package mcp implements the Model Context Protocol (MCP) specification version 2024-11-05.
// Reference: https://spec.modelcontextprotocol.io/specification/2024-11-05/
package mcp

import "encoding/json"

// ── JSON-RPC 2.0 base types ──────────────────────────────────────────────────

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
)

// ── MCP Protocol version ─────────────────────────────────────────────────────

const ProtocolVersion = "2024-11-05"

// ── initialize ───────────────────────────────────────────────────────────────

type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    ClientCaps `json:"capabilities"`
	ClientInfo      AppInfo    `json:"clientInfo"`
}

type ClientCaps struct {
	Sampling *struct{} `json:"sampling,omitempty"`
	Roots    *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"roots,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    ServerCaps `json:"capabilities"`
	ServerInfo      AppInfo    `json:"serverInfo"`
	Instructions    string     `json:"instructions,omitempty"`
}

type ServerCaps struct {
	Tools *ToolsCap `json:"tools,omitempty"`
}

type ToolsCap struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ── tools/list ───────────────────────────────────────────────────────────────

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// ── tools/call ───────────────────────────────────────────────────────────────

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ── helpers ──────────────────────────────────────────────────────────────────

func NewErrorResponse(id interface{}, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

func NewResultResponse(id interface{}, result interface{}) (*Response, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	}, nil
}

func NewNotification(method string, params interface{}) (*Notification, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	}, nil
}

func TextContent(text string) ToolCallResult {
	return ToolCallResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

func ErrorContent(msg string) ToolCallResult {
	return ToolCallResult{
		IsError: true,
		Content: []ContentBlock{{Type: "text", Text: msg}},
	}
}

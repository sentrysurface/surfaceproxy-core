package cdp

import "encoding/json"

type CDPMessage struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *CDPError       `json:"error,omitempty"`
}

type CDPError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// ParseFrame parses a raw byte slice into a CDPMessage.
func ParseFrame(data []byte) (*CDPMessage, error) {
	var msg CDPMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

package config

type Config struct {
	ListenAddr       string        `json:"listen_addr"`
	TargetBrowserURL string        `json:"target_browser_url"`
	MCPTransport     string        `json:"mcp_transport"` // "stdio" or "websocket"
	MCPListenAddr    string        `json:"mcp_listen_addr"`
	Firewall         FirewallConfig `json:"firewall"`
	Pruning          PruningConfig  `json:"pruning"`
}

type FirewallConfig struct {
	Allowlist []string `json:"allowlist"`
	Blocklist []string `json:"blocklist"`
}

type PruningConfig struct {
	OutputFormat string   `json:"output_format"` // "markdown" or "json"
	MaxTokens    int      `json:"max_tokens"`
	StripTags    []string `json:"strip_tags"`
}

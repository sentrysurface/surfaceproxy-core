package config

type Config struct {
	ListenAddr       string         `json:"listen_addr"`
	TargetBrowserURL string         `json:"target_browser_url"` // used if browser.mode = "external"
	MCPTransport     string         `json:"mcp_transport"`      // "stdio" or "websocket"
	MCPListenAddr    string         `json:"mcp_listen_addr"`
	Firewall         FirewallConfig `json:"firewall"`
	Pruning          PruningConfig  `json:"pruning"`
	Browser          BrowserConfig  `json:"browser"`
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

// BrowserConfig controls how SurfaceProxy manages the headless browser.
type BrowserConfig struct {
	// Mode: "auto" (detect system Chrome), "path" (explicit binary), "external" (pre-running WS endpoint)
	Mode       string `json:"mode"`
	// BinaryPath is used when Mode = "path"
	BinaryPath string `json:"binary_path"`
	// DebugPort is the remote debugging port to bind to (0 = random ephemeral port)
	DebugPort  int    `json:"debug_port"`
	// Args are extra Chrome CLI flags appended to the launch command
	Args       []string `json:"args"`
}

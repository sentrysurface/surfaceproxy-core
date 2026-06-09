package config

import (
	"os"
	"testing"
)

func TestLoader(t *testing.T) {
	data := `{
		"listen_addr": ":8443",
		"target_browser_url": "ws://localhost:9222",
		"mcp_transport": "stdio",
		"mcp_listen_addr": ":8444",
		"firewall": {
			"allowlist": ["^https?://github\\.com"],
			"blocklist": ["^https?://ad\\.com"]
		},
		"pruning": {
			"output_format": "markdown",
			"max_tokens": 100,
			"strip_tags": ["script"]
		}
	}`

	tmpFile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	l, err := NewLoader(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := l.GetConfig()
	if cfg.ListenAddr != ":8443" {
		t.Errorf("expected :8443, got %s", cfg.ListenAddr)
	}
	if len(cfg.Firewall.Allowlist) != 1 {
		t.Errorf("expected 1 allowlist item, got %d", len(cfg.Firewall.Allowlist))
	}
}

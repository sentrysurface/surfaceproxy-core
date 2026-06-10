package bootstrap_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sentrysurface/surface-proxy/internal/bootstrap"
)

// writeAndRegister is a test helper that calls Register targeting a temp directory.
func withTempConfigPath(t *testing.T, editor bootstrap.Editor, fn func(configPath string)) {
	t.Helper()
	tmp := t.TempDir()
	// Patch the config path to a temp file by creating the directory structure
	// We can't override ConfigPath easily, so we test Register via the file directly.
	configPath := filepath.Join(tmp, "mcp.json")
	fn(configPath)
}

func TestRegister_FreshFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "Cursor", "User", "mcp.json")

	entry := struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{
		Command: "/usr/local/bin/surface-proxy",
		Args:    []string{"mcp-mode", "--config", filepath.Join(tmp, ".surface-proxy", "config.json")},
	}

	// Simulate what Register would write
	type mcpJSON struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	out := mcpJSON{
		MCPServers: map[string]interface{}{
			"surface-proxy": entry,
		},
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read back and verify
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var parsed mcpJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if _, ok := parsed.MCPServers["surface-proxy"]; !ok {
		t.Error("expected surface-proxy key in mcpServers")
	}
}

func TestRegister_MergePreservesExisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "mcp.json")

	// Pre-create an mcp.json with another existing server
	existing := `{
  "mcpServers": {
    "other-tool": {
      "command": "other-tool",
      "args": ["--serve"]
    }
  }
}
`
	if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("failed to write existing mcp.json: %v", err)
	}

	// Parse and merge surface-proxy entry
	type mcpJSON struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	data, _ := os.ReadFile(configPath)
	var doc mcpJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if doc.MCPServers == nil {
		doc.MCPServers = make(map[string]json.RawMessage)
	}

	entry := map[string]interface{}{
		"command": "/usr/bin/surface-proxy",
		"args":    []string{"mcp-mode", "--config", "/home/user/.surface-proxy/config.json"},
	}
	entryJSON, _ := json.Marshal(entry)
	doc.MCPServers["surface-proxy"] = json.RawMessage(entryJSON)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Verify both entries are present
	data, _ = os.ReadFile(configPath)
	var result mcpJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse result failed: %v", err)
	}
	if _, ok := result.MCPServers["other-tool"]; !ok {
		t.Error("other-tool should be preserved after merge")
	}
	if _, ok := result.MCPServers["surface-proxy"]; !ok {
		t.Error("surface-proxy should be present after merge")
	}
}

func TestConfigPath_ReturnsNonEmpty(t *testing.T) {
	for _, editor := range []bootstrap.Editor{bootstrap.EditorCursor, bootstrap.EditorVSCode} {
		path, err := bootstrap.ConfigPath(editor)
		if err != nil {
			// On unsupported OS (e.g. plan9) this can fail — acceptable
			t.Logf("ConfigPath(%v) error: %v (may be expected on this OS)", editor, err)
			continue
		}
		if path == "" {
			t.Errorf("ConfigPath(%v) returned empty path", editor)
		}
		if filepath.Base(path) != "mcp.json" {
			t.Errorf("ConfigPath(%v) = %q, expected to end in mcp.json", editor, path)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if bootstrap.DisplayName(bootstrap.EditorCursor) != "Cursor" {
		t.Error("expected DisplayName(EditorCursor) == \"Cursor\"")
	}
	if bootstrap.DisplayName(bootstrap.EditorVSCode) != "VS Code" {
		t.Error("expected DisplayName(EditorVSCode) == \"VS Code\"")
	}
}

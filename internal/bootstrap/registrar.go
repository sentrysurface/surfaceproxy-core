// Package bootstrap implements the `surface-proxy init` self-registration feature.
// It locates the MCP configuration file for Cursor or VS Code and merges in
// a surface-proxy server entry without touching any existing server definitions.
package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Editor identifies a supported IDE target for MCP registration.
type Editor int

const (
	EditorCursor Editor = iota
	EditorVSCode
)

// mcpJSON is the top-level shape of an mcp.json file.
// We use RawMessage for the server values so any extra fields are preserved during roundtrip.
type mcpJSON struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// mcpServerEntry is the shape of a single server definition.
type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// ConfigPath returns the absolute path to the editor's mcp.json on the current OS.
// The directory may or may not exist yet.
func ConfigPath(editor Editor) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}

	var dir string
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		switch editor {
		case EditorCursor:
			dir = filepath.Join(base, "Cursor", "User")
		case EditorVSCode:
			dir = filepath.Join(base, "Code", "User")
		}
	case "linux":
		base := filepath.Join(home, ".config")
		switch editor {
		case EditorCursor:
			dir = filepath.Join(base, "Cursor", "User")
		case EditorVSCode:
			dir = filepath.Join(base, "Code", "User")
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable is not set")
		}
		switch editor {
		case EditorCursor:
			dir = filepath.Join(appData, "Cursor", "User")
		case EditorVSCode:
			dir = filepath.Join(appData, "Code", "User")
		}
	default:
		return "", fmt.Errorf("unsupported OS: %s — manual mcp.json configuration required", runtime.GOOS)
	}

	return filepath.Join(dir, "mcp.json"), nil
}

// Register writes or updates the surface-proxy entry in the target editor's mcp.json.
//
// Behaviour:
//   - If mcp.json exists, it is read and the surface-proxy key is merged/overwritten.
//     All other existing server definitions are preserved.
//   - If mcp.json does not exist, its parent directory is created and the file is written fresh.
//   - The write is atomic: a temporary file is written first, then renamed.
//   - If dryRun is true, the resulting JSON is printed to stdout and no files are touched.
//
// execPath should be the absolute path to the currently-running surface-proxy binary.
func Register(editor Editor, execPath string, dryRun bool) error {
	configPath, err := ConfigPath(editor)
	if err != nil {
		return err
	}

	// Build the surface-proxy server entry.
	// Use the absolute path for --config so editors that don't expand ~ work correctly.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	defaultConfig := filepath.Join(home, ".surface-proxy", "config.json")

	entry := mcpServerEntry{
		Command: execPath,
		Args:    []string{"mcp-mode", "--config", defaultConfig},
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal server entry: %w", err)
	}

	// ── Read existing mcp.json ────────────────────────────────────────────────
	existing := mcpJSON{MCPServers: make(map[string]json.RawMessage)}
	data, err := os.ReadFile(configPath)
	if err == nil {
		// File exists — decode while preserving unknown fields in other server entries
		if jsonErr := json.Unmarshal(data, &existing); jsonErr != nil {
			return fmt.Errorf("failed to parse existing %s: %w\n"+
				"Fix the JSON manually or delete the file and retry", configPath, jsonErr)
		}
		if existing.MCPServers == nil {
			existing.MCPServers = make(map[string]json.RawMessage)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	// ── Merge surface-proxy entry ─────────────────────────────────────────────
	existing.MCPServers["surface-proxy"] = json.RawMessage(entryJSON)

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp.json: %w", err)
	}
	out = append(out, '\n')

	// ── Dry-run output ────────────────────────────────────────────────────────
	if dryRun {
		fmt.Printf("[DRY-RUN] Would write %s (%s):\n%s\n", configPath, DisplayName(editor), string(out))
		return nil
	}

	// ── Atomic write ──────────────────────────────────────────────────────────
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	tmp := configPath + ".surface-proxy.tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmp, configPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to finalize mcp.json: %w", err)
	}

	fmt.Printf("✓  Registered surface-proxy in %s config:\n   %s\n\n", DisplayName(editor), configPath)
	fmt.Printf("   Restart %s to pick up the new MCP server.\n", DisplayName(editor))
	return nil
}

// DisplayName returns a human-readable name for the editor (suitable for log/UI messages).
func DisplayName(editor Editor) string {
	switch editor {
	case EditorCursor:
		return "Cursor"
	case EditorVSCode:
		return "VS Code"
	default:
		return "Unknown Editor"
	}
}

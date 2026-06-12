package firewall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

func TestRuleEngineDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "firewall-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "firewall.db")
	cfg := config.FirewallConfig{
		Allowlist: []string{
			"github.com",
			"google.com",
		},
		Blocklist: []string{
			"doubleclick.net",
		},
	}

	re, err := NewRuleEngine(dbPath, cfg)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}
	defer re.Close()

	// Test default evaluation
	allowed, reason, err := re.EvaluateURL("https://github.com/sentrysurface/surfaceproxy-core")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("expected github.com to be allowed, got blocked: %s", reason)
	}

	// Test subdomain matching (domain walk)
	allowed, reason, err = re.EvaluateURL("https://sub.github.com/path")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("expected sub.github.com to be allowed (via github.com), got blocked: %s", reason)
	}

	// Test blocklist domain walk
	allowed, reason, err = re.EvaluateURL("https://sub.doubleclick.net/ad.gif")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected sub.doubleclick.net to be blocked, got allowed")
	}

	// Test not in allowlist
	allowed, reason, err = re.EvaluateURL("https://malicious.com")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected malicious.com to be blocked (not in allowlist), got allowed")
	}

	// Test CRUD functions
	err = re.AddRule(true, "malicious.com", true)
	if err != nil {
		t.Fatalf("failed to add rule: %v", err)
	}

	allowed, reason, err = re.EvaluateURL("https://malicious.com")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("expected malicious.com to be allowed after adding rule, got blocked: %s", reason)
	}

	// Test disable rule
	err = re.UpdateRule(true, "malicious.com", true, "malicious.com", false)
	if err != nil {
		t.Fatalf("failed to update rule: %v", err)
	}

	allowed, reason, err = re.EvaluateURL("https://malicious.com")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected malicious.com to be blocked after disabling rule, got allowed")
	}

	// Test delete rule
	err = re.DeleteRule(true, "malicious.com")
	if err != nil {
		t.Fatalf("failed to delete rule: %v", err)
	}
}

func TestCleanJSONC(t *testing.T) {
	input := []byte(`{
		// this is a comment
		"key": "value", /* block comment */
		"array": [
			1,
			2
		]
	}`)
	cleaned := cleanJSONC(input)

	var obj map[string]interface{}
	if err := json.Unmarshal(cleaned, &obj); err != nil {
		t.Errorf("failed to parse cleaned JSONC: %v\nCleaned content:\n%s", err, string(cleaned))
	}

	if obj["key"] != "value" {
		t.Errorf("expected value, got %v", obj["key"])
	}
}

func TestCleanDomainPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		expected string
	}{
		{"https://code.visualstudio.com", "code.visualstudio.com"},
		{"*.github.com", "github.com"},
		{"github.com", "github.com"},
		{"^https?://([a-zA-Z0-9-]+\\.)*google\\.com(/.*)?$", "google.com"},
		{"^https?://([a-zA-Z0-9-]+\\.)*adservice\\.google\\.com(/.*)?$", "adservice.google.com"},
		{"http://test.org/some/path", "test.org"},
	}

	for _, tc := range tests {
		actual := cleanDomainPattern(tc.pattern)
		if actual != tc.expected {
			t.Errorf("cleanDomainPattern(%q) = %q, expected %q", tc.pattern, actual, tc.expected)
		}
	}
}

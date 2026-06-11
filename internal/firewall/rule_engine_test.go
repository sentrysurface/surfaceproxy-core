package firewall

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

func TestRuleEngine(t *testing.T) {
	cfg := config.FirewallConfig{
		Allowlist: []string{
			"^https?://([a-zA-Z0-9-]+\\.)*github\\.com(/.*)?$",
		},
		Blocklist: []string{
			"^https?://([a-zA-Z0-9-]+\\.)*adserver\\.com(/.*)?$",
		},
	}

	re, err := NewRuleEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}

	allowed, reason, err := re.EvaluateURL("https://github.com/sentrysurface/surfaceproxy-core")
	if err != nil {
		t.Error(err)
	}
	if !allowed {
		t.Errorf("expected allowed, got blocked: %s", reason)
	}

	allowed, _, err = re.EvaluateURL("https://google.com")
	if err != nil {
		t.Error(err)
	}
	if allowed {
		t.Error("expected blocked (not in allowlist), but got allowed")
	}

	allowed, _, err = re.EvaluateURL("https://sub.adserver.com/track")
	if err != nil {
		t.Error(err)
	}
	if allowed {
		t.Error("expected blocked (blocklist match), but got allowed")
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

func TestConvertPatternToRegex(t *testing.T) {
	tests := []struct {
		pattern string
		url     string
		allowed bool
	}{
		{"https://code.visualstudio.com", "https://code.visualstudio.com", true},
		{"https://code.visualstudio.com", "https://code.visualstudio.com/docs", true},
		{"https://github.com/microsoft/vscode/wiki/*", "https://github.com/microsoft/vscode/wiki/some-page", true},
		{"https://github.com/microsoft/vscode/wiki/*", "https://github.com/microsoft/vscode/wiki/", true},
		{"https://github.com/microsoft/vscode/wiki/*", "https://github.com/microsoft/vscode/other", false},
		{"fastapi.tiangolo.com", "https://fastapi.tiangolo.com", true},
		{"fastapi.tiangolo.com", "http://fastapi.tiangolo.com/docs", true},
	}

	for _, tc := range tests {
		regexStr := convertPatternToRegex(tc.pattern)
		r, err := regexp.Compile(regexStr)
		if err != nil {
			t.Errorf("failed to compile regex %q: %v", regexStr, err)
			continue
		}
		matched := r.MatchString(tc.url)
		if matched != tc.allowed {
			t.Errorf("pattern %q, URL %q: expected match %v, got %v (regex: %s)", tc.pattern, tc.url, tc.allowed, matched, regexStr)
		}
	}
}

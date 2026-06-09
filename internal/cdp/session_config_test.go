package cdp

import (
	"net/url"
	"testing"

	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
)

func TestParseSessionConfig(t *testing.T) {
	globalCfg := &config.Config{
		TargetBrowserURL: "ws://localhost:9222/devtools/browser/abc",
		Firewall: config.FirewallConfig{
			Allowlist: []string{`^https?://.*\.google\.com(/.*)?$`},
			Blocklist: []string{`^https?://.*\.ads\.com(/.*)?$`},
		},
	}

	globalEvaluator, err := firewall.NewRuleEngine(globalCfg.Firewall)
	if err != nil {
		t.Fatalf("Failed to create global evaluator: %v", err)
	}

	t.Run("Default - No Overrides", func(t *testing.T) {
		q := url.Values{}
		sc, err := ParseSessionConfig(q, globalCfg, globalEvaluator)
		if err != nil {
			t.Fatalf("ParseSessionConfig failed: %v", err)
		}

		if sc.BrowserWSURL != globalCfg.TargetBrowserURL {
			t.Errorf("Expected BrowserWSURL %q, got %q", globalCfg.TargetBrowserURL, sc.BrowserWSURL)
		}

		if sc.FirewallOverride != globalEvaluator {
			t.Error("Expected FirewallOverride to be the globalEvaluator instance")
		}

		// Verify global rules are still applied
		allowed, _, _ := sc.FirewallOverride.EvaluateURL("https://www.google.com")
		if !allowed {
			t.Error("Expected google.com to be allowed by global rules")
		}

		blocked, _, _ := sc.FirewallOverride.EvaluateURL("https://bad.ads.com")
		if blocked {
			t.Error("Expected ads.com to be blocked by global rules")
		}
	})

	t.Run("Override Target Browser URL", func(t *testing.T) {
		q := url.Values{}
		q.Set("target", "ws://localhost:9999")
		sc, err := ParseSessionConfig(q, globalCfg, globalEvaluator)
		if err != nil {
			t.Fatalf("ParseSessionConfig failed: %v", err)
		}

		if sc.BrowserWSURL != "ws://localhost:9999" {
			t.Errorf("Expected BrowserWSURL %q, got %q", "ws://localhost:9999", sc.BrowserWSURL)
		}
	})

	t.Run("Override Allowlist and Blocklist with Globs", func(t *testing.T) {
		q := url.Values{}
		q.Set("allowlist", "*.gov.au,*.nsw.gov.au")
		q.Set("blocklist", "*.badads.com")

		sc, err := ParseSessionConfig(q, globalCfg, globalEvaluator)
		if err != nil {
			t.Fatalf("ParseSessionConfig failed: %v", err)
		}

		if sc.FirewallOverride == globalEvaluator {
			t.Error("Expected FirewallOverride to be a new evaluator instance, not the global one")
		}

		// Test cases for new evaluator
		tests := []struct {
			url     string
			allowed bool
		}{
			{"https://nsw.gov.au", true},
			{"https://some.gov.au/path", true},
			{"https://www.google.com", true}, // global allowlist should still be active because we merged it
			{"https://badads.com", false},    // session blocklist
			{"https://bad.ads.com", false},   // global blocklist should still be active
			{"https://other.com", false},     // not in any allowlist
		}

		for _, tc := range tests {
			allowed, reason, err := sc.FirewallOverride.EvaluateURL(tc.url)
			if err != nil {
				t.Errorf("EvaluateURL(%q) returned error: %v", tc.url, err)
			}
			if allowed != tc.allowed {
				t.Errorf("EvaluateURL(%q) allowed=%t, expected %t (reason: %q)", tc.url, allowed, tc.allowed, reason)
			}
		}
	})
}

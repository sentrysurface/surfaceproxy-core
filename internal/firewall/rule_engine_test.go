package firewall

import (
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

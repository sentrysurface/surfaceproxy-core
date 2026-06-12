package cdp

import (
	"net/url"
	"strings"

	"github.com/sentrysurface/surface-proxy/internal/config"
	"github.com/sentrysurface/surface-proxy/internal/firewall"
)

// SessionConfig carries per-session overrides parsed from the /v1/session query parameters.
// These override global config for the lifetime of a single connection.
type SessionConfig struct {
	// FirewallOverride, if non-nil, replaces the global firewall evaluator for this session.
	FirewallOverride firewall.Evaluator
	// BrowserWSURL, if non-empty, overrides the target browser WebSocket endpoint for this session.
	BrowserWSURL string
	// NewPage, if true, requests that the proxy spin up a dynamically isolated tab
	NewPage bool
}

// ParseSessionConfig reads URL query parameters from a /v1/session connection request
// and constructs a SessionConfig with any per-session overrides.
//
// Supported query parameters:
//
//	allowlist=*.gov.au,*.example.com   — comma-separated URL glob patterns added to allowlist
//	blocklist=*.ads.com                — comma-separated URL glob patterns added to blocklist
//	target=ws://localhost:9223         — override the target browser WS endpoint
//	new_page=true                      — request a dynamically isolated tab session
func ParseSessionConfig(q url.Values, globalCfg *config.Config, globalEvaluator firewall.Evaluator) (*SessionConfig, error) {
	sc := &SessionConfig{
		BrowserWSURL: globalCfg.TargetBrowserURL,
		NewPage:      q.Get("new_page") == "true",
	}

	// Check for explicit target browser override
	if target := q.Get("target"); target != "" {
		sc.BrowserWSURL = target
	}

	// Build per-session firewall config by merging global rules with session overrides
	var globalAllow, globalBlock []string
	if re, ok := globalEvaluator.(*firewall.RuleEngine); ok {
		globalAllow, globalBlock = re.GetRules()
	}

	sessionFirewallCfg := config.FirewallConfig{}

	cleanParamDomains := func(rawVal string) []string {
		var domains []string
		for _, pattern := range strings.Split(rawVal, ",") {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				pattern = strings.TrimPrefix(pattern, "*")
				pattern = strings.TrimPrefix(pattern, ".")
				if pattern != "" {
					domains = append(domains, pattern)
				}
			}
		}
		return domains
	}

	sessionFirewallCfg.Allowlist = append(sessionFirewallCfg.Allowlist, globalAllow...)
	if rawAllow := q.Get("allowlist"); rawAllow != "" {
		sessionFirewallCfg.Allowlist = append(sessionFirewallCfg.Allowlist, cleanParamDomains(rawAllow)...)
	}

	sessionFirewallCfg.Blocklist = append(sessionFirewallCfg.Blocklist, globalBlock...)
	if rawBlock := q.Get("blocklist"); rawBlock != "" {
		sessionFirewallCfg.Blocklist = append(sessionFirewallCfg.Blocklist, cleanParamDomains(rawBlock)...)
	}

	hasOverrides := q.Get("allowlist") != "" || q.Get("blocklist") != ""
	if hasOverrides {
		re, err := firewall.NewRuleEngine("", sessionFirewallCfg)
		if err != nil {
			return nil, err
		}
		sc.FirewallOverride = re
	} else {
		sc.FirewallOverride = globalEvaluator
	}

	return sc, nil
}

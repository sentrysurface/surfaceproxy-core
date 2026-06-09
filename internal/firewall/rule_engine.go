package firewall

import (
	"regexp"
	"sync/atomic"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

type RuleEngine struct {
	allowlist atomic.Pointer[[]*regexp.Regexp]
	blocklist atomic.Pointer[[]*regexp.Regexp]
}

func NewRuleEngine(cfg config.FirewallConfig) (*RuleEngine, error) {
	re := &RuleEngine{}
	if err := re.UpdateRules(cfg); err != nil {
		return nil, err
	}
	return re, nil
}

func (re *RuleEngine) UpdateRules(cfg config.FirewallConfig) error {
	var newAllow []*regexp.Regexp
	for _, raw := range cfg.Allowlist {
		r, err := regexp.Compile(raw)
		if err != nil {
			return err
		}
		newAllow = append(newAllow, r)
	}

	var newBlock []*regexp.Regexp
	for _, raw := range cfg.Blocklist {
		r, err := regexp.Compile(raw)
		if err != nil {
			return err
		}
		newBlock = append(newBlock, r)
	}

	re.allowlist.Store(&newAllow)
	re.blocklist.Store(&newBlock)
	return nil
}

func (re *RuleEngine) EvaluateURL(targetURL string) (bool, string, error) {
	allowPtr := re.allowlist.Load()
	blockPtr := re.blocklist.Load()

	if allowPtr != nil {
		allowlist := *allowPtr
		if len(allowlist) > 0 {
			matched := false
			for _, r := range allowlist {
				if r.MatchString(targetURL) {
					matched = true
					break
				}
			}
			if !matched {
				return false, "URL not in allowlist", nil
			}
		}
	}

	if blockPtr != nil {
		blocklist := *blockPtr
		for _, r := range blocklist {
			if r.MatchString(targetURL) {
				return false, "URL matches blocklist pattern", nil
			}
		}
	}

	return true, "Allowed", nil
}

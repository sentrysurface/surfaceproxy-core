package firewall

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

// LogEvent records a single firewall evaluation decision.
type LogEvent struct {
	Timestamp time.Time `json:"timestamp"`
	URL       string    `json:"url"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason"`
}

const maxLogEvents = 200

type RuleEngine struct {
	allowlist atomic.Pointer[[]*regexp.Regexp]
	blocklist atomic.Pointer[[]*regexp.Regexp]

	// raw patterns stored for UI read-back
	rawMu     sync.RWMutex
	rawAllow  []string
	rawBlock  []string

	// ring-buffer for decision log
	logMu  sync.Mutex
	events []LogEvent
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
		if strings.HasPrefix(raw, "_disabled_:") {
			continue
		}
		r, err := regexp.Compile(raw)
		if err != nil {
			return err
		}
		newAllow = append(newAllow, r)
	}

	var newBlock []*regexp.Regexp
	for _, raw := range cfg.Blocklist {
		if strings.HasPrefix(raw, "_disabled_:") {
			continue
		}
		r, err := regexp.Compile(raw)
		if err != nil {
			return err
		}
		newBlock = append(newBlock, r)
	}

	re.allowlist.Store(&newAllow)
	re.blocklist.Store(&newBlock)

	re.rawMu.Lock()
	re.rawAllow = append([]string{}, cfg.Allowlist...)
	re.rawBlock = append([]string{}, cfg.Blocklist...)
	re.rawMu.Unlock()

	return nil
}

// GetRules returns the current raw pattern strings for allowlist and blocklist.
func (re *RuleEngine) GetRules() (allowlist []string, blocklist []string) {
	re.rawMu.RLock()
	defer re.rawMu.RUnlock()
	return append([]string{}, re.rawAllow...), append([]string{}, re.rawBlock...)
}

func (re *RuleEngine) EvaluateURL(targetURL string) (bool, string, error) {
	allowPtr := re.allowlist.Load()
	blockPtr := re.blocklist.Load()

	allowed := true
	reason := "Allowed"

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
				allowed = false
				reason = "URL not in allowlist"
			}
		}
	}

	if allowed && blockPtr != nil {
		blocklist := *blockPtr
		for _, r := range blocklist {
			if r.MatchString(targetURL) {
				allowed = false
				reason = "URL matches blocklist pattern"
				break
			}
		}
	}

	re.appendLog(LogEvent{
		Timestamp: time.Now(),
		URL:       targetURL,
		Allowed:   allowed,
		Reason:    reason,
	})

	return allowed, reason, nil
}

// RecentEvents returns up to n recent firewall evaluation events, newest first.
func (re *RuleEngine) RecentEvents(n int) []LogEvent {
	re.logMu.Lock()
	defer re.logMu.Unlock()
	if n > len(re.events) {
		n = len(re.events)
	}
	// return newest-first slice copy
	out := make([]LogEvent, n)
	for i := 0; i < n; i++ {
		out[i] = re.events[len(re.events)-1-i]
	}
	return out
}

func (re *RuleEngine) appendLog(ev LogEvent) {
	re.logMu.Lock()
	defer re.logMu.Unlock()
	re.events = append(re.events, ev)
	if len(re.events) > maxLogEvents {
		re.events = re.events[len(re.events)-maxLogEvents:]
	}
}

package firewall

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sentrysurface/surface-proxy/internal/config"
	bolt "go.etcd.io/bbolt"
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
	db *bolt.DB

	// in-memory cache for fast lookups
	cacheMu   sync.RWMutex
	allowlist map[string]bool // key: domain, value: enabled
	blocklist map[string]bool // key: domain, value: enabled

	// ring-buffer for decision log
	logMu  sync.Mutex
	events []LogEvent
}

func NewRuleEngine(dbPath string, cfg config.FirewallConfig) (*RuleEngine, error) {
	re := &RuleEngine{
		allowlist: make(map[string]bool),
		blocklist: make(map[string]bool),
	}

	if dbPath != "" {
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return nil, err
		}
		db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
		if err != nil {
			return nil, err
		}
		re.db = db

		// Set up buckets
		err = db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("allowlist"))
			if err != nil {
				return err
			}
			_, err = tx.CreateBucketIfNotExists([]byte("blocklist"))
			return err
		})
		if err != nil {
			db.Close()
			return nil, err
		}

		// Check if DB is empty. If it is, import config
		isEmpty := true
		err = db.View(func(tx *bolt.Tx) error {
			allowB := tx.Bucket([]byte("allowlist"))
			blockB := tx.Bucket([]byte("blocklist"))
			if allowB.Stats().KeyN > 0 || blockB.Stats().KeyN > 0 {
				isEmpty = false
			}
			return nil
		})
		if err != nil {
			db.Close()
			return nil, err
		}

		if isEmpty {
			if err := re.importConfig(cfg); err != nil {
				db.Close()
				return nil, err
			}
		} else {
			if err := re.loadFromDB(); err != nil {
				db.Close()
				return nil, err
			}
		}
	} else {
		// In-memory only (for session engines)
		for _, raw := range cfg.Allowlist {
			enabled := true
			domain := raw
			if strings.HasPrefix(raw, "_disabled_:") {
				enabled = false
				domain = strings.TrimPrefix(raw, "_disabled_:")
			}
			domain = cleanDomainPattern(domain)
			if domain != "" {
				re.allowlist[domain] = enabled
			}
		}
		for _, raw := range cfg.Blocklist {
			enabled := true
			domain := raw
			if strings.HasPrefix(raw, "_disabled_:") {
				enabled = false
				domain = strings.TrimPrefix(raw, "_disabled_:")
			}
			domain = cleanDomainPattern(domain)
			if domain != "" {
				re.blocklist[domain] = enabled
			}
		}
	}

	// Load dynamic VS Code rules if not in-memory session evaluator
	if dbPath != "" {
		re.ReloadVSCodeRules()
	}

	return re, nil
}

func (re *RuleEngine) Close() error {
	if re.db != nil {
		return re.db.Close()
	}
	return nil
}

func (re *RuleEngine) importConfig(cfg config.FirewallConfig) error {
	if re.db == nil {
		return nil
	}

	err := re.db.Update(func(tx *bolt.Tx) error {
		allowB := tx.Bucket([]byte("allowlist"))
		blockB := tx.Bucket([]byte("blocklist"))

		for _, raw := range cfg.Allowlist {
			enabled := true
			pattern := raw
			if strings.HasPrefix(raw, "_disabled_:") {
				enabled = false
				pattern = strings.TrimPrefix(raw, "_disabled_:")
			}
			domain := cleanDomainPattern(pattern)
			if domain != "" {
				val := []byte{1}
				if !enabled {
					val = []byte{0}
				}
				if err := allowB.Put([]byte(domain), val); err != nil {
					return err
				}
			}
		}

		for _, raw := range cfg.Blocklist {
			enabled := true
			pattern := raw
			if strings.HasPrefix(raw, "_disabled_:") {
				enabled = false
				pattern = strings.TrimPrefix(raw, "_disabled_:")
			}
			domain := cleanDomainPattern(pattern)
			if domain != "" {
				val := []byte{1}
				if !enabled {
					val = []byte{0}
				}
				if err := blockB.Put([]byte(domain), val); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return re.loadFromDB()
}

func (re *RuleEngine) loadFromDB() error {
	if re.db == nil {
		return nil
	}

	newAllow := make(map[string]bool)
	newBlock := make(map[string]bool)

	err := re.db.View(func(tx *bolt.Tx) error {
		allowB := tx.Bucket([]byte("allowlist"))
		blockB := tx.Bucket([]byte("blocklist"))

		err := allowB.ForEach(func(k, v []byte) error {
			enabled := len(v) > 0 && v[0] == 1
			newAllow[string(k)] = enabled
			return nil
		})
		if err != nil {
			return err
		}

		return blockB.ForEach(func(k, v []byte) error {
			enabled := len(v) > 0 && v[0] == 1
			newBlock[string(k)] = enabled
			return nil
		})
	})
	if err != nil {
		return err
	}

	re.cacheMu.Lock()
	re.allowlist = newAllow
	re.blocklist = newBlock
	re.cacheMu.Unlock()

	return nil
}

func (re *RuleEngine) UpdateRules(cfg config.FirewallConfig) error {
	if re.db != nil {
		return re.importConfig(cfg)
	}

	// In-memory only (session evaluator)
	re.cacheMu.Lock()
	re.allowlist = make(map[string]bool)
	re.blocklist = make(map[string]bool)
	for _, raw := range cfg.Allowlist {
		enabled := true
		domain := raw
		if strings.HasPrefix(raw, "_disabled_:") {
			enabled = false
			domain = strings.TrimPrefix(raw, "_disabled_:")
		}
		domain = cleanDomainPattern(domain)
		if domain != "" {
			re.allowlist[domain] = enabled
		}
	}
	for _, raw := range cfg.Blocklist {
		enabled := true
		domain := raw
		if strings.HasPrefix(raw, "_disabled_:") {
			enabled = false
			domain = strings.TrimPrefix(raw, "_disabled_:")
		}
		domain = cleanDomainPattern(domain)
		if domain != "" {
			re.blocklist[domain] = enabled
		}
	}
	re.cacheMu.Unlock()
	return nil
}

// AddRule adds a rule to the persistent store and updates the in-memory cache
func (re *RuleEngine) AddRule(isAllow bool, pattern string, enabled bool) error {
	domain := cleanDomainPattern(pattern)
	if domain == "" {
		return fmt.Errorf("invalid pattern: %s", pattern)
	}

	if re.db != nil {
		bucketName := []byte("allowlist")
		if !isAllow {
			bucketName = []byte("blocklist")
		}

		val := []byte{1}
		if !enabled {
			val = []byte{0}
		}

		err := re.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(bucketName)
			return b.Put([]byte(domain), val)
		})
		if err != nil {
			return err
		}
	}

	re.cacheMu.Lock()
	if isAllow {
		re.allowlist[domain] = enabled
	} else {
		re.blocklist[domain] = enabled
	}
	re.cacheMu.Unlock()

	return nil
}

// DeleteRule removes a rule from the persistent store and in-memory cache
func (re *RuleEngine) DeleteRule(isAllow bool, pattern string) error {
	domain := cleanDomainPattern(pattern)
	if domain == "" {
		return fmt.Errorf("invalid pattern: %s", pattern)
	}

	if re.db != nil {
		bucketName := []byte("allowlist")
		if !isAllow {
			bucketName = []byte("blocklist")
		}

		err := re.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(bucketName)
			return b.Delete([]byte(domain))
		})
		if err != nil {
			return err
		}
	}

	re.cacheMu.Lock()
	if isAllow {
		delete(re.allowlist, domain)
	} else {
		delete(re.blocklist, domain)
	}
	re.cacheMu.Unlock()

	return nil
}

// UpdateRule updates/replaces an existing rule in the database and cache
func (re *RuleEngine) UpdateRule(isAllow bool, oldPattern string, oldEnabled bool, newPattern string, newEnabled bool) error {
	if err := re.DeleteRule(isAllow, oldPattern); err != nil {
		return err
	}
	return re.AddRule(isAllow, newPattern, newEnabled)
}

func getVSCodeUserSettingsPath() string {
	var baseDir string
	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			return ""
		}
		return filepath.Join(baseDir, "Code", "User", "settings.json")
	case "darwin":
		home := os.Getenv("HOME")
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	default: // Linux and others
		home := os.Getenv("HOME")
		if home == "" {
			return ""
		}
		return filepath.Join(home, ".config", "Code", "User", "settings.json")
	}
}

func cleanJSONC(data []byte) []byte {
	blockComments := regexp.MustCompile(`(?s)/\*.*?\*/`)
	data = blockComments.ReplaceAll(data, nil)

	lineComments := regexp.MustCompile(`//.*`)
	data = lineComments.ReplaceAll(data, nil)

	trailingCommas := regexp.MustCompile(`,(\s*[}\]])`)
	data = trailingCommas.ReplaceAll(data, []byte("$1"))

	return data
}

func cleanDomainPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}

	clean := pattern
	clean = strings.TrimPrefix(clean, "^")
	clean = strings.TrimSuffix(clean, "$")
	clean = strings.TrimPrefix(clean, "https?://")
	clean = strings.TrimPrefix(clean, "http://")
	clean = strings.TrimPrefix(clean, "https://")

	if strings.Contains(clean, ")*") || strings.Contains(clean, ")+") {
		subdomainRegex := regexp.MustCompile(`^.*?\)\*?`)
		clean = subdomainRegex.ReplaceAllString(clean, "")
	}

	pathRegex := regexp.MustCompile(`[\(/].*$`)
	clean = pathRegex.ReplaceAllString(clean, "")

	clean = strings.ReplaceAll(clean, `\.`, ".")
	clean = strings.ReplaceAll(clean, `\`, "")

	clean = strings.TrimSpace(clean)
	clean = strings.TrimPrefix(clean, "*")
	clean = strings.TrimPrefix(clean, ".")
	clean = strings.Trim(clean, ".")

	return strings.ToLower(clean)
}

func LoadVSCodeApprovedRules() []string {
	var rules []string

	parseSettingsFile := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}

		cleaned := cleanJSONC(data)
		var settings struct {
			ChatToolsURLsAutoApprove map[string]bool `json:"chat.tools.urls.autoApprove"`
		}

		if err := json.Unmarshal(cleaned, &settings); err != nil {
			return
		}

		for urlPattern, approved := range settings.ChatToolsURLsAutoApprove {
			if !approved {
				continue
			}
			domain := cleanDomainPattern(urlPattern)
			if domain != "" {
				rules = append(rules, domain)
			}
		}
	}

	globalPath := getVSCodeUserSettingsPath()
	if globalPath != "" {
		parseSettingsFile(globalPath)
	}

	localPath := filepath.Join(".vscode", "settings.json")
	parseSettingsFile(localPath)

	return rules
}

func (re *RuleEngine) ReloadVSCodeRules() {
	vscRules := LoadVSCodeApprovedRules()

	re.cacheMu.Lock()
	defer re.cacheMu.Unlock()
	for _, domain := range vscRules {
		if domain != "" {
			re.allowlist[domain] = true
		}
	}
}

func (re *RuleEngine) GetRules() (allowlist []string, blocklist []string) {
	re.cacheMu.RLock()
	defer re.cacheMu.RUnlock()

	for domain, enabled := range re.allowlist {
		pattern := domain
		if !enabled {
			pattern = "_disabled_:" + domain
		}
		allowlist = append(allowlist, pattern)
	}

	for domain, enabled := range re.blocklist {
		pattern := domain
		if !enabled {
			pattern = "_disabled_:" + domain
		}
		blocklist = append(blocklist, pattern)
	}

	return allowlist, blocklist
}

func (re *RuleEngine) EvaluateURL(targetURL string) (bool, string, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false, "", err
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		host = strings.ToLower(targetURL)
	}

	re.cacheMu.RLock()
	defer re.cacheMu.RUnlock()

	parts := strings.Split(host, ".")
	for i := 0; i < len(parts); i++ {
		subdomain := strings.Join(parts[i:], ".")
		if enabled, exists := re.blocklist[subdomain]; exists && enabled {
			allowed := false
			reason := fmt.Sprintf("Blocked by blocklist rule: %s", subdomain)
			re.appendLog(LogEvent{
				Timestamp: time.Now(),
				URL:       targetURL,
				Allowed:   allowed,
				Reason:    reason,
			})
			return allowed, reason, nil
		}
	}

	hasAllowRules := false
	for _, enabled := range re.allowlist {
		if enabled {
			hasAllowRules = true
			break
		}
	}

	allowed := true
	reason := "Allowed"

	if hasAllowRules {
		matchedAllow := false
		for i := 0; i < len(parts); i++ {
			subdomain := strings.Join(parts[i:], ".")
			if enabled, exists := re.allowlist[subdomain]; exists && enabled {
				matchedAllow = true
				break
			}
		}
		if !matchedAllow {
			allowed = false
			reason = "Blocked: domain not in allowlist"
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

func (re *RuleEngine) RecentEvents(n int) []LogEvent {
	re.logMu.Lock()
	defer re.logMu.Unlock()
	if n > len(re.events) {
		n = len(re.events)
	}
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

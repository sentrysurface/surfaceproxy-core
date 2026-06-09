package browser

import (
	"os/exec"
	"runtime"
)

// commonChromePaths maps OS to a list of common Chrome/Chromium binary paths in priority order.
var commonChromePaths = map[string][]string{
	"darwin": {
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	},
	"linux": {
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/snap/bin/chromium",
		"/usr/bin/google-chrome-beta",
	},
	"windows": {
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Users\` + "%USERNAME%" + `\AppData\Local\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\Chromium\Application\chrome.exe`,
	},
}

// FindChromeBinary attempts to locate a Chrome or Chromium binary on the current OS.
// Returns the path and true if found, empty string and false otherwise.
func FindChromeBinary() (string, bool) {
	// First try $PATH resolution — handles snap, brew, and custom installs
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium-browser", "chromium", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}

	// Fall back to well-known OS-specific paths
	paths, ok := commonChromePaths[runtime.GOOS]
	if !ok {
		return "", false
	}

	for _, p := range paths {
		if _, err := exec.LookPath(p); err == nil {
			return p, true
		}
		// LookPath requires PATH resolution for absolute paths to work differently on windows
		// Use a stat-like check via exec instead
		cmd := exec.Command(p, "--version")
		if err := cmd.Run(); err == nil {
			return p, true
		}
	}

	return "", false
}

package browser

import (
	"os"
	"os/exec"
	"runtime"
)

// chromeCandidate pairs a filesystem path with a human-readable browser display name.
type chromeCandidate struct {
	path string
	name string
}

// pathBinaries are names tried via exec.LookPath (handles $PATH, Homebrew, snap, etc.).
var pathBinaries = []chromeCandidate{
	{"google-chrome", "Google Chrome"},
	{"google-chrome-stable", "Google Chrome"},
	{"chromium-browser", "Chromium"},
	{"chromium", "Chromium"},
	{"brave-browser", "Brave Browser"},
	{"microsoft-edge", "Microsoft Edge"},
	{"chrome", "Google Chrome"},
}

// unixChromePaths are well-known binary locations on macOS and Linux.
// Paths may contain $HOME or other env vars expanded at runtime via os.ExpandEnv.
var unixChromePaths = map[string][]chromeCandidate{
	"darwin": {
		{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "Google Chrome"},
		{"/Applications/Chromium.app/Contents/MacOS/Chromium", "Chromium"},
		{"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary", "Google Chrome Canary"},
		{"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser", "Brave Browser"},
		{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", "Microsoft Edge"},
	},
	"linux": {
		{"/usr/bin/google-chrome", "Google Chrome"},
		{"/usr/bin/google-chrome-stable", "Google Chrome"},
		{"/usr/bin/chromium-browser", "Chromium"},
		{"/usr/bin/chromium", "Chromium"},
		{"/snap/bin/chromium", "Chromium"},
		{"/usr/bin/brave-browser", "Brave Browser"},
		{"/usr/bin/microsoft-edge", "Microsoft Edge"},
		{"/opt/google/chrome/chrome", "Google Chrome"},
		{"/usr/bin/google-chrome-beta", "Google Chrome Beta"},
	},
}

// FindChromeBinary attempts to locate a Chrome-compatible binary on the current OS.
//
// Discovery order:
//  1. $PATH resolution (handles Homebrew, snap, custom installs)
//  2. Windows registry (Chrome and Edge install their path in HKLM/HKCU — most reliable on Windows)
//  3. OS-specific well-known filesystem paths (env vars expanded at runtime)
//
// Returns (path, browserDisplayName, found). The display name is suitable for log output,
// e.g. "Google Chrome", "Microsoft Edge", "Brave Browser".
func FindChromeBinary() (path, name string, found bool) {
	// 1. PATH resolution — fastest and most portable
	for _, c := range pathBinaries {
		if p, err := exec.LookPath(c.path); err == nil {
			return p, c.name, true
		}
	}

	// 2. Windows registry (most authoritative source on Windows)
	if runtime.GOOS == "windows" {
		if p, n, ok := findChromeBinaryFromRegistry(); ok {
			return p, n, true
		}
	}

	// 3. Platform-specific well-known paths
	candidates := platformChromePaths() // Windows: dynamic env-var expansion; others: static map
	if candidates == nil {
		candidates = unixChromePaths[runtime.GOOS]
	}

	for _, c := range candidates {
		expanded := os.ExpandEnv(c.path)
		if _, err := os.Stat(expanded); err == nil {
			return expanded, c.name, true
		}
	}

	return "", "", false
}

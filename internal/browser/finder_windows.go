//go:build windows

package browser

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// windowsRegistryKeys are Chrome/Edge registry paths that store the install path.
// Chrome and Edge both register their binary under BLBeacon\path in HKLM/HKCU.
var windowsRegistryKeys = []struct {
	subkey string
	value  string
	name   string
}{
	{`SOFTWARE\Google\Chrome\BLBeacon`, "path", "Google Chrome"},
	{`SOFTWARE\Microsoft\Edge\BLBeacon`, "path", "Microsoft Edge"},
	{`SOFTWARE\WOW6432Node\Google\Chrome\BLBeacon`, "path", "Google Chrome"},
	{`SOFTWARE\WOW6432Node\Microsoft\Edge\BLBeacon`, "path", "Microsoft Edge"},
}

// findChromeBinaryFromRegistry searches HKLM and HKCU for Chrome/Edge install paths.
// Returns the path, browser display name, and true if a usable binary was found.
func findChromeBinaryFromRegistry() (path, name string, found bool) {
	for _, entry := range windowsRegistryKeys {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			k, err := registry.OpenKey(root, entry.subkey, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			val, _, err := k.GetStringValue(entry.value)
			k.Close()
			if err != nil || val == "" {
				continue
			}
			if _, statErr := os.Stat(val); statErr == nil {
				return val, entry.name, true
			}
		}
	}
	return "", "", false
}

// platformChromePaths returns Windows-specific Chrome candidate paths, built dynamically
// from environment variables to handle custom install locations.
// Returned paths are already expanded (no further ExpandEnv needed).
func platformChromePaths() []chromeCandidate {
	// These are the three relevant root directories on Windows
	type root struct {
		dir  string
		note string
	}
	roots := []root{
		{os.Getenv("PROGRAMFILES"), ""},
		{os.Getenv("PROGRAMFILES(X86)"), " x86"},
		{os.Getenv("LOCALAPPDATA"), " local"},
	}

	type relEntry struct {
		rel  string
		name string
	}
	entries := []relEntry{
		{filepath.Join("Google", "Chrome", "Application", "chrome.exe"), "Google Chrome"},
		{filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"), "Microsoft Edge"},
		{filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"), "Brave Browser"},
	}

	var candidates []chromeCandidate
	for _, r := range roots {
		if r.dir == "" {
			continue
		}
		for _, e := range entries {
			candidates = append(candidates, chromeCandidate{
				path: filepath.Join(r.dir, e.rel),
				name: e.name,
			})
		}
	}
	return candidates
}

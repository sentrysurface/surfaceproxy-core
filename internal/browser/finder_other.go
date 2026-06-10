//go:build !windows

package browser

// findChromeBinaryFromRegistry is a no-op on non-Windows systems.
// The real implementation lives in finder_windows.go.
func findChromeBinaryFromRegistry() (path, name string, found bool) {
	return "", "", false
}

// platformChromePaths returns nil on non-Windows systems, signalling FindChromeBinary
// to use the unixChromePaths map instead.
func platformChromePaths() []chromeCandidate {
	return nil
}

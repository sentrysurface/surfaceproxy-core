package version

import "fmt"

var (
	Version   = "0.1.0-alpha"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// BuildInfo returns a formatted string containing build metadata.
func BuildInfo() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}

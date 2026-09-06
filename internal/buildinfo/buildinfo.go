// Package buildinfo owns version metadata shared by the two independently
// composed executables.
package buildinfo

import "fmt"

// These values are replaced by release builds with -ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}

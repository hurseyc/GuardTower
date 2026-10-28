// Package version holds build metadata injected via -ldflags.
package version

// Set at build time: -ldflags "-X github.com/hurseyc/guardtower/internal/version.Version=v1.2.3 ..."
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

package version

import "os"

// Build-time variables set via ldflags
var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

// Info represents version information
type Info struct {
	Version   string `json:"version" example:"v1.0.0"`
	BuildDate string `json:"build_date" example:"2025-01-01T12:00:00Z"`
	GitCommit string `json:"git_commit" example:"abc123"`
}

// Get returns the current version information. VERSION env var overrides the
// ldflags value so production deployments can inject the release tag without
// rebuilding the image promoted from staging.
func Get() Info {
	v := version
	if env := os.Getenv("VERSION"); env != "" {
		v = env
	}
	return Info{
		Version:   v,
		BuildDate: buildDate,
		GitCommit: gitCommit,
	}
}

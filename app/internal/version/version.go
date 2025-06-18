package version

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

// Get returns the current version information
func Get() Info {
	return Info{
		Version:   version,
		BuildDate: buildDate,
		GitCommit: gitCommit,
	}
}

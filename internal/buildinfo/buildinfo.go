// Package buildinfo exposes version metadata injected with -ldflags -X.
package buildinfo

// Set at link time by the Dockerfile.
var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

// BuildInfo is the wire shape of one component's build metadata.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}

// Info returns the build metadata of this binary.
func Info() BuildInfo {
	return BuildInfo{Version: Version, Commit: Commit, BuiltAt: BuiltAt}
}

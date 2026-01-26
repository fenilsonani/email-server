package version

import "runtime"

// Injected at build time via -ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Version   string
	Commit    string
	BuildTime string
	GoVersion string
}

func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
	}
}

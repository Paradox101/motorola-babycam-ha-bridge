// Package buildinfo reports which build of the bridge is running.
//
// The release workflow stamps Version at link time. Without a stamp the value
// is recovered from the Go build info, so a `go build` from a checkout still
// identifies itself by module version or VCS revision instead of claiming to
// be an unknown build.
package buildinfo

import (
	"runtime/debug"
	"strings"
	"sync"
)

// Version is set with -ldflags "-X .../internal/buildinfo.Version=v1.2.3".
var Version = ""

var resolve = sync.OnceValue(func() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
		return version
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision == "" {
		return "devel"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified == "true" {
		return "devel+" + revision + "-dirty"
	}
	return "devel+" + revision
})

// String returns the resolved build version. It never returns an empty string.
func String() string { return resolve() }

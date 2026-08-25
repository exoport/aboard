// version.go — which binary is actually serving.
//
// The top bar shows this, and the point of showing it is that it must never be a
// constant somebody has to remember to bump: those lie eventually, and a version
// string that lies is worse than one that admits it does not know.
package aboard

import (
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// version is overridable at link time (`-ldflags "-X ...aboard.version=v1.2.3"`)
// for a release build. Left empty for everything else, which is the normal case.
var version string

// Version resolves the build identity in three steps, most authoritative first:
// the linker stamp, then Go's own build info for THIS module, then "dev".
//
// Go stamps the VCS revision into a plain `go build`, so an unstamped local build
// still reports the commit it came from, with "+dirty" when the tree had
// uncommitted changes — which, on this project, it usually does.
func Version() string {
	if v := strings.TrimSpace(version); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	rev, dirty := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			rev = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if rev == "" {
		// `go install module@version` has no VCS settings but does carry a
		// module version, which is the only truth available there.
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		return rev + "+dirty"
	}
	return rev
}

// BuildStamp is when the running binary was written — the answer to "am I looking
// at the code I just built?", which the revision alone cannot give on a dirty
// tree. Best effort: an empty string is a normal outcome.
func BuildStamp() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	info, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

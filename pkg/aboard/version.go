// version.go — which binary is actually serving.
//
// The top bar shows this, and the point of showing it is that it must never be a
// constant somebody has to remember to bump: those lie eventually, and a version
// string that lies is worse than one that admits it does not know.
//
// Three EXPORTED package variables, because that is what the linker can reach.
// The Makefile and .goreleaser.yaml both pass
//
//	-X github.com/exoport/aboard/pkg/aboard.Version=…
//	-X github.com/exoport/aboard/pkg/aboard.BuildDate=…
//	-X github.com/exoport/aboard/pkg/aboard.GitCommit=…
//
// and `-X` against a symbol that does not exist is SILENTLY IGNORED by the Go
// linker — no warning, no error, just a binary that reports "dev" forever. So
// the names, the package and the file location are a contract with the build
// tooling, not an implementation detail: they mirror ape's exactly
// (`Version`/`BuildDate`/`GitCommit`, exported, in `package aboard`) so a
// released binary and a `make build` binary report identity by the same rules.
//
// Everything reads the identity through Build(); nothing reads these variables
// directly, so an unstamped build (go run, go install, a plain go build) still
// answers with whatever Go itself recorded.

package aboard

import (
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// devVersion is what a binary with no provenance at all calls itself. Named
// because it is also one of the placeholder strings realStamp rejects, and the
// two must be the same word or a `make build` in a tree with no git would report
// a version the resolver had already decided was meaningless.
const devVersion = "dev"

// modulePath is this module's own path, and it is here because an embedded board
// has to be able to FIND ITSELF in somebody else's build info.
//
// A constant rather than something derived: the value has to survive being read
// out of a binary that is not ours, where nothing else in the process knows or
// cares what this module is called. `TestTheModulePathMatchesGoMod` compares it
// against `go.mod`, so it cannot drift from the thing it names.
const modulePath = "github.com/exoport/aboard"

// The linker stamps these. Left empty for every build that is not a release or a
// `make build`, which is the normal case.
var (
	Version   string
	BuildDate string
	GitCommit string
)

// BuildIdentity is the provenance of the running binary: what it calls itself,
// when it was built, and which commit it came from.
//
// A struct rather than three functions because `aboard version --output-format
// json` has to say the same things the human line does, and the only way to
// guarantee that is for both to render one value.
type BuildIdentity struct {
	Version   string `json:"version"`
	BuildDate string `json:"buildDate,omitempty"`
	GitCommit string `json:"gitCommit,omitempty"`
}

// Build resolves the identity in four steps, most authoritative first: the
// linker stamps, then Go's own build info for THIS module (VCS revision, then
// module version), then "dev".
//
// "For THIS module" is the whole subtlety, and it is why this file differs from
// ape's `internal/buildident`, which it otherwise mirrors. ape is always the main
// module — it is a binary and nothing embeds it — so `info.Main` IS its identity.
// aboard is a LIBRARY that another CLI mounts (`ape aboard <cmd>`), and there
// `info.Main` is the host: its version, its commit, its build time. Reported
// unchanged, an embedded board would call itself by the host's version, silently
// and plausibly. Measured on 2026-08-28 by mounting the tree in a stand-in host:
// it reported `aboard dev`, and a host installed at a tag would have made it
// report the HOST's tag as aboard's own.
//
// Go stamps the VCS revision into a plain `go build`, so an unstamped local build
// still reports the commit it came from, with "+dirty" when the tree had
// uncommitted changes — which, on this project, it usually does. "dev" is what a
// binary with NO build info at all reports, which is rarer than three of this
// project's documents used to claim.
func Build() BuildIdentity {
	info, ok := debug.ReadBuildInfo()
	return resolveBuild(Version, BuildDate, GitCommit, info, ok)
}

// resolveBuild is Build with its one input made an argument.
//
// Split out so the `go install module@version` case can be TESTED. It cannot be
// tested through Build(): a `go test` binary carries no VCS settings and no
// module version, so every unstamped path collapses to "dev" there — which is
// exactly how three documents came to claim that a `go install` build reports
// `dev`. It reports the module version (verified with a real file-proxy
// `go install`: 0.1.0), or a VCS pseudo-version for a plain `go build`.
func resolveBuild(version, buildDate, gitCommit string, info *debug.BuildInfo, ok bool) BuildIdentity {
	// Placeholders are cleared FIRST, not tested for later. The Makefile stamps
	// `dev` and `unknown` in a tree with no git, so those strings arrive through
	// the same channel as real provenance — and a value only ever gets ONE chance
	// to be filled in below. Leaving them in place is how `GitCommit: "unknown"`
	// survived to be reported as a commit hash.
	id := BuildIdentity{
		Version:   realStamp(version),
		BuildDate: realStamp(buildDate),
		GitCommit: realStamp(gitCommit),
	}

	if !ok || info == nil {
		if id.Version == "" {
			id.Version = devVersion
		}
		return id
	}

	// Embedded in somebody else's binary. Everything in `info.Main` and in the VCS
	// settings describes the HOST, so the only honest source for a version is this
	// module's own entry in the dependency list — and there is no honest source for
	// a commit at all, because the source came from a module zip rather than from a
	// checkout. An empty GitCommit is the right answer, not a gap to fill.
	if hosted(info) {
		return hostedBuild(id, info)
	}

	rev, dirty := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			rev = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		case "vcs.time":
			if id.BuildDate == "" {
				id.BuildDate = setting.Value
			}
		}
	}
	if id.GitCommit == "" && rev != "" {
		id.GitCommit = rev
	}

	if id.Version != "" {
		return id
	}
	// `go install module@version` has no VCS settings but does carry a module
	// version, which is the only truth available there. Since Go 1.24 a plain
	// `go build` also fills this in as a VCS-derived pseudo-version, so it is
	// tried before the bare revision.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		id.Version = strings.TrimPrefix(v, "v")
		return id
	}
	if rev == "" {
		id.Version = devVersion
		return id
	}
	short := rev
	if len(short) > 7 {
		short = short[:7]
	}
	if dirty {
		short += "+dirty"
	}
	id.Version = short
	return id
}

// hosted reports whether this module is a dependency of the running binary
// rather than the binary itself.
//
// An empty Main.Path means "no module information", which is a plain `go test`
// binary and every hand-built BuildInfo in the tests — standalone, not hosted.
// Defaulting the unknown case to standalone is deliberate: it keeps the answer
// wrong only in the direction that was already true before this existed.
func hosted(info *debug.BuildInfo) bool {
	return info.Main.Path != "" && info.Main.Path != modulePath
}

// hostedBuild is the identity of an embedded board.
//
// BuildDate is the one field the host can legitimately answer: the binary on disk
// IS the host's binary, so when the host was built is when this board was built.
// The version comes from our own dependency entry — through `Replace`, so a
// `replace` directive during development names the replacement rather than the
// requirement it stood in for.
func hostedBuild(id BuildIdentity, info *debug.BuildInfo) BuildIdentity {
	if id.BuildDate == "" {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.time" {
				id.BuildDate = setting.Value
				break
			}
		}
	}
	if id.Version == "" {
		id.Version = depVersion(info, modulePath)
	}
	if id.Version == "" {
		id.Version = devVersion
	}
	return id
}

// depVersion is the version one dependency was built at, or "" when there is
// nothing true to say — an unreplaced module with no version, or a `replace`
// pointing at a directory, which carries "(devel)" and means exactly "dev".
func depVersion(info *debug.BuildInfo, path string) string {
	for _, dep := range info.Deps {
		if dep == nil || dep.Path != path {
			continue
		}
		v := dep.Version
		if dep.Replace != nil {
			v = dep.Replace.Version
		}
		if v == "" || v == "(devel)" {
			return ""
		}
		return strings.TrimPrefix(v, "v")
	}
	return ""
}

// VersionString is the one-word identity: what the top bar shows, what /health
// and the instance record carry, and what `--version` prints.
func VersionString() string { return Build().Version }

// BuildStamp is when the running binary was written — the answer to "am I looking
// at the code I just built?", which the revision alone cannot give on a dirty
// tree.
//
// The resolved BuildDate first (a release stamp, or Go's own vcs.time), and only
// then the executable's modification time. Best effort: an empty string is a
// normal outcome, so every consumer treats it as optional.
func BuildStamp() string {
	if d := Build().BuildDate; d != "" {
		return d
	}
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

// The placeholders a build system writes when it has nothing real to stamp. All
// of them are recognised rather than picking a winner and silently breaking the
// other, which is what a project inheriting ape's Makefile would hit.
const (
	unstampedUnknown = "unknown"
	unstampedNone    = "none"
)

// realStamp reduces a stamped value to "" unless it is real provenance. The
// Makefile falls back to `dev` and `unknown` in a tree with no git, and ape's
// two commands historically used different placeholders — all of them are
// recognised rather than picking a winner and silently breaking the other.
func realStamp(v string) string {
	switch trimmed := strings.TrimSpace(v); trimmed {
	case "", devVersion, unstampedUnknown, unstampedNone:
		return ""
	default:
		return trimmed
	}
}

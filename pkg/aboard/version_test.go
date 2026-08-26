package aboard

import (
	"runtime/debug"
	"strings"
	"testing"
)

// The linker stamps Version, BuildDate and GitCommit by NAME. `-X` against a
// symbol that does not exist is silently ignored by the Go linker — no warning,
// no error, just a binary that reports "dev" for its whole life — so the names
// and the package are a contract with the Makefile and .goreleaser.yaml, both of
// which write:
//
//	-X github.com/exoport/aboard/pkg/aboard.Version=…
//
// A test cannot re-link itself, so what it can do is prove the symbols are
// package-level strings in THIS package that the resolver actually reads.
func TestLdflagSymbolsAreRead(t *testing.T) {
	oldV, oldD, oldC := Version, BuildDate, GitCommit
	t.Cleanup(func() { Version, BuildDate, GitCommit = oldV, oldD, oldC })

	Version, BuildDate, GitCommit = "v1.2.3", "2026-08-25T00:00:00Z", "abc123def456"

	id := Build()
	if id.Version != "v1.2.3" {
		t.Errorf("Version = %q, want the stamped value", id.Version)
	}
	if id.BuildDate != "2026-08-25T00:00:00Z" {
		t.Errorf("BuildDate = %q, want the stamped value", id.BuildDate)
	}
	if id.GitCommit != "abc123def456" {
		t.Errorf("GitCommit = %q, want the stamped value", id.GitCommit)
	}
	if VersionString() != "v1.2.3" {
		t.Errorf("VersionString() = %q", VersionString())
	}
	if BuildStamp() != "2026-08-25T00:00:00Z" {
		t.Errorf("BuildStamp() = %q, want the stamped build date", BuildStamp())
	}
}

// The Makefile falls back to `dev` and `unknown` in a tree with no git, and
// stamps them. A resolver that stopped at those placeholders would report "dev"
// while Go's own build info held the real revision.
func TestPlaceholderStampsFallThrough(t *testing.T) {
	oldV, oldD, oldC := Version, BuildDate, GitCommit
	t.Cleanup(func() { Version, BuildDate, GitCommit = oldV, oldD, oldC })

	Version, BuildDate, GitCommit = "dev", "unknown", "unknown"

	id := Build()
	if id.Version == "" {
		t.Fatal("Version resolved to empty")
	}
	// Under `go test` the binary carries build info, so this should be something
	// better than the placeholder. If it is not, it must at least be the honest
	// fallback and never the string "unknown".
	if id.GitCommit == "unknown" || id.BuildDate == "unknown" {
		t.Errorf("a placeholder was passed through as provenance: %+v", id)
	}
	if strings.TrimSpace(id.Version) == "" {
		t.Error("Version is blank")
	}
}

// Nothing stamped at all still answers, and never with an empty string: a
// version that lies is worse than one that admits it does not know, and an empty
// one in the top bar reads as a rendering bug.
func TestUnstampedBuildStillReports(t *testing.T) {
	oldV, oldD, oldC := Version, BuildDate, GitCommit
	t.Cleanup(func() { Version, BuildDate, GitCommit = oldV, oldD, oldC })

	Version, BuildDate, GitCommit = "", "", ""
	if got := VersionString(); got == "" {
		t.Fatal("an unstamped build reports an empty version")
	}
}

// The claim three documents used to make — "a `go install` build reports
// `Version=dev`" — was false, and it was load-bearing: verify.md used it as the
// ARGUMENT for taking a signed archive over `go install`. Verified with a real
// file-proxy `go install github.com/exoport/aboard/cmd/aboard@v0.1.0`, whose
// binary reports `aboard 0.1.0`.
//
// This has to go through resolveBuild rather than Build: a `go test` binary
// carries neither VCS settings nor a module version, so every unstamped path
// collapses to "dev" under test — which is precisely why nothing caught the
// false claim.
func TestUnstampedProvenanceComesFromGoBuildInfo(t *testing.T) {
	cases := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{
			name: "go install module@version reports the module version, not dev",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			ok:   true,
			want: "0.1.0",
		},
		{
			name: "a plain go build reports the VCS-derived pseudo-version",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260826031230-f67e682b8f8a"}},
			ok:   true,
			want: "0.0.0-20260826031230-f67e682b8f8a",
		},
		{
			name: "with a revision and no module version, the short commit",
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "f67e682b8f8a7cc"}},
			},
			ok:   true,
			want: "f67e682",
		},
		{
			name: "a dirty tree says so",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "f67e682b8f8a7cc"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			ok:   true,
			want: "f67e682+dirty",
		},
		{
			name: "only a binary with no build info at all reports dev",
			info: nil,
			ok:   false,
			want: devVersion,
		},
		{
			name: "build info with nothing in it reports dev too",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			ok:   true,
			want: devVersion,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveBuild("", "", "", tc.info, tc.ok)
			if got.Version != tc.want {
				t.Errorf("Version = %q, want %q", got.Version, tc.want)
			}
		})
	}
}

// The linker stamp still beats everything Go recorded: a released binary must
// report its tag, not the pseudo-version of the commit it was cut from.
func TestALinkerStampBeatsBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.0.0-20260826031230-f67e682b8f8a"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "f67e682b8f8a7cc"}},
	}
	if got := resolveBuild("v1.2.3", "", "", info, true); got.Version != "v1.2.3" {
		t.Errorf("Version = %q, want the stamp", got.Version)
	}
}

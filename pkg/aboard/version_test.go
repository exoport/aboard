package aboard

import (
	"os"
	"path/filepath"
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
// EMBEDDED: the board is a dependency of somebody else's binary.
//
// Every row here reports the HOST in `Main` and the VCS settings, which is what
// Go actually records when `ape aboard` runs. Before 2026-08-28 the resolver read
// those as its own and an embedded board called itself by the host's version —
// found by mounting the tree in a stand-in host, not by any test, because every
// hand-built BuildInfo in this file left Main.Path empty and so looked standalone.
func TestAHostedBoardReportsItsOwnModuleVersion(t *testing.T) {
	const host = "github.com/exoport/apex_process_ape"

	cases := []struct {
		name          string
		info          *debug.BuildInfo
		want          string
		wantNoCommit  bool
		wantBuildDate string
	}{
		{
			name: "the host installed at a tag does not lend the board its version",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: host, Version: "v1.2.3"},
				Deps: []*debug.Module{{Path: modulePath, Version: "v0.1.0"}},
			},
			want:         "0.1.0",
			wantNoCommit: true,
		},
		{
			name: "the host built from a checkout does not lend the board its commit",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: host, Version: "(devel)"},
				Deps: []*debug.Module{{Path: modulePath, Version: "v0.2.0"}},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "f67e682b8f8a9c1d2e3f4a5b6c7d8e9f00112233"},
					{Key: "vcs.time", Value: "2026-08-28T12:00:00Z"},
				},
			},
			want:          "0.2.0",
			wantNoCommit:  true,
			wantBuildDate: "2026-08-28T12:00:00Z",
		},
		{
			name: "a replace directive during development is honestly dev",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: host, Version: "(devel)"},
				Deps: []*debug.Module{{
					Path:    modulePath,
					Version: "v0.1.0",
					Replace: &debug.Module{Path: "../aboard", Version: "(devel)"},
				}},
			},
			want:         devVersion,
			wantNoCommit: true,
		},
		{
			name: "a host that does not list us at all is dev, never the host's version",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: host, Version: "v9.9.9"},
				Deps: []*debug.Module{{Path: "github.com/spf13/cobra", Version: "v1.10.1"}},
			},
			want:         devVersion,
			wantNoCommit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveBuild("", "", "", tc.info, true)
			if got.Version != tc.want {
				t.Errorf("Version = %q, want %q", got.Version, tc.want)
			}
			if tc.wantNoCommit && got.GitCommit != "" {
				t.Errorf("GitCommit = %q, want empty — an embedded board has no checkout, "+
					"and the host's commit is not its provenance", got.GitCommit)
			}
			if tc.wantBuildDate != "" && got.BuildDate != tc.wantBuildDate {
				t.Errorf("BuildDate = %q, want %q — the binary on disk IS the host's, so its "+
					"build time is the board's too", got.BuildDate, tc.wantBuildDate)
			}
		})
	}
}

// A stamped build stays stamped even when embedded: if a host goes to the trouble
// of passing our -X flags, that is more authoritative than any inference.
func TestALinkerStampBeatsTheHostsBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/exoport/apex_process_ape", Version: "v1.2.3"},
		Deps: []*debug.Module{{Path: modulePath, Version: "v0.1.0"}},
	}
	if got := resolveBuild("9.9.9", "", "", info, true).Version; got != "9.9.9" {
		t.Errorf("Version = %q, want 9.9.9", got)
	}
}

// The module path is a constant that has to keep naming this module.
func TestTheModulePathMatchesGoMod(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(body)), "\n")
	want := strings.TrimSpace(strings.TrimPrefix(first, "module"))
	if want != modulePath {
		t.Errorf("go.mod says module %q, version.go says %q — an embedded board looks itself up "+
			"by this string and would silently stop finding itself", want, modulePath)
	}
}

func TestALinkerStampBeatsBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.0.0-20260826031230-f67e682b8f8a"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "f67e682b8f8a7cc"}},
	}
	if got := resolveBuild("v1.2.3", "", "", info, true); got.Version != "v1.2.3" {
		t.Errorf("Version = %q, want the stamp", got.Version)
	}
}

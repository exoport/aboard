package aboard

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// "The bingo pin, reached through make, is authoritative everywhere" is a rule the
// human decided and nothing enforced — which is how the repo ended up with two
// copies of the linter in the first place, one behind `make lint` and one behind a
// pre-commit hook, disagreeing by eleven findings over a tree that had not changed.
//
// A rule about which BINARY runs cannot be checked by running it: both copies are
// called `golangci-lint` and both exit 0 on a good day. So the check is on the
// files that choose it. Anything that names a Go tool directly, rather than a make
// target, is a second copy waiting to happen.
func TestNoGateInvokesAGoToolFromPATH(t *testing.T) {
	// The tools whose version this repo pins. `make` is how each of them is
	// reached; `bingo` itself is exempt, since moving a pin is the one job that
	// cannot go through the pin.
	pinned := regexp.MustCompile(`\b(golangci-lint|gofumpt|govulncheck|goreleaser)\b`)

	// The lines that RUN something. `id:` is in the list for the pre-commit
	// config ONLY, and it is the line that matters most: the violation this repo
	// actually had was `id: golangci-lint-mod` — a hook from a third-party repo,
	// with no command written anywhere in this tree, whose whole body is a call
	// to $PATH's golangci-lint. Checking only run/entry/args would let exactly
	// that shape back in, which is a check that passes over the bug it was
	// written for. `id:` is NOT checked in the workflows, where it names a step
	// and a step legitimately gets named after the tool it drives through make.
	runLines := map[string][]string{
		"../../.pre-commit-config.yaml":       {"run:", "entry:", "args:", "id:"},
		"../../.github/workflows/ci.yml":      {"run:", "entry:", "args:"},
		"../../.github/workflows/release.yml": {"run:", "entry:", "args:"},
	}
	paths := make([]string, 0, len(runLines))
	for path := range runLines {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			// Comments are where the reasoning lives, and this repo's reasoning
			// names these tools constantly. A check that fired on its own
			// documentation would get muted.
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			interesting := false
			for _, prefix := range runLines[path] {
				if strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, "- "+prefix) {
					interesting = true
					break
				}
			}
			if !interesting {
				continue
			}
			_, cmd, _ := strings.Cut(trimmed, ":")
			cmd = strings.TrimSpace(cmd)
			// `make govulncheck` names a tool because the TARGET is named after
			// it, which is the point rather than the violation.
			if strings.HasPrefix(cmd, "make ") || cmd == "make" {
				continue
			}
			if pinned.MatchString(cmd) {
				t.Errorf("%s:%d reaches a pinned tool by name instead of through make: %s",
					filepath.Base(path), i+1, trimmed)
			}
		}
	}
}

// The two gates the hook runs are the two gates the ladder runs. Asserted by name
// rather than left to a reader, because the failure mode is silent: a hook that
// stops running one of them still passes, and the first anyone hears about it is a
// red CI on somebody else's push.
func TestThePreCommitHooksAreTheMakeGates(t *testing.T) {
	body, err := os.ReadFile("../../.pre-commit-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"entry: make lint", "entry: make fmt-check"} {
		if !strings.Contains(string(body), want) {
			t.Errorf(".pre-commit-config.yaml no longer has `%s` — the hook and the ladder have to be the same commands", want)
		}
	}
}

// goreleaser is the one pin written in two places: `.bingo/goreleaser.mod` decides
// what `make snapshot` runs, and `goreleaser-action`'s `version:` decides what the
// RELEASE runs. A snapshot proves nothing about a release built by a different
// program, and the difference only ever shows up in the run that publishes — which
// is the run nobody gets to retry cleanly.
func TestTheReleaseWorkflowPinsTheSameGoreleaserAsBingo(t *testing.T) {
	vars, err := os.ReadFile("../../.bingo/Variables.mk")
	if err != nil {
		t.Fatal(err)
	}
	pin := regexp.MustCompile(`GORELEASER := \$\(GOBIN\)/goreleaser-(v\d\S*)`).FindSubmatch(vars)
	if pin == nil {
		t.Fatal("no GORELEASER line in .bingo/Variables.mk — bingo's generator changed shape; update this check rather than dropping it")
	}

	workflow, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	used := regexp.MustCompile(`(?m)^\s*version:\s*(v\d\S*)`).FindSubmatch(workflow)
	if used == nil {
		t.Fatal("no `version:` under goreleaser-action in release.yml — the action must be pinned, never `latest`")
	}
	if !bytes.Equal(pin[1], used[1]) {
		t.Errorf("bingo pins goreleaser %s, release.yml runs %s — `bingo get` moves a pin and this workflow together",
			pin[1], used[1])
	}
}

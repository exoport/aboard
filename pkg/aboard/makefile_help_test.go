package aboard

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// `make help` is the first command CLAUDE.md offers a session ("available
// targets"), so a target it omits is a target that does not exist as far as the
// reader is concerned. It omitted exactly one: `e2e`, the browser suite — the
// help rule's grep was anchored on `^[a-zA-Z_-]+:` and the `2` matched nothing.
// Twenty-three documented targets printed, the twenty-fourth did not, and
// nothing said so because the rule's output is a list with no expected length.
//
// Two halves, and both had to be built carefully or they assert nothing:
//
//   - The pattern is READ OUT OF the help recipe, never copied into this file. A
//     copy is the one thing that cannot catch a change to the rule: with the
//     current pattern written here as a literal, this half passes against the
//     BROKEN Makefile, which is exactly the run it exists for.
//   - `make help` is then run, and its output is matched on the target COLUMN
//     the awk prints, not with a substring search. `strings.Contains(out,
//     "test")` is satisfied by the line for `test-cover`, and `"check"` by
//     `docs-check`, so a substring search calls two of the twenty-four listed
//     when they are not.
func TestEveryDocumentedMakeTargetIsListedByHelp(t *testing.T) {
	src, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	// Every line that DECLARES a target and documents it. Deliberately looser
	// than the help rule's own pattern — that is the thing under test, so it
	// cannot also be the thing that decides what should have been listed.
	declared := regexp.MustCompile(`(?m)^([^\s:#=]+):[^=#\n]*## `)

	// The pattern the help rule actually greps with, lifted from the recipe.
	rule := regexp.MustCompile(`@grep -hE '([^']+)'`).FindSubmatch(src)
	if rule == nil {
		t.Fatal("the help recipe no longer looks like `@grep -hE '<pattern>'` — this test reads its pattern from there rather than keeping a copy, so update the extraction, do not paste the pattern in here")
	}
	listed, err := regexp.Compile("(?m)" + string(rule[1]))
	if err != nil {
		t.Fatalf("the help rule's pattern %q does not compile in Go: %v", rule[1], err)
	}

	matches := declared.FindAllStringSubmatch(string(src), -1)
	targets := make([]string, 0, len(matches))
	for _, m := range matches {
		targets = append(targets, m[1])
	}
	if len(targets) < 20 {
		t.Fatalf("found only %d documented targets in the Makefile — the parse is wrong, not the Makefile", len(targets))
	}

	for _, target := range targets {
		found := false
		for line := range strings.SplitSeq(string(src), "\n") {
			if strings.HasPrefix(line, target+":") && listed.MatchString(line+"\n") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("`%s` is documented with a ## comment but the help rule's grep does not match its line, so `make help` never mentions it", target)
		}
	}

	// And the rule as actually run. Skipped rather than failed when make is
	// absent: this is a repo-hygiene check, not a property of the binary, and a
	// machine with no make can still be one where the Go suite must pass.
	if _, err := exec.LookPath("make"); err != nil {
		t.Logf("make is not on PATH; the static half of this test still ran")
		return
	}
	out, err := exec.Command("make", "-C", "../..", "help").Output()
	if err != nil {
		t.Fatalf("running `make help`: %v", err)
	}
	// The awk prints "  <colour><target padded to 18><reset> <description>", so
	// the target is the first word of a line and nothing else on that line can
	// be mistaken for it.
	printed := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(stripANSI(line))
		if len(fields) > 0 {
			printed[fields[0]] = true
		}
	}
	for _, target := range targets {
		if !printed[target] {
			t.Errorf("`make help` does not list `%s`", target)
		}
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

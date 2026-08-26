package aboard

import (
	"os"
	"os/exec"
	"path/filepath"
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

// `make fmt-check` is a GATE, and a gate is only worth its failure modes. The
// recipe captures gofumpt's output and fails when the list is non-empty — which
// on its own is green whenever the tool itself fails, because `gofumpt -l`
// prints nothing when it cannot parse a file. Measured: a file with a syntax
// error in it passed this gate, printing "fmt-check ok" and exiting 0.
//
// The recipe's shell is LIFTED OUT OF the Makefile rather than restated here,
// for the same reason the help test reads the help rule's pattern: a copy passes
// against the broken Makefile, which is the one run it exists for. $(GOFUMPT) is
// then pointed at a stub, so the three cases are exactly the three answers the
// real tool can give.
func TestFmtCheckFailsWhenGofumptItselfFails(t *testing.T) {
	src, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	recipe := fmtCheckRecipe(t, string(src))

	for _, tc := range []struct {
		name    string
		stub    string
		wantErr bool
	}{
		// gofumpt could not parse something. It prints a diagnostic on stderr
		// and lists no files, so "no files listed" must not be read as "clean".
		{"the tool fails", "#!/bin/sh\necho 'x.go:3:16: expected )' >&2\nexit 2\n", true},
		// The tree needs formatting.
		{"a file needs formatting", "#!/bin/sh\necho pkg/aboard/x.go\nexit 0\n", true},
		// Clean.
		{"the tree is clean", "#!/bin/sh\nexit 0\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := filepath.Join(t.TempDir(), "gofumpt-stub")
			if err := os.WriteFile(stub, []byte(tc.stub), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-c", strings.ReplaceAll(recipe, "@GOFUMPT@", stub))
			out, err := cmd.CombinedOutput()
			if tc.wantErr && err == nil {
				t.Errorf("the recipe exited 0 over a stub that %s — the gate is green having checked nothing:\n%s", tc.name, out)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("the recipe failed over a clean tree: %v\n%s", err, out)
			}
		})
	}
}

// fmtCheckRecipe returns the fmt-check recipe as a shell script, with $(GOFUMPT)
// replaced by the marker @GOFUMPT@ and make's `$$` unescaped back to `$`.
func fmtCheckRecipe(t *testing.T, src string) string {
	t.Helper()
	lines := strings.Split(src, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "fmt-check:") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatal("no `fmt-check:` target in the Makefile — this test reads the recipe from there rather than keeping a copy, so update the extraction, do not paste the recipe in here")
	}
	body := []string{}
	for _, line := range lines[start:] {
		if !strings.HasPrefix(line, "\t") {
			break
		}
		body = append(body, strings.TrimPrefix(strings.TrimPrefix(line, "\t"), "@"))
	}
	if len(body) == 0 {
		t.Fatal("the fmt-check target has no recipe lines")
	}
	script := strings.Join(body, "\n")
	script = strings.ReplaceAll(script, "$(GOFUMPT)", "@GOFUMPT@")
	// make's escape for a literal `$` in a recipe.
	return strings.ReplaceAll(script, "$$", "$")
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `make docs-cli` regenerates docs/reference/cli.md on every run. If the output
// moved — a timestamp, a map iteration, an unsorted flag list — the file would
// show up in every commit and stop being read, which is the failure mode a
// generated reference has.
func TestGenDocsIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")

	for _, out := range []string{a, b} {
		if _, _, err := run(t, "gen-docs", "--out", out); err != nil {
			t.Fatalf("gen-docs --out %s: %v", out, err)
		}
	}
	first, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("two gen-docs runs produced different files")
	}

	body := string(first)
	// Every user-facing command has a section, and the hidden maintainer ones do
	// not: the reference is the surface, not the toolbox.
	for _, want := range []string{
		"## aboard", "## aboard serve", "## aboard init",
		"## aboard recipes list", "## aboard recipes show",
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("no section for %q", want)
		}
	}
	for _, unwanted := range []string{"gen-docs", "recipes index"} {
		if strings.Contains(body, "## aboard "+unwanted) {
			t.Errorf("the hidden command %q reached the reference", unwanted)
		}
	}
	// The root's persistent flags reach every command's "Global flags" table,
	// which is where a reader looks for --name after reading about `status`.
	if !strings.Contains(body, "`--name`") {
		t.Error("the reference never documents --name")
	}
}

// --out - writes to stdout, which is what makes the command usable without
// clobbering a file while you look at what it would write.
func TestGenDocsToStdout(t *testing.T) {
	out, _, err := run(t, "gen-docs", "--out", "-")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# aboard CLI reference\n") {
		t.Errorf("unexpected output:\n%.80s", out)
	}
}

// And the file in the tree has to MATCH the tree that generates it.
//
// Determinism above is only half the property: `make docs-cli` produced a stable
// file, and nothing anywhere noticed when the committed copy stopped being that
// file. It happened immediately — `serve`'s Long gained two sentences about
// --base-path validation in the same change that added the validation, the
// generated reference was not regenerated, and the whole suite stayed green with
// a reference that described a flag the binary no longer had.
//
// The recipe index got this gate for the same reason (see caps_test.go); the CLI
// reference was then the last generated artifact without one. The remedy is in
// the failure message because that is where somebody reads it.
func TestTheGeneratedCLIReferenceIsNotStale(t *testing.T) {
	want, _, err := run(t, "gen-docs", "--out", "-")
	if err != nil {
		t.Fatalf("gen-docs --out -: %v", err)
	}
	// From this package to the checkout: the generated file lives in the repo,
	// not in the embedded tree, so a test that checks it has to say where.
	path := filepath.Join("..", "..", "..", "docs", "reference", "cli.md")
	raw, err := os.ReadFile(path) //nolint:gosec // a fixed path inside this checkout
	if err != nil {
		t.Fatalf("reading the generated CLI reference: %v", err)
	}
	got := string(raw)
	if got == want {
		return
	}
	t.Errorf("docs/reference/cli.md no longer matches the command tree — run `make docs-cli` and commit it\nfirst difference: %s", firstDifference(got, want))
}

// firstDifference names the line that moved, so the failure above is actionable
// without running a diff by hand.
func firstDifference(got, want string) string {
	g := strings.Split(got, "\n")
	w := strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return fmt.Sprintf("line %d\n  in the file:  %q\n  generated:    %q", i+1, g[i], w[i])
		}
	}
	return fmt.Sprintf("the file has %d lines, the command tree generates %d", len(g), len(w))
}

package cli

import (
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

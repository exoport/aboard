package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

// runHosted executes one command line and returns everything it wrote.
//
// A non-empty argv0 MOUNTS the tree under a stand-in host root, exactly as ape
// does, rather than running it as its own root with a different Argv0. That
// matters: cobra's `Usage:` line derives from the command PATH, so an unmounted
// tree correctly prints `aboard history` however it was configured, and a test
// that skipped the mount would be asserting against the wrong thing.
func runHosted(t *testing.T, argv0 string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer

	var root *cobra.Command
	if argv0 == "" {
		root = NewRootCmd(Options{Stdout: &out, Stderr: &out})
		root.SetArgs(args)
	} else {
		board := NewRootCmd(Options{Host: aboard.HostApe, Argv0: argv0, Stdout: &out, Stderr: &out})
		root = &cobra.Command{Use: "ape", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(board)
		root.SetArgs(append([]string{board.Name()}, args...))
	}
	// Cobra writes --help to its own writer; the commands write to
	// Options.Stdout. Both go to the same buffer so one helper covers both.
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

// The defect this whole change is for: under a host, every sentence naming a
// command named one the reader does not have — directly under a cobra `Usage:`
// line that was always correct.
func TestHostedHelpNamesTheHostedCommand(t *testing.T) {
	// `boards` is deliberately absent. Its help explains that the /proc scan
	// matches on cmdline rather than comm precisely BECAUSE it must find both
	// an `aboard serve` and an `ape aboard serve`, so naming both spellings
	// literally is the content of the sentence, not a defect in it.
	for _, tc := range []struct{ name, args string }{
		{"serve", "serve"},
		{"init", "init"},
		{"requests", "requests"},
		{"history", "history"},
		{"uploads", "uploads"},
		{"capabilities", "capabilities"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runHosted(t, "ape aboard", tc.args, "--help")
			if err != nil {
				t.Fatalf("--help: %v", err)
			}
			// Nothing may offer a bare `aboard <verb>` the hosted reader
			// cannot type. "ape aboard" contains "aboard", so strip the
			// correct spelling before looking for the wrong one.
			stripped := strings.ReplaceAll(out, "ape aboard", "")
			for _, verb := range []string{
				"aboard serve", "aboard init", "aboard status", "aboard apply",
				"aboard requests", "aboard boards", "aboard history", "aboard recipes",
			} {
				if strings.Contains(stripped, verb) {
					t.Errorf("hosted help offers %q, which the reader cannot type:\n%s", verb, out)
				}
			}
		})
	}
}

// The mirror. Standalone help must be exactly what it always was.
func TestStandaloneHelpStillNamesAboard(t *testing.T) {
	out, err := runHosted(t, "", "init", "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "aboard init") {
		t.Errorf("standalone help must still say `aboard init`:\n%s", out)
	}
}

// The invariant the two identities rest on: a client reading the manifest must
// not be able to tell which host served it. Checked over every rendered
// format, because the generated skill reference and controls module are
// committed files that `make caps` must produce identically for anyone.
func TestCapabilitiesAreHostIndependent(t *testing.T) {
	for _, format := range []string{"", "md", "js"} {
		name := format
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			args := []string{"capabilities"}
			if format != "" {
				args = append(args, "--format", format)
			}
			standalone, err := runHosted(t, "", args...)
			if err != nil {
				t.Fatalf("standalone: %v", err)
			}
			hosted, err := runHosted(t, "ape aboard", args...)
			if err != nil {
				t.Fatalf("hosted: %v", err)
			}
			if standalone != hosted {
				t.Errorf("capabilities --format %s differs between hosts; "+
					"if capsHash moved, that is the bug", name)
			}
		})
	}
}

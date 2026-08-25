package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
)

// run executes the tree in-process and returns what it wrote. Commands never
// call os.Exit — they return errors and Execute maps them — which is what makes
// this possible at all.
func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRootCmd(Options{Host: aboard.HostStandalone, Stdout: &out, Stderr: &errOut})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

// --name is on the ROOT now. It used to sit on serve and apply only, so
// `aboard serve --name review` worked and `aboard status --name review` was an
// unknown-flag error — which reads as the second board not existing.
func TestNameIsAcceptedByEveryBoardCommand(t *testing.T) {
	root := NewRootCmd(Options{Host: aboard.HostStandalone})

	// Every command that opens a board document. `capabilities` and `version` are
	// absent on purpose: they describe the binary, not a board, and inherit the
	// flag harmlessly without ever reading it.
	for _, name := range []string{
		"serve", "status", "init", "apply", "wait", "poke",
		"journal", "watch", "log", "export",
	} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		// Inherited, not local: one declaration on the root that every command
		// gets, which is what makes the manifest's rootFlags entry true.
		if cmd.InheritedFlags().Lookup("name") == nil {
			t.Errorf("%s does not accept --name", name)
		}
	}
}

// --name selects the DOCUMENT, and status has to report the named one.
func TestStatusReportsTheNamedBoard(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir, Name: "review"}); err != nil {
		t.Fatal(err)
	}

	out, _, err := run(t, "--cwd", dir, "status", "--name", "review", "--output-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "instance.review.json") {
		t.Errorf("status --name review did not resolve the named board (expected the review instance file):\n%s", out)
	}
}

// The environment is the fallback, resolved at USE and not as a flag default: a
// default that changed with the environment would be reported by the capability
// manifest, and capsHash would move when somebody exported a variable.
func TestBoardNameFallsBackToTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir, Name: "review"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ABOARD_NAME", "review")

	out, _, err := run(t, "--cwd", dir, "status", "--output-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "instance.review.json") {
		t.Errorf("ABOARD_NAME was not honoured:\n%s", out)
	}

	// And the flag beats the environment, which is the direction people assume
	// without checking.
	out, _, err = run(t, "--cwd", dir, "status", "--name", "", "--output-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "instance.review.json") {
		t.Errorf("an empty --name should defer to the environment:\n%s", out)
	}
}

// The manifest's root flags and cobra's must agree; the parity test asserts
// that. This asserts the thing behind it — that --name really is inherited
// rather than repeated on each command, which is what makes the declaration
// true.
func TestNameIsInheritedNotRepeated(t *testing.T) {
	root := NewRootCmd(Options{Host: aboard.HostStandalone})
	if root.PersistentFlags().Lookup("name") == nil {
		t.Fatal("--name is not a persistent flag on the root")
	}
	for _, sub := range root.Commands() {
		if f := sub.Flags().Lookup("name"); f != nil && sub.LocalFlags().Lookup("name") != nil {
			t.Errorf("%s redeclares --name locally; the root's would be shadowed", sub.Name())
		}
	}
}

// The refusal a session hits in a directory that has never had a board. The
// message has to name the command that fixes it — and recognise the spike's
// `.board/`, because a reader looking at a directory that appears to be exactly
// what was asked for concludes the tool is broken.
func TestNoRootMessageNamesInitAndTheLegacyDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := run(t, "--cwd", dir, "status"); err == nil {
		t.Fatal("status in an empty directory succeeded")
	} else if !strings.Contains(err.Error(), "aboard init") {
		t.Errorf("the refusal does not name the fix: %v", err)
	}

	legacy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(legacy, ".board"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := run(t, "--cwd", legacy, "status")
	if err == nil {
		t.Fatal("status in a directory with only a stale .board/ succeeded")
	}
	if !strings.Contains(err.Error(), ".board/") {
		t.Errorf("the refusal does not mention the stale .board/: %v", err)
	}
	if !strings.Contains(err.Error(), aboard.DirName) {
		t.Errorf("the refusal does not name the directory aboard actually wants: %v", err)
	}
}

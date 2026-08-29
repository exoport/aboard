package cli

import (
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
)

// The third command of the resume protocol — status, capabilities, journal — used
// to exit 1 in any project whose board was stopped. The first two answer with no
// server by design; the journal is an append-only FILE and needs one even less.
func TestJournalReadsFromDiskWithNoBoardRunning(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}, aboard.DefaultInvocation); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := run(t, "--cwd", dir, "journal", "--limit", "5")
	if err != nil {
		t.Fatalf("journal with no board running failed: %v", err)
	}
	// Where the answer came from is a DIAGNOSTIC, so it goes to stderr: a
	// consumer piping stdout should not have to filter prose back out of it.
	if !strings.Contains(errOut, "from disk") {
		t.Errorf("stderr does not say the answer came from disk:\n%s", errOut)
	}
	if strings.Contains(out, "from disk") {
		t.Errorf("the provenance line leaked into stdout:\n%s", out)
	}
}

// And it says nothing at all in a structured format, where a prose line would be
// something for jq to choke on.
func TestJournalDiskNoticeIsHumanOnly(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}, aboard.DefaultInvocation); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := run(t, "--cwd", dir, "journal", "--output-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut, "from disk") {
		t.Errorf("the provenance line was printed in json mode:\n%s", errOut)
	}
	if strings.TrimSpace(out) != "[]" && !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("json output is not an array:\n%s", out)
	}
}

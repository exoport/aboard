package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
)

// `boards` is the one board command that must answer where there is no board.
// It is machine-wide, so demanding a project root would make it unusable from
// the one place a reader most often types it: anywhere but a project.
//
// Fails before: the command did not exist; it fails again the moment somebody
// adds a projectRoot(cmd) call to it out of habit, which every sibling command
// in this package begins with.
func TestBoardsNeedsNoProjectOfItsOwn(t *testing.T) {
	dir := t.TempDir() // no .aboard/ anywhere above it that matters

	code, out, errOut := exitOf(t, "--cwd", dir, "boards")
	if code != aboard.ExitOK {
		t.Fatalf("boards exited %d from a directory with no board: %s", code, errOut)
	}
	if !strings.Contains(out, "process") {
		t.Errorf("the listing does not say how much of the machine it saw:\n%s", out)
	}
}

// The structured form is what another tool reads, so it has to be a document
// rather than the human listing with quotes around it.
func TestBoardsJSONIsADocument(t *testing.T) {
	dir := t.TempDir()

	code, out, errOut := exitOf(t, "--cwd", dir, "boards", "--output-format", "json")
	if code != aboard.ExitOK {
		t.Fatalf("boards --output-format json exited %d: %s", code, errOut)
	}
	var got aboard.BoardsReport
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the json does not parse: %v\n%s", err, out)
	}
	if got.Inspected == 0 {
		t.Errorf("inspected 0 processes; this machine is running at least this test\n%s", out)
	}
}

// An unknown format is refused before the process table is walked — the same
// rule every other command in this package follows, and the reason exit 2 means
// "detected before anything was contacted".
func TestBoardsRefusesAnUnknownOutputFormat(t *testing.T) {
	code, _, errOut := exitOf(t, "--cwd", t.TempDir(), "boards", "--output-format", "nope")
	if code != aboard.ExitUsage {
		t.Fatalf("exited %d, want %d: %s", code, aboard.ExitUsage, errOut)
	}
}

// The refusal a macOS or Windows reader gets has to be exit 2, and nothing on
// this machine can produce it end to end — /proc is right there. So the branch
// that decides it is asserted directly, which is the only way this claim stops
// being "it looked right in the diff".
//
// Fails before: the mapping lived inline in RunE, where no test could reach it,
// and it fails again if somebody simplifies it to `return err` — which would
// hand a Mac exit 1, the code that means "the scan ran and found no board".
func TestAPlatformWithNoProcessTableExitsUsageNotFailure(t *testing.T) {
	refusal := fmt.Errorf("%w: /proc exists on Linux only — this is darwin", aboard.ErrNoProcessTable)
	if code, _ := ExitCode(boardsExit(refusal)); code != aboard.ExitUsage {
		t.Errorf("a platform with no process table exits %d, want %d", code, aboard.ExitUsage)
	}

	// And a real failure to read a real /proc stays a 1: the reader's next move
	// is to look at their machine, not at their operating system.
	if code, _ := ExitCode(boardsExit(errors.New("read /proc: permission denied"))); code != aboard.ExitFailed {
		t.Errorf("an unreadable process table exits %d, want %d", code, aboard.ExitFailed)
	}
}

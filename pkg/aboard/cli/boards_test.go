package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
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
// On a platform with no /proc there is nothing to scan, and `boards` says so and
// exits 2 rather than pretending. That is the documented behaviour (see
// boards_other.go), so these two tests assert THAT there instead of asserting a
// listing that cannot exist — a skip would leave the honest-everywhere promise
// untested on the platforms it was written for.
func boardsIsAScan() bool { return runtime.GOOS == "linux" }

// The refusal travels on the returned ERROR, not on stderr: the command returns
// it and the caller renders it, which is what makes an exit status testable at
// all here. Asserting against the captured stderr found it empty and reported
// "the refusal does not name the platform" — with nothing after the colon,
// because there was nothing there.
func assertHonestElsewhere(t *testing.T, err error, out string) {
	t.Helper()
	if err == nil {
		t.Fatalf("boards succeeded on %s, where there is no process table to read", runtime.GOOS)
	}
	if code, _ := ExitCode(err); code != aboard.ExitUsage {
		t.Fatalf("boards exited %d on %s, want %d — the command exists everywhere and is honest where it cannot scan",
			code, runtime.GOOS, aboard.ExitUsage)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Errorf("the refusal does not name the platform: %v", err)
	}
	if !strings.Contains(err.Error(), "aboard status") {
		t.Errorf("the refusal does not point at the per-project command: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("it printed a listing anyway:\n%s", out)
	}
}

func TestBoardsNeedsNoProjectOfItsOwn(t *testing.T) {
	dir := t.TempDir() // no .aboard/ anywhere above it that matters

	out, errOut, runErr := run(t, "--cwd", dir, "boards")
	if !boardsIsAScan() {
		assertHonestElsewhere(t, runErr, out)
		return
	}
	code, _ := ExitCode(runErr)
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

	out, errOut, runErr := run(t, "--cwd", dir, "boards", "--output-format", "json")
	if !boardsIsAScan() {
		assertHonestElsewhere(t, runErr, out)
		return
	}
	code, _ := ExitCode(runErr)
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

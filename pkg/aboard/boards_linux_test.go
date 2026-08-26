//go:build linux

package aboard

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

/* ---------- a fake process table ---------- */

// fakeProc is a /proc-shaped directory under t.TempDir(): one directory per
// pid, a NUL-separated `cmdline`, and `cwd` as a real symlink.
//
// The scanner takes its root as a parameter for exactly this. The machine's own
// process table is not a fixture — it holds whatever happens to be running, it
// cannot be made to hold an `ape aboard serve`, and a test that asserted against
// it would pass or fail for reasons belonging to the machine.
type fakeProc struct{ dir string }

func newFakeProc(t *testing.T) *fakeProc {
	t.Helper()
	dir := t.TempDir()
	// `self` is what the scanner stats to decide this is a process table at all,
	// and the other three are here to be SKIPPED: /proc holds a dozen non-numeric
	// entries and a scan that tried to read a pid out of `sys` would count them.
	for _, name := range []string{"self", "sys", "net", "irq"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &fakeProc{dir: dir}
}

// add writes one process. An empty cwd means "no cwd entry at all", which is how
// the kernel behaves for a process this user may not inspect: the link is there
// but reading it is refused, and either way os.Readlink returns an error.
func (p *fakeProc) add(t *testing.T, pid int, cwd string, argv ...string) {
	t.Helper()
	dir := filepath.Join(p.dir, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// NUL-separated AND NUL-terminated, as the kernel writes it.
	body := strings.Join(argv, "\x00") + "\x00"
	if len(argv) == 0 {
		body = "" // a kernel thread
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
}

// project makes a directory that FindRoot will accept.
func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// Resolved, because FindRoot resolves — /tmp is a symlink on some machines
	// and the comparison below would then be against a path nothing produces.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func scanFake(t *testing.T, p *fakeProc) BoardsReport {
	t.Helper()
	rep, err := scanBoards(context.Background(), p.dir)
	if err != nil {
		t.Fatalf("scanning the fake proc tree: %v", err)
	}
	return rep
}

func projectsIn(rep BoardsReport) []string {
	out := make([]string, 0, len(rep.Boards))
	for i := range rep.Boards {
		out = append(out, rep.Boards[i].Project)
	}
	return out
}

/* ---------- what the scan must recognise ---------- */

// Both shapes, and the second is the whole reason this reads `cmdline` rather
// than `comm`: under `ape aboard serve` the process's `comm` is `ape`, so a name
// filter finds nothing at all for one of the two ways this project is meant to
// run.
//
// Fails before: there was no scanner. It fails again the moment `boardArgs`
// stops looking past argv[0] — which is precisely the "just match the process
// name" simplification the original design died of.
func TestTheScanFindsBothInvocationShapes(t *testing.T) {
	standalone, hosted := project(t), project(t)
	p := newFakeProc(t)
	p.add(t, 101, standalone, "/usr/local/bin/aboard", "serve")
	p.add(t, 102, hosted, "/home/x/go/bin/ape", "aboard", "serve", "--port", "41999")

	rep := scanFake(t, p)
	if len(rep.Boards) != 2 {
		t.Fatalf("found %d boards, want 2: %v", len(rep.Boards), projectsIn(rep))
	}
	found := map[string]int{}
	for _, row := range rep.Boards {
		found[row.Project] = row.PID
	}
	if found[standalone] != 101 {
		t.Errorf("`aboard serve` in %s was not found as pid 101: %v", standalone, found)
	}
	if found[hosted] != 102 {
		t.Errorf("`ape aboard serve` in %s was not found as pid 102: %v", hosted, found)
	}
	if rep.Inspected != 2 {
		t.Errorf("inspected %d processes, want 2 — the non-numeric /proc entries must not be counted", rep.Inspected)
	}
}

// --cwd in the argv beats the process's own working directory, in all four
// spellings, because it is what the process itself resolved its root from.
func TestTheScanHonoursCwdInTheArgv(t *testing.T) {
	elsewhere := t.TempDir()
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"--cwd DIR", []string{"aboard", "--cwd", "", "serve"}},
		{"--cwd=DIR", []string{"aboard", "--cwd=", "serve"}},
		{"-C DIR", []string{"aboard", "-C", "", "serve"}},
		{"after the subcommand", []string{"aboard", "serve", "--cwd", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			argv := append([]string(nil), tc.argv...)
			for i, a := range argv {
				switch a {
				case "":
					argv[i] = root
				case "--cwd=":
					argv[i] = "--cwd=" + root
				}
			}
			p := newFakeProc(t)
			p.add(t, 200, elsewhere, argv...)

			rep := scanFake(t, p)
			if len(rep.Boards) != 1 || rep.Boards[0].Project != root {
				t.Fatalf("%v resolved to %v, want %s", argv, projectsIn(rep), root)
			}
		})
	}
}

// A RELATIVE --cwd was typed against the serving process's working directory, so
// that is what it has to be joined to. Joining it against OURS would resolve a
// perfectly good path to a different project, or to none.
func TestARelativeCwdIsResolvedAgainstTheProcessOwnDirectory(t *testing.T) {
	root := project(t)
	parent := filepath.Dir(root)
	p := newFakeProc(t)
	p.add(t, 300, parent, "aboard", "--cwd", filepath.Base(root), "serve")

	rep := scanFake(t, p)
	if len(rep.Boards) != 1 || rep.Boards[0].Project != root {
		t.Fatalf("a relative --cwd resolved to %v, want %s", projectsIn(rep), root)
	}
}

// The word `serve` has to be the SUBCOMMAND, not merely present. `aboard export`
// and a program that happens to be called aboard are both processes this scan
// must walk past.
func TestAProcessThatIsNotServingABoardIsNotABoard(t *testing.T) {
	root := project(t)
	p := newFakeProc(t)
	p.add(t, 401, root, "aboard", "status")
	p.add(t, 402, root, "aboard", "export", "bb1")
	p.add(t, 403, root, "aboard", "wait", "--note", "serve")
	p.add(t, 404, root, "/usr/bin/notaboard", "serve")
	p.add(t, 405, root, "ape", "pipeline", "serve")
	p.add(t, 406, root) // a kernel thread: an empty cmdline

	rep := scanFake(t, p)
	if len(rep.Boards) != 0 {
		t.Fatalf("found boards where there are none: %v", projectsIn(rep))
	}
	if rep.Inspected != 6 {
		t.Errorf("inspected %d, want 6", rep.Inspected)
	}
	if rep.Unreadable != 0 {
		t.Errorf("counted %d unreadable; none of these is a board the scan failed to place", rep.Unreadable)
	}
}

// Another user's board. The scan knows it is there and cannot say whose, and
// that is the one case where a silent skip would report the machine as quieter
// than it is.
func TestABoardWhoseDirectoryCannotBeReadIsCountedNotDropped(t *testing.T) {
	p := newFakeProc(t)
	p.add(t, 500, "", "aboard", "serve") // no cwd link at all
	p.add(t, 501, t.TempDir(), "aboard", "serve")

	rep := scanFake(t, p)
	if len(rep.Boards) != 0 {
		t.Errorf("listed a board it could not place: %v", projectsIn(rep))
	}
	if rep.Unreadable != 2 {
		t.Errorf("unreadable = %d, want 2 (one refused cwd, one directory that is not a project)", rep.Unreadable)
	}
	if !strings.Contains(rep.Human(), "2 processes could not be inspected") {
		t.Errorf("the listing does not say what it could not see:\n%s", rep.Human())
	}
	if rep.Inspected != 2 {
		t.Errorf("inspected %d, want 2", rep.Inspected)
	}
}

// Two serve processes in one project that NO instance record names are two rows,
// not one. The row's empty name means "nothing told us", not "the default
// board", so deduplicating on (project, name) collapsed them — and collapsed an
// unrecorded process into the project's real default board as well, which is a
// running board disappearing from a listing whose whole point is that its two
// counters never let one disappear silently.
//
// Fails before: the key was `row.Project + "\x00" + row.Name` with nothing else in
// it, and this found 1 board where there are 2.
func TestTwoUnrecordedBoardsInOneProjectAreTwoRows(t *testing.T) {
	root := project(t)
	p := newFakeProc(t)
	// 71 and 700, deliberately: ReadDir returns /proc's directories sorted as
	// TEXT, where "700" comes before "71". A listing that kept that order would
	// be sorted by neither pid nor anything else a reader could name.
	p.add(t, 700, root, "aboard", "serve")
	p.add(t, 71, root, "aboard", "serve", "--name", "review")

	rep := scanFake(t, p)
	if len(rep.Boards) != 2 {
		t.Fatalf("found %d rows for two serve processes in %s: %v", len(rep.Boards), root, projectsIn(rep))
	}
	if rep.Boards[0].PID != 71 || rep.Boards[1].PID != 700 {
		t.Errorf("rows came out in pid order %d, %d; want 71, 700", rep.Boards[0].PID, rep.Boards[1].PID)
	}
	if rep.Unreadable != 0 {
		t.Errorf("unreadable = %d; both processes were read fine", rep.Unreadable)
	}
}

// An absolute --cwd survives an unreadable working directory: the flag says
// where the project is without needing the link the kernel just refused.
func TestAnAbsoluteCwdFlagRescuesAnUnreadableProcess(t *testing.T) {
	root := project(t)
	p := newFakeProc(t)
	p.add(t, 600, "", "aboard", "--cwd", root, "serve")

	rep := scanFake(t, p)
	if len(rep.Boards) != 1 || rep.Boards[0].Project != root {
		t.Fatalf("resolved to %v, want %s", projectsIn(rep), root)
	}
	if rep.Unreadable != 0 {
		t.Errorf("unreadable = %d; the flag told us everything we needed", rep.Unreadable)
	}
}

// A directory that is not a process table is refused with the same sentinel the
// non-Linux stub uses, so one branch in the cli maps both to exit 2.
func TestATreeWithNoSelfEntryIsNotAProcessTable(t *testing.T) {
	_, err := scanBoards(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNoProcessTable) {
		t.Fatalf("scanning a directory that is not /proc returned %v, want ErrNoProcessTable", err)
	}
}

/* ---------- the real /proc ---------- */

// Everything above runs against a tree this test wrote, so everything above
// would still pass if the kernel's own format were something else. This is the
// rung that holds the fixture honest: our OWN pid, out of the machine's real
// process table.
func TestTheRealProcAnswersForThisProcess(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	argv, err := readArgv(procDir, pid)
	if err != nil {
		t.Fatalf("reading our own cmdline from %s: %v", procDir, err)
	}
	if len(argv) == 0 || argv[0] == "" {
		t.Fatalf("our own argv came back empty: %q", argv)
	}
	if strings.Contains(argv[0], "\x00") {
		t.Errorf("argv[0] still holds a NUL, so the split did not happen: %q", argv[0])
	}
	dir, ok := startDirOf(procDir, pid, nil)
	if !ok {
		t.Fatal("could not read our own working directory through /proc")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatal(err)
	}
	if dir != resolved {
		t.Errorf("/proc says our cwd is %s, os.Getwd says %s", dir, resolved)
	}
}

// The whole path, end to end, against a board that is genuinely running: a real
// process in the real /proc, a real instance record, a real /health and a real
// board document.
//
// The child is this test binary re-executed with argv[0] set to "aboard", which
// is what /proc/<pid>/cmdline then reports — so the scan matches it on exactly
// the evidence it will use in the field. A `go test` binary is called
// `aboard.test`, so without that it could never be recognised, and the one thing
// this test exists to prove would be assumed instead.
func TestBoardsFindsALiveBoardThroughTheRealProc(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitConfig{Dir: dir, Example: true}); err != nil {
		t.Fatal(err)
	}
	root, err := FindRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Args = []string{"aboard", "-test.run=^TestServeHelperProcess$", "serve"}
	cmd.Dir = root.String()
	cmd.Env = append(os.Environ(), serveHelperEnv+"=1", serveHelperDir+"="+root.String())
	var childLog bytes.Buffer
	cmd.Stdout, cmd.Stderr = &childLog, &childLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	inst := waitForBoard(t, root, &childLog)

	rep, err := scanBoards(t.Context(), procDir)
	if err != nil {
		t.Fatalf("scanning the real %s: %v", procDir, err)
	}
	var got *BoardRow
	for i := range rep.Boards {
		if rep.Boards[i].PID == cmd.Process.Pid {
			got = &rep.Boards[i]
		}
	}
	if got == nil {
		t.Fatalf("the live board (pid %d, %s) is not among the %d board(s) found in %d processes: %v\n%s",
			cmd.Process.Pid, root, len(rep.Boards), rep.Inspected, projectsIn(rep), childLog.String())
	}
	if got.Project != root.String() {
		t.Errorf("project %s, want %s", got.Project, root)
	}
	if !got.Recorded || !got.Answering {
		t.Errorf("recorded=%v answering=%v; the board is up and its record is on disk", got.Recorded, got.Answering)
	}
	if got.App != HostStandalone {
		t.Errorf("app %q, want %q", got.App, HostStandalone)
	}
	if got.URL != inst.URL || got.Port != inst.Port {
		t.Errorf("url/port %s:%d, record says %s:%d", got.URL, got.Port, inst.URL, inst.Port)
	}
	// The example board is the fixture `init --example` seeds; the count comes
	// from GET /aboard.json, so a zero here means the summary fetch silently did
	// nothing and the row would have looked fine anyway.
	if got.Tabs == 0 {
		t.Errorf("tabs = 0 for a board seeded from the example; the document summary did not arrive")
	}
	if got.Name != "" {
		t.Errorf("name %q, want the default board's empty name", got.Name)
	}
}

// The environment the helper looks for. Named constants because the parent sets
// them and the child reads them, in two functions that do not otherwise share
// anything.
const (
	serveHelperEnv = "ABOARD_SERVE_HELPER"
	serveHelperDir = "ABOARD_SERVE_DIR"
)

func waitForBoard(t *testing.T, root Root, out *bytes.Buffer) Instance {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		rec, err := RunningInstance(root, "")
		if err == nil && ProbeBoard(t.Context(), rec.Port, rec.Base) != nil {
			return rec
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper board never answered within 20s:\n%s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestServeHelperProcess is not a test. It is the body of the child process the
// test above starts: the standard Go helper-process pattern, which needs a
// Test-prefixed function because a test binary can only be told to run one of
// those. It skips in every ordinary run, and the skip line says so rather than
// looking like a check that gave up.
func TestServeHelperProcess(t *testing.T) {
	if os.Getenv(serveHelperEnv) != "1" {
		t.Skip("not a check: the body of the child process TestBoardsFindsALiveBoardThroughTheRealProc starts")
	}
	root, err := FindRoot(os.Getenv(serveHelperDir))
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Serve(ctx, Options{Logger: log.New(os.Stderr, "helper: ", 0)}, ServeConfig{Root: root}); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

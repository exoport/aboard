//go:build unix

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/exoport/aboard/pkg/aboard"
)

// asBinary turns this test binary into the aboard binary for a subprocess, so a
// detached start — which re-runs os.Executable() — can be tested without a build
// step: the child is this same file, and TestMain hands it straight to Execute.
const asBinary = "ABOARD_CLI_TEST_AS_BINARY"

func TestMain(m *testing.M) {
	if os.Getenv(asBinary) == "1" {
		os.Exit(Execute(Options{Host: aboard.HostStandalone, Argv0: aboard.AppName}))
	}
	os.Exit(m.Run())
}

// aboardProcess runs this binary as `aboard <args>` in a process group of its
// own, the way an agent session's shell runs a command, and returns the group id
// alongside what it printed.
func aboardProcess(t *testing.T, args ...string) (out string, code, pgid int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("finding the test binary: %v", err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), asBinary+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	body, err := cmd.CombinedOutput()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return string(body), code, cmd.Process.Pid
}

// The claim --detach makes, run: the board survives the death of the process
// group that started it. That is what an agent session restarting does to a
// `nohup aboard serve &`, and it is the whole of the Moonwatcher report.
//
// Then the refusal, because a detached start must be no way around it: a second
// --detach in the same project exits non-zero and says the board is already
// running, in the words of the process that refused.
func TestADetachedBoardOutlivesTheProcessGroupThatStartedIt(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := run(t, "--cwd", dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	out, code, pgid := aboardProcess(t, "--cwd="+dir, "serve", "--detach")
	if code != 0 || !strings.Contains(out, "aboard running at") || !strings.Contains(out, "detached") {
		t.Fatalf("serve --detach exited %d:\n%s", code, out)
	}

	root, err := aboard.FindRoot(dir)
	if err != nil {
		t.Fatalf("finding the root: %v", err)
	}
	body, err := os.ReadFile(root.InstanceFile(""))
	if err != nil {
		t.Fatalf("no instance record after a detached start: %v", err)
	}
	var inst aboard.Instance
	if err := json.Unmarshal(body, &inst); err != nil {
		t.Fatalf("unreadable instance record: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(inst.PID, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && aboard.ProbeBoard(context.Background(), inst.Port, inst.Base) != nil {
			time.Sleep(50 * time.Millisecond)
		}
	})

	// Everything left in the group that ran the command. The starting process has
	// already exited; a server that had stayed in its group dies here.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	time.Sleep(300 * time.Millisecond)

	live := aboard.ProbeBoard(context.Background(), inst.Port, inst.Base)
	if live == nil {
		t.Fatalf("the detached board (pid %d) died with the process group that started it", inst.PID)
	}
	if live.PID != inst.PID || live.Project != root.String() {
		t.Errorf("port %d answers as pid %d for %s, want pid %d for %s", inst.Port, live.PID, live.Project, inst.PID, root)
	}
	again, code, _ := aboardProcess(t, "--cwd="+dir, "serve", "--detach")
	if code == 0 {
		t.Fatalf("a second serve --detach in the same project succeeded:\n%s", again)
	}
	if !strings.Contains(again, "already running") {
		t.Errorf("the refusal does not say the board is already running:\n%s", again)
	}

	// Read AFTER the refusal, deliberately. The first version read it before, and
	// passed while a refused second start was truncating the running board's log.
	if log, err := os.ReadFile(root.ServeLog("")); err != nil || !strings.Contains(string(log), "aboard  ->") {
		t.Errorf("the running board's log at %s no longer holds its startup lines after a refused second start (err %v):\n%s",
			root.ServeLog(""), err, log)
	}
}

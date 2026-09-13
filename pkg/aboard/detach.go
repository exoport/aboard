// detach.go — a board that outlives the shell that started it.
//
//	aboard serve --detach
//
// A server started as `aboard serve … &` from an agent session belongs to that
// session's process group, and a session that restarts takes its group down —
// `nohup` only ignores the hangup, it does not leave the group. The board then
// dies with its record still on disk: `status` reports a stale record, and every
// `apply` fails until somebody notices. Reported by a Moonwatcher session on
// 2026-09-11, and this repository's own briefs have said `setsid nohup … &` for
// as long as they have started servers by hand.
//
// So --detach starts `serve` again as a process in a session of its own, with its
// output in run/serve.log, and waits for it to answer. It does not daemonise in
// the classic sense — no double fork, no pid file of its own: the instance record
// is already the pid file, and /health is already the readiness check.
//
// What it deliberately does NOT add is a service manager. A systemd user unit per
// project would be state outside .aboard/, written once and then stale — the same
// argument that keeps `aboard boards` a process scan with no registry.

package aboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// How long a detached start may take to answer before the parent stops waiting.
// A board answers in well under a second; this covers a cold disk and a large
// document, and the process is left running either way.
const detachReadyTimeout = 15 * time.Second

// DetachedExitError is a detached server that exited before it answered — a refusal
// (this project's board is already running), a missing document, a bad flag.
// Code is its exit status and Log is everything it wrote, which is where the
// reason is.
type DetachedExitError struct {
	Code int
	Log  string
}

func (e *DetachedExitError) Error() string {
	return fmt.Sprintf("the detached server exited with status %d before it answered", e.Code)
}

// StartDetached runs exe with args — the same `serve` command, minus --detach —
// in a session of its own, with stdin closed and stdout and stderr in
// root.ServeLog(name), and returns the instance once that process answers as
// this project's board.
//
// Readiness is the record naming the CHILD's pid and /health answering with it,
// never merely "a board answers": a board that was already running would pass
// that test while the child was busy refusing to start.
//
// The refusal of a second board is asked HERE first, before the log is opened:
// the log is truncated on a start, and a start that the child would refuse a
// moment later had already emptied the log of the board it was refusing in
// favour of. Found by running it, not by the test, which read the log too early.
// The child still makes the same check, and still wins any race.
func StartDetached(ctx context.Context, root Root, name string, inv Invocation, exe string, args []string) (Instance, error) {
	var none Instance
	if err := refuseLiveRecord(ctx, root, name, inv); err != nil {
		return none, err
	}
	logPath := root.ServeLog(name)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		return none, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return none, fmt.Errorf("opening %s: %w", logPath, err)
	}
	defer func() { _ = logFile.Close() }()

	// WithoutCancel, and that is the feature rather than a workaround: a Ctrl+C in
	// the starting shell cancels this command's context, and a context-bound
	// child would be killed by the very interruption it exists to survive.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	cmd.SysProcAttr = detachedProcAttr()
	if err := cmd.Start(); err != nil {
		return none, fmt.Errorf("starting %s: %w", exe, err)
	}
	pid := cmd.Process.Pid

	// Reaped here, so a parent that lives on (a test, a host) does not collect
	// zombies; a CLI parent exits long before the child does.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.NewTimer(detachReadyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			code := 1
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				code = exitErr.ExitCode()
			}
			body, _ := os.ReadFile(logPath)
			return none, &DetachedExitError{Code: code, Log: strings.TrimRight(string(body), "\n")}
		case <-deadline.C:
			return none, fmt.Errorf("the detached server (pid %d) has not answered after %s — it is still running; see %s",
				pid, detachReadyTimeout, logPath)
		case <-ctx.Done():
			return none, ctx.Err()
		case <-tick.C:
			if inst, ok := detachedReady(ctx, root, name, pid); ok {
				return inst, nil
			}
		}
	}
}

func detachedReady(ctx context.Context, root Root, name string, pid int) (Instance, bool) {
	body, err := os.ReadFile(root.InstanceFile(name))
	if err != nil {
		return Instance{}, false
	}
	var rec Instance
	if json.Unmarshal(body, &rec) != nil || rec.PID != pid {
		return Instance{}, false
	}
	live := ProbeBoard(ctx, rec.Port, rec.Base)
	if live == nil || live.PID != pid || live.Project != root.String() || live.Name != name {
		return Instance{}, false
	}
	return *live, true
}

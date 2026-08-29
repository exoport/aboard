//go:build linux

// boards_linux.go — the walk. The only Linux-specific half of `aboard boards`.
//
// The build tag is redundant beside the filename and is here to be read: this is
// the file the whole "Linux only, honest elsewhere" decision hangs off, and a
// constraint carried by a naming convention is one nobody sees while editing.

package aboard

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procDir is the machine's own process table. A variable-free constant, and
// never what the scanner reads directly — scanBoards takes the root as a
// parameter so its tests can hand it a fake tree.
const procDir = "/proc"

// The two subcommand shapes a board can be running under. `ape` mounts this
// whole cobra tree as `ape aboard <command>`, so both are real ways for a board
// to exist and a scan that knew only one would report a hosted board as absent.
const (
	hostBinary = "aboard"
	apeBinary  = "ape"
	serveVerb  = "serve"
)

// cmdlineFile and cwdLink are the two things read per process.
const (
	cmdlineFile = "cmdline"
	cwdLink     = "cwd"
)

// scanBoards walks a process table and reports the boards it finds.
//
// It reads `cmdline` and NOT `comm`, and the difference is the whole reason this
// design was once written off. `comm` is the kernel's 15-character name for the
// process, so it is both TRUNCATED and, under `ape aboard serve`, the name of the
// HOST — a `comm == "aboard"` filter silently misses every board ape is running
// on somebody's behalf, which is one of the two ways this project is meant to
// run. `/proc/<pid>/cmdline` is the whole argv, NUL-separated, so it can see the
// subcommand as well as the binary.
func scanBoards(ctx context.Context, procRoot string, inv Invocation) (BoardsReport, error) {
	if _, err := os.Stat(procSelfPath(procRoot)); err != nil {
		return BoardsReport{}, noProcFS(procRoot, inv)
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return BoardsReport{}, err
	}

	rep := BoardsReport{Boards: []BoardRow{}}
	// One row per (project, name), so a pid that somehow resolves to a board
	// another pid already produced does not appear twice. Keyed on the pair
	// because two boards in one project are two rows and must not collapse.
	seen := map[string]bool{}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		pid, ok := processID(entry.Name())
		if !ok {
			continue
		}
		rep.Inspected++

		argv, err := readArgv(procRoot, entry.Name())
		if err != nil {
			if os.IsPermission(err) {
				rep.Unreadable++
			}
			continue
		}
		args, ok := boardArgs(argv)
		if !ok || !servesABoard(args) {
			continue
		}

		start, ok := startDirOf(procRoot, entry.Name(), args)
		if !ok {
			// Another user's board. Counted, never dropped: this is the one case
			// where the scan KNOWS a board is running and cannot say whose, and a
			// silent skip would report the machine as quieter than it is.
			rep.Unreadable++
			continue
		}
		root, err := FindRoot(start)
		if err != nil {
			// A serve process whose working directory no longer resolves to a
			// project: the directory was removed under it, or it lives in a mount
			// namespace whose paths mean nothing here. Same honest counter — the
			// process is real and this scan cannot place it.
			rep.Unreadable++
			continue
		}

		row := describeBoard(ctx, root, pid)
		key := boardKey(row, pid)
		if seen[key] {
			continue
		}
		seen[key] = true
		rep.Boards = append(rep.Boards, row)
	}
	sortRows(rep.Boards)
	return rep, nil
}

// boardKey is the identity a row is deduplicated on: the (project, name) pair,
// which is what makes two boards in one project two rows.
//
// An UNRECORDED row has no such identity. Its name is empty because no instance
// record named it, not because it is the default board — so keying it on the
// pair would collide it with that project's real default board, and one of two
// genuinely running boards would vanish from a listing whose two counters cannot
// even report that it happened. Which of the two survived would be decided by
// the order ReadDir happened to return the pids in. The pid is the only identity
// such a row has, so it is what the key uses.
func boardKey(row BoardRow, pid int) string {
	key := row.Project + "\x00" + row.Name
	if !row.Recorded {
		key += "\x00" + strconv.Itoa(pid)
	}
	return key
}

// processID accepts only an all-digit directory name. `/proc` holds `self`,
// `net`, `sys` and a dozen others beside the processes.
func processID(name string) (int, bool) {
	pid, err := strconv.Atoi(name)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// readArgv reads one process's argv. The file is NUL-separated and NUL-terminated,
// and it is EMPTY for a kernel thread — which is why the trailing separator is
// trimmed before splitting rather than after: without that, every process ends
// with an empty final argument and a kernel thread parses as one argument that
// is the empty string.
func readArgv(procRoot, pid string) ([]string, error) {
	body, err := os.ReadFile(procEntryPath(procRoot, pid, cmdlineFile))
	if err != nil {
		return nil, err
	}
	raw := strings.TrimRight(string(body), "\x00")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\x00"), nil
}

// boardArgs recognises the two shapes and returns what follows the aboard token,
// which is where the subcommand and the flags are.
//
// filepath.Base, because argv[0] is whatever the caller typed: `aboard`,
// `./aboard`, `/usr/local/bin/aboard` and `~/go/bin/aboard` are all the same
// program and a scan that matched the full string would find only one of them.
func boardArgs(argv []string) ([]string, bool) {
	if len(argv) == 0 {
		return nil, false
	}
	switch filepath.Base(argv[0]) {
	case hostBinary:
		return argv[1:], true
	case apeBinary:
		// `ape aboard serve`: the mounted tree, where argv[0] names the host and
		// the board's own command starts one token later.
		if len(argv) > 1 && argv[1] == hostBinary {
			return argv[2:], true
		}
	}
	return nil, false
}

// servesABoard reports whether the first SUBCOMMAND in these arguments is
// `serve`. It is not a search for the word: `aboard export serve` would match
// one, and so would a `--note serve` on any command.
//
// Only the root's two persistent flags take a value before a subcommand can
// appear, so those are the only ones whose next token is skipped. A bare
// `--flag` is treated as valueless, which is right for every other flag the tree
// has at this position and is the safe direction to be wrong in — it stops the
// walk at the subcommand rather than past it.
func servesABoard(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return arg == serveVerb
		}
		if valueFlag(arg) && !strings.Contains(arg, "=") {
			i++
		}
	}
	return false
}

// The flags that can legitimately appear BEFORE the subcommand, which is the
// whole set that takes a value there: --cwd and --name are the root's two
// persistent flags. `-C` is not a shorthand this tree declares today; it is
// honoured because a `-C` that never appears costs nothing, while a `-C` that
// appeared and was ignored would resolve a board to the wrong project.
const (
	flagCwdLong  = "--cwd"
	flagCwdShort = "-C"
	flagNameLong = "--name"
)

// valueFlag names the flags that consume the following token.
func valueFlag(arg string) bool {
	return arg == flagCwdLong || arg == flagNameLong || arg == flagCwdShort
}

// startDirOf is the directory FindRoot walks up from: `--cwd` where the process
// was given one, and the process's own working directory otherwise.
//
// The flag is searched across the WHOLE argument list, not only before the
// subcommand, because --cwd is persistent — `aboard serve --cwd /x` is as valid
// as `aboard --cwd /x serve` and cobra accepts both.
func startDirOf(procRoot, pid string, args []string) (string, bool) {
	cwd, err := os.Readlink(procEntryPath(procRoot, pid, cwdLink))
	if err != nil {
		// Another user's process. --cwd would still tell us the project, but only
		// if it is absolute; a relative one is meaningless without the directory we
		// have just been refused.
		if flag, ok := cwdFlag(args); ok && filepath.IsAbs(flag) {
			return flag, true
		}
		return "", false
	}
	if flag, ok := cwdFlag(args); ok {
		return resolveAgainst(cwd, flag), true
	}
	return cwd, true
}

// cwdFlag pulls `--cwd DIR`, `--cwd=DIR`, `-C DIR` or `-C=DIR` out of an argv.
// The LAST one wins, as pflag's own parse does.
func cwdFlag(args []string) (string, bool) {
	dir, found := "", false
	for i, arg := range args {
		switch {
		case arg == flagCwdLong || arg == flagCwdShort:
			if i+1 < len(args) {
				dir, found = args[i+1], true
			}
		case strings.HasPrefix(arg, flagCwdLong+"="):
			dir, found = strings.TrimPrefix(arg, flagCwdLong+"="), true
		case strings.HasPrefix(arg, flagCwdShort+"="):
			dir, found = strings.TrimPrefix(arg, flagCwdShort+"="), true
		}
	}
	return dir, found
}

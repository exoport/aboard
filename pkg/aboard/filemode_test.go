//go:build unix

// syscall.Umask is Unix-only, and this tree cross-compiles AND test-compiles for
// Windows (`make xcompile-windows`). File modes are a Unix concept anyway, so the
// whole file is tagged rather than each test guarded — a guard would still have
// to compile the call.

package aboard

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// 0o644 is the repo-wide file-mode policy (see the note in init.go): the board's
// whole purpose is to be read by the tools the developer already runs.
// `writeAtomic` used os.CreateTemp, which creates at 0600, and the rename
// carried that mode onto the state file — so the document `aboard init` wrote at
// 0644 silently dropped to 0600 on the server's FIRST accepted write, and only a
// second user or a differently-uid'd tool would ever notice.
func TestWriteAtomicKeepsTheDocumentedMode(t *testing.T) {
	// The umask is process-wide, so it is set explicitly and restored: an
	// inherited 0077 would make this assert the wrong number for the wrong
	// reason.
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	s := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)
	if err := os.Remove(s.stateFile); err != nil {
		t.Fatal(err)
	}
	if err := s.writeAtomic([]byte(`{"version":3,"rev":2,"nextId":1,"tabs":[]}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("state file mode is %04o, want 0644 (the documented policy)", got)
	}
	// And no temp file is left behind to inherit it either.
	entries, err := os.ReadDir(filepath.Dir(s.stateFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Name()) > 8 && e.Name()[:8] == ".aboard-" {
			t.Errorf("writeAtomic left %s behind", e.Name())
		}
	}
}

// The umask is the user's decision and a program does not get to overrule it.
// Asking the kernel for 0o644 through O_CREATE is what makes this true; a chmod
// after the fact would hand a 0077 user a group-readable board they had
// explicitly asked not to have.
func TestWriteAtomicRespectsTheUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })

	s := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)
	if err := os.Remove(s.stateFile); err != nil {
		t.Fatal(err)
	}
	if err := s.writeAtomic([]byte(`{"version":3,"rev":2,"nextId":1,"tabs":[]}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("state file mode is %04o, want 0600 — 0644 masked by a 0077 umask", got)
	}
}

// A file that already exists keeps its own mode. Somebody who chmod-ed their
// board is not overruled by an ordinary write.
func TestWriteAtomicPreservesAnExistingMode(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	s := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)
	if err := os.Chmod(s.stateFile, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.writeAtomic([]byte(`{"version":3,"rev":2,"nextId":1,"tabs":[]}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("state file mode is %04o; a write reset a mode its owner chose", got)
	}
}

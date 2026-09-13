//go:build unix

package aboard

import "syscall"

// A session of its own, which is also a process group of its own: what takes a
// `nohup … &` board down is its group being killed, and a new session leaves it.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

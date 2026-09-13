//go:build windows

package aboard

import "syscall"

// DETACHED_PROCESS has no console to die with, and CREATE_NEW_PROCESS_GROUP keeps
// a Ctrl+C aimed at the starting console from reaching it. Literals, because the
// syscall package names only the second.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup, HideWindow: true}
}

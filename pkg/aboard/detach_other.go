//go:build !unix && !windows

package aboard

import "syscall"

// Nowhere this binary is released for. The server still starts; it is simply not
// in a session of its own.
func detachedProcAttr() *syscall.SysProcAttr { return nil }

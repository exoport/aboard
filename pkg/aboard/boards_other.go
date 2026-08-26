//go:build !linux

// boards_other.go — what `aboard boards` says where there is no process table.
//
// A stub rather than an absent command, and that is the decision: the reader who
// types `aboard boards` on a Mac gets the reason and the alternative in one line,
// where an unknown-command error would tell them only that they had mistyped
// something. The command is declared on every platform for the same reason —
// the manifest describes this binary's surface, and a surface that changes
// shape per OS is one an agent cannot reason about from a capability listing.

package aboard

import (
	"context"
	"runtime"
)

// procDir is empty here because there is nothing to point it at. Declared so
// Boards compiles from one body on every platform.
const procDir = ""

// scanBoards refuses, with the reason and the alternative. The message itself is
// built in boards.go, where it is compiled and tested on Linux too — a refusal
// that only exists on the platforms nobody develops on is a refusal nobody ever
// reads before a user does.
func scanBoards(context.Context, string) (BoardsReport, error) {
	return BoardsReport{}, noProcessTable(runtime.GOOS)
}

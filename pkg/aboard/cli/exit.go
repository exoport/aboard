// exit.go — errors that carry the process status they should produce.
//
// Copied in shape from ape's internal/apecmd/report.go, on purpose: a developer
// who knows one CLI in this tree should not have to learn a second exit
// convention. The property that matters is that command bodies never call
// os.Exit — they return an error, Execute maps it, and cmd/aboard/main.go is the
// only place a status leaves the process. That is also what makes the commands
// testable in-process: os.Exit inside a RunE would kill the test binary on the
// first assertion.
package cli

import (
	"errors"

	"github.com/exoport/aboard/pkg/aboard"
)

// exitError couples an error with the exit status the command should return.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// usageErr marks a flag or argument the command cannot act on — detected before
// anything was contacted.
func usageErr(err error) error { return &exitError{code: aboard.ExitUsage, err: err} }

// codeErr carries an explicit status out of a command that computed one itself
// (`wait` timing out, `capabilities --check` finding drift). err may be nil when
// the command already printed its own account of the outcome.
func codeErr(code int, err error) error {
	if code == aboard.ExitOK {
		return nil
	}
	if err == nil {
		return &exitError{code: code, err: errSilent}
	}
	return &exitError{code: code, err: err}
}

// errSilent stands in for "the command already said what happened". Execute
// recognises it and prints nothing further, so a `wait` that timed out does not
// get an "Error: exit status 3" line underneath the event it just printed.
var errSilent = errors.New("")

// ExitCode maps the error Execute received onto a process status. `silent`
// reports that the error already conveyed its outcome, so the caller can skip a
// redundant "Error:" line.
func ExitCode(err error) (code int, silent bool) {
	if err == nil {
		return aboard.ExitOK, false
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, errors.Is(ee.err, errSilent)
	}
	return aboard.ExitFailed, false
}

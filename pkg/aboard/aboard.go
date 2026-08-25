// Package aboard is the board engine: the server, the state document and its
// guarantees, the capability manifest, and the client-side helpers every
// subcommand is built from.
//
// It is a LIBRARY, not a program. The cobra tree lives in pkg/aboard/cli and the
// process entry point in cmd/aboard, because this tree is meant to be mounted
// inside another CLI as a subcommand. That embedding is what the constraints in
// here are for, and each one is a thing a host binary cannot recover from:
//
//   - no os.Exit outside cli.Execute and main — a library that exits takes the
//     host's process with it, including its deferred cleanup;
//   - no flag.Parse and no package-level cobra variables — a host owns its own
//     flag set, and package-level command state cannot be mounted twice;
//   - no reads of os.Args — the process name belongs to the host, so identity is
//     injected through Options instead of sniffed;
//   - no log.SetFlags/log.SetOutput at package level — global logger surgery is
//     the host's call, so server logging goes through Options.Logger.
//
// The engine takes an fs.FS for its web assets rather than embedding them, which
// is the same seam that makes `serve --dev` work: os.DirFS over the tree on disk
// instead of the compiled-in copy.
package aboard

import (
	"io"
	"log"
)

// AppName is what the board calls ITSELF, in the capability manifest and in
// user-facing prose. It describes the board, not the process serving it, so it
// does not change when ape hosts the tree — which is what keeps capsHash
// independent of the host.
const AppName = "aboard"

// The two identities a running board can have. They appear in /health and in the
// instance file as `app`, so a client can tell whose port it just found, and
// probeBoard accepts either.
//
// Standalone and hosted are distinguished rather than merged because the answer
// to "how do I talk to this thing" differs: one is `aboard <cmd>`, the other is
// `ape aboard <cmd>`. A single identity would make an error message guess.
const (
	HostStandalone = "aboard"
	HostApe        = "ape-aboard"
)

// Options is what a host tells the engine about itself. Everything in here is
// something the engine must not go and find out on its own.
type Options struct {
	// Host is HostStandalone or HostApe. Empty means HostStandalone.
	Host string

	// Argv0 is the command the user actually typed, for error messages and for
	// the instance record. Read from os.Args at the cmd layer and passed in:
	// the engine never reads os.Args, so an embedded tree cannot mistake the
	// host's name for its own.
	Argv0 string

	// Logger receives the server's operational output. nil means log.Default().
	Logger *log.Logger

	// Stdin, Stdout and Stderr are the streams the client-side helpers read and
	// write. nil means the process's own, resolved by the cli layer.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// HostID resolves Options.Host, defaulting to standalone.
func (o Options) HostID() string {
	if o.Host == "" {
		return HostStandalone
	}
	return o.Host
}

// Log resolves Options.Logger, defaulting to the standard logger. Resolved at
// use rather than at construction so a zero Options is usable.
func (o Options) Log() *log.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return log.Default()
}

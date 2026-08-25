// Command board serves a project's shared visual board and drives it from the
// terminal.
//
// This file is deliberately the whole of the process layer: it is the only place
// in the tree that turns an outcome into an exit status, which is what lets the
// same command tree be mounted inside another CLI without taking that host's
// process down with it.
package main

import (
	"os"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/exoport/aboard/pkg/aboard/cli"
)

func main() {
	os.Exit(cli.Execute(cli.Options{
		Host:  aboard.HostStandalone,
		Argv0: os.Args[0],
	}))
}

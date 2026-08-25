// Package cli is the board's cobra tree.
//
// It is built by a FUNCTION rather than assembled into package-level variables,
// because the whole point of this shape is that another CLI can mount it:
//
//	root.AddCommand(cli.NewRootCmd(cli.Options{Host: aboard.HostApe}))
//
// Package-level cobra state cannot be mounted twice, an init() that registers
// commands runs whether or not the host wanted them, and a command that calls
// os.Exit takes the host's process down with it. So: no package vars, no init,
// no os.Exit outside Execute.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

// Options is what the host tells the board about itself. Aliased to the
// engine's type so there is one shape and no conversion to keep in step.
type Options = aboard.Options

// NewRootCmd builds the board command tree.
func NewRootCmd(opts Options) *cobra.Command {
	root := &cobra.Command{
		Use:   "aboard",
		Short: "A shared visual board for a human and one or more agent sessions",
		Long: `aboard serves a browser UI for a project and keeps its state in a file both
sides read and write. Tabs are DATA, not code: an agent opens one for whatever it
needs to show — a graph, a chart, a question form, an annotated screenshot, a
channel to another session, a bespoke widget — and reads back what the human
changed.

State lives under .aboard/ at the project root, which is found by walking up from
--cwd. Each project gets its own port, derived from that root, so the URL is the
same every time and two checkouts never collide.

Start with:

  aboard serve            run the server for this project
  aboard status           what is running here, and on which port
  aboard capabilities     what this board can do (no server needed)`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       aboard.Version(),
	}

	// Flag parse failures are usage errors, not run failures, and cobra hands
	// them out as plain errors — so they are typed here, once, on the root that
	// every subcommand inherits from.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })

	// No generated `completion` subcommand. This tree is meant to be mounted
	// inside another CLI, where shell completion belongs to the host's root and
	// a nested one would be wrong — and here it would be the one command not in
	// the declared table, which is the property the parity test rests on.
	root.CompletionOptions.DisableDefaultCmd = true

	root.PersistentFlags().String("cwd", "", "directory to resolve the project root from (default: the working directory)")

	root.AddCommand(
		newServeCmd(opts),
		newStatusCmd(opts),
		newApplyCmd(opts),
		newWaitCmd(opts),
		newPokeCmd(opts),
		newJournalCmd(opts),
		newWatchCmd(opts),
		newLogCmd(opts),
		newExportCmd(opts),
		newCapabilitiesCmd(opts),
		newVersionCmd(opts),
	)
	return root
}

// Execute runs the tree with a signal-cancelled context and returns the process
// status. The ONLY caller that should turn this into os.Exit is a main().
func Execute(opts Options) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	root := NewRootCmd(opts)
	root.SetIn(stdin(opts))
	root.SetOut(stdout(opts))
	root.SetErr(stderr(opts))

	err := root.ExecuteContext(ctx)
	stop() // release the signal handler before returning to main

	// Cobra has no typed error for an unknown subcommand, so it is recognised by
	// the one thing available. Getting it wrong costs a 1 where a 2 belonged;
	// leaving it out would report every typo'd command as a run failure.
	if err != nil && strings.HasPrefix(err.Error(), "unknown command") {
		err = usageErr(err)
	}

	code, silent := ExitCode(err)
	if err != nil && !silent {
		fmt.Fprintf(stderr(opts), "Error: %s\n", err)
	}
	return code
}

/* ---------- shared resolution ---------- */

func stdin(o Options) io.Reader {
	if o.Stdin != nil {
		return o.Stdin
	}
	return os.Stdin
}

func stdout(o Options) io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

func stderr(o Options) io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// startDir is --cwd, or the working directory.
func startDir(cmd *cobra.Command) (string, error) {
	dir, err := cmd.Flags().GetString("cwd")
	if err != nil {
		return "", usageErr(err)
	}
	if dir != "" {
		return dir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine the working directory: %w", err)
	}
	return wd, nil
}

// projectRoot resolves the root every path hangs off. A missing root is a
// FAILURE with a message that names what was looked for, not a silent fallback
// to the working directory: writing a board into whatever directory you happened
// to be in is how the spike ended up with two of them.
func projectRoot(cmd *cobra.Command) (aboard.Root, error) {
	start, err := startDir(cmd)
	if err != nil {
		return "", err
	}
	root, err := aboard.FindRoot(start)
	if err != nil {
		if errors.Is(err, aboard.ErrNoRoot) {
			return "", fmt.Errorf("%w — create %s/ in the project you want a board for", err, aboard.DirName)
		}
		return "", err
	}
	return root, nil
}

// looseRoot is projectRoot for the commands that describe the BINARY rather than
// a board. `capabilities` must answer in a directory that has never held a
// board — that is the property that lets an agent ask what a copied binary can
// do before deciding to use it — so a failed walk falls back to the start
// directory instead of refusing.
func looseRoot(cmd *cobra.Command) (aboard.Root, error) {
	start, err := startDir(cmd)
	if err != nil {
		return "", err
	}
	if root, err := aboard.FindRoot(start); err == nil {
		return root, nil
	}
	return aboard.NewRoot(start)
}

// boardName resolves --name, falling back to the environment.
//
// ABOARD_NAME rather than a flag on every command: only `serve` and `apply` take
// the flag, and the rest read the environment, so a session working on a named
// board exports it once instead of repeating it.
func boardName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return os.Getenv("ABOARD_NAME")
}

// envInt reads a positive integer from the environment, for the settings that
// have an env fallback but must still declare a static flag default — a default
// that changed with the environment would move capsHash with it.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return def
}

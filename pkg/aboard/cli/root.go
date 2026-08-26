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
		Version:       aboard.VersionString(),
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

	// --name is PERSISTENT, not per-command. It used to sit on `serve` and
	// `apply` only, with every other command reading ABOARD_NAME from the
	// environment — so `aboard serve --name review` worked and `aboard status
	// --name review` was an unknown-flag error, which reads as the second board
	// not existing. A board name selects the DOCUMENT; every command that touches
	// a board needs it, so it belongs to the root.
	//
	// The default is the empty string and NOT os.Getenv("ABOARD_NAME"), even
	// though the environment is the fallback: a flag default that changed with
	// the environment would be reported by the manifest, and capsHash would move
	// when someone exported a variable. The environment is resolved at use, in
	// boardName.
	root.PersistentFlags().String("name", "", "board name, for a second isolated board in the same project (env ABOARD_NAME)")

	root.AddCommand(
		newServeCmd(opts),
		newStatusCmd(opts),
		newBoardsCmd(opts),
		newInitCmd(opts),
		newApplyCmd(opts),
		newRequestsCmd(opts),
		newWaitCmd(opts),
		newPokeCmd(opts),
		newJournalCmd(opts),
		newHistoryCmd(opts),
		newWatchCmd(opts),
		newLogCmd(opts),
		newRenderedCmd(opts),
		newUploadsCmd(opts),
		newExportCmd(opts),
		newCapabilitiesCmd(opts),
		newRecipesCmd(opts),
		newVersionCmd(opts),
		newGenDocsCmd(opts),
	)

	// Argument-count errors are USAGE errors, and cobra hands them out untyped.
	// Without this, `aboard export` with no argument exited 1 — indistinguishable
	// from `aboard export nosuchtab`, which is a board that ran and could not find
	// the tab — while the declared table advertises 2 for exactly this case.
	//
	// Applied by walking the finished tree rather than at each declaration: there
	// are seventeen `Args:` in this package and a new command that forgot the
	// wrapper would report the wrong status with nothing to notice it. One call
	// here covers the tree, subcommands included, and it is asserted by
	// TestArgumentCountErrorsExitUsage.
	typeArgErrors(root)
	return root
}

// typeArgErrors wraps every Args validator in the tree so its failures carry
// exit 2. A nil validator is left alone: cobra treats that as "arbitrary args",
// so there is nothing that can fail.
func typeArgErrors(cmd *cobra.Command) {
	if check := cmd.Args; check != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			if err := check(c, args); err != nil {
				return usageErr(err)
			}
			return nil
		}
	}
	for _, sub := range cmd.Commands() {
		typeArgErrors(sub)
	}
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
			return "", fmt.Errorf("no %s/ here — run `aboard init` in the project you want a board for%s",
				aboard.DirName, legacyBoardHint(start))
		}
		return "", err
	}
	return root, nil
}

// legacyBoardHint recognises the spike's directory and says so.
//
// The board this grew out of kept its state in `.board/`, and a checkout that
// used it has one sitting there. Without this the message is "no .aboard/ here"
// beside a directory the reader can see that looks exactly like what was asked
// for, and the obvious conclusion is that the tool is broken rather than that
// the name changed. Not a migration — nothing is read out of it — just a
// sentence so the reader stops looking.
func legacyBoardHint(start string) string {
	if info, err := os.Stat(aboard.LegacyBoardDir(start)); err == nil && info.IsDir() {
		return " (this directory has a .board/ from the board spike — aboard keeps its state in " +
			aboard.DirName + "/ and does not read the old one)"
	}
	return ""
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

// boardName resolves the board this command acts on: the persistent --name
// flag, then ABOARD_NAME, then the default board.
//
// One function, called by every command that opens a document, so "which board"
// is answered the same way everywhere. A missing flag is not an error here — a
// host that mounts this tree without the root flag still gets the environment
// and the default.
//
// It VALIDATES, and it is the only place that has to: the name is interpolated
// into `.aboard/aboard.<name>.json` and `.aboard/run/instance.<name>.json`, so
// `--name ../../../../evil` used to write both files outside the project and
// report success. Refused here, before any path is joined, and as a usage error
// (exit 2) because nothing has been contacted yet.
func boardName(cmd *cobra.Command) (string, error) {
	name := ""
	if cmd != nil {
		if explicit, err := cmd.Flags().GetString("name"); err == nil {
			name = explicit
		}
	}
	if name == "" {
		name = strings.TrimSpace(os.Getenv("ABOARD_NAME"))
	}
	if err := aboard.ValidateBoardName(name); err != nil {
		return "", usageErr(err)
	}
	return name, nil
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

package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newServeCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var (
		state    string
		devDir   string
		basePath string
		port     int
		dev      bool
		detach   bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the board server for this project",
		Long: `Serve this project's board over HTTP and watch its state file for changes.

The port is derived from the discovered project root, so the URL is the same
every run and two checkouts never collide; --port or PORT overrides it. The
running instance is recorded in .aboard/run/instance.json, which is how every
other command finds the board and how restart.sh stops the right process.

--base-path serves the whole board under a URL prefix, for putting it behind a
reverse proxy or inside another tool's routing. The prefix is injected into the
shell, so every fetch, the SSE stream and an html tab's iframe all build from it.
Because it is injected, it is also validated: one or more /segments of letters,
digits, dot, underscore, tilde or hyphen. Anything else is a usage error.

--detach starts the same server in a session of its own, with its output in
.aboard/run/serve.log, and returns once it answers — so a board started from an
agent session outlives that session restarting, where one started with
` + "`nohup … &`" + ` dies with it. It still refuses a second board for this project, and
says so with the log of the process that refused. Stop it by the pid it prints.`,
		Args:    cobra.NoArgs,
		Example: "  " + inv.Cmd("serve") + "\n  " + inv.Cmd("serve --detach") + "\n  " + inv.Cmd("serve --dev") + "\n  " + inv.Cmd("serve --base-path /aboard"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Before the root is even resolved: the declared table says exit 2
			// means "detected before anything was contacted", and a base path
			// that cannot be one is exactly that. Serve refuses it again for an
			// embedder that never came through this command.
			if err := aboard.ValidateBasePath(basePath); err != nil {
				return usageErr(err)
			}
			root, err := projectRoot(cmd, opts.Invocation())
			if err != nil {
				return err
			}
			if port == 0 {
				port = envInt("PORT", 0)
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			cfg := aboard.ServeConfig{
				Root:      root,
				Name:      name,
				Port:      port,
				Dev:       dev,
				DevDir:    devDir,
				BasePath:  basePath,
				StateFile: root.Resolve(state),
			}
			if !dev && devDir != "" {
				return usageErr(errors.New("--dev-dir has no effect without --dev"))
			}
			if detach {
				return serveDetached(cmd, opts, root, name)
			}
			return aboard.Serve(cmd.Context(), opts, cfg)
		},
	}
	cmd.Flags().StringVar(&basePath, "base-path", "", "serve under a URL prefix, e.g. /aboard (default: the server root)")
	cmd.Flags().BoolVar(&detach, flagDetach, false, "start the server in a session of its own, log to .aboard/run/serve.log, and return once it answers")
	cmd.Flags().BoolVar(&dev, "dev", false, "serve the web tree from disk instead of the embedded copy")
	cmd.Flags().StringVar(&devDir, "dev-dir", "", "with --dev, the web tree to serve (default: pkg/aboard/web under the root)")
	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (0 derives one from the project root; env PORT)")
	cmd.Flags().StringVar(&state, "state", "", "state file to serve (default: .aboard/aboard.json under the root)")
	return cmd
}

const flagDetach = "detach"

// serveDetached re-runs this very command, minus --detach, as a process in a
// session of its own — the engine does the starting and the waiting, and this
// half only knows how to say the command again.
func serveDetached(cmd *cobra.Command, opts Options, root aboard.Root, name string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding this binary to start it detached: %w", err)
	}
	inst, err := aboard.StartDetached(cmd.Context(), root, name, opts.Invocation(), exe, detachArgs(cmd))
	if exit, ok := errors.AsType[*aboard.DetachedExitError](err); ok {
		// Its own words, which already say what went wrong — a board already
		// running here, no document, a busy port.
		fmt.Fprintln(stderr(opts), exit.Log)
		return codeErr(exit.Code, nil)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout(opts), "aboard running at %s (pid %d, detached)\n  log     %s\n", inst.URL, inst.PID, root.ServeLog(name))
	return nil
}

// detachArgs is the command line that reaches this command again from its own
// binary: the command path without the root's name — `serve` standalone, `aboard
// serve` under ape, whose binary is the one os.Executable names — and every flag
// that was set, inherited ones included, except --detach itself.
func detachArgs(cmd *cobra.Command) []string {
	args := strings.Fields(cmd.CommandPath())[1:]
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name == flagDetach {
			return
		}
		args = append(args, "--"+f.Name+"="+f.Value.String())
	})
	return args
}

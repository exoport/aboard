package cli

import (
	"errors"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newServeCmd(opts Options) *cobra.Command {
	var (
		state    string
		devDir   string
		basePath string
		port     int
		dev      bool
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
shell, so every fetch, the SSE stream and an html tab's iframe all build from it.`,
		Args:    cobra.NoArgs,
		Example: "  aboard serve\n  aboard serve --dev\n  aboard serve --base-path /aboard",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			if port == 0 {
				port = envInt("PORT", 0)
			}
			cfg := aboard.ServeConfig{
				Root:      root,
				Name:      boardName(cmd),
				Port:      port,
				Dev:       dev,
				DevDir:    devDir,
				BasePath:  basePath,
				StateFile: root.Resolve(state),
			}
			if !dev && devDir != "" {
				return usageErr(errors.New("--dev-dir has no effect without --dev"))
			}
			return aboard.Serve(cmd.Context(), opts, cfg)
		},
	}
	cmd.Flags().StringVar(&basePath, "base-path", "", "serve under a URL prefix, e.g. /aboard (default: the server root)")
	cmd.Flags().BoolVar(&dev, "dev", false, "serve the web tree from disk instead of the embedded copy")
	cmd.Flags().StringVar(&devDir, "dev-dir", "", "with --dev, the web tree to serve (default: pkg/aboard/web under the root)")
	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (0 derives one from the project root; env PORT)")
	cmd.Flags().StringVar(&state, "state", "", "state file to serve (default: .aboard/aboard.json under the root)")
	return cmd
}

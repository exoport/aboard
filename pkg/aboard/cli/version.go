package cli

import (
	"fmt"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

// versionReport is what `board version` knows about the binary running it.
type versionReport struct {
	App     string `json:"app"`
	Host    string `json:"host"`
	Version string `json:"version"`
	Built   string `json:"built,omitempty"`
	Schema  int    `json:"schema"`
}

func newVersionCmd(opts Options) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the build identity of this binary",
		Long: `Which binary is actually serving — never a constant somebody has to remember to
bump, because those lie eventually. Go stamps the VCS revision into a plain
build, so a local binary reports the commit it came from, with "+dirty" when the
tree had uncommitted changes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := versionReport{
				App:     aboard.AppName,
				Host:    opts.HostID(),
				Version: aboard.Version(),
				Built:   aboard.BuildStamp(),
				Schema:  aboard.SchemaVersion,
			}
			return renderOutput(stdout(opts), outputFormat, rep, func() string {
				return fmt.Sprintf("%s %s  (schema v%d, host %s)\n", rep.App, rep.Version, rep.Schema, rep.Host)
			})
		},
	}
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, "human, json or yaml")
	return cmd
}

package cli

import (
	"fmt"
	"strings"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

// versionReport is what `aboard version` knows about the binary running it.
//
// All three provenance fields are here and not only the version string: the
// linker stamps three, and a report that showed one of them would leave the
// other two unverifiable — which is how `-X` against a misnamed symbol goes
// unnoticed for a release cycle.
type versionReport struct {
	App       string `json:"app"                 yaml:"app"`
	Host      string `json:"host"                yaml:"host"`
	Version   string `json:"version"             yaml:"version"`
	BuildDate string `json:"buildDate,omitempty" yaml:"buildDate,omitempty"`
	GitCommit string `json:"gitCommit,omitempty" yaml:"gitCommit,omitempty"`
	Schema    int    `json:"schema"              yaml:"schema"`
}

func newVersionCmd(opts Options) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the build identity of this binary",
		Long: `Which binary is actually serving — never a constant somebody has to remember to
bump, because those lie eventually. Go stamps the VCS revision into a plain
build, so a local binary reports the commit it came from, with "+dirty" when the
tree had uncommitted changes.

A release build carries all three stamps (version, build date, commit) through
ldflags; --output-format json prints them whether or not they were stamped, so a
build that reports "dev" says so plainly instead of looking finished.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			id := aboard.Build()
			rep := versionReport{
				App:       aboard.AppName,
				Host:      opts.HostID(),
				Version:   id.Version,
				BuildDate: id.BuildDate,
				GitCommit: id.GitCommit,
				Schema:    aboard.SchemaVersion,
			}
			return renderOutput(stdout(opts), outputFormat, rep, rep.human)
		},
	}
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	return cmd
}

// human is one line, plus a second only when there is provenance worth a second
// line. A build with nothing stamped prints one line rather than two padded with
// dashes.
func (r versionReport) human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s  (schema v%d, host %s)\n", r.App, r.Version, r.Schema, r.Host)
	parts := make([]string, 0, 2)
	if r.GitCommit != "" {
		parts = append(parts, "commit "+r.GitCommit)
	}
	if r.BuildDate != "" {
		parts = append(parts, "built "+r.BuildDate)
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, "  %s\n", strings.Join(parts, "  ·  "))
	}
	return b.String()
}

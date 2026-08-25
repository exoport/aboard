package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newStatusCmd(opts Options) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report this project's running board, if any, and the caps beacon",
		Long: `Say whether a board is running for this project, where, and since when — and
whether the committed skill reference still matches this binary.

The caps line is the beacon: an agent runs status as its first act, so a skill
that was generated for a different capsHash is reported in a command it was
going to run anyway. A MISSING reference is not staleness; a project that never
copied the skill has nothing to be out of date.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			rep := aboard.Status(root, boardName(""), aboard.WebFS())
			return renderOutput(stdout(opts), outputFormat, rep, rep.Human)
		},
	}
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, "human, json or yaml")
	return cmd
}

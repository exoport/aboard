package cli

import (
	"fmt"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newExportCmd(opts Options) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "export <tab|key>",
		Short: "Print one tab as text, for pasting into the project's own documents",
		Long: `Turn a tab into markdown (or CSV, where it has rows) so its conclusions can be
promoted into a spec, an ADR, or whatever this project's own documents are.

Reads the board document from disk, so it works with no server running — for the
same reason ` + "`capabilities`" + ` does: an agent should never have to start a server to
read out a conclusion.

The strategy is not to promote early. It is to make LATE promotion cheap, and
retyping was the cost that made it expensive.`,
		Args:    cobra.ExactArgs(1),
		Example: "  aboard export bb128\n  aboard export table-example --format csv",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "md", "csv":
			default:
				return usageErr(fmt.Errorf("--format must be md or csv, got %q", format))
			}
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			return aboard.Export(root.StateFile(boardName("")), args[0], format,
				stdout(opts), stderr(opts))
		},
	}
	cmd.Flags().StringVar(&format, "format", "md", "md or csv")
	return cmd
}

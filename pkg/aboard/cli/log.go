package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newLogCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "log <tab>",
		Short: "Read stdin and append it to a tab's sidecar log, line by line",
		Long: `Pipe a long-running command's output onto the board, so the human can watch it
happen rather than waiting for it to finish.

The stream lives in a sidecar file under .aboard/run/logs/, NOT inside the board
document: that document is rewritten whole on every write, so an appending log
inside a tab's state would mean rewriting the entire board once per line. The
tab's state holds only a pointer.

A named board writes into .aboard/run/logs/<name>/ instead — tab ids are
allocated per board, so both boards have a ab1 and one directory would have
interleaved two commands' output in one file.

Lines are echoed to stdout as well — piping output to the board should not mean
losing it from the terminal you are watching.`,
		Args:    cobra.ExactArgs(1),
		Example: "  go test ./... 2>&1 | aboard log ab126",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			return aboard.Log(cmd.Context(), root, name, args[0], stdin(opts), stdout(opts))
		},
	}
}

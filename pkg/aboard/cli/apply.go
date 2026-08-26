package cli

import (
	"errors"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newApplyCmd(opts Options) *cobra.Command {
	var (
		by    string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Write a board document from stdin, through the running board",
		Long: `Read a whole board document on stdin and POST it to the running board, which
applies it under compare-and-set.

Never edit the state file directly while a board is running: a direct write has
no compare-and-set, so a concurrent change from the browser or another session is
destroyed with no error. A 409 here means somebody got there first — re-read,
redo the edit, apply again.

Warnings print on stderr before the write: a schema version the board does not
write, state no renderer reads, an unknown ui component or prop, a {bind} that
resolves nowhere, a colour name this board no longer has. They warn rather than
refuse, because a spec can lag its renderer — but read them, because "applied"
is not evidence that anything rendered.

The compare-and-set base is the ` + "`rev`" + ` inside the document you submit — the one
you read. A document with no ` + "`rev`" + ` is refused (exit 2) rather than written
unconditionally; --force writes it anyway and says so on stderr.`,
		Args:    cobra.NoArgs,
		Example: "  aboard apply --by agent-1 < next.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			if err := aboard.Apply(cmd.Context(), root, name, by, force, aboard.WebFS(),
				stdin(opts), stdout(opts), stderr(opts)); err != nil {
				// A document with no base is a USAGE refusal: nothing was
				// contacted, and the fix is to the document rather than to the
				// board. Exit 2 is what the declared table promises for that.
				if errors.Is(err, aboard.ErrNoBase) {
					return usageErr(err)
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&by, "by", "agent-1", "actor recorded in lastEditedBy and on every tab this write touched")
	cmd.Flags().BoolVar(&force, "force", false, "write without compare-and-set, overwriting anything since you read the document")
	return cmd
}

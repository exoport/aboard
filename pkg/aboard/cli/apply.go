package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newApplyCmd(opts Options) *cobra.Command {
	var name, by string
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
is not evidence that anything rendered.`,
		Args:    cobra.NoArgs,
		Example: "  aboard apply --by agent-1 < next.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			return aboard.Apply(root, boardName(name), by, aboard.WebFS(),
				stdin(opts), stdout(opts), stderr(opts))
		},
	}
	cmd.Flags().StringVar(&by, "by", "agent-1", "actor recorded in lastEditedBy and on every tab this write touched")
	cmd.Flags().StringVar(&name, "name", "", "board name (env ABOARD_NAME)")
	return cmd
}

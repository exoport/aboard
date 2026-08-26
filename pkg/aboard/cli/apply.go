package cli

import (
	"errors"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newApplyCmd(cliOpts Options) *cobra.Command {
	var opts aboard.ApplyOptions
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
is not evidence that anything rendered. The same warnings are recorded on the
journal entry and shown to the human on the tab, so a write that warns is no
longer something only the terminal knows about.

--check runs those checks and stops: nothing is posted, no board need be
running, and the document needs no ` + "`rev`" + `. --strict turns any warning into a
refusal (exit 1, nothing written) — the guard for a loop that must stop rather
than ship a wrong tab. Together they are "tell me and exit non-zero".

--label records WHY this write is happening on the journal entry, where the
journal, watch and trace commands show it. It is navigation inside a local,
rotating file — never a record to cite anywhere permanent.

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
			if err := aboard.Apply(cmd.Context(), root, name, opts, aboard.WebFS(),
				stdin(cliOpts), stdout(cliOpts), stderr(cliOpts)); err != nil {
				// A document with no base is a USAGE refusal: nothing was
				// contacted, and the fix is to the document rather than to the
				// board. Exit 2 is what the declared table promises for that.
				if errors.Is(err, aboard.ErrNoBase) {
					return usageErr(err)
				}
				// --strict refusing a warning document is exit 1, not 2. Nothing
				// about the INVOCATION was wrong — the flags were right and were
				// obeyed — and a script that tells "you called me wrongly" apart
				// from "the thing you asked me to guard against happened" needs
				// those to differ.
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.By, "by", aboard.DefaultActor, "actor recorded in lastEditedBy and on every tab this write touched")
	cmd.Flags().StringVar(&opts.Label, "label", "", "why this write is happening; recorded on the journal entry, not in the board")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "write without compare-and-set, overwriting anything since you read the document")
	cmd.Flags().BoolVar(&opts.Check, "check", false, "run the write warnings and stop: nothing is posted, and no board need be running")
	cmd.Flags().BoolVar(&opts.Strict, "strict", false, "refuse the write if anything warns (exit 1, nothing written)")
	return cmd
}

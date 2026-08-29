package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newRequestsCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var (
		tab          string
		all          bool
		outputFormat string
	)
	cmd := &cobra.Command{
		Use:   "requests",
		Short: "List the human's notes to an agent",
		Long: `What the human has asked for, tab by tab, oldest first.

Everything else on a board flows one way: an agent shows them something and
reads back what they changed. A request is the other direction — they point at a
tab and say "this is wrong, fix it" — so it needs a channel an agent can find
without being told to look, because by definition it arrives while nobody is
watching.

Run this at the start of a turn, next to ` + "`" + inv.Cmd("status") + "`" + ` (which prints the
count). Then say you did one:

  ` + inv.Cmd("requests") + ` done ab199 --by agent-1 --note "redrew the arrow"

Only the human writes these. An agent write that creates, edits, reorders or
deletes one has it restored by the server; adding a done stamp is the one change
an agent may make, and the stamp is never cleared — the human deleting the whole
note is how it goes away.

Needs no running board: it falls back to the state file. Stamping one does need
the board, for the same reason ` + "`" + inv.Cmd("apply") + "`" + ` does.`,
		Args:    cobra.NoArgs,
		Example: "  " + inv.Cmd("requests") + "\n  " + inv.Cmd("requests --tab ab14 --all"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkOutputFormat(outputFormat); err != nil {
				return err
			}
			root, err := projectRoot(cmd, opts.Invocation())
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			list, err := aboard.ListRequests(cmd.Context(), root, name, tab, all, opts.Invocation())
			if err != nil {
				return err
			}
			return renderOutput(stdout(opts), outputFormat, list,
				func() string { return aboard.RequestsHuman(list, tab, all, name, opts.Invocation()) })
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include the ones already stamped done")
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	cmd.Flags().StringVar(&tab, "tab", "", "only this tab's requests, by id or name")

	cmd.AddCommand(newRequestDoneCmd(opts))
	return cmd
}

func newRequestDoneCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var by, note string
	cmd := &cobra.Command{
		Use:   "done <request-id>",
		Short: "Say you acted on one of the human's requests",
		Long: `Stamp one request done: who did it, when, and optionally a line about what.

The board strikes the note through and prints the tick beside your name, which
is the only feedback the human gets that anything happened — so write the
` + "`--note`" + `. "done" tells them nothing they could not have guessed; "redrew the
arrow, it points at the worker now" is an answer.

A thin ` + "`apply`" + `: it reads the board, changes one field, and posts it back with
compare-and-set. It does not merge a conflict — run it again instead, which is
cheaper than any merge could be. Re-running one that is already stamped says so
and writes nothing.

--by human is refused: the stamp says which SESSION acted, and the human answers
their own requests by deleting them.`,
		Args:    cobra.ExactArgs(1),
		Example: `  ` + inv.Cmd("requests") + ` done ab199 --by agent-1 --note "redrew the arrow"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot(cmd, opts.Invocation())
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			return aboard.CompleteRequest(cmd.Context(), root, name, args[0], by, note, stdout(opts), opts.Invocation())
		},
	}
	cmd.Flags().StringVar(&by, "by", aboard.DefaultActor, "which session did it; shown beside the tick")
	cmd.Flags().StringVar(&note, "note", "", "one line about what you did, shown with the tick")
	return cmd
}

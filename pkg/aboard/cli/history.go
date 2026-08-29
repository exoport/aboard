package cli

import (
	"fmt"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

// defaultHistoryLimit mirrors the engine's, declared here because the flag's
// default is part of the manifest and a default that came from somewhere else
// would be one more thing to keep in step.
const defaultHistoryLimit = 20

func newHistoryCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var (
		at           int
		limit        int
		outputFormat string
	)
	cmd := &cobra.Command{
		Use:   "history <tab>",
		Short: "List what a tab said before, from the journal",
		Long: `Per-tab history, read out of the journal the board already keeps.

Every accepted write records each changed tab AS IT WAS, which is the unit
somebody undoing a bad write actually wants. This lists those versions newest
first, naming who replaced each one — and says plainly where the record ends,
because rotation keeps one older generation and a listing that just stopped would
read as "this tab has only ever been written twice".

  ` + inv.Cmd("history") + ` ab133                          what it said, and when
  ` + inv.Cmd("history") + ` ab133 --at 1 | ` + inv.Cmd("apply") + ` --by agent-1     put version 1 back

--at prints a WHOLE document with that one tab put back, not the tab on its own:
a single-tab document is a document that deletes every other tab, and the server
would answer it with a removal request on each one. It carries the board's
current ` + "`rev`" + `, so a restore built while somebody else was writing is refused
rather than clobbering.

What a restore carries depends on which generation of the record it came from,
and the listing marks each version with it. An entry stamped ` + "`schema: 2`" + ` holds
the whole tab, so the name, type, note, stateFrom and key come back with the state; an
older entry holds a bare state, and then only the state moves. Neither ever puts
back ` + "`touched`" + `, ` + "`pendingRemoval`" + ` or ` + "`seen`" + ` — re-raising a dot the human
dismissed, or re-opening a removal request they answered, is not an undo.

Reads from the running board when there is one and from
.aboard/run/journal.jsonl when there is not — journal.<name>.jsonl on a named
board, whose history is its own. The restore line the listing prints carries
--name on both halves for exactly that reason.`,
		Args:    cobra.ExactArgs(1),
		Example: "  " + inv.Cmd("history ab133") + "\n  " + inv.Cmd("history ab133 --at 1") + " | " + inv.Cmd("apply --by agent-1"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOutputFormat(outputFormat); err != nil {
				return err
			}
			if at < 0 {
				return usageErr(fmt.Errorf("--at must be 1 or more (1 is the most recent recorded version), got %d", at))
			}
			root, err := projectRoot(cmd, opts.Invocation())
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			if at > 0 {
				// A document on stdout and nothing else: this is meant to be piped
				// into `aboard apply`, so --output-format has nothing to say about
				// it and any prose here would corrupt the pipe.
				return aboard.Restore(cmd.Context(), root, name, args[0], at, stdout(opts), opts.Invocation())
			}
			got, err := aboard.History(cmd.Context(), root, name, args[0], limit, opts.Invocation())
			if err != nil {
				return err
			}
			if outputFormat == formatHuman {
				switch got.Source {
				case aboard.JournalFromDisk:
					fmt.Fprintf(stderr(opts), "(from disk: %s — no board running)\n", root.JournalFile(name))
				case aboard.JournalFromDiskStale:
					fmt.Fprintf(stderr(opts), "(from disk: %s — the recorded board is not answering; the instance record is stale)\n", root.JournalFile(name))
				}
			}
			return renderOutput(stdout(opts), outputFormat, got, func() string { return got.Human(opts.Invocation()) })
		},
	}
	cmd.Flags().IntVar(&at, "at", 0, "print the document that restores version N instead of listing (1 is the most recent)")
	cmd.Flags().IntVar(&limit, "limit", defaultHistoryLimit, "how many versions to list")
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	return cmd
}

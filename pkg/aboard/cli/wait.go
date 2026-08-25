package cli

import (
	"time"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newWaitCmd(opts Options) *cobra.Command {
	var (
		by      string
		forWhat string
		note    string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Block until the board is poked, or until a predicate matches",
		Long: `Block on one long-poll request until the human presses the notify button, until
another session pokes, or until the write you named arrives.

The predicate vocabulary is deliberately tiny, and an unknown one is refused up
front rather than accepted and never fired:

  poke                 the human pressed Notify (or another session poked)
  change               any accepted write at all
  tab bb71             that tab changed
  answer bb15          that tab changed AND a human made the change
  node bb58=done       that node reached that status

While you are waiting the human's button says who is waiting, why, and for how
long — so fill in --note. A waiter is an open connection, so the count cannot go
stale: if this process dies, the button stops claiming anyone is listening.

Exit 0 means released. Exit 3 means the timeout ran out and nobody came.`,
		Args:    cobra.NoArgs,
		Example: `  aboard wait --for "answer bb128" --note "waiting on the gate"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			code, err := aboard.Wait(root, boardName(""), by, forWhat, note, timeout, stdout(opts))
			if err != nil {
				if code == aboard.ExitUsage {
					return usageErr(err)
				}
				return err
			}
			// The event has already been printed; a further "Error:" line under
			// a clean timeout would read as a malfunction.
			return codeErr(code, nil)
		},
	}
	cmd.Flags().StringVar(&by, "by", "agent-1", "who is waiting; shown on the human's notify button")
	cmd.Flags().StringVar(&forWhat, "for", "poke", `what to wait for: poke | change | "tab <id>" | "answer <id>" | "node <id>=<status>"`)
	cmd.Flags().StringVar(&note, "note", "", "why you are waiting; shown on the button beside your name")
	cmd.Flags().DurationVar(&timeout, "timeout", aboard.WaitDefault, "how long to block before giving up")
	return cmd
}

func newPokeCmd(opts Options) *cobra.Command {
	var by, note string
	cmd := &cobra.Command{
		Use:   "poke",
		Short: "Release every session waiting on this board",
		Long: `Do what the human's notify button does: release every session currently blocked
on ` + "`aboard wait`" + `, and tell them who released them and why.

Nothing here starts an agent. A session is released only if it had already
decided to listen; a board with nobody waiting is simply not listening, and this
command says so rather than pretending otherwise.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			return aboard.Poke(root, boardName(""), by, note, stdout(opts))
		},
	}
	cmd.Flags().StringVar(&by, "by", "agent-1", "who is releasing them")
	cmd.Flags().StringVar(&note, "note", "", "a message for the waiting sessions")
	return cmd
}

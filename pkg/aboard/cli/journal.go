package cli

import (
	"fmt"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

// defaultJournalLimit is how many entries `aboard journal` prints unasked —
// about a screenful, which is the window a session resuming after a context
// clear actually reads.
const defaultJournalLimit = 40

func newJournalCmd(opts Options) *cobra.Command {
	var (
		limit        int
		outputFormat string
	)
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Print recent accepted writes: when, who, which tabs",
		Long: `Per-write history of this board: the time, the actor, and the tabs that changed.

Reads from the running board when there is one and from .aboard/run/journal.jsonl
when there is not, so this works in a project whose board is stopped — it is the
third command of the resume protocol, and a session that has just cleared its
context has no reason to start a server before asking what happened.

With two sessions and a human writing one document, "who changed the plan while I
was thinking?" otherwise has no answer except git archaeology over a file that
moves constantly. Every accepted write funnels through one function, so this
cannot be bypassed by an agent that forgot to record something.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			entries, source, err := aboard.JournalEntries(cmd.Context(), root, name, limit)
			if err != nil {
				return err
			}
			// Said on stderr and only in human mode: structured output goes to a
			// consumer that would have to filter prose back out of it, and the
			// provenance of the answer is a diagnostic, not data.
			if outputFormat == formatHuman {
				switch source {
				case aboard.JournalFromDisk:
					fmt.Fprintf(stderr(opts), "(from disk: %s — no board running)\n", root.JournalFile())
				case aboard.JournalFromDiskStale:
					fmt.Fprintf(stderr(opts), "(from disk: %s — the recorded board is not answering; the instance record is stale)\n", root.JournalFile())
				}
			}
			return renderOutput(stdout(opts), outputFormat, entries,
				func() string { return aboard.JournalHuman(entries) })
		},
	}
	cmd.Flags().IntVar(&limit, "limit", defaultJournalLimit, "how many entries to print")
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, "human, json or yaml")
	return cmd
}

func newWatchCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Follow every change as JSON lines until interrupted",
		Long: `Stream each accepted write as one JSON object per line, as it happens.

Not SSE: the consumer here is a shell pipeline, and ` + "`data: `" + ` prefixes would just
be something for jq to strip. Each line says THAT something changed and which
tabs; re-read the board for the contents.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(cmd)
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			return aboard.Watch(cmd.Context(), root, name, stdout(opts))
		},
	}
}

package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newJournalCmd(opts Options) *cobra.Command {
	var (
		limit        int
		outputFormat string
	)
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Print recent accepted writes: when, who, which tabs",
		Long: `Per-write history of this board: the time, the actor, and the tabs that changed.

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
			entries, err := aboard.JournalEntries(root, boardName(""), limit)
			if err != nil {
				return err
			}
			return renderOutput(stdout(opts), outputFormat, entries,
				func() string { return aboard.JournalHuman(entries) })
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 40, "how many entries to print")
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
			return aboard.Watch(root, boardName(""), stdout(opts))
		},
	}
}

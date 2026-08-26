package cli

import (
	"errors"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newBoardsCmd(opts Options) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "boards",
		Short: "List every board running on this machine, from the process table (Linux only)",
		Long: `Every running board on this machine, whichever project it belongs to.

This is the cross-project half of ` + "`aboard status`" + `. It needs no project of its
own — it works from a directory that has never held a board — because it asks
the PROCESS TABLE rather than a registry: it walks /proc for an ` + "`aboard serve`" + `
or an ` + "`ape aboard serve`" + `, resolves each one's project root, and then does exactly
what status does for one project — read the instance record, verify it over
/health, and report what the board says about itself.

One row per (project, name), because two boards in one project are two boards.
The project path is printed in full: the point of a machine-wide listing is that
you are not standing in the project it names.

Nothing is trusted that a live board did not just confirm. A record whose process
has gone is listed as "recorded but not answering" rather than dropped — a stale
record is information — and the count of processes inspected is printed with the
result, because "no board found" after 3 processes and after 400 mean different
things.

/proc is Linux only. Everywhere else this command exits 2 and says so, and the
per-project answer is ` + "`aboard status`" + ` inside each project.`,
		Args:    cobra.NoArgs,
		Example: "  aboard boards\n  aboard boards --output-format json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkOutputFormat(outputFormat); err != nil {
				return err
			}
			rep, err := aboard.Boards(cmd.Context())
			if err != nil {
				return boardsExit(err)
			}
			return renderOutput(stdout(opts), outputFormat, rep, rep.Human)
		},
	}
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	return cmd
}

// boardsExit maps what the scan refused with onto the status the declared table
// promises.
//
// Exit 2, not 1, for a platform with no process table: "this system cannot
// answer" is decided before anything is contacted and is not fixable by
// retrying, which is exactly what the table's 2 means — while a 1 would read as
// "the scan ran and found nothing", the one conclusion that must not be drawn
// from it. Anything else is a real failure to read a real /proc, which is a 1.
//
// Its own function because it is the ONE line of this command that no machine in
// this project ever executes: the refusal it dispatches on cannot be produced on
// Linux, so without a seam the branch deciding what a macOS reader's shell sees
// would be asserted by nothing at all.
func boardsExit(err error) error {
	if errors.Is(err, aboard.ErrNoProcessTable) {
		return usageErr(err)
	}
	return err
}

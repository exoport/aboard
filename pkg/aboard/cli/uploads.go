package cli

import (
	"errors"
	"fmt"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newUploadsCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var (
		prune        bool
		yes          bool
		outputFormat string
	)
	cmd := &cobra.Command{
		Use:   "uploads",
		Short: "List the files under .aboard/uploads/ and the tabs that mention them",
		Long: `Every image the human pasted or dropped, with its size and the tabs that name it.

The reference scan reads each tab's RAW state text, plus its name and note — not
its declared fields. An html widget's markup can name a file no spec knows
about, and a scan over declared fields would call that file an orphan and offer
to delete an image somebody is looking at.

  ` + inv.Cmd("uploads") + `                    list them, unreferenced ones marked *
  ` + inv.Cmd("uploads") + ` --prune            show exactly what deleting them would remove
  ` + inv.Cmd("uploads") + ` --prune --yes      delete them

--prune on its own prints and REFUSES: deletion is irreversible and .aboard/ is
gitignored, so there is no copy anywhere to go back to.

The accounting is per PROJECT, not per board, so --name does not narrow it:
.aboard/uploads/ is shared by every board in the project, and a scan of one
board's tabs would call another board's image an orphan. Tab ids from a named
board are printed as <board>:<tab>.

Reads the state files directly, so it needs no server.`,
		Args:    cobra.NoArgs,
		Example: "  " + inv.Cmd("uploads") + "\n  " + inv.Cmd("uploads --prune --yes"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkOutputFormat(outputFormat); err != nil {
				return err
			}
			if yes && !prune {
				return usageErr(errors.New("--yes only means something with --prune; there is nothing to confirm"))
			}
			root, err := projectRoot(cmd, opts.Invocation())
			if err != nil {
				return err
			}
			rep, err := aboard.Uploads(root, opts.Invocation())
			if err != nil {
				return err
			}
			if err := renderOutput(stdout(opts), outputFormat, rep,
				func() string { return rep.Human(prune, opts.Invocation()) }); err != nil {
				return err
			}
			if !prune || rep.Orphaned == 0 {
				return nil
			}
			if !yes {
				// Printed on stdout with the listing it refers to, because it is the
				// answer to what the user asked, not a diagnostic about it.
				fmt.Fprintf(stdout(opts),
					"\nwould delete %d unreferenced file(s). Nothing has been removed — add --yes to actually delete them.\n",
					rep.Orphaned)
				return nil
			}
			removed, err := aboard.PruneUploads(root, rep.Files)
			for _, name := range removed {
				fmt.Fprintf(stdout(opts), "removed %s\n", name)
			}
			if err != nil {
				return fmt.Errorf("removing uploads: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "show which unreferenced files would be deleted")
	cmd.Flags().BoolVar(&yes, "yes", false, "with --prune, actually delete them")
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	return cmd
}

package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newCapabilitiesCmd(opts Options) *cobra.Command {
	var (
		format string
		check  bool
	)
	cmd := &cobra.Command{
		Use:   "capabilities [type]",
		Short: "Print what this board can do: types, state fields, controls, endpoints, commands",
		Long: `Ask the BINARY what it can do, rather than reconstructing it from a document.

Every renderer declares its own surface in views/<type>.spec.json beside the code
it describes, and this aggregates those with the declared command table and the
route list. It needs no running server and no project: a fresh checkout, a copied
binary, or another session holding the port all still answer.

  board capabilities            the whole manifest, as JSON
  board capabilities kanban     one type — cheap, for a mid-task lookup
  board capabilities --format md    the markdown reference the skill commits
  board capabilities --check    exit 1 if that committed reference is stale

--check treats a MISSING reference as "nothing to check": a project that never
copied the skill has nothing to be out of date.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := looseRoot(cmd)
			if err != nil {
				return err
			}
			only := ""
			if len(args) == 1 {
				only = args[0]
			}
			code, err := aboard.Capabilities(root, aboard.WebFS(), format, only, check, stdout(opts))
			if err != nil {
				if code == aboard.ExitUsage {
					return usageErr(err)
				}
				return err
			}
			// --check already printed which file is stale and what to run.
			return codeErr(code, nil)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit 1 if the committed skill reference is stale")
	cmd.Flags().StringVar(&format, "format", "json", "json, md, or js (the generated control module)")
	return cmd
}

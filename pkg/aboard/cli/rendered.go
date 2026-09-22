package cli

import (
	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newRenderedCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "rendered [tab]",
		Short: "Print what the browser reported it drew",
		Long: `What a real browser actually put on screen for a tab, as it reported it.

` + "`" + inv.Cmd("apply") + "`" + ` printing "applied" is evidence a write was accepted, not that
anything renders — an unknown ` + "`ui`" + ` component draws a marker and an unknown PROP
draws nothing at all. After every mount the shell posts the control ids it drew,
the ones somebody pressed, and any unknown-component markers, and this prints
them.

It also says what did NOT FIT, at the width that browser had: a ` + "`ui`" + `
component or an ` + "`html`" + ` widget whose content is larger than its box — cut off
(overflow hidden), spilling past its edge (an unbroken URL), or scrolling inside
the tab (a wide code block or table). Only those two renderers measure; the
others draw the board's own layouts, which truncate on purpose.

This is NOT a DOM sweep. Every id here is already declared in
views/<type>.spec.json; nothing is scraped and nothing is matched against prose.

Two things it is deliberately not evidence of, printed with the output so they
travel with it: no receipt means nobody had the tab OPEN, and a control listed
here was REACHED — never that it behaved correctly.

Reads .aboard/run/rendered.json — or rendered.<name>.json on a named board — so
it needs no server. With no argument it prints every tab that has a receipt.`,
		Args:    cobra.MaximumNArgs(1),
		Example: "  " + inv.Cmd("rendered ab133") + "\n  " + inv.Cmd("rendered"),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			tab := ""
			if len(args) == 1 {
				tab = args[0]
			}
			list, err := aboard.Rendered(cmd.Context(), root, name, tab)
			if err != nil {
				return err
			}
			return renderOutput(stdout(opts), outputFormat, list,
				func() string { return aboard.RenderedHuman(tab, list) })
		},
	}
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	return cmd
}

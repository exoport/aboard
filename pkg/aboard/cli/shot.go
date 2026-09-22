package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newShotCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var (
		outputFormat string
		shot         aboard.ShotOptions
	)
	cmd := &cobra.Command{
		Use:   "shot <tab>...",
		Short: "Screenshot tabs of the running board with a headless browser",
		Long: `Take a picture of a tab as the human sees it, and print where it went. Then
READ the picture: ` + "`" + inv.Cmd("apply") + "`" + ` exits 0 for a tree that draws an empty box, and
the write warnings cannot see a layout that is legal and still unreadable.

A tab is named by id, key or type. The board must be running — the picture is of
the page the binary serving it draws — and a chromium-family browser must be
installed: chromium, google-chrome or Edge, found on $PATH or in its usual place,
or named with --browser (or ABOARD_BROWSER).

Pictures go to .aboard/run/shots/<tab>.png, and the previous one is removed
first, so a file there is this run's or nothing.

Under each picture it lists what did NOT FIT in that window, as the page itself
measured it: a ui component or an html widget whose content is larger than its
box, cut off, spilling past its edge, or scrolling inside the tab. That catches
what a picture hides — the text below a clipped edge, the column a table
scrolls away — and it is measured at --width, so try a narrower one too.

What it knows so you do not have to:

  a ui tab's panels     --node <panel label> opens that panel; without it the
                        picture shows the FIRST one, and the output says so
  an html tab           is shot on its own route, because a headless browser
                        does not reliably paint the frame inside the board
  a snap browser        renders into its own directory and the file is moved,
                        because a snap cannot write to a project under a hidden
                        directory such as ~/.cache, or outside $HOME
  mount receipts        none are posted: ` + "`" + inv.Cmd("rendered") + "`" + ` and
                        ` + "`" + inv.Cmd(`wait --for "rendered <id>"`) + "`" + ` go on meaning a person
                        had the tab open

It never writes to the board.`,
		Args: cobra.MinimumNArgs(1),
		Example: "  " + inv.Cmd("shot ab24") + "\n" +
			"  " + inv.Cmd("shot ab24 --node Summary") + "\n" +
			"  " + inv.Cmd("shot kanban dag --theme light --width 1000"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOutputFormat(outputFormat); err != nil {
				return err
			}
			switch shot.Theme {
			case "", "dark", "light":
			default:
				return usageErr(fmt.Errorf("--theme must be dark or light, got %q", shot.Theme))
			}
			if shot.Width <= 0 || shot.Height <= 0 {
				return usageErr(fmt.Errorf("--width and --height must be positive, got %dx%d", shot.Width, shot.Height))
			}
			if shot.Timeout <= 0 {
				return usageErr(fmt.Errorf("--timeout must be positive, got %s", shot.Timeout))
			}
			// One node, one tab: an id or a panel label belongs to a tab, and the
			// same word on five tabs is five different things or, more likely, a
			// mistake.
			if shot.Node != "" && len(args) > 1 {
				return usageErr(fmt.Errorf("--node names something inside ONE tab; got %d tabs", len(args)))
			}
			root, err := projectRoot(cmd, inv)
			if err != nil {
				return err
			}
			name, err := boardName(cmd)
			if err != nil {
				return err
			}
			// The environment is read here, at use, and not as the flag's
			// default: a default that moved with the environment would move
			// capsHash with it.
			if shot.Browser == "" {
				shot.Browser = strings.TrimSpace(os.Getenv("ABOARD_BROWSER"))
			}
			shot.Targets = args

			results, err := aboard.Shot(cmd.Context(), root, name, shot, inv)
			if results != nil {
				if rerr := renderOutput(stdout(opts), outputFormat, results,
					func() string { return aboard.ShotsHuman(results) }); rerr != nil {
					return rerr
				}
			}
			// A failed picture is already in the output with its reason; the
			// error adds the one line that says the run as a whole did not
			// succeed, and exit 1.
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&shot.Browser, "browser", "", "the chromium-family browser to drive, a path or a name on $PATH (env ABOARD_BROWSER; default: the first one found)")
	f.IntVar(&shot.Height, "height", aboard.ShotDefaultHeight, "window height in pixels")
	f.BoolVar(&shot.HelpPanel, "help-panel", false, "open the board's help panel over the tab")
	f.StringVar(&shot.Node, "node", "", "a node's id or a ui panel's label to open first; refused if the tab has none")
	f.StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	f.StringVar(&shot.Theme, "theme", "", "dark or light (default: the board's own default)")
	f.DurationVar(&shot.Timeout, "timeout", aboard.ShotDefaultTimeout, "how long each picture may take")
	f.IntVar(&shot.Width, "width", aboard.ShotDefaultWidth, "window width in pixels")
	return cmd
}

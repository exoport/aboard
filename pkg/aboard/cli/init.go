package cli

import (
	"fmt"
	"strings"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newInitCmd(opts Options) *cobra.Command {
	var example, gitignore bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create .aboard/ in this directory and write an empty board",
		Long: `Create a board for the project you are standing in: ` + "`.aboard/`" + ` with an empty
document, an uploads directory, a recipes directory and the run directory.

This is the ONE command that does not walk up. Every other command finds the
project root by climbing from --cwd, because a board belongs to a project rather
than to whichever subdirectory you happened to be in — but there is nothing to
find yet, and climbing would mean ` + "`aboard init`" + ` in a subdirectory quietly doing
nothing while reporting success. So it creates a root where you stand, and
refuses when that would make a second one, naming the root it found.

It never overwrites an existing board document. --name opens a SECOND board in
the same project, with its own state file and its own port.

--example seeds the board compiled into this binary: fifteen tabs, one per
renderer, each noted with what it demonstrates. It is a worked example, not your
work — delete what you do not want.

--gitignore adds ` + "`" + aboard.GitignoreLine + "`" + ` to the project's .gitignore. A board is a
LOCAL, persistent, non-authoritative channel: several developers on one repo each
get their own, and a committed one is a whole-file JSON conflict on every merge
over a conversation that was never theirs.`,
		Args:    cobra.NoArgs,
		Example: "  aboard init\n  aboard init --example --gitignore\n  aboard init --name review",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := startDir(cmd)
			if err != nil {
				return err
			}
			res, err := aboard.Init(aboard.InitConfig{
				Dir:       dir,
				Name:      boardName(cmd),
				Example:   example,
				Gitignore: gitignore,
			})
			if err != nil {
				return err
			}
			return renderOutput(stdout(opts), outputFormatOf(cmd), res, func() string { return initHuman(res) })
		},
	}
	cmd.Flags().BoolVar(&example, "example", false, "seed the board with the example tabs compiled into this binary")
	cmd.Flags().BoolVar(&gitignore, "gitignore", false, "append "+aboard.GitignoreLine+" to the project's .gitignore if it is not already ignored")
	cmd.Flags().String("output-format", formatHuman, "human, json or yaml")
	return cmd
}

// initHuman says what was created and what to do next. The gitignore line is
// printed whether or not --gitignore was passed: a project that does not commit
// its board is the default posture, and a reader who has to be told twice is a
// reader who committed one.
func initHuman(res aboard.InitResult) string {
	var b strings.Builder
	what := "empty board"
	if res.Seeded {
		what = fmt.Sprintf("example board, %d tabs", res.Tabs)
	}
	fmt.Fprintf(&b, "created %s (%s)\n", res.StateFile, what)
	for _, p := range res.Created {
		if p == res.StateFile {
			continue
		}
		fmt.Fprintf(&b, "  %s\n", p)
	}

	switch res.GitignoreState {
	case aboard.GitignoreAdded:
		fmt.Fprintf(&b, "added %s to %s\n", res.GitignoreLine, res.GitignoreFile)
	case aboard.GitignorePresent:
		fmt.Fprintf(&b, "%s already ignores %s\n", res.GitignoreFile, res.GitignoreLine)
	default:
		fmt.Fprintf(&b, "\nadd this to .gitignore (or re-run with --gitignore):\n  %s\n", res.GitignoreLine)
	}

	start := "aboard serve"
	if res.Name != "" {
		start += " --name " + res.Name
	}
	fmt.Fprintf(&b, "\nstart it with `%s`\n", start)
	return b.String()
}

// outputFormatOf reads a command's own --output-format. Commands that own the
// flag as a local variable read the variable; init reads it back off the flag
// set so the human renderer can stay a package-level function.
func outputFormatOf(cmd *cobra.Command) string {
	v, err := cmd.Flags().GetString("output-format")
	if err != nil {
		return formatHuman
	}
	return v
}

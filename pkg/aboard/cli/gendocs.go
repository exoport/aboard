// gendocs.go — the CLI reference, generated from the tree that serves it.
//
// A hidden maintainer command, wired to `make docs-cli`. Same shape as ape's
// internal/apecmd/gendocs.go on purpose: hand-rolled markdown (no cobra/doc
// dependency), deterministic, no timestamps, hidden and help and completion
// skipped. Regenerating on an unchanged tree must produce no diff, or the
// generated file becomes a thing that shows up in every commit and stops being
// read.
//
// It is HIDDEN for the same reason `recipes index` is: it maintains this repo,
// it is not something a user of a board does, and a maintainer command in the
// declared table would move capsHash for a surface no agent can use.

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newGenDocsCmd(opts Options) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:    "gen-docs",
		Short:  "Regenerate docs/reference/cli.md from the command tree (repo maintenance)",
		Hidden: true,
		Long: `Write a single-file markdown reference covering the root command and every
visible subcommand, rendered from the live cobra tree so the reference cannot
drift from the code.

Deterministic — sorted by command path, no timestamps — so running it on an
unchanged tree writes an identical file. Hidden commands (this one, ` + "`recipes index`" + `)
and cobra's generated help/completion are skipped: the reference is the
user-facing surface.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var buf bytes.Buffer
			writeCLIMarkdown(cmd.Root(), &buf)
			if out == "-" {
				_, err := stdout(opts).Write(buf.Bytes())
				return err
			}
			if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil { //nolint:gosec // generated docs are world-readable by design
				return err
			}
			fmt.Fprintf(stderr(opts), "wrote %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "docs/reference/cli.md", "output file, or - for stdout")
	return cmd
}

func writeCLIMarkdown(root *cobra.Command, w io.Writer) {
	fmt.Fprint(w, "# aboard CLI reference\n\n")
	fmt.Fprint(w, "> Generated from the command tree by `make docs-cli` (which runs the hidden\n"+
		"> `aboard gen-docs`). Do not edit by hand — change the command definitions in\n"+
		"> `pkg/aboard/cli/` and regenerate.\n\n")

	var cmds []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if skipInDocs(c) {
			return
		}
		cmds = append(cmds, c)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].CommandPath() < cmds[j].CommandPath() })

	for _, c := range cmds {
		writeCommandSection(w, c)
	}
}

// skipInDocs is the one place "not part of the user-facing surface" is decided,
// so gen-docs and the subcommand walk cannot disagree about it.
func skipInDocs(c *cobra.Command) bool {
	return c.Hidden || c.Name() == "help" || c.Name() == "completion"
}

func writeCommandSection(w io.Writer, c *cobra.Command) {
	fmt.Fprintf(w, "## %s\n\n", c.CommandPath())
	if c.Short != "" {
		fmt.Fprintf(w, "%s\n\n", c.Short)
	}
	fmt.Fprintf(w, "```\n%s\n```\n\n", c.UseLine())
	if len(c.Aliases) > 0 {
		fmt.Fprintf(w, "Aliases: `%s`\n\n", strings.Join(c.Aliases, "`, `"))
	}
	if long := c.Long; long != "" && long != c.Short {
		fmt.Fprintf(w, "%s\n\n", long)
	}
	if subs := visibleSubcommands(c); len(subs) > 0 {
		fmt.Fprint(w, "Subcommands:\n\n")
		for _, s := range subs {
			fmt.Fprintf(w, "- `%s` — %s\n", s.Name(), s.Short)
		}
		fmt.Fprintln(w)
	}
	if c.Example != "" {
		fmt.Fprintf(w, "Examples:\n\n```\n%s\n```\n\n", c.Example)
	}
	writeFlagTable(w, "Flags", c.NonInheritedFlags())
	writeFlagTable(w, "Global flags", c.InheritedFlags())
}

func visibleSubcommands(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, s := range c.Commands() {
		if skipInDocs(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// writeFlagTable emits a markdown table for the non-hidden flags in fs. No-op
// when there are none, so a command with no flags gets no empty table.
func writeFlagTable(w io.Writer, title string, fs *pflag.FlagSet) {
	var flags []*pflag.Flag
	fs.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			flags = append(flags, f)
		}
	})
	if len(flags) == 0 {
		return
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	fmt.Fprintf(w, "%s:\n\n", title)
	fmt.Fprint(w, "| Flag | Type | Default | Description |\n")
	fmt.Fprint(w, "| ---- | ---- | ------- | ----------- |\n")
	for _, f := range flags {
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", " + name
		}
		def := f.DefValue
		if def == "" {
			def = "—"
		}
		fmt.Fprintf(w, "| `%s` | %s | `%s` | %s |\n", name, f.Value.Type(), def, mdInline(f.Usage))
	}
	fmt.Fprintln(w)
}

// mdInline flattens a multi-line flag usage into one table-cell-safe line.
func mdInline(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			out = append(out, ' ')
		case '|':
			out = append(out, '\\', '|')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

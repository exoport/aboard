package cli

import (
	"errors"
	"fmt"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
)

func newRecipesCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recipes",
		Short: "List the recipes available here, or print one",
		Long: `A recipe is a worked method for one shape of board work — put a structure in
front of the human, ask for a decision, annotate a screenshot, coordinate with
another session. The bodies live in files rather than in the skill, so the skill
stays small and can never disagree with the recipe it describes.

Four tiers, first wins by name:

  _apex/aboard/recipes/   the wider workspace's house style
  _aboard/recipes/        committed, shared with the team
  .aboard/recipes/        this checkout only, gitignored with the rest
  built-in                compiled into this binary

Shadowing is allowed and always reported: a project that overrides a built-in
recipe is doing something deliberate, and the row says what it replaced. A file
that does not parse is not skipped either — it is listed as invalid with the
reason, because a recipe the author is looking at and the tool pretends does not
exist is the worst of the three outcomes.`,
		Args: cobra.NoArgs,
		// A bare `aboard recipes` prints help rather than guessing at `list`.
		// Guessing would make `recipes` and `recipes list` two spellings of one
		// thing, and then the day a third subcommand arrives one of them changes
		// meaning.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newRecipesListCmd(opts), newRecipesShowCmd(opts), newRecipesIndexCmd(opts))
	return cmd
}

func newRecipesListCmd(opts Options) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every recipe available in this project, and where each came from",
		Long: `Every recipe this project can use, one row each: the name, the tier it came
from, and what it is for.

This is the only complete answer. The skill's generated index lists what is
compiled into the binary and cannot know what a project added, so an agent that
reads only that index is incomplete.

Rows carry what is wrong with them rather than being dropped: a shadowed file is
named under the recipe that won, a file that does not parse is marked INVALID
with the reason, and a recipe needing a newer board schema says so.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := recipeRoot(cmd)
			if err != nil {
				return err
			}
			found, err := aboard.DiscoverRecipes(root, opts.Invocation())
			if err != nil {
				return err
			}
			return renderOutput(stdout(opts), outputFormat, aboard.RecipeOutputs(found),
				func() string { return aboard.RecipeListHuman(found, opts.Invocation()) })
		},
	}
	cmd.Flags().StringVar(&outputFormat, "output-format", formatHuman, aboard.UsageOutputFormat)
	return cmd
}

func newRecipesShowCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	var template bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Print one recipe's body, or just its tab skeleton",
		Long: `Print the recipe an agent should follow: a title line naming it and saying what
it is for, then the body. The frontmatter is stripped — it is metadata for the
list, and YAML at the top of something meant to be read as prose is noise.

--template prints ONLY the JSON tab skeleton the recipe carries, so it pipes
straight into an edit and then into ` + "`" + inv.Cmd("apply") + "`" + `. A recipe with no skeleton
exits 1 saying so, rather than printing an empty document that would be applied
as an empty tab.`,
		Args:    cobra.ExactArgs(1),
		Example: "  " + inv.Cmd("recipes show apply-a-write") + "\n  " + inv.Cmd("recipes show my-recipe --template") + " | jq .",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := recipeRoot(cmd)
			if err != nil {
				return err
			}
			r, err := aboard.FindRecipe(root, args[0], opts.Invocation())
			if err != nil {
				return err
			}
			if !r.Valid() {
				// The file exists and the author is looking at it; the failure has
				// to name the file, not just the name they typed.
				return fmt.Errorf("recipe %q (%s) does not parse: %s", r.Name, r.Path, r.Err)
			}
			if template {
				body, err := r.TemplateJSON(opts.Invocation())
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(stdout(opts), body)
				return err
			}
			_, err = fmt.Fprint(stdout(opts), aboard.RecipeShowText(r, opts.Invocation()))
			return err
		},
	}
	cmd.Flags().BoolVar(&template, "template", false, "print only the recipe's JSON tab skeleton")
	return cmd
}

// newRecipesIndexCmd is HIDDEN: repo maintenance, like gen-docs.
//
// `make caps` pipes it into the skill's references/recipes.md. It is not part of
// the user-facing surface — an agent runs `recipes list`, which is the complete
// answer — and hiding it keeps it out of the generated CLI reference and out of
// the declared command table, so adding a maintainer command never moves
// capsHash.
func newRecipesIndexCmd(opts Options) *cobra.Command {
	inv := opts.Invocation()
	return &cobra.Command{
		Use:    "index",
		Short:  "Print the markdown index of the built-in recipes (repo maintenance)",
		Hidden: true,
		Long: `Emit the markdown index that .claude/skills/aboard/references/recipes.md is
generated from. Wired to ` + "`make caps`" + `.

BUILT-IN recipes only, and deliberately: the file it generates ships inside a
skill directory that gets copied between projects, so a table listing one
project's own recipes would be wrong everywhere else it was copied. The
paragraph underneath the table is what makes that harmless — it leads with
` + "`" + inv.Cmd("recipes list") + "`" + `, which is the only complete answer.

Deterministic: sorted by name, no timestamps. Running it twice on an unchanged
tree produces no diff.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			built, err := aboard.BuiltinRecipes(opts.Invocation())
			if err != nil {
				return err
			}
			for i := range built {
				r := &built[i]
				if !r.Valid() {
					// A built-in that does not parse is a build defect, not a user
					// error: it would emit a row of empty cells into a committed
					// document. Refuse rather than generate a wrong index.
					return fmt.Errorf("built-in recipe %s does not parse: %s", r.Path, r.Err)
				}
			}
			_, err = fmt.Fprint(stdout(opts), aboard.RecipeIndexMarkdown(built))
			return err
		},
	}
}

// recipeRoot resolves the project root for a recipes command.
//
// A missing root is NOT a failure here, unlike everywhere else: the built-in
// tier ships in the binary, so `aboard recipes list` answers in a directory that
// has never held a board. That is the same property `capabilities` has, and for
// the same reason — an agent should be able to ask what a copied binary knows
// before deciding to use it.
func recipeRoot(cmd *cobra.Command) (aboard.Root, error) {
	root, err := looseRoot(cmd)
	if err != nil && !errors.Is(err, aboard.ErrNoRoot) {
		return "", err
	}
	return root, nil
}

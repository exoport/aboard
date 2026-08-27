# Brief 21 — the recipes reference page (docs only)

Read `COMMON.md` first. **You work in the git WORKTREE `/home/diegos/_dev/exoport/aboard-wt-21`
(branch `wt/21`)** — every path COMMON.md writes as the aboard repo means that worktree (another
workflow is changing main). Keep the diff to `docs/`, `README.md`'s one link if needed, and the
skill if it links the page. Do not run `git worktree` commands. Do not commit.

The human asked for **docs for the recipes: every supported folder and its precedence, and how a
recipe must be formatted — mainly the frontmatter schema.** Today `docs/how-to/write-a-recipe.md`
is a how-to; what is missing is the REFERENCE page (Diátaxis: a technical description a reader
looks things up in). Write `docs/reference/recipes.md`, derived from the code, not from memory:
`pkg/aboard/recipes.go` (tiers, precedence, frontmatter struct, tolerances — BOM, CRLF, padded
delimiters — the template-block extraction rules, what makes a file invalid and how it is
reported), `pkg/aboard/layout.go` (the exact directories), `pkg/aboard/cli/recipes.go` and
`commands.go` (`recipes list|show|index`, flags, exit codes, output formats), `recipes/README.md`
and `CLAUDE.md`'s decision bullet (the two kinds of recipe), the nine built-ins and the two
library files as the worked examples.

## The page must contain

1. **The kinds and the folders**: built-in (embedded, nine, `pkg/aboard/recipes/builtin/` in this
   repo — not a folder a project has); the three project tiers with their exact paths and what
   each is for (`_apex/aboard/recipes/` — apex-managed; `_aboard/recipes/` — the workspace;
   `.aboard/recipes/` — this project); the curated library `recipes/` in the aboard repository
   (NOT a discovery tier — copy from it). A precedence table: which wins when two tiers hold the
   same name, and how the loser is reported (`shadowedBy`). Subdirectories: reported, not recursed
   (say what `recipes list` prints). Unreadable/dangling files: listed with the reason.
2. **The file format**: `<name>.md`; the name is the filename; UTF-8; the frontmatter block
   delimited by `---` (tolerances); the body is the method in markdown; the optional
   ```` ```json aboard-template ```` block (exact fence info string — read the code) holding ONE
   tab skeleton (no `id`, no server-owned fields — say which are refused by the skeleton test), and
   how `recipes show --template` extracts it (a decoy ```json fence must not match — say the rule).
3. **The frontmatter schema**, as a table: field · type · required · meaning · example — `name`
   (must equal the filename? check), `description`, `when_to_use`, `tags` (list), `requires`
   (`min_schema` and anything else the struct has), and what is NOT allowed (unknown keys: ignored
   or refused? check and say). A complete minimal example and a complete full example, both
   copied from real files. What `aboard recipes list --output-format json` prints per recipe
   (`name`, `description`, `whenToUse`, `tags`, `requires`, `scope`, `path`, `hasTemplate`,
   `shadowedBy`, `error`) with the scope vocabulary.
4. **The commands**: `recipes list [--output-format]`, `recipes show <name> [--template]`,
   `recipes index` (what it generates and where it lands), exit codes; every command line on the
   page executed once on a scratch project seeded with `aboard init --example --gitignore` plus
   one copied library recipe and one deliberately shadowed name (a `_aboard/recipes/<same>.md`) so
   the precedence claims are shown with real output.
5. **How an agent uses one** in three lines, linking the how-to for the long form; the "no
   `--template | apply` one-liner" trap from `recipes/README.md`.
6. Link the page from `docs/README.md`'s Reference section and `docs/reference/README.md`; from
   `docs/how-to/write-a-recipe.md` ("the reference"); from `recipes/README.md`; from the skill's
   `references/recipes.md` only if that file is hand-written (it is generated — check; if generated,
   add the pointer to `recipeIndexParagraph` in `recipes.go` and regenerate with `make caps`).
   `make docs-check` green; `make caps` idempotent if touched.

## Done when

The reference exists, every claim traceable to code, every command run once; the ladder for a
docs change: `make docs-check`, `make lint`, `make fmt-check`, `make pre-commit`, `go test -race
./...`, `make e2e` once (say it was untouched).

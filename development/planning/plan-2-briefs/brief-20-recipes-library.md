# Brief 20 — the two new recipes are a LIBRARY in `recipes/`, not built-ins

Read `COMMON.md` first. Item 19 shipped `decision-wizard-with-live-summary.md` and
`human-checklist.md` as BUILT-INS under `pkg/aboard/recipes/builtin/` (embedded in the binary).
The human's correction, 2026-08-26: they were meant to be two recipe FILES in a new top-level
folder `/home/diegos/_dev/exoport/aboard/recipes/` — the repository's recipe library, where
recipes are collected and from which a project copies the ones it wants into one of its own
discovery tiers (`_apex/aboard/recipes` > `_aboard/recipes` > `.aboard/recipes`, see
`pkg/aboard/recipes.go` and `docs/how-to/write-a-recipe.md`). The nine original built-ins stay
built-ins; the library is for recipes that are worth sharing but not worth shipping in every
binary.

## Scope

1. `git mv` both files from `pkg/aboard/recipes/builtin/` to `recipes/`. Built-in floor back to
   nine; every "eleven" in `recipes.go`, `recipes_test.go`, docs and `CHANGELOG.md` corrected;
   `make caps` regenerates the skill's recipe index (it lists built-ins only) — check whether the
   index should ALSO list the library under its own heading ("in the repository's `recipes/`
   folder — copy into your project"), and do that if the generator can be taught cheaply (say
   which you chose and why).
2. `recipes/README.md`: what the folder is, how to use one (`cp recipes/<name>.md
   <project>/.aboard/recipes/` or `_aboard/recipes/` for a workspace, then `aboard recipes list`
   shows it with its scope; `aboard recipes show <name> --template | aboard apply --by agent-1`),
   how to contribute one (frontmatter, the template block, run it once), and a table of the
   recipes in it with their `when_to_use`.
3. Tests: the test that validated built-in templates (`TestBuiltinTemplatesAreCleanTabSkeletons`)
   ALSO walks `recipes/*.md` with the same assertions (parses, frontmatter complete, template is a
   clean tab skeleton, zero `writeWarnings`); `TestRecipeTemplateExtraction` reads a library file
   (it was changed in item 19 to read the built-in copy). A test that a library recipe, copied
   into a scratch project's `.aboard/recipes/`, is discovered with scope `project` and its
   template applies (`recipes show --template | apply --check` = 0) — run it for real once.
4. Docs: `docs/how-to/write-a-recipe.md` (the library as the place to look before writing one, and
   the place to put one that is general), `CLAUDE.md` directory map (`recipes/` row) and the
   "nine built-ins" claim, `README.md` one line, `docs/reference/layout.md` only if it enumerates
   repo folders, `development/handoffs/handoff-phase-e-finish.md`'s item-19 note corrected,
   `CHANGELOG.md` (rewrite item 19's line rather than adding a contradicting one).
5. `make caps` idempotent; `capsHash` unmoved (say so); `make lint`, `make fmt-check`,
   `make pre-commit`, `go test -race ./...`, `make docs-check`, `make e2e` once.
The human's boards on 47781 and 44917 must not be touched; scratch projects only.

## Done when

`ls pkg/aboard/recipes/builtin | wc -l` is 9, `ls recipes/*.md | wc -l` is 2 (+ README), the
library is documented and tested, the skill index is correct, ladder green.

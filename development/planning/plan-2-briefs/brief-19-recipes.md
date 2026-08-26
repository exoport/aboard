# Brief 19 — two recipes: the decision wizard, migrated, and a human checklist

Read `COMMON.md` first. **You work in the git WORKTREE `/home/diegos/_dev/exoport/aboard-wt-19`
(branch `wt/19`)** — every path COMMON.md writes as the aboard repo means that worktree; the
orchestrator rebases it onto main afterwards (another workflow is changing main concurrently:
keep your diff to the recipes, their tests, the generated recipe index and the docs that list
recipes). Built-in recipes live in `pkg/aboard/recipes/builtin/*.md` with frontmatter (read
`pkg/aboard/recipes.go`, `docs/how-to/write-a-recipe.md`, and the nine existing files for the
exact shape and tone); `make caps` regenerates the skill's recipe index (`recipes index`), and
`recipes_test.go` asserts every built-in parses and its template extracts.

## 1. `decision-wizard-with-live-summary.md` — migrated from the spike, reviewed

Source: `/home/diegos/_dev/ai/board/_output/recipes/decision-wizard-with-live-summary.md`
(read-only; never write to the spike). It was written on the spike on 2026-08-24: one `ui` tab
with internal `tabs` panels, a read-only Summary panel bound to the same `state.data`, so the
summary cannot go stale. Migrate it: aboard names (`aboard.json`, `aboard apply`, `views/ui.js`
without line numbers — name symbols), the `bind` scoping claims re-verified against the CURRENT
`views/ui.js`/`html.js`/`aboard.html` (the table of three dead ends must still be true — check
each), the `{bind}` and `kv` behaviour after plan-2 (kv resolves binds now), `export` now renders
a `ui` tree (item 6) which changes the "read it back" section, `apply --check` exists. Frontmatter
`when_to_use` written for the discovery listing. The template must APPLY cleanly (`aboard apply
--check` then a real apply on a scratch board), render (screenshot LOOKED at), and its
read-back commands run once each.

## 2. `human-checklist.md` — things the human must do, each with its explanation beside it

What the human asked for, after using a stack (a markdown block of instructions on top and a
tick table below): "I had to scroll top to bottom to read the instruction and then go down for
the check, up for the next instruction". The recipe's shape: ONE `ui` tab; per item a `card`
holding the item's title, its explanation (a `text`/`caption`, markdown if the component
supports it — check `capabilities ui`), a `checklist` or checkbox `field` bound to
`{bind: "items.<id>.done"}`, and a `field` (textarea) bound to `{bind: "items.<id>.notes"}` —
explanation, check and notes TOGETHER, in that order. A `kv`/`stat` header bound to the data
showing "N of M done" that cannot go stale. The doc explains: when to use it (a hand-verification
list, an install checklist, anything only the human can confirm), how the agent writes the items,
how it reads the result back (`aboard export <tab>` prints the outline with binds resolved;
`state.data.items` is the record), and that ids for items are the agent's (semantic ids are fine
inside `data`). Template with two example items. Apply + render + screenshot LOOKED at; the
read-back commands run once.

## Also

- `docs/how-to/write-a-recipe.md` and the skill's `references/recipes.md` (generated) list both;
  `docs/reference/`… wherever the built-ins are enumerated says eleven. `CHANGELOG.md`.
- `make caps` moves the recipe index (and `capsHash` if the recipe set is part of it — say).
- Ladder in the worktree: `make lint`, `make fmt-check`, `make pre-commit`, `go test -race`,
  `make e2e` once (nothing here should touch it — say so), `make docs-check`.

## Done when

Both recipes ship, parse, apply, render and read back; the index is regenerated; docs list them.

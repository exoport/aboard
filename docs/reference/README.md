# Reference

Reference docs are **information** — exhaustive, accurate, neutral. They describe what
exists; they don't teach (that's [Tutorials](../tutorials/)) and they don't recommend
(that's [How-to guides](../how-to/) and [Explanation](../explanation/)). A reader
consults reference when they need a specific fact.

For aboard, reference is the surface area: every command, every route, every path under
`.aboard/`, every field of the state document.

## Available reference

- [cli.md](cli.md) — every command, flag and default, generated from the cobra tree by `make docs-cli`. Do not hand-edit.
- [layout.md](layout.md) — the `.aboard/` tree: what is content and what is machine-local runtime, how the project root is discovered, and how the port is derived from it.
- [state-file.md](state-file.md) — the schema v3 essentials: the document's server-managed fields, the id invariant, the shape of a tab, and where per-type state is documented in full.
- [http-api.md](http-api.md) — every route the server answers, with parameters, bodies and status codes, including the compare-and-set contract and the SSE frames.
- [capabilities.md](capabilities.md) — the capability manifest: what it contains, what `capsHash` fingerprints, what `--check` gates, and the declared command table.
- [theme.md](theme.md) — colour: the 21 tokens and their roles, both built-in variants with their measured contrast ratios, the `.aboard/theme.json` schema, the switch, and the theme message an embedder may send.
- [recipes.md](recipes.md) — the recipe format: the four discovery tiers and their precedence, the frontmatter schema, the `aboard-template` block, why a file is rejected, and the `recipes list|show|index` commands with their exit codes.

## Planned reference

- **Exit codes.** The table lives in the declared command table today and is printed per command by `aboard capabilities`; a page is worth writing once a command needs a code outside the shared four.
- **The `aboard.*` bridge.** The `get` / `set` / `onData` / `fit` contract an `html` tab's widget scripts against.

## Writing reference

- Match the structure of the thing you're documenting: commands → command-shaped pages, routes → route-shaped pages.
- Be exhaustive within the topic — don't leave out edge cases.
- Be neutral. No recommendations, no opinions. Save those for [Explanation](../explanation/).
- **Prefer generated over written** where the fact already exists in the code: the CLI page is generated, and the per-type capability inventory is printed by `aboard capabilities` rather than duplicated here. A hand-copied fact is a fact that will disagree.

See the [Diátaxis reference rubric](https://diataxis.fr/reference/).

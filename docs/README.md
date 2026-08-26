# aboard documentation

These docs follow the [Diátaxis](https://diataxis.fr/) framework, which splits
documentation into four quadrants by user need. Pick the quadrant that matches what
you're trying to do:

| If you want to…                                     | Read…                       |
| --------------------------------------------------- | --------------------------- |
| **Learn** aboard by following a guided walkthrough   | [Tutorials](tutorials/)     |
| **Solve** a specific problem with aboard             | [How-to guides](how-to/)    |
| **Look up** a command, a route, or a state field     | [Reference](reference/)     |
| **Understand** why aboard is the way it is           | [Explanation](explanation/) |

The four quadrants serve different needs and are written in different styles. Tutorials
and how-to guides are practical (action-oriented); reference and explanation are
theoretical (cognition-oriented). Tutorials and explanation are study-oriented; how-to
guides and reference are work-oriented. See the [Diátaxis
compass](https://diataxis.fr/compass/) if you're unsure where a given doc belongs.

## Index

### Tutorials — _learning by doing_

- [Your first board](tutorials/first-board.md) — install → `init --example` → `serve` → dock it in VS Code → change a tab from the terminal → watch it land → export it.

### How-to guides — _recipes for specific problems_

- [How to install aboard](how-to/install.md)
- [How to run aboard inside VS Code](how-to/run-in-vscode.md)
- [How to write a recipe](how-to/write-a-recipe.md)
- [How to embed aboard in ape](how-to/embed-in-ape.md)
- [How to verify a release artifact](how-to/verify.md)
- [How to run the browser suite](how-to/run-the-browser-suite.md) — `make e2e`: what it needs, what it leaves behind when it fails, and the checks it inherited from the retired shell suite

### Reference — _technical descriptions_

- [CLI reference](reference/cli.md) — every command, flag and default (generated from the cobra tree)
- [The `.aboard/` layout](reference/layout.md) — what lives where, root discovery, port derivation
- [The state file](reference/state-file.md) — the schema v3 essentials: the document, ids, a tab, per-type state
- [HTTP API](reference/http-api.md) — every route the server answers, with its parameters and status codes
- [The capability manifest](reference/capabilities.md) — what `aboard capabilities` prints, `capsHash`, `--check`, and the declared command table

### Explanation — _the why and the what_

- [Why a local, non-authoritative channel](explanation/why-a-local-non-authoritative-channel.md) — what the board is FOR, the three tiers, and how a conclusion gets promoted
- [Why tabs are data](explanation/why-tabs-are-data.md) — a tab is a name, a type and its own state, and what that buys
- [Why four guarantees are server-enforced](explanation/why-four-guarantees-are-server-enforced.md) — the things an agent must not be able to do, even by accident
- [Why nothing in the UI starts a session](explanation/why-nothing-in-the-ui-starts-a-session.md) — the board may ask; a session may choose to listen
- [Why there is no diff renderer](explanation/why-no-diff-renderer.md) — closed, not deferred, and the reason kept so nobody re-derives it
- [Why html tabs are sandboxed](explanation/why-html-tabs-are-sandboxed.md) — `connect-src 'none'`, an opaque origin, and the `frame-ancestors` story
- [Why two identities](explanation/why-two-identities.md) — `aboard` and `ape-aboard` over one `.aboard/`
- [Why writes are serialised](explanation/why-writes-are-serialised.md) — one writer at a time, what a 409 means under concurrency, and the two locks
- [Why the manifest is declared](explanation/why-the-manifest-is-declared.md) — declaration as the canonical source, controls as a list, and the unverifiable remainder

## Contributing to the docs

When adding a new doc, place it in the quadrant that matches its primary user need, not
its topic. A page about recipes could live in any of the four depending on whether it's
teaching, recipe-giving, listing facts, or explaining design. If a page mixes two
purposes, split it.

Two mechanical rules, both enforced by `make docs-check`: every relative link must
resolve, and every page must be reachable from this index. Link a new page from its
quadrant's README **and** from the index above.

One convention worth stating: **do not link to source files or plans from a doc page**
— name them in backticks instead (`pkg/aboard/layout.go`). A link into the source tree
is a link that goes stale silently, and the link checker cannot tell the difference
between a path that moved and a path that never existed.

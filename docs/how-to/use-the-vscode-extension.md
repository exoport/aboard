# How to use the VS Code extension

There are two ways to put a board inside VS Code, and they solve different problems.

- **The Simple Browser** needs nothing installed: paste the board's URL into a built-in webview and it docks as an editor tab. That is [How to run aboard inside VS Code](run-in-vscode.md), and it is what most people should use today.
- **The extension** adds a sidebar: a tree of the board's tabs, with the board itself in a panel beside your code, and the handful of actions only a human is allowed to take. This page is about that.

> **Status: verified in a real VS Code on 2026-08-26, twice, and not packaged.** The human
> worked the extension's hand-verification checklist through in an Extension Development
> Host over two passes. Observed working: activation and discovery (including a board
> started *after* the window was open), the tree, the panel rendering the board without its
> own tab strip, tab switching with no page reload, the panel surviving a drag to another
> editor group, `html` tabs painting with a clean console, dots arriving live, removal
> requests answered from the sidebar, rename and set-note, `]` inside the panel moving the
> sidebar highlight, two viewers disagreeing about chrome, a server restart, a forced `409`,
> the Start-the-board fallback, and the board following the VS Code theme. **Still
> unobserved**: the extension's own reconnect backoff against a board that will not come
> back, the old-binary warning, Remote SSH / Codespaces, and the two fixes that came out of
> the second pass (the notify bell and Copy Reference) — those are proven by tests, not by
> eye. The extension's own `README.md` is the live status; there is no `.vsix`.
>
> Six defects came out of those two passes — five by the human looking at the screen and
> one while reviewing a fix — and every one was invisible to a suite that had been green
> for days. Five were in the extension; one was in *this* repository, and it is the
> one worth knowing about here: the board called `window.confirm`, which a webview
> swallows, so the removal banner's **Remove tab** did nothing at all. That is fixed — see
> [why the board never pops up an OS
> dialog](run-in-vscode.md#why-the-board-never-pops-up-an-os-dialog). If you want a board
> in VS Code with nothing installed, the Simple Browser is still the shorter route.

## Where it lives

The **aboard-vscode repository** — a separate repo from this one, deliberately. It has
its own release cycle, its own language and its own dependency tree, and coupling them
would mean an aboard release every time a VS Code API moved.

It is **not published** — not to the VS Code Marketplace, and for now not to Open VSX
either. That is a decision rather than an omission: to anyone without an aboard project
in their workspace the extension installs, finds nothing, and does nothing. So you get it
by cloning the repository, from wherever this copy of aboard came from.

## What it is — and what it is not

It is a **viewer**. No rendering, no state and no schema knowledge live in it. Everything
it shows comes from a running `aboard` (or `ape aboard`) server over plain HTTP, exactly
the way any other client reads it. When aboard grows a sixteenth renderer the extension
needs zero changes; if it ever does need one, something in it is wrong.

What it puts on screen:

- **A tree of the board's tabs** in the sidebar, in the board document's own order — the order is the human's, so it is never re-sorted. The label is the tab name (`(unnamed)` when it has none, as the board itself says), the description is its id, and the tooltip is the id and the type's label — read from `/capabilities`, never hardcoded — followed by the tab's `note` verbatim.
- **A dot per tab that needs attention** — periwinkle for a change an agent made, red for a pending removal request, removal winning when a tab has both. The view's badge counts the changed ones.
- **The board itself in a webview panel**, in an `<iframe>` on the running server, with VS Code's port mapping so it also works over Remote SSH and Codespaces.
- **More than one board at once** — a multi-root workspace, or one project serving a [named board](run-a-second-board.md) beside its default, gets a row each.

The actions it offers are the writes the board permits from a human: dismiss a change
marker, approve or deny a removal request, rename a tab, set its `note`, release a
waiting session, copy an id or a deep link. Those are human-only on purpose — the server
refuses them from an agent — which is why the extension writes as `__by: "human"`.

One button is not a write at all: when the sidebar finds an `.aboard/` but nothing
answering, it offers **Start the Board**, which runs `aboard serve` (or `ape aboard
serve`, by what is on your `PATH`) in a terminal for you. That starts a *server*, not an
agent session — the distinction the next paragraph is about.

Nothing in it starts an agent session, and nothing about it changes that rule. See
[why nothing in the UI starts a session](../explanation/why-nothing-in-the-ui-starts-a-session.md).

## How it finds a board

The same way aboard itself does, and for the same reason: **never assume a port.**

1. From each workspace folder, walk **up** for a `.aboard/run/instance.json` — plus `instance.<name>.json` for any named board on the same project.
2. Read the port and `base` out of it.
3. Verify over `GET /health`, comparing the `project` field against the root it just discovered. A stale instance file left by a dead server is otherwise indistinguishable from a live one.
4. Follow `GET /events` for live refresh, and re-read `/aboard.json` when a write lands.

So the board has to be **running** and the workspace folder has to be at or under its
project root. If the sidebar is empty, run `aboard status` in a terminal in that folder:
if that cannot find a board either, the extension is right.

## What it needs from the board, and what that costs you

Two things, both of which the board's own shell already does. There is nothing to enable:

- **`?chrome=notabs`** on the board URL, which suppresses the board's own tab strip for that viewer. Without it the panel shows two tab strips, the extension's and the board's.
- **`{ __aboard: 'active', tab: '<id>' }`**, which the board posts to its parent frame whenever the active tab changes — including the tab it picks at load, and the ones `[`, `]` and `1`–`9` reach. Without it the sidebar highlight goes stale the moment the human uses a key inside the panel, and a highlight that lies is worse than none.

Neither is server state and neither lets a host make the board *do* anything. Both are
per-viewer, which is why they are asked for in the URL rather than stored: two viewers
must be able to disagree about chrome while agreeing about content.

A third thing the panel needs, and gets for free: **the board's questions are its own.**
Confirming a removal, renaming a tab and resetting a form are drawn in the page as
`<dialog>` elements, never as `window.confirm`/`window.prompt` — which a webview
suppresses outright. There is nothing for the extension to grant or to reimplement, and
nothing that behaves differently inside the panel from a browser tab. The reasoning, and
what it looked like when it was not true, is in [How to run aboard inside VS
Code](run-in-vscode.md#why-the-board-never-pops-up-an-os-dialog).

The full coupling between the two repositories is a **contract, not a shared file**: it
is `docs/reference/http-api.md` in this repository, reduced to the parts a viewer uses —
`/health`, `/aboard.json`, `/events`, `/capabilities`, `POST /aboard.json`, `/poke`,
`/waiters`, and `#tab=<id>` on the board URL. If you are changing either side, that page
is the thing to read.

## Build it

From a clone of the aboard-vscode repository. It needs Node 20 or later; there are no
runtime dependencies at all, and the dev ones are TypeScript, esbuild and two `@types`
packages.

```sh
npm ci
npm run build     # → dist/extension.js
npm test          # node --test, no framework
```

`npm test` should end with `# fail 0` — it reported `# tests 191` across `# suites 49`
from a clean copy on 2026-08-27, and the counter to read is the failure one, not the
total.
Everything with a rule worth arguing about — the discovery walk, the document-to-tree
mapping, the edits, the SSE frame parsing, the URL construction — lives on the
non-`vscode` side of the code so that it can be reached without a running editor. That
is also why the test count is meaningful and the coverage is not: none of it touches the
editor.

## Run it

`F5` from the repository opens an **Extension Development Host** — a second VS Code
window with the extension loaded. Open a project that has a board running, and the
sidebar should populate.

**Expect to find bugs.** Two passes through the checklist in a real host produced six of
them, and none was visible to `node --test`; the extension's failure mode is *silence*, so
the thing to do is look at the screen rather than at the output channel. There is no
`.vsix` to install and no `vsce` in the dependency list, on purpose: packaging comes after
somebody has used it, not before.

If you are *contributing* to aboard rather than using it, note that the extension's own
`README.md` asks you not to be the one who takes that step: packaging or installing a
`.vsix` is gated on the maintainer's first run, so that the first report comes from
somebody who can act on it. Pressing `F5` in your own clone to look around is a
different thing, and it is yours to do.

Every board request the extension makes is a read or a write the board already permits,
so the worst case there is an empty sidebar or a failed action — it cannot corrupt a
board that a browser could not corrupt the same way. The Start button is the one thing
that reaches outside that: it runs a command in a terminal, where you can see it.

## See also

- [How to run aboard inside VS Code](run-in-vscode.md) — the Simple Browser route, which needs nothing installed, and the `?chrome=` and blank-widget-tab notes that apply to both.
- [HTTP API](../reference/http-api.md) — the contract the extension consumes, including what the shell posts to an embedder.
- [How to run a second board in one project](run-a-second-board.md) — why the tree can show two boards for one project.

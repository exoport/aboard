# How to use the VS Code extension

There are two ways to put a board inside VS Code, and they solve different problems.

- **The Simple Browser** needs nothing installed: paste the board's URL into a built-in webview and it docks as an editor tab. That is [How to run aboard inside VS Code](run-in-vscode.md), and it is the shorter route if you do not want to build anything.
- **The extension** adds a sidebar: a tree of the board's tabs, with the board itself in a panel beside your code, and the handful of actions only a human is allowed to take. This page is about that.

> **Status: packaged, installed and in daily use here since 2026-08-27.** It is built as
> a `.vsix` and installed into a normal editor — not run under F5 — and the board this
> repository develops against is read through it. Observed working in a real editor:
> activation and discovery (including a board started *after* the window was open), the
> tree, the panel rendering the board without its own tab strip, tab switching with no
> page reload, the panel surviving a drag to another editor group, `html` tabs painting
> with a clean console, dots arriving live, removal requests answered from the sidebar,
> rename and set-note, `]` inside the panel moving the sidebar highlight, two viewers
> disagreeing about chrome, a server restart, a forced `409`, the Start-the-board
> fallback, the board following the editor theme (including both high-contrast themes),
> the sidebar's **New Tab** button, the **nudge** button releasing a parked session, and
> a cropped image reaching the system clipboard through `xclip`. **Still unobserved**:
> the extension's own reconnect backoff against a board that will not come back, the
> old-binary `?chrome=` warning, and Remote SSH / Codespaces. The extension's own
> `README.md` is the live status.
>
> **What that use has been worth, and it is the argument for installing rather than
> reading:** every defect of consequence in this pairing was found by a human looking at
> a screen, and not one was visible to either test suite. Two `F5` passes on 2026-08-26
> produced six; installing the `.vsix` on 2026-08-27 produced four more that F5 had not
> (a purpose strip that read as a notification, the `+` costing a row of a small panel,
> and the palette mapping wrong twice, both times built from individually valid
> colours); the clipboard round trip on 2026-08-28 produced three, one of which was a
> failure the board could not describe because it had been discovering its host by
> timing out. The one that belongs on this page: the board called `window.confirm`,
> which a webview swallows, so the removal banner's **Remove tab** did nothing at all —
> fixed, and explained in [why the board never pops up an OS
> dialog](run-in-vscode.md#why-the-board-never-pops-up-an-os-dialog).
>
> If you want a board in VS Code with nothing installed, the Simple Browser is still the
> shorter route.

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
- **A New Tab button in the view's title bar**, because `?chrome=notabs` hides the board's own `+` along with the strip. It posts `{__aboard: 'newtab'}` and the BOARD draws the sheet: the host knows nothing about types or empty states, which is what keeps this extension free of the board's schema.
- **A nudge button**, which releases every session parked on `aboard wait`. It was a bell until 2026-08-27, and a bell reads as "notifications about the board" — the opposite of what it does, which is to poke an agent.

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

## Copying an image out of a panel

A `markup` tab can copy a cropped region, or the current view, to the system clipboard.
In a browser that is `navigator.clipboard.write` and there is nothing to arrange. **In a
webview it cannot work and never will**: Chromium refuses with *"The Clipboard API has
been blocked because of a permissions policy applied to the current document"*, the
webview document holds that policy, and VS Code exposes no way to lift it — there is no
permission field on `WebviewOptions`, and `vscode.env.clipboard` is text only.

So the extension does it instead, because an extension host is an ordinary process and
can run a program. The board posts the PNG out of the frame, the host writes a temp file
and runs **`xclip`** (or `wl-copy` on Wayland), and the answer comes back. Install one of
them:

```bash
sudo apt install xclip     # or: sudo apt install wl-clipboard
```

With neither installed the board says so by name and offers the picture in a dialog, with
a button that adds it to the tab as a new image — the one route that asks permission from
nobody.

Two things worth knowing when it does not work:

- **The board is told what its host can do, rather than finding out by trying.** The panel announces `{__aboard: 'host', name, clipboard}` on every frame load. Without that announcement a failure is a six-second silence, and a silence cannot distinguish "nothing framed me" from "an older extension framed me" from "the host broke" — nor any of them from a working host a moment before it succeeds. One failure survived three rounds of reinstall-and-restart on exactly that evidence.
- **An announcement explains a failure; it does not authorise the attempt.** The board asks any host at all, announced or not, and only skips one that has said `clipboard: false`. Gating the ask on the announcement was a regression that lasted about an hour: a panel one build older announces nothing and copies perfectly well.

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
npm run build       # → dist/extension.js
npm test            # node --test, no framework
npm run install:dev # package a .vsix, install it, and print the version that landed
```

`npm test` should end with `# fail 0` — it reported `# tests 221` across `# suites 56`
from a clean copy on 2026-08-28, and the counter to read is the failure one, not the
total.

`install:dev` prints the installed version as its last line, and that line is the point
of it: a dev build that fails to land looks exactly like a bug in whatever you were
testing. Reload the window afterwards (*Developer: Reload Window*) — a new version is a
new folder under `~/.vscode/extensions/`, and the extension host picks it up on a
reload.
Everything with a rule worth arguing about — the discovery walk, the document-to-tree
mapping, the edits, the SSE frame parsing, the URL construction — lives on the
non-`vscode` side of the code so that it can be reached without a running editor. That
is also why the test count is meaningful and the coverage is not: none of it touches the
editor.

## Run it

`F5` from the repository opens an **Extension Development Host** — a second VS Code
window with the extension loaded. Open a project that has a board running, and the
sidebar should populate. `npm run install:dev` is the other way, and the two are not
equivalent: F5 runs from the source tree with a debugger attached, while an installed
`.vsix` runs from the packaged file list with none. Four defects have been found only by
the second, one of them a `.vscodeignore` question and one an inspector interfering with
the SSE stream.

**Expect to find bugs.** Every defect of consequence here was found by looking at the
screen, and none was visible to `node --test`; the extension's failure mode is *silence*.
When something appears to do nothing, the **Aboard** output channel is the first place to
look — it names the running version at activation and logs each clipboard request with
its outcome and timing.

Every board request the extension makes is a read or a write the board already permits,
so the worst case there is an empty sidebar or a failed action — it cannot corrupt a
board that a browser could not corrupt the same way. The Start button is the one thing
that reaches outside that: it runs a command in a terminal, where you can see it.

## See also

- [How to run aboard inside VS Code](run-in-vscode.md) — the Simple Browser route, which needs nothing installed, and the `?chrome=` and blank-widget-tab notes that apply to both.
- [HTTP API](../reference/http-api.md) — the contract the extension consumes, including what the shell posts to an embedder.
- [How to run a second board in one project](run-a-second-board.md) — why the tree can show two boards for one project.

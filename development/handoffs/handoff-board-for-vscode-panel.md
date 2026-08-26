# Handoff — `aboard`-side changes to let a VS Code extension own the tab strip

**Status: landed, 2026-08-26 (plan-2 item 7).** The framing fix (§2) and the three
items the port already satisfies (§3) came with the port itself. The three genuine
prerequisites — `?chrome=` (§4), the `active` message (§5) and the storage guards
(§6) — are **shipped**, with the browser suite covering all three (`test/e2e/embed_test.go`).
§3's open question is answered and needed no code: `GET /health` has exposed the
configured base path all along, as `base`.

**Rewritten from:** `handoff-board-for-vscode-panel.md`, written 2026-08-24 on the
`board` spike by `agent-research`, stamped against spike commit `7e5a179`.

**Scope:** the changes *inside `aboard`* needed so an external VS Code extension can
host the board in a webview with the tab list moved into VS Code's sidebar.

**Out of scope:** the extension itself — see the sibling handoff
(`docs/handoff.md` in the `aboard_vscode` repo).

> Line numbers from the spike original are dropped throughout. At the time of this
> rewrite the port has landed its first of three commits (verbatim port only —
> plan-1 decision 2); the split and rename that give these files their final names
> and locations have not landed yet, so a line number would be fiction. Anchor by
> symbol name instead — every reference below names a function, not a line.

---

## 1. Headline: the board is already almost entirely ready

The extension is a **viewer**. It does not render tabs, does not cache state, does
not learn the schema.

| what the extension needs | status | where the answer lives |
|---|---|---|
| the shell can be framed cross-origin | works, carries over | `GET /` sends no CSP and no `X-Frame-Options` — unchanged by the port |
| html tabs can be framed inside a webview | works, carries over | `pkg/aboard/htmltab.go`'s CSP already lists `vscode-webview:` and `vscode-file:` — the fix predates the port and needs no renaming, since none of those literal origin strings mention "board" |
| **base path, for an extension talking through a reverse proxy or a prefixed URL** | **done by the port** | plan-1 decision 7 — see §3 |
| **which of the two binaries answered**, for choosing a "start it" fallback command | **done by the port** | plan-1 decision 6 — see §3 |
| **where to find the instance file**, walking up from the workspace folder | **done by the port** | plan-1 decisions 4–5 — see §3 |
| navigate to a tab from outside | works, carries over | `#tab=bb13` deep-link resolution — the mechanism, not the id scheme, is what matters here |
| …without reloading the page | works, carries over | a fragment-only `iframe.src` change is a fragment navigation in every browser; nothing about the port affects this |
| a live tab list | works, carries over | `GET /aboard.json` (renamed from `/board.json` — plan-1 decision 4) carries `id`/`name`/`type`/`note`/`touched`/`pendingRemoval`; `GET /events` (SSE) fans out to every client, a webview included |
| type labels without duplicating the renderer registry | works, carries over | `GET /capabilities` |
| write back as the human | works, carries over, **one behaviour changed** | `POST /aboard.json` with `__base`/`__by`/`__origin` — **`__by` is no longer optional in practice**: `bb360` (see `handoff-13-features.md` §0) makes an absent `__by` default to `"unknown"`, not `"human"`, so the extension must always send `__by: "human"` explicitly or it silently loses every human-only power it needs (dismiss, delete, answer a removal request) |
| the notify channel | works, carries over | `POST /poke`, `GET /waiters` |
| discovery | works, carries over, **path changed** | `.aboard/run/instance.json` (was `.board/instance.json`) + `GET /health` |
| **hide the board's own tab strip** | **landed** | §4 |
| **tell the embedder which tab is active** | **landed** | §5 |
| **survive storage being refused in a third-party frame** | **landed** | §6 |

No new endpoint beyond the one this document itself proposes nowhere — the surface is
`/aboard.json` + `/events` + `/capabilities` + `/health` + `/poke` + `/waiters`, same
shape as the spike, one route renamed.

## 2. The `frame-ancestors` fix: carries over unchanged

The spike's fix (`htmltab.go`, CSP header) lists `'self' vscode-webview: vscode-file:
https://*.vscode-cdn.net` as the allowed framing ancestors. None of those literal
values contain the word "board", so the port's verbatim-copy step already carries this
forward with zero changes, and the eventual rename step has nothing to do here either.

Two checks still worth doing once against the ported binary specifically, not because
the fix is in doubt but because *this specific instance* has not been checked:

1. Open an `html` tab in the docked Simple Browser once `aboard` is built, run
   *Developer: Open Webview Developer Tools*, confirm the widget paints and the
   console is silent.
2. `aboard status` (once built) should report a build identity that postdates
   whatever commit last touched `htmltab.go` — the spike's own gotcha ("a Go change
   needs a rebuild, easy to forget") applies here just as much.

Do **not** narrow the origin list back to `'self'`. The containment is `connect-src
'none'` plus `sandbox="allow-scripts"` without `allow-same-origin` — unchanged,
unrelated to the rename.

## 3. Already satisfied by the port

Three things this handoff's spike-era version had to propose from scratch are now
handled by decisions already made for reasons that have nothing to do with the VS
Code extension — they just happen to cover exactly what it needs.

- **Base path (plan-1 decision 7).** `aboard serve --base-path /prefix` injects a
  single constant into the served shell that every browser→server URL is built from.
  An extension that talks to a board running behind a path prefix (a remote/Codespaces
  reverse proxy, for instance) needs to know that prefix too, since it makes its own
  HTTP calls directly rather than through the shell's JS. **Open question, not yet
  answered by the port:** whether `GET /health` exposes the configured base path for
  a client to read, or whether the extension is expected to already know it (e.g.
  because the user typed it into a setting).

  **Answered, and the answer is that it was never missing.** `GET /health` returns
  the whole `Instance` record, and `Instance.Base` (`pkg/aboard/server.go`) has
  carried the configured prefix since the port landed — `json:"base"`, `omitempty`,
  so it is simply absent in the common case where no `--base-path` was given. No
  code was added for this; `http-api.md`'s `/health` section now says so out loud,
  because a field that exists and is undocumented is, to a client author, the same
  as a field that does not exist. The one ordering trap worth naming: a prefixed
  board answers `/health` only at `<base>/health`, so a client cannot discover the
  prefix from `/health` itself — it reads it from `.aboard/run/instance.json`,
  which is exactly what an extension walking up from the workspace folder finds
  first anyway.

  Note the name. The field is **`base`**, not `basePath`; plan-2's brief for this
  item guessed the latter, and the extension reads both.
- **Two identities (plan-1 decision 6).** `GET /health` and the instance file carry
  `app: "aboard"` when the standalone binary serves, `app: "ape-aboard"` when `ape`
  hosts it. An extension's "start the board" fallback (offered when discovery finds
  nothing running) can now branch on which binary is on `PATH` and shell out to
  `aboard serve` or `ape aboard serve` accordingly, rather than guessing or hardcoding
  one. See the sibling extension handoff (`docs/handoff.md`) for exactly this logic.
- **Instance file location (plan-1 decisions 4–5).** `.aboard/run/instance.json`,
  found by walking up from a starting directory the same way the server itself finds
  its project root — so an extension pointed at a subfolder of the actual project
  still finds the right instance file, which the spike's flat `.board/instance.json`
  lookup did not guarantee on its own.

## 4. `?chrome=` — suppress the board's own tab strip, per viewer

**The one change the extension actually requires.**

### Why it has to be a URL parameter

Two viewers can look at one board in the same second — the extension's webview and a
plain browser — and they must disagree about chrome while agreeing about content.
Chrome visibility is per-viewer navigation state, which this project's own rule (carried
over from the spike, unchanged by the port) keeps out of `.aboard/aboard.json`
entirely, alongside selection, zoom, collapsed blocks and chat drafts. A URL parameter
is per-viewer by construction: no write, no conflict, nothing an agent can clobber.
`?nosse=1` is the existing precedent for exactly this shape and needs no change.

The extension cannot do this from its side — the frame is cross-origin, so it can
neither inject CSS nor reach the DOM. It can only ask, via the URL.

### What to hide, and what must stay

The head of `pkg/aboard/web/aboard.html` (`board.html` until the rename lands) wraps
three rows under `.board-head`:

| row | under `chrome=notabs` |
|---|---|
| `.topbar` — title, meta, save-state, version badge, Notify button | **stays** |
| `.tabstrip` → `.tabs` (the button list) | **hidden** |
| `.tabstrip` → `#add-tab` (the `+`) | **stays** |
| `.tab-note` — "this tab is for …" | **stays** |

Keep the `+`: it is the only trigger for the new-tab dialog, and hiding the whole
`.tabstrip` either strands a human working inside the extension or forces the
extension to reimplement a dialog that belongs to the board. Keep the topbar: it
carries the Notify button and the which-binary-is-serving badge, neither duplicated
in a sidebar.

### Shape

```
?chrome=full      (default, and what an unparameterised URL gets)
?chrome=notabs    hide .tabs — what the extension loads
?chrome=none      hide .board-head entirely — screenshots, embedding elsewhere
```

Stamp it once as `document.body.dataset.chrome`; an unknown value falls back to
`full` — a typo that blanks the UI is worse than a typo that does nothing. It must
compose with the deep link (`?chrome=notabs#tab=bb71`) and survive the board's own
self-reload paths (`reload.go`, unrenamed, ported verbatim) — a reload preserves
query and fragment already, this is free but still worth one test.

### Verification

*(As built.)* Assert on rendered DOM, in a real browser: `#tabs .tab:visible` is zero
under `notabs` and non-zero without, and `#tabs .tab` is still non-zero either way —
the strip is hidden, not unbuilt, so a count over ALL of them would have been the
wrong assertion and would have quietly demanded a shell with two code paths. Do not
extract containers or count closing tags (the spike's own gotcha, hit twice there
already). `#add-tab`, `.topbar`, `#poke` and `.tab-note` are asserted to survive
`notabs`. Rebuild (`pkg/aboard/web` is embedded) before checking — `make e2e` depends
on `build`, so the suite cannot test a stale copy.

## 5. Announce the active tab to the embedder

### Why this is not optional polish

The board switches tabs from inside itself even with the strip hidden — `[`/`]` for
previous/next, `1`–`9` for the nth tab, both calling the same `activate()` the
strip's own clicks call. A sidebar that can only ever *send* navigation drifts out of
sync with what the human is looking at within seconds of them pressing `]`, and a
tree whose highlight lies is worse than a tree with no highlight — it is the thing
they will trust to answer "where am I". Same problem at load: the board picks its own
initial active tab (last-used, else the first), so the extension cannot know what it
is showing until told.

### Shape

Inside `activate(id)`, after the active id is set:

```js
// A host that embeds the board (the VS Code panel) keeps its own tab list, and the
// board switches tabs on its own — [ ] and 1-9, and the default on load. A sidebar
// that only ever sends navigation would drift, so say what happened.
if (window.parent !== window) {
  try { parent.postMessage({ __aboard: 'active', tab: id }, '*'); } catch {}
}
```

**The envelope key is `__aboard`, not `__board`.** Plan-1 decision 12 renames the
bridge outright, with no alias — `window.aboard`, `window.__ABOARD_DATA__`, and every
`{__board: 'set' | 'height' | 'data'}` message the `html`-tab bridge already speaks
(verified against the ported, pre-rename source: those are the three literal values
in use today, not the two the spike-era version of this document guessed) becomes
`{__aboard: 'set' | 'height' | 'data'}`. This `'active'` message reuses the same
renamed namespace rather than inventing a second one — one vocabulary, one place to
look, same reasoning as the original.

Other details, unchanged from the spike version of this handoff and still correct:
`'*'` as target origin is fine (the tab id is already in the page's own URL, and the
embedder's `vscode-webview://<uuid>` origin is not knowable in advance — the receiver
authenticates by comparing `event.source`, not origin); guard on `window.parent !==
window` so a plain browser posts nothing; do not send the tab list, state, or notices
this way — the extension reads `/aboard.json` and `/events` like every other client,
and a second weaker channel for the same data is a bug factory.

### One correction to the sketch above: it announces a CHANGE

The code in this section posts from inside `activate(id)` unconditionally, and that
is not quite right. `repaint()` ends with `activate(activeId)`, and a repaint runs on
every write that arrives over `/events` — so the same id is posted again every time an
AGENT touches the board, at whatever rate somebody else happens to be working. The
extension answers each message with `TreeView.reveal(node, { select: true })`, so the
human's sidebar selection would be dragged back under their cursor by writes that
changed nothing they were looking at.

The shell remembers the last id it announced and posts nothing when it has not moved.
That is one variable, and it makes "posted whenever the active tab changes" — which is
what every document here already said — true.

### Verification

*(As built.)* A same-origin harness is enough, no VS Code needed: a wrapper page
iframes `/?chrome=notabs&probe=1`, records every message whose `event.source` is the
frame, and asserts both halves — one arrives at LOAD naming the tab the board chose
for itself, and pressing `]` inside the frame produces another naming a different tab,
which is then confirmed to be the tab actually on screen. A second test pins the
envelope: every message the board posted is shaped `{__aboard, tab}` and no other. A
third drives a repaint through the `?probe=1` seam — a foreign write with the write
taken out — and asserts no two consecutive announcements name the same tab.

One trap, and it cost a run: `Locator.Press` on the frame's `<body>` sends the key to
the WRAPPER, because `<body>` is not focusable and the keystroke goes to whatever
frame has focus. Click something inside the frame first. The symptom is an `active`
message that never arrives, which reads exactly like the feature being broken.

## 6. Do not let `localStorage` take the page down in a third-party frame

`activate()` calls `localStorage.setItem(...)` unguarded, and the read at load time is
unguarded too — the only two call sites in the whole UI. Inside a webview the board
runs as a third-party frame, where storage is partitioned and, in some
configurations, refused outright — a refusal is a thrown `SecurityError`, not a
`null`, and thrown from inside `activate()` it takes out tab switching entirely,
which is the one gesture the extension depends on. A one-line try/catch on each side
converts a total failure into a forgotten preference. Cheap insurance; do it in the
same pass as §5, since it is the same file and the same function.

## 7. Deliberately not doing

Unchanged reasoning from the spike version, restated with new names:

- **No new endpoint.** `/aboard.json` + `/events` + `/capabilities` + `/health` +
  `/poke` + `/waiters` is the whole surface. A `/tabs`-shaped convenience endpoint
  would be a second source of truth.
- **No auth, and no relaxing `connect-src 'none'`.** The server still cannot
  distinguish a browser write from an agent one.
- **No "vscode mode" on the server.** Everything in §4–§6 is per-request or
  per-viewer, never a server-side flag for who is looking.
- **The active tab does not go in `.aboard/aboard.json`.** Two viewers, two active
  tabs — shared state is wrong for one of them the instant there are two, and it
  would grow a spurious `touched` dot besides.
- **No rendering moves into the extension.** The board serves the board.
- **`touched` stays a single human-facing marker.** Dismissing it in the extension
  clears it in the browser too — one human, one "have you looked". Per-actor read
  state is what `seen` is for, unaffected by any of this.

## 8. Order of work — done in this order

1. **`?chrome=` (§4).** Nothing else in this document unblocks the extension without it.
2. **`{__aboard:'active'}` (§5).** Without it the sidebar highlight lies the moment
   the human touches `]`.
3. **`localStorage` guards (§6).** Same file as §5, same pass.

All three landed together. No Go changed: §4 is a stamp in `aboard.html` plus two
CSS rules keyed off it, and §5–§6 are three small functions beside `activate()`.
The suite is `make e2e` now (`make smoke` is gone), and `test/e2e/embed_test.go`
carries seven tests — every one of them seen failing against the previous shell
before the change went in.

Three things worth keeping from the build:

- **The stamp is a classic `<script>` at the top of `<body>`, not a line in the
  module.** The module is deferred, so stamping there paints the tab strip and then
  removes it — a visible flicker in every embedder, on every load.
- **`.tabs` is hidden by CSS, not left unbuilt.** The strip still exists in the DOM
  under `notabs`; `chrome` decides pixels, not whether the shell has a second code
  path. That is also why the test counts `#tabs .tab:visible` rather than
  `#tabs .tab` — the original verification note in §4 said "count is zero", which
  is only true of the version of this feature that forks the shell. The rule is
  scoped `body[data-chrome="notabs"] .board-head .tabs`: `.tabs` is the shell's own
  list, and a renderer growing an element by that name inside a view must not have
  part of its contents blanked by somebody else's chrome switch.
- **The `active` message reports a change, not a redraw** — see the correction in
  §5. The first version of it posted from every `activate()` call, and `repaint()`
  makes one of those on every foreign write.

## 9. What the extension loses if an item does not land

All three landed on 2026-08-26, so this table is now a record of what was avoided
rather than a risk register. Kept because it is the reason each item was worth
doing.

| item | status | what was avoided |
|---|---|---|
| §4 `?chrome=` | **landed** | Two tab lists, one above the other. Ugly, fully functional. |
| §5 `active` message | **landed** | Sidebar highlight drifting on `[`, `]`, `1`–`9`, and unknown until the human's first sidebar click. |
| §6 storage guards | **landed** | Nothing visible today; a webview that refuses partitioned storage would have broken tab switching outright, and it would have looked like the extension's fault rather than the board's. |

# board

A shared board for a human and Claude Code. `board.json` is the single source of
truth, on disk, in the repo. The browser is one editor of it; Claude Code is another.

## Run

```sh
./restart.sh            # builds the Go binary, frees the port, serves
./restart.sh -dev       # same, but serves the UI from disk (no rebuild per edit)
```

Or via make: `make run`, `make dev`, `make status`, `make dist`.

It prints its URL on startup. Inside VS Code: `Ctrl/Cmd+Shift+P` →
**Simple Browser: Show** → paste that URL. It docks as an editor tab, so the
board sits beside your code.

## Ports, and running several boards

**The port is derived from the project's absolute path** (a hash into
41000–48999), so:

- two checkouts never collide — each gets its own port automatically;
- the same checkout keeps the same URL between runs, so the Simple Browser tab
  and any bookmark stay valid.

If the derived port is taken by something that is *not* a board, it walks forward
and records where it actually landed. If it is taken by *this project's own*
board, it refuses to start a duplicate and points at the running one:

```
this project's board is already running at http://localhost:46624 (pid 568001)
```

The running board is recorded in `.board/instance.json` (gitignored):

```json
{ "app": "board", "project": "/home/you/proj", "port": 46624,
  "url": "http://localhost:46624", "state": "board.json", "pid": 568001 }
```

That file is the discovery mechanism. `restart.sh` reads it to stop *only this
project's* board rather than every `./board` on the machine; the test scripts read
it instead of assuming a port; and Claude Code can read it to learn the URL
without being told. `./board -status` prints it, and `GET /health` returns the
same record so one board can identify another over the wire.

### Two sessions, one board

This is the default and it works: **one server, one `board.json`, both sessions
reading and writing it, one browser tab showing everything.** The server is a
single process on the project's port — whichever session starts it, the other
finds it.

Two rules make it safe:

- `./restart.sh` **leaves a healthy board alone** and just prints its URL. Only
  `-force` restarts. A second session therefore cannot yank the server out from
  under the first.
- Write through `./board -apply`, not by editing the file. Direct writes have no
  compare-and-set, so concurrent edits are silently lost:

```sh
./board -apply -by "agent-1" < edited.json
```

`-apply` takes the `updatedAt` already inside the submitted document as its
base, so "read, edit, apply" is safe by construction. A stale write is refused:

```
refused: the board changed since you read it (…) — re-read board.json, redo the edit, apply again
```

`-by` lands in `lastEditedBy`, so the browser's "Claude changed this" banner
fires and each session can see who moved what. See `CLAUDE.md`.

### Two separate boards in one project

When sessions should *not* share — a side investigation that must not disturb the
main board:

```sh
./restart.sh -name review     # own port, own board.review.json, own instance record
```

`-name` derives a different port, uses `board.<name>.json` as state, and records
itself separately, so the two never interfere.

Overrides, in precedence order: `-port N`, then `PORT=N`, then the derived port.

The UI is compiled into the binary with `//go:embed`, so deploying the board is
copying one file — no Node, no `node_modules`, no asset directory to ship
alongside. `board.json` deliberately stays on disk: it is the shared state both
the browser and Claude Code read and write.

Use `-dev` while changing `views/` or `app.css` — it reads those from the working
directory instead of the embedded copies, so a reload is enough. Without it the
embedded copies win and edits appear to do nothing.

`server.js` is the original Node implementation, kept as a fallback for machines
with no Go toolchain (`restart.sh` picks it automatically). The two are
behaviourally identical; the Go one additionally serves `ETag`s, so the 3.5 MB
mermaid bundle revalidates as a `304` instead of being resent on every reload.

## The loop

- **You edit** — in any of the five views below. Each gesture POSTs the new state
  and the server writes `board.json`.
- **Claude edits** — reads `board.json`, changes it, writes it back. The server
  watches the file and pushes an SSE ping; open boards reload and show a banner.
- **Neither side clobbers the other** — a write carries the `updatedAt` it was
  based on. If the file moved on in between, the server answers `409` and the
  browser reloads rather than overwriting.
- **No echo loops** — each browser tags its writes with a client id and ignores
  the notification for its own change.

## Tabs

**Tabs are data, not code.** `board.json` carries a `tabs[]` array; each tab is a
name, a `type` that picks a renderer, and its own `state`. An agent opens one for
whatever it needs to show and names it accordingly — the types below are
capabilities, not a fixed set of tabs.

| type | what it renders |
|---|---|
| `dag` | nodes and parent links as a tidy tree; drag a node onto another to reparent |
| `kanban` | the same nodes grouped by `status`; drag between columns |
| `diagram` | mermaid — 23 diagram and chart types |
| `form` | sliders, selects, checkboxes, text: structured answers |
| `markup` | images with a drawing layer; normalized coordinates an agent can interpret |
| `chat` | a work channel — agents coordinate where the human can watch and interject |
| `notes` | free text; the plain escape hatch |
| `html` | agent-authored HTML/CSS/JS in a sandboxed frame with no network access |
| `stack` | several of the above in one tab, top to bottom, collapsible |

A tab may set `stateFrom` to render another tab's state, so a `dag` and a
`kanban` can be two readings of one dataset. A new capability is a line in the
`TYPES` registry in `board.html`, not a new tab.

### Change markers

When an agent's write changes a tab, the server stamps it `touched`. That raises
a dot on the tab and a banner inside it; **only the human dismissing it clears
either**. An agent write cannot remove the marker — the server carries it
forward — so a later write can never hide an earlier change.

### Removal needs acknowledgement

**An agent cannot delete a tab.** A write that drops one has the tab restored
with `pendingRemoval` set, which the human answers in the tab with *Keep* or
*Remove tab*. Agents ask; the human decides. Enforced in `tabs.go`, not left to
convention.

### The html type

Served from `/tab/<id>/html` into an iframe with `sandbox="allow-scripts"` (no
`allow-same-origin`) behind a CSP whose `connect-src` is `'none'`. So a widget can
do anything local — canvas, SVG, WebGL, animation — and nothing over the network.
That matters because this server has no authentication: anything that can reach it
can rewrite the whole board. State round-trips through a `postMessage` bridge
(`board.get()` / `board.set()` / `board.onData()` / `board.fit()`), so an
interactive widget persists like any other tab.

## Dependencies

There is no `package.json`, no `node_modules`, and no `go.sum`. `go build` is the
whole setup step, and the result is one file.

| layer | depends on |
|---|---|
| server (Go) | **stdlib only** — no `go.sum`, zero external modules |
| server (Node fallback) | Node built-ins only — `http`, `fs`, `path` |
| browser | vanilla ES modules, no framework, no build step |
| fonts | system stacks (`ui-sans-serif`, `ui-monospace`) — no webfonts |
| network at runtime | none; the board works fully offline |

The Go server watches `board.json` by polling its content hash every 200 ms
rather than using `fsnotify`. That keeps the module count at zero, and it is also
more robust than a single-file OS watch, which a rename-based save silently
breaks.

One third-party file exists: **`vendor/mermaid.min.js`** (mermaid 11.17.0, MIT),
committed rather than installed, loaded only when the Diagram tab is first
opened. It is self-contained and carries mermaid's own dependency tree inlined —
d3, dagre, elkjs, cytoscape, DOMPurify, marked, khroma, dayjs, roughjs — so it is
one file to trust holding a dozen upstream libraries. See `vendor/README.md` for
its provenance, checksum, and update procedure.

The test scripts additionally expect a `chromium`-family browser and `curl` on
`PATH`, but nothing at runtime does.

## For Claude Code

`.claude/skills/board/` is a skill that teaches a session how to use this board:
which tab suits which kind of explanation, the `board.json` schema, how to read
the user's edits back, and how two sessions share one board without losing each
other's writes. It is auto-discovered — no registration needed.

`CLAUDE.md` carries only the two rules that must not wait for a skill load: write
through `./board -apply`, and do not restart a healthy server.

## Files

| path | role |
|---|---|
| `board.json` | all state: `nodes`, `columns`, `diagram`, `form`, `markup` — the only file not embedded |
| `main.go` | the server: embedded UI, static serve, compare-and-set POST, poll → SSE |
| `Makefile` | `run` / `dev` / `build` / `check` / `test` / `dist` |
| `server.js` | the original Node server, kept as a no-Go fallback |
| `board.html` | shell: tab router, save plumbing, live reload |
| `app.css` | the single token set the whole UI is coloured from |
| `views/*.js` | one ES module per tab, each exporting `mount<Name>(root, ctx)` |
| `.claude/skills/board/` | the skill: when to use the board, schema, recipes, multi-session |
| `CLAUDE.md` | the two always-on safety rules |
| `vendor/mermaid.min.js` | vendored so the Diagram tab works with no network |
| `assets/` | images the Markup view can point at |

## Test

```sh
./test/smoke.sh          # needs the server running, and a chromium-family browser
```

It mounts every view against the real `board.json` in headless Chromium and fails
if any module throws, exports the wrong name, or renders nothing — the failure a
syntax check cannot catch. Then it loads the real shell once per tab and checks
each one activates.

Two URL flags exist for it, both useful by hand too:

- `?tab=dag` — deep-link a view instead of the last one used.
- `?nosse=1` — skip the live-reload stream. The stream never closes, which stops
  a headless browser from ever reaching network-idle.

```sh
./test/shot.sh              # screenshot every tab into .shots/ (gitignored)
./test/shot.sh dag diagram  # or just some
```

Worth doing after any restyle: two bugs in this tool were invisible to colour
assertions and obvious in a screenshot — a DAG that never auto-fit, and mermaid
labels clipped by a font-weight override applied after mermaid had measured them.

`test/theme-probe.html` dumps the palette actually in effect, quicker than
reading colours out of an image when a token looks wrong.

## Colours

Single dark theme, taken from the **FireFly Pro** VS Code theme
(`ankitcode.firefly`) — specifically that theme's *neutral* black family rather
than its blue-tinted editor chrome, so the board is black with lighter blacks
layered on it. Depth runs upward from black: `bg → sunken → surface → raised`.

| token | value | source in the theme |
|---|---|---|
| `--bg` | `#000000` | `titleBar` / `input.background` |
| `--sunken` | `#0a0a0a` | `list.hoverBackground` |
| `--surface` | `#151515` | `dropdown.background` |
| `--raised` | `#202020` | — (derived) |
| `--line` | `#2a2a2a` | `scrollbarSlider.background` |
| `--line-strong` | `#3d3d3d` | near `tab.activeBackground` |
| `--text` | `#ccd4e0` | `editor.foreground`, brightened for contrast |
| `--accent` | `#a4bd00` | `button.background` (also strings) |
| `--edge` | `#4a4a4a` | — (neutralised line number) |
| `--mark` | `#fb8c00` | attribute-name orange |
| `--agent` | `#a7adf4` | `support.function` periwinkle |

`--text` keeps the theme's slightly cool cast, since that is what the editor
beside it uses; everything structural is neutral.

Text colours are pinned to **WCAG AAA (≥7:1)** against surface, sunken and black,
because most type here is small. Hierarchy is carried by size and weight, not by
fading colour toward the background:

| token | on `--surface` | on black |
|---|---|---|
| `--text` `#ccd4e0` | 12.2:1 | 14.1:1 |
| `--muted` `#b4b4b4` | 8.8:1 | 10.1:1 |
| `--dim` `#a4a4a4` | 7.3:1 | 8.5:1 |

## Diagrams

### What renders

Verified by rendering one sample of every type against the vendored bundle
(`test/mermaid-probe.html` — rerun it after upgrading mermaid). **23 of 24 work:**

| | |
|---|---|
| **graphs** | `flowchart` / `graph`, `block-beta`, `architecture-beta`, `mindmap`, `gitGraph` |
| **software** | `sequenceDiagram`, `classDiagram`, `stateDiagram-v2`, `erDiagram`, `requirementDiagram`, `C4Context`, `packet-beta` |
| **charts** | `pie`, `xychart-beta`, `quadrantChart`, `radar-beta`, `sankey-beta`, `treemap-beta` |
| **planning** | `gantt`, `timeline`, `journey`, `kanban` |

Only `zenuml` is unavailable — it needs a separate plugin that isn't in the bundle.

Gotcha found while probing: in `requirementDiagram`, a `text:` field containing
punctuation must be quoted (`text: "must round-trip"`) or the parser swallows the
following line.

### Colouring

The Diagram tab feeds the board's tokens to mermaid as `themeVariables`, so an
uncoloured diagram already matches the tool. To colour individual nodes, hit
**Add colours** in the toolbar: it appends `classDef` classes resolved from the
live tokens, so hand-coloured nodes stay in the palette instead of fighting it.

Two families, because solid fills rarely work on black:

```mermaid
%% ring — dark surface, coloured border. Quiet; use freely.
classDef accent fill:#151515,stroke:#a4bd00,stroke-width:2px,color:#ccd4e0

%% fill — solid with dark ink. Loud; use on the one node that matters.
classDef accentFill fill:#a4bd00,stroke:#a4bd00,color:#151515
```

Apply either way:

```mermaid
class H,P accent          %% several nodes at once
NodeId:::accentFill       %% inline on one node
style S fill:#151515,stroke:#39bae6   %% one-off, no class
```

Hues map to meaning already in use elsewhere in the board: lime `accent` for the
human path, periwinkle `agent`, orange `warn` for shared state, cyan `info`,
`quiet` for background detail. Chart types (`pie`, `xychart`, `radar`) don't take
`classDef` — colour those through `themeVariables` in `views/diagram.js`.

There is no light variant by design, so every colour is stated once and
`color-scheme: dark` lets native controls render dark too. Views reference only
tokens — no hardcoded hex — so a retheme is a change to `app.css` alone. The
Diagram tab reads the same tokens at render time and feeds them to mermaid as
`themeVariables`.

## Adding a view

Write `views/thing.js` exporting `mountThing(root, ctx)` where `ctx` gives you
`state` (live), `save()` (debounced POST), and `refreshOthers(id)`. Return
`{ refresh() }` — the shell calls it when the file changed underneath or the tab
was re-activated. Then add it to the `VIEWS` array in `board.html`.

Two rules that matter: keep per-viewer UI state (selection, active tool, zoom) in
local JS, never in `board.json`; and in `refresh()`, never clobber an input the
human currently has focused.

# Brief 22 — themes: dark/light switch, `.aboard/theme.json`, no product names in the palette

Read `COMMON.md` first. Items 1–20 are landed; the suite is `make e2e`; the board draws its own
dialogs (`views/dialog.js`); wire keys live in `pkg/aboard/wire.go`; `capsHash` is `34af0bc9`.
The human asked for five things on 2026-08-26; the first four are this brief, the fifth
(deriving colours from the VS Code theme in the extension) is item 23 and needs one hook from
you (see §4). Another workflow is writing docs in the worktree `aboard-wt-21` — do not touch it;
if you and it both edit `docs/README.md`, keep your edit to one added line.

## 1. Remove the product name from the theme

`app.css` and the docs describe the palette as "FireFly Pro's neutral-black family" (grep
`-ri firefly` across the tree, docs and the spike-derived comments). Remove the name everywhere;
describe the palette by what it is (a neutral near-black surface ramp, periwinkle accent, …).
No colour changes in this step.

## 2. A dark/light switch, dark the default

- `app.css` today is a single dark theme with every colour a `:root` token (21 of them — list
  them from the file). Add a **light** variant of the same tokens under
  `:root[data-theme="light"]`, designed to the same rules: every text/background pair pinned to
  WCAG **AAA** for small type (compute the contrast ratios and put them in a table in the
  commit body — do not eyeball), the same semantic roles (`--agent` periwinkle stays a periwinkle
  that passes on light; `--danger`, `--mark`, `--status-*` likewise), tokens only, no hex outside
  `app.css`. Mermaid's theme config in `views/diagram.js` reads tokens — verify diagrams and the
  `html`-tab frame (`htmltab.go` splices `rootDeclarations`) follow the switch: the frame is a
  separate document, so the switch must reach it (post the token set on change, the way the
  bridge already sends `data`, or reload the frame — say which and why).
- The switch: a control in the topbar (through `button()` — chrome, not a declared control;
  or declare it in a shell spec if the shell has one — check how the help/notify buttons are
  declared), tokens only, with a tooltip. Per-viewer: the choice lives in `localStorage`
  (`aboard.theme`), guarded like the existing storage calls; **dark is the default** when nothing
  is stored and no theme.json says otherwise (§3). `?chrome=` unaffected. Keyboard: a shortcut is
  optional; if added, list it in the help panel.
- e2e: switching flips `data-theme` on `<html>`, survives a self-reload, is per-viewer (a second
  context is dark), the html-tab frame's `--bg` follows within a second, the `ui` gallery and a
  mermaid diagram render under light (screenshots LOOKED at, both themes). A Go test parses both
  variants out of `app.css` and asserts the same token SET in each (a light token missing is the
  drift `htmltab.go` already guards against).

## 3. `.aboard/theme.json` overrides the default theme

- Shape (declare it, document it, validate it):
  ```json
  { "version": 1, "default": "dark",
    "dark":  { "--bg": "#…", "--accent": "#…" },
    "light": { "--bg": "#…" } }
  ```
  Only the 21 declared token names are accepted, in either variant, each optional (an
  override is a PATCH over the built-in variant); values are CSS colours; `default` is `dark` or
  `light` and decides the initial theme for a viewer with nothing stored. Unknown token → a
  warning naming the accepted set (the same voice as the colour-name warning `apply` gives);
  an unparseable file → the built-in theme plus a warning, never a blank board.
- Where it lives: `<root>/.aboard/theme.json` (`Root.ThemeFile()` in `layout.go` — the only file
  that joins paths); project content, not `run/` — it is meant to be committed by projects that
  want a house style, so say so in the docs (the repo's own `.aboard/` stays ignored).
- How it reaches the browser: `GET /theme.json` (declared in `declaredRoutes`; 404 → built-in
  theme; ETag like the document) and the shell applies it as inline `:root` / `:root[data-theme]`
  declarations before first paint (no flash — same discipline as the `?chrome=` stamp); the html
  frame gets the merged token set through `rootDeclarations` (make it take the override); the file
  watcher notices a change to theme.json and pushes it over SSE like a UI change (or say why a
  reload is enough). `aboard status` prints "theme: .aboard/theme.json (default light)" when one is
  present; `aboard capabilities` lists the token names (they are already the declared palette for
  `ui`/`markup` colours — check `tones`/`colors` in the specs and reuse, do not duplicate).
- Tests: Go — parsing, validation messages, the patch semantics, the route; e2e — a theme.json
  on the temp board changes `--accent` on screen and in the html frame, `default: light` boots
  light for a fresh context, a stored choice beats `default`.

## 4. The hook the extension will need (item 23)

The board must accept a theme handed to it by an EMBEDDER: a `{__aboard:'theme', tokens:{…},
kind:'dark'|'light'}` message from `window.parent` (authenticate by `e.source === window.parent`,
the same rule as the `active` message going the other way), applied as a per-viewer override
on top of theme.json's, never written anywhere. Validate token names the same way. e2e: a
same-origin wrapper posts a theme and `--bg` changes; a message from a non-parent source is
ignored. Document the message in `http-api.md` beside `active`.

## 5. Docs

`docs/reference/theme.md` (new: the tokens with their roles and both built-in values, the
theme.json schema, the switch, the embedder message, the AAA rule); `docs/how-to/` a short
"give a project a house style" page; `docs/reference/layout.md` (the file), `http-api.md` (the
route, the message), `capabilities.md` if the manifest changed; `CLAUDE.md` (the colours decision
bullet gains the two variants and theme.json; the FireFly sentence goes); the skill (colours
come from tokens; a project may have a theme.json — read it before naming colours? no — colour
NAMES are what agents use, values are the theme's; say that); `CHANGELOG.md`. `make caps` if a spec
or the command table changed (capsHash moves — say so).

## Done when

All four parts with tests; screenshots of both themes looked at (gallery, a dag, a markup tab, an
html widget, a mermaid diagram); ladder green (`make lint`, `make fmt-check`, `make pre-commit`,
`make caps`+`--check`, `make docs-check`, `make e2e` once per tool call, `make ci-local` once).
The human's boards on 47781 and 44917 must not be touched; scratch projects only.

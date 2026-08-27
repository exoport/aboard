# Colour and themes

Every colour on this board is a CSS custom property declared in one file
(`pkg/aboard/web/app.css`) and nothing else. No renderer contains a hex value, no
widget has to restate the palette, and an agent that wants a colour names one of these
tokens rather than picking one.

There are **two variants of the same 21 tokens** — dark, which is the default, and
light. The choice is the viewer's, made in the page and stored in their browser. A
project may patch either variant from `.aboard/theme.json`, and an embedder that frames
the board may hand it a palette of its own.

## The tokens

Each row is one semantic role. The role is the stable thing: `--agent` is the colour of
everything an agent says on this board, in both themes and whatever the values are.

| token | role | dark | light |
| --- | --- | --- | --- |
| `--bg` | the page ground | `#000000` | `#ffffff` |
| `--sunken` | recessed wells — inputs, the note and request strips | `#0a0a0a` | `#f6f7f9` |
| `--surface` | panels and cards | `#151515` | `#f0f2f6` |
| `--raised` | the ground under a button | `#202020` | `#e6e9ef` |
| `--text` | body text | `#ccd4e0` | `#141a24` |
| `--muted` | secondary text — labels, metadata | `#b4b4b4` | `#3a4250` |
| `--dim` | tertiary text — hints, at 0.83rem | `#a4a4a4` | `#464f5e` |
| `--line` | hairlines | `#2a2a2a` | `#d7dae2` |
| `--line-strong` | a border that has to be seen | `#3d3d3d` | `#b2b8c4` |
| `--accent` | the one accent: the primary button, the selected tab's rule | `#a4bd00` | `#454f00` |
| `--accent-ink` | text ON the accent | `#151515` | `#ffffff` |
| `--accent-dim` | the accent, quietened — borders on action strips | `#6d7d0a` | `#67780e` |
| `--mark` | what the HUMAN asks for: request strips, write warnings | `#fb8c00` | `#763e00` |
| `--agent` | what an AGENT says: unread dots, change banners, the note strip | `#a7adf4` | `#363ba3` |
| `--edge` | graph edges, lane and author colours | `#4a4a4a` | `#8a919f` |
| `--focus` | the focus ring, and links inside a widget | `#39bae6` | `#0d4f66` |
| `--danger` | a removal request, a destructive button | `#ff0066` | `#a30040` |
| `--drop` | the tint under a valid drop target | `#1b2109` | `#ebf1d2` |
| `--status-todo` | a node or card not started | `#6b6b6b` | `#767d8a` |
| `--status-doing` | in progress (the accent) | `#a4bd00` | `#454f00` |
| `--status-done` | finished, and therefore quiet | `#3d3d3d` | `#b2b8c4` |

Depth runs **upward from black** in dark and **downward from white** in light:
`bg → sunken → surface → raised`, each layer a little further from the ground. That is
the same sentence in both directions, which is what makes the light theme a mirror
rather than an inversion.

A renderer that takes a colour by NAME — `ui`'s `tone`, `markup`'s `color` — takes it
without the `--`. Those two lists are declared in `views/ui.spec.json` and
`views/markup.spec.json`, and a test asserts every name in them is a token this table
has: the palette is declared once and checked against, never copied.

## The contrast rule

**Text is pinned to WCAG AAA (≥7:1) on the page ground, `--sunken` and `--surface`**,
because most type on this board is small — `--dim` carries hints at 0.83rem and was the
one that originally failed. Hierarchy is carried by size and weight, not by fading a
colour toward its background.

Measured, not eyeballed (contrast ratio, ink on ground):

| ink | dark: bg / sunken / surface / raised | light: bg / sunken / surface / raised |
| --- | --- | --- |
| `--text` | 14.1 / 13.3 / 12.2 / 10.9 | 17.5 / 16.3 / 15.6 / 14.4 |
| `--muted` | 10.1 / 9.5 / 8.8 / 7.9 | 10.1 / 9.4 / 9.0 / 8.3 |
| `--dim` | 8.4 / 7.9 / 7.3 / 6.5 | 8.3 / 7.7 / 7.4 / 6.8 |
| `--accent` | 9.9 / 9.3 / 8.6 / 7.6 | 8.9 / 8.3 / 7.9 / 7.3 |
| `--agent` | 9.9 / 9.4 / 8.6 / 7.7 | 9.1 / 8.5 / 8.1 / 7.5 |
| `--mark` | 8.9 / 8.3 / 7.7 / 6.9 | 8.5 / 8.0 / 7.6 / 7.0 |
| `--focus` | 9.3 / 8.8 / 8.1 / 7.2 | 9.0 / 8.4 / 8.1 / 7.4 |
| `--danger` | 5.4 / 5.1 / 4.7 / 4.2 | 8.0 / 7.4 / 7.1 / 6.6 |

Three notes on what that table does and does not claim:

- **`--raised` is outside the pin, in both themes.** It is the ground under a button,
  where the text is a label rather than prose, and pinning it would have cost `--dim`
  its place in the hierarchy. Dark has always been that way; light matches it.
- **`--danger` does not reach AAA in dark** (4.7:1 on `--surface`) and does in light
  (7.1:1). It is a pink-red that would stop being one if it were darkened on a black
  ground. Recorded rather than quietly rounded up.
- **`--edge`, `--line`, `--line-strong` and `--accent-dim` are structure, not prose** —
  strokes, hairlines, lane dots — and are matched to their dark counterparts rather than
  held to a text rule they were never meant to pass.

Three pairs where the ink is not one of the text tokens:

| pair | dark | light |
| --- | --- | --- |
| `--accent-ink` on `--accent` (the primary button) | 8.6 | 8.9 |
| `--bg` on `--mark` (the request count badge) | 8.9 | 8.5 |
| `--bg` on `--danger` (a destructive button, hovered) | 5.4 | 8.0 |

## The switch

A button in the topbar, labelled with the theme that is on (`☾ dark` / `☀ light`) and
titled with what pressing it does. It sets `data-theme` on the root element, which is
what the stylesheet keys off:

```css
:root                        { /* dark: the default, and what an absent attribute means */ }
:root[data-theme="light"]    { /* light */ }
```

The choice is **per viewer**: it lives in `localStorage` under `aboard.theme` and never
in the board document. Two people can look at one board in the same second and must
disagree about theme while agreeing about content — the same rule that keeps selection,
zoom, scroll position and `?chrome=` out of the state file.

It is stamped **before the first paint**, by a classic script in the document head, so a
page never shows the wrong theme and then corrects itself. `?chrome=` is unaffected;
there is no keyboard shortcut, deliberately — a shell-level key would be taken away from
every renderer that might want it, and the button is always on screen.

Three things do not follow a custom property and are told about the switch instead:

| what | why | how |
| --- | --- | --- |
| an `html` tab's frame | a separate document with its own `:root` | both variants are spliced into it, and the parent posts `{__aboard:'theme', kind, tokens}` — the `kind` flips an attribute, and `tokens` carries `.aboard/theme.json`'s overrides for that variant as inline properties, because the frame's copy of them is whatever was spliced in when it LOADED and an edit to theme.json does not reach an open document. Told rather than reloaded, so the widget keeps whatever it was holding |
| a `diagram` tab | mermaid writes literal colours into the SVG it renders | it re-renders |
| a mermaid fence in `notes` or `markdown` | the same, with no mount to call | the markdown module re-renders every fence on screen |

## `.aboard/theme.json`

A project's house style. It is a **patch** over the built-in variants, not a
replacement: name the handful of tokens you disagree with and everything else keeps its
value, so a theme file written today does not lose a token added tomorrow.

```json
{
  "version": 1,
  "default": "dark",
  "dark":  { "--bg": "#07090c", "--accent": "#7fb2ff" },
  "light": { "--bg": "#fffdf7" }
}
```

| field | meaning |
| --- | --- |
| `version` | `1`. A different number warns and the file is applied anyway — refusing good colours over a number is the worse trade. |
| `default` | `dark` or `light`: which variant a viewer with **nothing stored** boots into. A viewer who has pressed the switch keeps their own choice. |
| `dark` / `light` | token overrides. Every key must be one of the 21 names above; every value must be a hex colour, a CSS colour keyword, or a function call such as `rgb(10 10 10 / 0.6)`. |

Values are **validated, not sanitised**, because they are spliced into three documents
(the shell's inline style, the script that hands the page the same object, and the html
frame). Anything with a `<`, a quote, a brace or a semicolon in it is refused. A value
CSS would drop anyway — a misspelt keyword — is not this file's business to catch.

**Nothing here can blank a board.** An unknown token, an unusable value or an unknown
`default` is dropped with a warning naming what is available; an unparseable file is
ignored entirely with a warning saying so; and the built-in palette applies in every one
of those cases. The warnings reach three places, because they are three different people
at three different moments:

- the serve log, for whoever started the board;
- `aboard status`, which prints `theme   <path> (default light)` and any warnings under it;
- `GET /theme.json` and the browser console, for whoever is looking at the board.

**Where it lives, and whether to commit it.** `<root>/.aboard/theme.json` is *content*,
beside the board document and `uploads/`, not machine-local runtime under `run/`. It is
meant to be committed by projects that want a house style — which means un-ignoring that
one path, since `aboard init --gitignore` ignores `.aboard/` wholesale. See
[how to give a project a house style](../how-to/give-a-project-a-house-style.md).

The file is watched. An edit reaches every open page over the SSE stream, without a
reload, so a colour can be iterated on with the board on screen — including the inside of
an `html` tab's frame, which is told the new values rather than being rebuilt.

## A theme from an embedder

A host that frames the board can hand it a palette — a VS Code panel derives one from
the editor's own theme, so the board belongs in the window instead of being a dark
rectangle inside a light IDE.

```js
frame.contentWindow.postMessage({
  __aboard: 'theme',
  kind: 'light',                          // optional: which variant to switch to
  tokens: { '--bg': '#fffdf7', '--text': '#1a1a1a' },
}, '*');
```

Three rules, and each of them is a refusal:

- **Authenticated by source**, never by origin: the board ignores anything whose
  `event.source` is not `window.parent`. A webview's origin is a uuid nobody can know in
  advance, so an origin check would have to be `'*'`. This is the same rule the
  `{__aboard:'active'}` message obeys going the other way.
- **Per viewer, and written nowhere** — not the board document, not `localStorage`.
  It is the host's opinion, not the human's choice, so closing the panel leaves nothing
  behind.
- **Validated against the same 21 names**, with the same value grammar. An unknown name
  is dropped with a console warning rather than set, because a custom property the board
  never reads would look exactly like the message not arriving.

The tokens are applied as inline custom properties on the root element, which outrank
every stylesheet rule in both variants — so a host does not have to know which variant
the viewer is in. Pressing the board's own switch clears them: a human pressing a button
and nothing happening is worse than a panel that stops matching its host until the host
speaks again, which it does on its own next theme change.

## Asking the binary

```console
$ aboard capabilities --format json | jq .theme
{
  "tokens": ["--accent", "--accent-dim", "…"],
  "variants": ["dark", "light"],
  "default": "dark",
  "file": ".aboard/theme.json"
}
```

The token list is parsed out of `app.css` rather than listed in Go, so the manifest
cannot disagree with the stylesheet — and adding a token to the stylesheet moves
`capsHash`, which is correct: the palette is part of the declared surface.

## See also

- [How to give a project a house style](../how-to/give-a-project-a-house-style.md) — the practical version
- [The `.aboard/` layout](layout.md) — where `theme.json` sits, and why it is content rather than runtime
- [HTTP API](http-api.md) — `GET /theme.json`, and the messages that cross the frame boundary
- [The capability manifest](capabilities.md) — how the token list gets into `aboard capabilities`
- [Why html tabs are sandboxed](../explanation/why-html-tabs-are-sandboxed.md) — why the frame is a separate document at all

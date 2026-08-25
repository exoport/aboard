# Why html tabs are sandboxed

An `html` tab runs code an agent wrote, in the human's browser, on a server with no
authentication. That is a large amount of trust, and it is the price of the one thing no
fixed renderer can do: a bespoke interactive widget for one task — a canvas, a
drag-and-drop simulation, a sketch pad.

The blast radius is closed off two ways rather than assumed away.

## The containment is two things, and neither is `frame-ancestors`

**1. An opaque origin.** The tab is served as its own document and framed with
`sandbox="allow-scripts"` and **without** `allow-same-origin`. Its origin is opaque, so it
cannot reach into the board shell's DOM, its storage, or its cookies.

**2. No network egress.** The response carries a Content-Security-Policy whose
`connect-src` is `'none'`. `fetch`, `XHR`, `WebSocket` and form posts are all refused.
Inline script and inline style are allowed — that is the entire point of the type — and
images, fonts and media are limited to `data:` and `blob:`.

`connect-src 'none'` is the load-bearing half, and the reason is the server it sits in
front of: **this server has no authentication**, so anything that can make a request to it
can read and rewrite the whole board. A widget with network access would not need a
vulnerability to do damage; it would only need a URL. Do not relax that to "just let it
fetch".

State still round-trips, without any network access at all: a small bridge exposes
`aboard.get()` / `aboard.set()` / `aboard.onData()` / `aboard.fit()`, which `postMessage` to
the parent, and the parent writes the tab's state through the normal compare-and-set path.
That works identically for an `html` block nested inside a `stack` tab — the block's own
state getter is handed down, and the served document, its CSP and its sandbox are
byte-identical to a top-level tab's.

## The `frame-ancestors` story

`frame-ancestors` is **not** the containment. It decides who may *display* the frame,
which matters only because the bridge `postMessage`s to its parent: a page allowed to
embed the tab receives whatever the widget sends.

It is also where this design got bitten, in a way worth recording because the symptom
pointed at the wrong thing.

The policy originally said `frame-ancestors 'self'`. Every `html` tab came up **blank in
the docked VS Code browser**, while the board shell itself — which sends no framing header
— loaded fine.

The cause: **`frame-ancestors` is checked against the whole ancestor chain, not the
immediate parent.** Inside VS Code the chain is

```
vscode-webview://<uuid>  →  http://localhost:<port>/  →  /tab/<id>/html
```

so the non-`self` *grandparent* blocks the frame even though the tab's immediate parent is
the board itself. Chromium reports the blocked frame's **origin**, which makes it look as
though the shell were the thing being refused — the misdirection that cost the time. It
was reproduced headlessly with a cross-origin wrapper: the nested case is refused exactly
like the direct one.

So the list is now `'self' vscode-webview: vscode-file: https://*.vscode-cdn.net`.

**Do not tighten it back to `'self'`.** If another host shows a blank tab, its webview
console names the origin it tried to frame from — add *that origin*, rather than widening
to `*`. Widening to `*` would give away the one thing this directive protects, which is
who receives the widget's messages.

## Prefer `ui` when you can

The sandbox makes an `html` tab *safe*; it does not make it *good*. Prefer a `ui` tab
whenever a component tree can express what you want:

- a component tree cannot get the theme, the contrast or the type sizes wrong, because it is drawn by trusted components from the board's own tokens;
- it has nothing to contain, so none of the machinery above is involved;
- and the next session can change one node of it, instead of reading a page of someone else's JavaScript.

Reach for `html` when the **interaction itself** is the point — canvas, drag-and-drop, a
simulation — and not merely because a layout is unusual.

## A trap that looks like a styling bug

A literal `</script>` inside a widget's HTML ends the script block early. The escape
`<\/script>` is only correct **inside a JavaScript string literal**; written into raw
HTML — which is what happens when a widget is built in a language's raw string — the
browser never sees a closing tag, the script runs to the end of the document, and
**nothing executes**. The static markup still renders, so it presents as a styling problem
rather than a dead script. Check for that before blaming the sandbox.

## See also

- [HTTP API](../reference/http-api.md) — the `/tab/<id>/html` route and its headers.
- [How to run aboard inside VS Code](../how-to/run-in-vscode.md) — the practical version of the blank-frame section.
- [Why nothing in the UI starts a session](why-nothing-in-the-ui-starts-a-session.md) — the rest of "a stray click is harmless".

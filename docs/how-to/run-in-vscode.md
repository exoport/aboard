# How to run aboard inside VS Code

The board is meant to sit **beside your code**, not in a separate browser window you
alt-tab to. VS Code's built-in Simple Browser renders it as an editor tab, so it docks,
splits and restores like any other tab.

## Dock it

```bash
cd ~/work/your-project
aboard serve          # leave this running; it prints the URL
```

In a second terminal, or from the server's own output:

```bash
aboard status         # url, pid, state file, caps beacon
```

Then in VS Code, with the same folder open:

1. `Ctrl/Cmd+Shift+P`
2. **Simple Browser: Show**
3. paste the URL

Drag the tab into a split to put the board next to the file you are working on.

**The URL is stable.** The port is derived from the project root, so the tab you docked
today is still valid next week and after a restart — and two checkouts of the same repo
get different ports rather than fighting over one. Read it from `aboard status` or from
`.aboard/run/instance.json`; never assume a port.

## Keep it running

Give the server its own terminal (a VS Code integrated terminal is ideal — it dies with
the window, which is usually what you want). If you would rather it survive the
terminal, run it detached and let the instance file be the record of where it went:

```bash
aboard serve > /tmp/aboard-serve.log 2>&1 &
aboard status
```

`aboard serve` refuses to start a second board for the same project and prints the URL
of the one already running. That is deliberate: a second session must not be able to
yank the server out from under the first. If you genuinely want a separate board — a
side investigation that should not disturb the main one — give it a name:

```bash
aboard serve --name review
```

That derives its own port, uses its own state file and records itself separately, so
the two never interfere.

## When the page reloads itself

The server hands every connected page a signature of the UI it is serving. After you
rebuild, the stream drops, the browser reconnects on its own, the signature no longer
matches what the page loaded, and the page reloads itself. A stylesheet-only change
re-links the stylesheet instead of reloading, so scroll position and selection survive,
and a reload waits for focus to leave an editable — losing a half-typed sentence to a
CSS edit would be worse than a moment of staleness.

So you should **not** need "Developer: Reload Webviews". It remains the manual fallback
for the one case the mechanism cannot cover: a page whose stream dropped while the
server was down, and therefore never reconnected.

## If a widget tab comes up blank

`html` tabs are served into a sandboxed iframe with a Content-Security-Policy, and
`frame-ancestors` is checked against the **whole** ancestor chain — not just the
immediate parent. Inside VS Code that chain includes the webview document hosting the
Simple Browser, so the policy admits `vscode-webview:`, `vscode-file:` and
`https://*.vscode-cdn.net` alongside `'self'`.

If a widget tab is blank in some other host, open that host's webview developer console:
a blocked frame is reported with the origin it tried to frame from. Add **that origin**
to the list in the server's CSP rather than widening it to `*` — and do not "tighten"
the list back to `'self'`, which is the change that made every widget tab blank in the
docked browser in the first place. The reasoning is in
[why html tabs are sandboxed](../explanation/why-html-tabs-are-sandboxed.md).

The other blank-frame cause is much simpler: a literal `</script>` inside a widget's own
HTML ends the script block early, so the markup renders and none of the code runs. It
looks like a styling problem. Check the widget's source for an unescaped closing tag
before blaming the sandbox.

## Behind a prefix

If something in front of the board is routing by path — a reverse proxy, another tool's
webview — serve it under a prefix:

```bash
aboard serve --base-path /aboard
```

The prefix is injected into the shell as a single constant, and every request the page
makes builds from it: the stylesheet and module imports, every fetch, the SSE stream,
and an `html` tab's iframe `src`. Point the Simple Browser at the prefixed URL.

## When something else draws the tab list

A host that frames the board and provides its own navigation — a VS Code extension
with a sidebar tree, for instance — asks for the board without its own tab strip:

```
http://localhost:<port>/?chrome=notabs#tab=bb13
```

`?chrome=notabs` hides the tab button list and keeps everything else: the topbar, the
`+` that opens the new-tab dialog, and the tab note. `?chrome=none` drops the whole
head, for an embedding that wants the view alone. An unrecognised value is treated as
`full`. It has to be asked for in the URL because the frame is cross-origin: a host
cannot inject CSS into it or reach its DOM.

Navigation goes both ways. The host points the frame at `#tab=<id>` — a fragment
change, so the page does not reload — and the board posts
`{ __aboard: 'active', tab: '<id>' }` to its parent whenever the active tab changes,
including the tab it picks at load and the ones `[`, `]` and `1`–`9` reach. Without
that second half a sidebar highlight goes stale the moment the human uses a key. It is
a *change*: a redraw after somebody else's write repeats nothing, so a host is free to
reveal or select on every message it gets.
**Authenticate the message by `event.source`, never by origin** — the board posts with
`'*'`, because a webview's origin is not knowable in advance.

The full contract is the shell section of [the HTTP API](../reference/http-api.md),
including what `GET /health` reports about a board served under a prefix.

## Screenshots of a docked board

For scripted screenshots, two things bite and both have the same fix — use the repo's
`test/shot.sh` (`make shot`) rather than a hand-rolled chromium command:

- a headless shot needs `?nosse=1`, because the SSE stream never closes, so the browser never reaches network-idle and writes no file at all;
- headless chromium does not reliably paint iframe content, so shoot an `html` tab at `/tab/<id>/html` directly instead of shooting the tab that frames it.

## See also

- [Your first board](../tutorials/first-board.md) — the whole loop including this step.
- [The `.aboard/` layout](../reference/layout.md) — the instance file the URL comes from.
- [HTTP API](../reference/http-api.md) — the routes the page is using while you watch it.

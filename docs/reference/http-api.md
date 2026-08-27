# HTTP API

The server binds `127.0.0.1` on the [derived port](layout.md#port-derivation) and
answers the routes below. It has **no authentication** — anything that can reach the
port can read and rewrite the whole board — which is why it is loopback-only, why
`html` tabs get no network egress, and why nothing in the browser executes anything.

Every route is also *declared* in the capability manifest, and `aboard capabilities`
prints the same table; the browser suite asserts that every declared path answers.

## Routes

| Method | Path                | Purpose                                                                  |
| ------ | ------------------- | -------------------------------------------------------------------------- |
| GET    | `/`, `/aboard.html` | The board shell.                                                          |
| GET    | `/aboard.json`      | Current state.                                                            |
| POST   | `/aboard.json`      | Write, compare-and-set (`__base`, `__origin`, `__by`, `__label`).          |
| GET    | `/events`           | SSE: state changes, waiter count, and the UI signature.                   |
| GET    | `/health`           | Who owns this port, and which binary is serving.                          |
| GET    | `/capabilities`     | The capability manifest.                                                  |
| GET    | `/theme.json`       | The project's `.aboard/theme.json`, validated; `404` when it has none.    |
| GET    | `/tab/<id>/html`    | One `html` tab as a standalone sandboxed document.                        |
| GET    | `/wait`             | Long poll: block until poked or until a predicate matches.                |
| POST   | `/poke`             | Release every waiting session.                                            |
| GET    | `/waiters`          | Who is waiting right now.                                                 |
| GET    | `/journal`          | Recent accepted writes, with the previous state of each changed tab.      |
| GET    | `/history`          | One tab's recorded prior states, newest first.                            |
| GET    | `/watch`            | Those writes as JSON lines, as they happen.                               |
| POST   | `/rendered`         | A mount receipt from the browser: what it drew, and what was pressed.     |
| POST   | `/log`              | Append output to a tab's sidecar log.                                     |
| GET    | `/log`              | The tail of one.                                                          |
| POST   | `/upload`           | An image pasted or dropped by the human.                                  |
| GET    | `/uploads`          | List the uploads.                                                         |
| GET    | `/uploads/<file>`   | Serve one, from disk.                                                     |
| GET    | *anything else*     | Static asset from the embedded web tree (`ETag`, so a reload revalidates; `X-Content-Type-Options: nosniff`). |

What an unmatched request actually gets, in the order the server decides it:

1. **`403`, before the path is looked at.** The two refusals below run first, so a
   request with a disallowed `Host`, or a write from a foreign `Origin`, is `403`
   whether or not the path it asked for exists.
2. **`404`** for a method that is not listed for a path that is — `POST /`, `HEAD /`,
   `DELETE /aboard.json`.
3. **`403 forbidden`** for a `GET` of a path outside the static allow-list — anything
   that is not `app.css`, `aboard.html`, or under `assets/`, `views/`, `lib/` or
   `test/`. The last table row serves the embedded tree, and it serves only those; a
   name outside them is refused rather than looked up, so the refusal says nothing
   about what the tree does or does not contain. Every asset that IS served carries
   `X-Content-Type-Options: nosniff`, so the `Content-Type` the server derived from the
   extension is the type the browser uses — an asset cannot be sniffed into a document.
4. **`404`** for a `GET` of a path that is inside the allow-list and holds no file —
   `/views/nosuchview.js`.

Steps 3 and 4 are the pair worth remembering: `/nope.js` is `403` and
`/views/nope.js` is `404`, and the reference said `404` for both until 2026-08-26.

### Who is allowed to ask

Two checks run in `route`, before anything looks at the path. The board has **no
authentication** and cannot have any, so these are not authentication — they are the two
things that stop a *browser* from being the thing that reaches it on somebody else's
behalf.

| check | applies to | refused with |
| ----- | ---------- | ------------ |
| **Host allow-list** — `localhost`, `127.0.0.1`, `[::1]`, with or without a port | every route, `/health` included | `403`, naming the allow-list |
| **Same-origin** — `Sec-Fetch-Site: cross-site`, or an `Origin` that is present and is not this server's own | every mutating method (anything but `GET`/`HEAD`/`OPTIONS`) | `403`, naming the header that refused it |

What still passes, and must: `Sec-Fetch-Site` of `same-origin`, `same-site` or `none`; no
`Origin` header at all, which is curl, `aboard apply` and every other non-browser client;
and the board's own page, whose `Origin` matches its `Host`.

The Host check is the DNS-rebinding guard. The bind is loopback, but *any* name that
resolves to `127.0.0.1` reaches it, and a page served from that name is then same-origin
with the board — able to read `/aboard.json`, `/journal` and `/health`, which discloses
the absolute project path and the pid. A name is not accepted merely because it resolves
to loopback; that is the attack. `403` rather than `421`: `421` invites a client to retry
on a fresh connection, and this is a deliberate refusal rather than a misdirection.

**Behind a reverse proxy**, forward a loopback `Host` upstream — nginx's `proxy_pass
http://127.0.0.1:<port>/` does this by default. A proxy configured to pass the original
public `Host` through will be refused, which is the check doing its job.

### Base path

With `serve --base-path /prefix`, the prefix is stripped before routing, so every route
above is reached at `/prefix/...`. A request for exactly `/prefix` is answered with a
`301` to `/prefix/` — without the trailing slash the shell's relative URLs would resolve
one level too high. A path that neither equals the prefix nor starts with `prefix/` is
`404`.

## `GET /` · `GET /aboard.html`

The shell. Everything below is **per-viewer**: it lives in the URL and never in the
state file, because two viewers can look at one board in the same second and must
disagree about chrome and position while agreeing about content. The rule that keeps
selection, zoom and collapsed blocks out of the document is the same rule.

| in the URL      | what it does                                                                     |
| --------------- | ---------------------------------------------------------------------------------- |
| `?tab=<id>`     | Open on that tab. `#tab=<id>&node=<id>` addresses a node inside one, and a fragment change moves the view without reloading the page. |
| `?chrome=`      | `full` (the default) · `notabs` · `none`. See below.                              |
| `?nosse=1`      | Do not open the event stream. For headless screenshots, which otherwise never reach network-idle. |
| `?probe=1`      | A test seam: exposes the shell's document plumbing on `window.__aboardProbe` for the browser suite. Nothing is exposed without it. |

Two more pieces of per-viewer state live in the browser rather than in the URL, because
nobody would want to type them: which tab you were last on (`localStorage`,
`aboard.tab`), and where you were on each tab (`sessionStorage`, `aboard.scroll.<tab>`).
Every tab shares one scrolling document, so without the second one, leaving a long list
half way down and glancing at another tab lost your place. `sessionStorage` rather than
`localStorage` because the lifetime that matters is this sitting: it has to survive the
board [reloading its own code](../how-to/run-in-vscode.md#when-the-page-reloads-itself),
and it should not still be pointing half way down a tab next week. The board also sets
`history.scrollRestoration = 'manual'`: the browser's own restoration puts back the
DOCUMENT's offset on whichever tab comes up first, which is right only by coincidence.
Every access is wrapped, because a third-party frame can be refused storage outright and
the refusal is a thrown `SecurityError`, not a null.

### `?chrome=`

Stamped once as `document.body.dataset.chrome`; the rules are CSS keyed off that
attribute.

| value    | effect                                                                            |
| -------- | ----------------------------------------------------------------------------------- |
| `full`   | Everything. What an unparameterised URL gets, and what an **unrecognised value** gets — a typo that blanks the UI would be worse than one that does nothing. |
| `notabs` | Hides the tab button list. The topbar (notify button, version badge), the `+` and the tab note all stay. |
| `none`   | Hides `.board-head` entirely — the view and nothing around it.                     |

This exists for a host that embeds the board and draws its own tab list — a VS Code
panel, say. It has to be asked for in the URL: the frame is cross-origin, so an
embedder can neither inject CSS nor reach the DOM, and chrome is a viewer's business
rather than the board's. `notabs` keeps the `+` on purpose, because it is the only
trigger for the new-tab dialog: hiding it either strands a human working inside the
embedder or forces every embedder to reimplement a dialog that belongs to the board.

It composes with the deep link (`?chrome=notabs#tab=bb71`) and survives the board's
own [self-reload](../how-to/run-in-vscode.md#when-the-page-reloads-itself), which
preserves query and fragment.

### What the shell posts to an embedder

When the page is framed, `activate()` tells the parent which tab is now on screen:

```js
{ __aboard: 'active', tab: 'bb13' }
```

Posted with `'*'` as the target origin, because an embedder's
`vscode-webview://<uuid>` origin is not knowable in advance and the tab id is already
in this page's own URL. **The receiver authenticates by comparing `event.source`, not
by origin.** An unframed page posts nothing.

It is sent whenever the active tab CHANGES — including the tab the board picks for
itself at load, and the ones `[`, `]` and `1`–`9` reach, which is the whole reason it
exists: an embedder that only ever *sends* navigation drifts out of sync the moment the
human uses a key, and a sidebar highlight that lies is worse than none.

A change, not a redraw. The shell re-activates the current tab at the end of every
repaint, and it repaints on every write that reaches it over `/events` — so an embedder
that acted on each message would be answering somebody else's agent write by moving the
human's selection. The board remembers the id it last announced and says nothing when
it has not moved.

**Nothing else travels this way.** The tab list, the document and the notices are read
from `/aboard.json` and `/events` like every other client; a second, weaker channel
for the same data is a bug factory.

### What an embedder may post to the shell

One message, in the other direction: a palette. A host that owns the window — a VS Code
panel deriving colours from the editor's own theme — hands the board its colours so it
belongs there instead of being a dark rectangle inside a light IDE.

```js
frame.contentWindow.postMessage({
  __aboard: 'theme',
  kind: 'light',                                   // optional: which variant to switch to
  tokens: { '--bg': '#fffdf7', '--text': '#1a1a1a' },
}, '*');
```

**Authenticated by `event.source === window.parent`**, the mirror of the rule the
`active` message above asks its receiver to apply, and the same rule an `html` tab's
bridge uses on messages from ITS parent. A message from any other window — a sibling
frame, an opener, a script in the console — is ignored.

`tokens` keys must be among the token names `aboard capabilities` reports under `theme`;
values must be a hex colour, a CSS colour keyword or a function call such as
`rgb(10 10 10 / 0.6)`. Anything else is dropped with a console warning rather than set,
because a custom property the board never reads is indistinguishable from the message
never arriving.

It is applied as inline custom properties on the root element — outranking both
variants, so a host need not know which one the viewer is in — and is written **nowhere**:
not the board document, not `localStorage`. See
[colour and themes](theme.md#a-theme-from-an-embedder).

### `GET /theme.json`

The project's house style, validated: unknown token names and unusable values are
already gone, and the `warnings` array carries the sentences that say so. `404` when the
project has no `.aboard/theme.json`, which the page reads as "use the built-in palettes"
— an empty object would have meant something different, since a theme file that
overrides nothing is a thing a project can legitimately have.

```json
{ "version": 1, "default": "light", "light": { "--accent": "#1f5f8b" } }
```

`ETag` over the served bytes with `Cache-Control: no-cache`, like the document: send the
tag back as `If-None-Match` and an unchanged theme answers `304`.

The shell does not normally fetch it. The server SPLICES the same object into
`aboard.html` before the page paints, because a fetch is asynchronous and would mean a
visible flash of the built-in palette on every load. This route is what the page re-reads
when the `{"theme": …}` frame says the file changed, and what any other client asks.

## `GET /aboard.json`

Returns the state file verbatim, `Content-Type: application/json`, with an
**`ETag`** over the exact bytes and `Cache-Control: no-cache`.

`no-cache` means *revalidate every time*, not *do not keep a copy*: send the tag
back as `If-None-Match` and an unchanged board answers **`304`** with no body.
The board shell fetches with `cache: 'no-cache'` for exactly this, so a reload of
a board nobody has written to costs a conditional request instead of the whole
document.

The tag is a hash of the bytes and **not** the `rev` counter, deliberately. `rev`
moves only on an accepted `POST`, and the state file is a file — a person editing
it, a `git checkout`, another tool — so a document can change without `rev`
changing, and a tag that missed that would answer `304` for a board that no
longer exists.

The server answers from memory, and re-reads only when the file's size or
modification time has moved. A write it made itself is published to that cache as
it lands, so a `GET` immediately after a `POST` is never stale. A re-read stats
the file on both sides of itself and only believes a stamp the two agree on: the
state file is replaced by rename, so a read that straddles a write returns the
old bytes under the new file's stat, and caching that pair would pin the old
document — ETag and all — until something else happened to move the file.

## `POST /aboard.json`

The whole document, with four control fields alongside the board's own:

| field       | meaning                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------ |
| `__base`    | The `rev` of the document this write was built from — a number, or its decimal string. Omit (or send `null`) only for an unconditional write; a `__base` that is present and is neither is `400`, because ignoring it would skip the check the caller asked for. |
| `__by`      | The actor. `"human"` from the browser; an agent name from the CLI. Absent means `"unknown"`, which gets agent-level powers only. |
| `__origin`  | An opaque client id, echoed on the SSE frame so a browser can ignore the notification for its own write. Defaults to `"browser"`. |
| `__label`   | Why this write is happening, in the caller's own words (`aboard apply --label`). Recorded on the [journal](#get-journal) entry and nowhere in the board; whitespace-collapsed and clamped to 200 characters. |

All four are stripped before the document is written.

**The reply.** `{"ok": true, "rev": N, "updatedAt": "…"}`, plus `checked` when the
write changed anything and `warnings` when it set something no renderer reads:

```json
{
  "ok": true,
  "rev": 44,
  "updatedAt": "2026-08-26T09:12:44.310Z",
  "checked": ["bb133"],
  "warnings": {
    "bb133": ["bb133 (ui): root is a stat, which does not read \"caption\" …"]
  }
}
```

`warnings` is keyed by tab id, and covers only the tabs **this write touched** —
never the whole board, or every pre-existing mistake would be re-reported as
though this write had made it. `checked` is every tab the checks ran over, warned
or not, and it is the half that lets a warning be taken back down: a clean tab is
simply absent from `warnings`, which is the same shape as a tab this write never
looked at, so without `checked` a banner could be raised and never lowered. The
same two fields ride the next SSE change frame, which is how a warning reaches a
browser that did not make the write. Neither is ever written into a tab's own
`state`. A warning does not refuse a write: a spec can legitimately lag its
renderer, and `aboard apply --strict` is the opt-in that does refuse.

**Compare-and-set.** The token is `rev`, a counter the server increments on every accepted
write (see [the state file](state-file.md#the-document)). If `__base` is present and does
not equal the live `rev`, the write is refused with `409`, the live revision, and a
sentence saying how far behind the caller is:

```json
{
  "error": "conflict",
  "live": "43",
  "base": "41",
  "reason": "your base is rev 41 and the board is at rev 43 — re-read the document, redo the edit, apply again"
}
```

Re-read, redo the edit, and post again.

The token used to be `updatedAt`, and a millisecond timestamp is not a token: two writes
inside one millisecond share a string, so a base built from the first still matched after
the second had landed — 4 collisions in 60 sequential writes, each an accepted write that
destroyed another. A **non-numeric `__base`** is read as one of those old timestamps and
is accepted only while the live document has no `rev` of its own — a board whose last
write predates the counter, and which gets one on that very write. After that it is
refused with a message saying `__base` must be the `rev`. The check is whole-document, so any concurrent
write conflicts with any other; that is coarse on purpose, and the browser handles its
own case by merging rather than discarding what the human just typed.

**Under concurrency.** Writes are serialised: the server holds one lock across the whole
read → compare-and-set → reconcile → write span, so posts that arrive together are
applied one at a time rather than interleaved. Of N simultaneous writes built on the
same base, exactly one gets `200` and the rest get `409` — the losers are refused, not
queued and applied on top, because each was built on a document that no longer exists by
the time its turn comes. A refused write reaches neither the state file nor the journal.
A write that omits `__base` is still serialised, but it is not compared against anything
— it is applied whole, and the last one in wins.

The lock is process-local: it orders the writers inside ONE server and nothing else.
That is why `apply` posts here rather than writing the file itself, which is what puts
an agent's write in the same queue as the browser's. It is also why a second server on
one project has to be prevented a level up, and the check asks about the **board** rather
than about a port: before binding anything, `serve` reads this project's instance record
for this name and asks the process it names over `/health` whether it is this project's
board of this name. If it is, `serve` refuses and prints its URL and pid — whatever port
was requested, `--port` and `PORT` included. A record that does not answer is stale and
is overwritten; the per-port probe stays as well, for a live board whose record was
deleted underneath it.

One board, one server, one lock — so `--name` is how you get a second board, and
`--port` only ever moves one. See
[why writes are serialised](../explanation/why-writes-are-serialised.md) and
[named boards](layout.md#named-boards).

**What the server stamps, whatever you sent:** `version` (this server writes its own
schema version by definition), `rev` (the previous revision plus one), `nextId`
(reconciled so it never regresses or falls behind an id in use), `updatedAt`, and
`lastEditedBy`.

**What the server enforces on the tab list:** the five guarantees — a dropped tab is
restored as a removal request, a `touched` marker cannot be cleared by an agent, chat
acknowledgements are carried forward, an actor may stamp only its own `seen` key, and the
human's `requests` survive an agent write untouched except for a `done` stamp added to one
that already exists.
See [why the guarantees are server-enforced](../explanation/why-the-guarantees-are-server-enforced.md).

Success is `200`, and carries the revision this write produced — which is the base for
the caller's next one:

```json
{ "ok": true, "rev": 44, "updatedAt": "2026-08-25T11:05:31.004Z" }
```

**What the parser refuses.** The document is parsed with
[`encoding/json/v2`](https://github.com/go-json-experiment/json), which is
stricter than the parser this server used to run, and three of the differences
are visible to a caller:

| written | v1 did | now |
| --- | --- | --- |
| the same object name twice — `{"nextId":1,"nextId":2}` | took the last one | `400`, naming the member |
| invalid UTF-8 in a string | replaced the bytes with `U+FFFD` | `400` |
| a field in the wrong case — `"ID"` for `"id"` | matched it anyway | the field is not matched |

The first is the one worth knowing about, because it used to be *silent*: a
generated document that set a key twice was accepted, one of the two values was
kept, and nothing said which. `aboard apply` parses stdin the same way and
refuses it in your own terminal before the write leaves the machine — which
matters, because `apply` re-encodes what it decodes, so a lenient parse there
would have collapsed the duplicate before the server could ever see it.

**Body limit: 32 MiB.** `MaxBytesReader` refuses a larger body before any parser
runs, so this is the size a board can grow to, not just the size of one request.
It was 8 MiB while a write cost a multiple of the whole document; it is not any
more. Uploads have their own, lower limit (12 MiB) because an image is not a
document.

Other statuses: `400` for a body that is not JSON, has no `tabs` array, exceeds the
body limit, or carries a `__base` that is not a revision; `403` for a cross-site write or a non-loopback `Host` (see [who is allowed to
ask](#who-is-allowed-to-ask)); `500` if the write itself failed. The write is atomic —
temp file in the same directory, then rename — so a reader never sees a half-written
document.

## `GET /events`

Server-sent events. Each frame is a JSON object in `data:`, distinguished by its key:

| frame                  | meaning                                                                       |
| ---------------------- | ------------------------------------------------------------------------------- |
| `{"ui": {…}}`          | The signature of the UI this server is serving. Sent **first, on connect**, so a page whose code no longer matches reloads itself. |
| `{"origin": "…"}`      | The state file changed; the value is the writer's client id (`null` if unknown), so a browser can ignore its own write. |
| `{"checked": […], "warnings": {…}}` | Carried on that same change frame: the tabs the write-time checks ran over, and what they said, keyed by tab. This is how a warning from an `apply` on somebody else's terminal reaches the human's screen — and how a banner comes down when the next write to that tab is clean. Both are omitted when there is nothing to say. |
| `{"waiters": N}`       | How many sessions are blocked on `/wait` right now — this is what enables the notify button. |
| `{"theme": "…"}`       | The project's `.aboard/theme.json` changed on disk; the value is a signature, not the file. The page re-reads `/theme.json` rather than trusting the frame — the same discipline every other ping here follows. |

The stream never closes. That matters for tooling: a headless browser will never reach
network-idle, so add `?nosse=1` to the page URL when scripting screenshots.

## `GET /health`

The instance record — `app`, `host`, `argv0`, `version`, `built`, `project`, `name`,
`port`, `url`, `base`, `state`, `pid`, `started`. Same shape as
`.aboard/run/instance.json`. `app` is `aboard` or `ape-aboard`, so a client can tell
whose port it just found.

Two fields are the ones an outside client actually needs:

- **`base`** is the URL prefix this board is served under — whatever
  `serve --base-path /prefix` was given, or **absent** (it is `omitempty`) when the
  board is at the server root, which is the common case. A client that builds its own
  request URLs has to prepend it; a client that only follows `url` already has it.
  This is what a VS Code extension reads before pointing an iframe anywhere.
- **`project`** is the absolute project root, which is how a client tells this
  project's board from an unrelated process squatting on a port it guessed at.

Note the ordering problem this creates for a prefixed board: `/health` is reachable
only *at* `<base>/health`, so a client that does not already know the prefix cannot
read it from here. The instance file is the way in — it carries the same `base`, and
it is found by walking up from the workspace folder.

## `GET /tab/<id>/html`

Serves one `html` tab as a standalone document: the widget's HTML, its data injected as
a global, and the `aboard.*` bridge. `<id>` may be a compound `<tab>/<block>` path, which
resolves to an `html` block inside a `stack` tab and serves that block's own html, data
and title — with a byte-identical CSP.

`?theme=dark|light` says which variant the frame should paint FIRST. Both variants are
spliced into the document — the built-in palette plus whatever `.aboard/theme.json`
overrides — so a theme switch afterwards is a `postMessage`, not a reload. An absent or
unrecognised value falls back to the board's own default, exactly as `?chrome=` does.

The parent posts `{__aboard: 'theme', kind, tokens}` into the frame on a switch and
whenever `.aboard/theme.json` changes: `kind` sets `data-theme` on the frame's root, and
`tokens` — that variant's project overrides — is applied as inline custom properties,
which outrank both spliced blocks. The second half exists because the spliced blocks are
a snapshot: an edit to theme.json cannot reach a document that is already open, and
reloading the frame to deliver it would throw away whatever the widget was holding.

Response headers: the sandbox CSP (`sandbox allow-scripts`, `connect-src 'none'`,
`frame-ancestors` listing `'self'` plus VS Code's webview origins),
`Cache-Control: no-store`, `X-Content-Type-Options: nosniff`. The `sandbox` directive
keeps the opaque origin when the document is fetched **standalone** rather than framed;
`connect-src 'none'` is the containment. A wrong path is answered with a message naming what
was wrong rather than a blank `404`. See
[why html tabs are sandboxed](../explanation/why-html-tabs-are-sandboxed.md).

## `GET /wait`

A long poll that blocks.

| query      | default | meaning                                                                                    |
| ---------- | ------- | -------------------------------------------------------------------------------------------- |
| `for`      | `poke`  | `poke` · `change` · `tab <id>` · `answer <id>` (that tab changed AND a human did it) · `node <id>=<status>` · `rendered <id>` (a browser mounted that tab and posted a receipt — released by `POST /rendered`, not by a write) · `request [<tab>]` (the human has a note waiting for an agent). |
| `timeout`  | server default | Whole seconds, capped by the server maximum.                                        |
| `by`       | `agent` | Who is waiting; shown on the human's notify button.                                        |
| `note`     | —       | Why, in up to 140 characters; shown beside the name.                                       |

Returns `200` with `{"event": "poke", …}` when released or `{"event": "timeout"}` when it
gave up. An unrecognised predicate is `400` up front, rather than blocking on something
that can never fire. A waiter is an open connection, so the count cannot go stale: if the
caller hangs up, the waiter disappears.

`request` is the one predicate that can be satisfied **before it is asked**, and it is
checked once at registration as well as on every write: a note the human left an hour ago
is a fact about the document rather than an event still to come, and blocking on it would
be asking them to write the same note twice. When one is already pending the request
returns at once and the waiter is dropped again before the notify button hears about it —
so the button never claims a session is listening when none is.

Either way it answers `{"event": "request"}`. Every other predicate reports `change`,
which is true for them because a write is the only thing that can satisfy one; a request
can be satisfied by a write *or* by a note that was already there, so the field a caller
branches on would otherwise depend on the human's timing.

## `POST /poke`

Body `{"by": "…", "note": "…"}` (both optional; `by` defaults to `"human"`). Releases
every waiter and answers `{"ok": true, "released": N, "at": "…", "by": "…"}`.

`released` is what the shell's notify button flashes back at the human ("notified 2
sessions", or "no session was waiting" for zero, which is reachable when the last
waiter times out between the repaint and the click). It is a transient notice beside
the button and not the button's own label, because the poke changes the waiter count
and the `waiters` frame that follows repaints the button a moment later.

## `GET /waiters`

`{"waiting": N, "waiters": [...], "lastPoke": {...}}`. The UI enables its notify button
from this.

## `GET /journal`

Recent accepted writes, oldest first; `?limit=N` bounds the count, keeping the most
recent N. Each entry carries the timestamp, the actor, the origin, and every tab the
write changed **as it was before** — which is what lets the `trace` renderer show a
change without keeping history in the board document.

The entries are **this board's**: each board in a project writes its own file
(`journal.jsonl`, `journal.<name>.jsonl`), because tab ids are allocated per board and a
shared record held two boards' `bb1` under one id.

```json
{
  "schema": 2,
  "at": "2026-08-26T09:12:44.310Z",
  "by": "agent-1",
  "origin": "apply",
  "rev": 41,
  "label": "rebuilding the gallery",
  "tabs": ["bb133"],
  "names": { "bb133": "UI gallery" },
  "warnings": { "bb133": ["bb133 (ui): root is a stat, which does not read …"] },
  "before": {
    "bb133": { "id": "bb133", "name": "UI gallery", "type": "ui",
               "note": "every component, rendered", "state": { "root": "…" } }
  }
}
```

### The two generations of an entry

`schema` says how to read `before`, and **a reader must dispatch per entry**:

| `schema` | `before[<id>]` is |
| --- | --- |
| absent, or `1` | a tab's bare `state` blob, and nothing about its name, type or note |
| `2` | the whole tab — `id`, `name`, `type`, `note`, `stateFrom`, `state`, and the markers |

Generation 1 is every entry written before the record widened, and it does not go
away with a migration: rotation keeps one older generation, so `journal.jsonl.1`
can hold generation-1 lines while the live file holds generation-2 ones, and this
endpoint concatenates them. A reader that decided per FILE would be wrong on
exactly the boards that have been running longest.

`before[<id>]` being **absent** means the tab did not exist at all — the write
created it. A tab that existed with no state is still recorded (as a tab with no
`state` key), because "did it exist" and "did it have content" are different
questions and `apply`'s merge reads the first one.

`rev` is the revision this write PRODUCED — the compare-and-set token the board
carried once it landed. It is what lets a reader ask "which tabs moved since the
revision I read", which `apply`'s 409 merge does and which `at`, a millisecond
clock, cannot answer. Absent (zero) on entries written before it landed, and a
reader must treat 0 as "unknown" rather than as revision zero.

`label` is the caller's `__label`; `warnings` is what the write-time checks said,
keyed by tab. Both describe the WRITE, which is why they live here and not in the
board document — a note about a write is not content, and one stored on a tab
would be copied forward by the next write as though it were still true. `before`
is dropped from the `/watch` stream (a watcher wants to know THAT something
changed, then re-read the board); `label` and `warnings` are not.

## `GET /history`

`?tab=<id>` is **required** — history is per tab, and a whole-board history is what
`/journal` already is. `?limit=N` bounds the list (default 20).

Answers `{"tab", "versions": [...], "scanned", "oldest", "truncated", "limited", "ends"}`.
Each version is `{"n", "at", "by", "origin", "rev", "name", "bytes", "state", "schema",
"was", "tab"}` — what the tab held **before** the write, newest first, numbered from 1 so
`1` is the undo. `at`/`by` describe the write that *replaced* that version, not the one
that produced it.

`schema` is which generation of the journal record the version came out of (see
`/journal` above), and it is per VERSION rather than per listing because a rotated
journal can hand one listing both. `tab` is the whole recorded tab, present only for
a generation-2 record, and it is what lets `aboard history --at N` put a NAME back
and not only a state. `was` is what the tab called itself at that version — distinct
from `name`, which is what it was called *after* the write that replaced it, since an
entry records a change and its `names` map holds the new name.

`ends` is a sentence saying where the record stops, and it is a field rather than a
derivation because the terminal prints the same one: the journal keeps one rotated
generation, so a tab's past is bounded and an empty list would otherwise be
indistinguishable from "everything about it rotated away".

The response also carries `board`: which board this history is OF, absent for the
default one. It is there to be printed — both the terminal listing and the shell's
change banner end with a `aboard history … | aboard apply` line, and that line without
`--name` would read and write the default board. The banner builds its flag from this
field, which is why it is on the wire rather than known only to the CLI.

The change banner in the shell reads this; `aboard history` reads it too, falling back to
`.aboard/run/journal.jsonl` when no board is running — `journal.<name>.jsonl` on a named
board, which owns its own journal.

## `GET /watch`

The same entries as JSON lines, streamed as they happen, until the client disconnects —
**minus `before`**, which is stripped from every streamed line. A watcher wants to know
THAT something changed and then re-read the board; the record of what each tab was is for
the file on disk, where `history` and `apply`'s merge read it. So the rule above — an
absent `before[<id>]` means the write created that tab — is about `/journal` and the file,
and says nothing on this stream, where `before` is absent for every entry. `schema` still
rides along, and still describes the entry rather than the file it will land in.

## `POST /rendered`

A **mount receipt**: what the browser drew for one tab, posted after every mount and,
debounced, after a control is pressed.

```json
{ "tab": "bb133", "type": "ui", "mount": true,
  "controls": ["fit"], "undeclared": [], "unknown": ["sparkline"], "fired": {"fit": 1} }
```

`controls` are the declared control ids on screen; `undeclared` are ones the renderer
built that no `views/<type>.spec.json` declares (`controlsFor` draws those as `?id`);
`unknown` are the markers a renderer put up because it did not recognise a component or a
prop. `fired` is a **delta** since the last post — the server accumulates it — and `mount`
distinguishes a mount from a press report, so "mounted 9×" does not come to mean
"somebody clicked eight times".

`tab` must be a plain id; anything else is `400`, because it becomes a key in a file a
terminal prints. Everything lands in `.aboard/run/rendered.json` — `rendered.<name>.json`
on a named board, since tab ids are allocated per board and one file would have held two
boards' `bb1` under one key. A **sidecar**, never the state document, for the same reason
selection and zoom are not in it — and
`aboard rendered` prints it. A receipt also releases a session waiting on
`aboard wait --for "rendered <id>"`.

It **reports**; it does not act, and it never writes a tab.

## `POST /log` · `GET /log`

`?tab=<id>` is required and must be a plain id — it becomes a filename, so anything else
is `400`.

POST appends the request body to `.aboard/run/logs/<tab>.log` — `logs/<name>/<tab>.log`
on a named board, which owns its own — adding a trailing newline
if the chunk lacks one, and rotates to `<tab>.log.1` when the file would exceed its size
cap. Answers `{"ok": true, "bytes": N}`; an empty body is a no-op success. A chunk over
the per-request cap is refused.

GET returns the tail: `{"lines": [...], "size": N}`, with `?tail=N` (bounded) choosing how
many. A log that does not exist yet is `200` with `"missing": true` — not an error, since
a `log` tab is usually created before anything has written to it.

Logs live in sidecar files **on purpose**: streaming command output through the state
document would rewrite the whole board on every line.

## `POST /upload` · `GET /uploads`

POST takes the raw image bytes with `?name=` as a hint, and answers `{"url": "…"}`. The
write path is deliberately narrow, because this is an unauthenticated server: a size cap
(12 MiB — a screenshot, not a video), an allow-list of PNG / JPEG / GIF / WebP checked
against the **bytes** rather than the claimed name, and a filename the **server** chooses
from a timestamp plus a slug of the hint. Nothing the caller sends is ever used as a
path. SVG is refused: it can carry script.

Uploads land in `.aboard/uploads/` and are served from disk in both embedded and `--dev`
modes — the embedded `assets/` directory is compiled in, so a file written there at
runtime would be invisible until the next build.

`GET /uploads` lists them (`{"files": [{"url", "bytes", "at"}]}`); `GET /uploads/<file>`
serves one. `aboard uploads` is the accounting view of the same directory: it adds the
tabs that mention each file and can prune the ones nothing does.

## See also

- [The state file](state-file.md) — the document these routes read and write.
- [The `.aboard/` layout](layout.md) — the paths behind `/log`, `/uploads` and the instance record.
- [The capability manifest](capabilities.md) — where this route table is declared.
- [Colour and themes](theme.md) — `/theme.json`, `?theme=`, and the theme message across the frame boundary.

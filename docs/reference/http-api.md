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
| POST   | `/aboard.json`      | Write, compare-and-set (`__base`, `__origin`, `__by`).                     |
| GET    | `/events`           | SSE: state changes, waiter count, and the UI signature.                   |
| GET    | `/health`           | Who owns this port, and which binary is serving.                          |
| GET    | `/capabilities`     | The capability manifest.                                                  |
| GET    | `/tab/<id>/html`    | One `html` tab as a standalone sandboxed document.                        |
| GET    | `/wait`             | Long poll: block until poked or until a predicate matches.                |
| POST   | `/poke`             | Release every waiting session.                                            |
| GET    | `/waiters`          | Who is waiting right now.                                                 |
| GET    | `/journal`          | Recent accepted writes, with the previous state of each changed tab.      |
| GET    | `/watch`            | Those writes as JSON lines, as they happen.                               |
| POST   | `/log`              | Append output to a tab's sidecar log.                                     |
| GET    | `/log`              | The tail of one.                                                          |
| POST   | `/upload`           | An image pasted or dropped by the human.                                  |
| GET    | `/uploads`          | List the uploads.                                                         |
| GET    | `/uploads/<file>`   | Serve one, from disk.                                                     |
| GET    | *anything else*     | Static asset from the embedded web tree (`ETag`, so a reload revalidates). |

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
   about what the tree does or does not contain.
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

The whole document, with three control fields alongside the board's own:

| field       | meaning                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------ |
| `__base`    | The `rev` of the document this write was built from — a number, or its decimal string. Omit (or send `null`) only for an unconditional write; a `__base` that is present and is neither is `400`, because ignoring it would skip the check the caller asked for. |
| `__by`      | The actor. `"human"` from the browser; an agent name from the CLI. Absent means `"unknown"`, which gets agent-level powers only. |
| `__origin`  | An opaque client id, echoed on the SSE frame so a browser can ignore the notification for its own write. Defaults to `"browser"`. |

All three are stripped before the document is written.

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
one project has to be prevented a level up: `serve` recognises this project's own board
before it binds and refuses to start beside it, whether the port was derived or given
explicitly with `--port`/`PORT`. (The explicit port used to skip that check, so two
servers on one state file was one flag away — and neither lock would have seen the
other.) See
[why writes are serialised](../explanation/why-writes-are-serialised.md).

**What the server stamps, whatever you sent:** `version` (this server writes its own
schema version by definition), `rev` (the previous revision plus one), `nextId`
(reconciled so it never regresses or falls behind an id in use), `updatedAt`, and
`lastEditedBy`.

**What the server enforces on the tab list:** the four guarantees — a dropped tab is
restored as a removal request, a `touched` marker cannot be cleared by an agent, chat
acknowledgements are carried forward, and an actor may stamp only its own `seen` key.
See [why four guarantees are server-enforced](../explanation/why-four-guarantees-are-server-enforced.md).

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
| `{"waiters": N}`       | How many sessions are blocked on `/wait` right now — this is what enables the notify button. |

The stream never closes. That matters for tooling: a headless browser will never reach
network-idle, so add `?nosse=1` to the page URL when scripting screenshots.

## `GET /health`

The instance record — `app`, `host`, `argv0`, `version`, `built`, `project`, `name`,
`port`, `url`, `base`, `state`, `pid`, `started`. Same shape as
`.aboard/run/instance.json`. `app` is `aboard` or `ape-aboard`, so a client can tell
whose port it just found.

## `GET /tab/<id>/html`

Serves one `html` tab as a standalone document: the widget's HTML, its data injected as
a global, and the `aboard.*` bridge. `<id>` may be a compound `<tab>/<block>` path, which
resolves to an `html` block inside a `stack` tab and serves that block's own html, data
and title — with a byte-identical CSP.

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
| `for`      | `poke`  | `poke` · `change` · `tab <id>` · `answer <id>` (that tab changed AND a human did it) · `node <id>=<status>`. |
| `timeout`  | server default | Whole seconds, capped by the server maximum.                                        |
| `by`       | `agent` | Who is waiting; shown on the human's notify button.                                        |
| `note`     | —       | Why, in up to 140 characters; shown beside the name.                                       |

Returns `200` with `{"event": "poke", …}` when released or `{"event": "timeout"}` when it
gave up. An unrecognised predicate is `400` up front, rather than blocking on something
that can never fire. A waiter is an open connection, so the count cannot go stale: if the
caller hangs up, the waiter disappears.

## `POST /poke`

Body `{"by": "…", "note": "…"}` (both optional; `by` defaults to `"human"`). Releases
every waiter and answers `{"ok": true, "released": N, "at": "…", "by": "…"}`.

## `GET /waiters`

`{"waiting": N, "waiters": [...], "lastPoke": {...}}`. The UI enables its notify button
from this.

## `GET /journal`

Recent accepted writes, newest first; `?limit=N` bounds the count. Each entry carries the
timestamp, the actor, the origin, and the tabs the write changed with their previous
state — which is what lets the `trace` renderer show a diff without keeping history in
the board document.

## `GET /watch`

The same entries as JSON lines, streamed as they happen, until the client disconnects.

## `POST /log` · `GET /log`

`?tab=<id>` is required and must be a plain id — it becomes a filename, so anything else
is `400`.

POST appends the request body to `.aboard/run/logs/<tab>.log`, adding a trailing newline
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

`GET /uploads` lists them; `GET /uploads/<file>` serves one.

## See also

- [The state file](state-file.md) — the document these routes read and write.
- [The `.aboard/` layout](layout.md) — the paths behind `/log`, `/uploads` and the instance record.
- [The capability manifest](capabilities.md) — where this route table is declared.

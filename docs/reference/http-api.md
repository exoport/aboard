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

Anything not matched, and any method not listed for a matched path, is `404`.

### Base path

With `serve --base-path /prefix`, the prefix is stripped before routing, so every route
above is reached at `/prefix/...`. A request for exactly `/prefix` is answered with a
`301` to `/prefix/` — without the trailing slash the shell's relative URLs would resolve
one level too high. A path that neither equals the prefix nor starts with `prefix/` is
`404`.

## `GET /aboard.json`

Returns the state file verbatim, `Content-Type: application/json`,
`Cache-Control: no-store`.

## `POST /aboard.json`

The whole document, with three control fields alongside the board's own:

| field       | meaning                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------ |
| `__base`    | The `updatedAt` this write was based on. Omit only for an unconditional write.                 |
| `__by`      | The actor. `"human"` from the browser; an agent name from the CLI. Absent means `"unknown"`, which gets agent-level powers only. |
| `__origin`  | An opaque client id, echoed on the SSE frame so a browser can ignore the notification for its own write. Defaults to `"browser"`. |

All three are stripped before the document is written.

**Compare-and-set.** If `__base` is present and does not equal the live `updatedAt`, the
write is refused with `409` and the live value:

```json
{ "error": "conflict", "live": "2026-08-25T11:04:02.117Z" }
```

Re-read, redo the edit, and post again. The check is whole-document, so any concurrent
write conflicts with any other; that is coarse on purpose, and the browser handles its
own case by merging rather than discarding what the human just typed.

**What the server stamps, whatever you sent:** `version` (this server writes its own
schema version by definition), `nextId` (reconciled so it never regresses or falls
behind an id in use), `updatedAt`, and `lastEditedBy`.

**What the server enforces on the tab list:** the four guarantees — a dropped tab is
restored as a removal request, a `touched` marker cannot be cleared by an agent, chat
acknowledgements are carried forward, and an actor may stamp only its own `seen` key.
See [why four guarantees are server-enforced](../explanation/why-four-guarantees-are-server-enforced.md).

Success is `200`:

```json
{ "ok": true, "updatedAt": "2026-08-25T11:05:31.004Z" }
```

Other statuses: `400` for a body that is not JSON, has no `tabs` array, or exceeds the
body limit; `500` if the write itself failed. The write is atomic —
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

Response headers: the sandbox CSP (`connect-src 'none'`, `frame-ancestors` listing
`'self'` plus VS Code's webview origins), `Cache-Control: no-store`,
`X-Content-Type-Options: nosniff`. A wrong path is answered with a message naming what
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

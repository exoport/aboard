# How to put aboard behind a reverse proxy

Something in front of the board is routing by path — nginx, Caddy, a dev server's proxy
table, another tool's webview — and you want the board at `http://localhost:8080/aboard/`
rather than on a port of its own.

```bash
aboard serve --base-path /aboard
```

That is the whole change on the aboard side. The rest of this page is the four things
that bite, and one of them decides whether the arrangement can work at all.

> **This is not a way to publish a board.** The server has no authentication and cannot
> have any: anything that reaches the port can read and rewrite the whole board. A proxy
> is for fitting the board into a routing scheme on a machine you already trust — and, as
> [trap 4](#trap-4--writes-only-work-from-a-loopback-origin) shows, the board will not
> accept browser writes through a proxy on a public hostname anyway.

## What the prefix does

`serve` prints the prefixed URL and records it:

```
aboard  ->  http://localhost:41237/aboard   (embedded UI, 0.1.0)
```

Every route moves under the prefix — `/aboard/aboard.json`, `/aboard/events`,
`/aboard/tab/ab72/html` — and the instance record and `GET /health` gain a `base` field
holding it:

```json
{ "url": "http://localhost:41237/aboard", "base": "/aboard", "...": "..." }
```

`base` is `omitempty`, so a board at the server root has no such field. A client that
builds its own request URLs must prepend it.

Note that `url` is the **board's own** URL, not the proxy's — the board has no way to
know what is in front of it. A client that blindly follows `url` will bypass your proxy,
which is fine on the same machine and useless anywhere else.

The prefix is **injected into the shell as one constant** —
`window.ABOARD_BASE = "/aboard";` — and every browser-to-server URL is built from it in
one module: the state fetch, the `POST`, the SSE stream, uploads, the journal, and an
`html` tab's iframe `src`. Because it is spliced into a JavaScript string literal, it is
also validated: one or more `/segments` of letters, digits, dot, underscore, tilde or
hyphen, and no segment of only dots. Anything else is a usage error before the server
binds:

```console
$ aboard serve --base-path "a b"
Error: base path "a b" is not usable: it must be one or more /segments of letters,
digits, dot, underscore, tilde or hyphen, and no segment may be `.` or `..` —
for example /aboard
```

A leading slash is optional: `--base-path aboard` and `--base-path /aboard` are the same
prefix.

## Trap 1 — the trailing slash is load-bearing

The stylesheet link and the ES module imports stay **relative** (`href="app.css"`),
because an `import` specifier cannot be built from a runtime constant. Relative
specifiers resolve against the document URL, so the document URL has to end in a slash.

The server handles this itself: a request for exactly `/aboard` is answered with a `301`
to `/aboard/`.

```console
$ curl -s -o /dev/null -w "%{http_code} -> %{redirect_url}\n" http://localhost:41237/aboard
301 -> http://localhost:41237/aboard/
```

**So the proxy must let that redirect through** rather than swallowing it or rewriting
the location back to a slashless form. If the board loads as unstyled HTML with console
errors about missing modules, this is why — check the address bar for the trailing slash.

Most proxies match `location /aboard/` and never see a request for bare `/aboard`, so
add your own redirect for it.

## Trap 2 — `/health` is only at `<base>/health`

The prefix is stripped before routing, so nothing answers at the server root:

```console
$ curl -s -o /dev/null -w "%{http_code}\n" http://localhost:41237/health
404
$ curl -s http://localhost:41237/aboard/health | head -c 40
{"app":"aboard","host":"aboard","argv0":
```

That is an ordering problem for any client that wants to discover the board: `/health`
reports the prefix, but you need the prefix to reach `/health`. **The instance file is
the way in** — `.aboard/run/instance.json`, found by walking up from the project
directory, carries the same `base` and the same `url`. Read the file first, then the
endpoint.

A path that neither equals the prefix nor starts with `<prefix>/` is a plain `404`.

## Trap 3 — the `Host` must be loopback

The board refuses any request whose `Host` header is not `localhost`, `127.0.0.1` or
`[::1]` (with or without a port). This is the DNS-rebinding guard: the bind is loopback,
but *any* name that resolves to `127.0.0.1` reaches it, and a page served from that name
would then be same-origin with the board — able to read the document, the journal and
`/health`, which discloses the absolute project path and the pid.

```console
$ curl -s -H "Host: example.com" http://localhost:41237/aboard/health
refused: this board answers only on localhost, 127.0.0.1 or [::1] — a hostname that
merely resolves to loopback is how a page on another site reads a local board
```

A `403` here is the check doing its job, not a misconfiguration of the board.

## Trap 4 — writes only work from a loopback origin

Every mutating request (anything but `GET`, `HEAD`, `OPTIONS`) is refused when
`Sec-Fetch-Site` says `cross-site`, or when an `Origin` header is present and is not
`http(s)://` **plus the `Host` the board received**:

```console
$ curl -s -X POST -H "Origin: https://evil.example" -d '{"tabs":[]}' \
    http://localhost:41237/aboard/aboard.json
refused: this write did not come from the board's own page (Origin: https://evil.example)
— the board has no authentication, so a cross-site write is refused rather than trusted
```

Put traps 3 and 4 together and one arrangement survives, which is the thing to design
around:

| the browser reaches the proxy at | the proxy sends upstream | reads | browser writes |
| --- | --- | --- | --- |
| `http://localhost:8080/aboard/` | the same `Host: localhost:8080` | ✅ | ✅ — `Origin` matches `Host`, and `localhost` is on the allow-list |
| `https://box.internal/aboard/`  | `Host: 127.0.0.1:41237`        | ✅ | ❌ — the browser's `Origin` is `https://box.internal`, which is not the board's `Host` |
| `https://box.internal/aboard/`  | the same `Host: box.internal`  | ❌ `403` | ❌ |

**So: proxy on a loopback name, and pass the client's `Host` straight through.** That is
the one configuration where both checks are satisfied at once, and it is the case the
feature is for — fitting the board into a local routing scheme, not exposing it.

A proxy on a public hostname can still *serve* the board read-only, if it rewrites the
`Host` to loopback. Nobody will be able to change anything from the browser.

Neither check is authentication. They stop a *browser* from being the thing that reaches
the board on somebody else's behalf; they do nothing about anything that can open a
socket. Non-browser clients — `curl`, `aboard apply` — send no `Origin` at all and are
unaffected either way.

### An nginx sketch

```nginx
# Reach this at http://localhost:8080/aboard/ — a loopback name, per trap 4.
location = /aboard  { return 301 /aboard/; }     # trap 1

location /aboard/ {
    proxy_pass http://127.0.0.1:41237;           # no URI: pass the path through untouched

    # Do NOT rewrite Host. The board needs it to be loopback (trap 3) AND to match
    # the Origin the browser will send (trap 4); `localhost:8080` satisfies both.
    proxy_set_header Host $http_host;

    # /events never closes.
    proxy_buffering    off;
    proxy_read_timeout 24h;
    proxy_http_version 1.1;
}
```

The behaviour above was verified against a minimal pass-through proxy rather than against
nginx: a `GET` of the document, the stylesheet and `aboard.json` through the proxy all
answered `200`, bare `/aboard` redirected to `/aboard/`, and a `POST` carrying the
proxy's own origin was accepted (`{"ok":true,"rev":N}`) with the client `Host` passed
through and refused with it rewritten. Treat the directive names as a sketch to adapt;
treat the three rules they encode as tested.

## What to check in the browser's network panel

Load the prefixed URL and look for these, in order. Each one fails in a way that looks
like a different problem:

| what to look at | good | what a failure looks like |
| --- | --- | --- |
| the **document** request | ends in `/` (after the `301` if you typed it without) | unstyled page, module import errors — trap 1 |
| `app.css` and `views/*.js` | `200`, requested at `<prefix>/app.css` | `404` at the server root — the document URL lost its slash |
| `aboard.json` | `200`, at `<prefix>/aboard.json` | `403` naming the allow-list — trap 3 |
| `events` | pending forever, `Content-Type: text/event-stream` | closes after a minute or so and the board stops updating — proxy buffering or a read timeout |
| any `POST aboard.json` (edit a tab note) | `200` with `{"ok": true, "rev": N}` | `403` naming the header that refused it — trap 4 |
| an `html` tab's iframe | `200` at `<prefix>/tab/<id>/html` | blank frame — see below |

`html` tabs keep the same sandbox under a prefix: `connect-src 'none'`, an opaque origin,
and a `frame-ancestors` list of `'self'` plus VS Code's webview origins. A proxy does not
change that list, so a board framed by some *other* host shows blank widget tabs until
that host's origin is added — see
[why html tabs are sandboxed](../explanation/why-html-tabs-are-sandboxed.md).

## See also

- [HTTP API](../reference/http-api.md#who-is-allowed-to-ask) — the two refusals, exactly, and the base-path routing rules.
- [How to run aboard inside VS Code](run-in-vscode.md) — the other reason to serve under a prefix.
- [How aboard runs](../explanation/how-aboard-runs.md) — where the port and the instance record come from.

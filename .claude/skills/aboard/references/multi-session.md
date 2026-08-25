# Two sessions, one project

The default is that sessions **share** a board: one server, one
`.aboard/aboard.json`, one browser tab, both sessions reading and writing. That is
usually what you want — the user sees one board regardless of which session drew
on it.

Three mechanisms keep that from turning into a mess.

## 1. One server per project, and it is not yours to restart

The port is derived from the discovered project root — the nearest ancestor
directory holding `.aboard/` — so a checkout always gets the same port and two
checkouts never collide. Whichever session starts the server, the other finds it.

```sh
aboard status          # running? where? whose?
aboard serve           # run the server for this project
```

**Run `aboard status` before you ever run `aboard serve`.** If it reports a board
already running, that is success, not an obstacle — use the URL it prints. `serve`
enforces this rather than trusting you to: a second `aboard serve` for the same
project exits non-zero with `this project's board is already running at <url>
(pid N)` instead of taking the port.

```
board running at http://localhost:46624 (pid 577548)
```

Killing it and starting your own takes the first session's server out from under
it, and its browser tab with it. Restart only after changing Go code or the
embedded web tree, and prefer saying so, since it briefly drops the other
session's browser connection (the page reconnects on its own — `retry: 1000` on
the SSE stream — but it is still a visible blip).

While iterating on the web tree, `aboard serve --dev` serves it from disk so no
rebuild is needed at all.

## 2. Never write the state file directly

This is the one that actually loses work. Direct `Edit`/`Write` has no
compare-and-set. Two sessions reading the same snapshot and both writing means the
second silently erases the first — measured, not hypothetical:

```
session A node present: false
session B node present: true
-> A was silently lost: true
```

Through the server, the same race is refused instead:

```
A applies  -> applied
B applies  -> refused: the board changed since you read it — re-read, redo, apply again
B retries  -> applied; both changes present
```

So always:

```sh
aboard apply --by "agent-1" < /tmp/next-aboard.json
```

A `409` is not an error to route around. It means a real change landed while you
were thinking. Re-read `.aboard/aboard.json`, redo your edit on the fresh copy,
apply again.

## 3. Say who you are

`--by` lands in `lastEditedBy` and on every tab you touched. Use `agent-1`,
`agent-2`, or `agent-<role>` — never `claude`, which reads as one participant
when several may be writing:

```sh
aboard apply --by "agent-1" < /tmp/next-aboard.json
```

Why it matters: it is what the browser's change banner shows ("agent-1 changed
this tab") and the only way the other session (or the user, later) can tell which
of two agents moved something. `lastEditedBy: "human"` means there are user edits
you have not read yet.

`--by human` is refused from the CLI, and a write that names no actor at all is
recorded as `unknown` with agent-level powers rather than being trusted as the
human. The human writes from the browser; nothing an agent runs should be able to
sign their name.

## Discovery

`.aboard/run/instance.json` is the source of truth for "what is running here":

```json
{ "app": "aboard", "project": "/home/you/proj", "port": 46624,
  "url": "http://localhost:46624", "state": ".aboard/aboard.json", "pid": 577548 }
```

Read it rather than assuming a port. `GET /health` returns the same record, which
is how the binary tells its own board apart from an unrelated process squatting on
the port. A record whose `pid` is dead is stale.

`app` also says which binary is serving: `aboard` for the standalone binary, and a
host-qualified name when the board is mounted inside another CLI. Either is a real
board; both answer the same routes.

Named boards get their own record: `.aboard/run/instance.<name>.json`.

## When sessions should NOT share

A side investigation that must not disturb the main board:

```sh
aboard serve --name review
```

That gives a different derived port, `.aboard/aboard.review.json` as its state
file, and its own instance record. The two boards cannot interfere. Every other
command takes the same `--name` (or reads `ABOARD_NAME` from the environment), so
a session working on the named board exports it once instead of repeating it.
Tell the user which URL belongs to which, since they now have two tabs to keep
straight.

Use this sparingly. A shared board is the feature; two boards is a fallback for
when the work genuinely does not overlap.

## Etiquette summary

- Read before you write, always. The board may have moved.
- Report what the user changed before acting on it, so they know you read it.
- Do not restart a healthy server.
- Do not clear someone else's marks, notes, or nodes unless asked. Additive edits
  are cheap; destructive ones need a reason.
- If you replace `nodes` or `form.fields` wholesale, say so — you may be
  discarding another session's work or the user's answers.
- `aboard journal --limit 20` and a `trace` tab are how you tell who did what.
  `lastEditedBy` only ever names the last one.

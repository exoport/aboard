# Two sessions, one project

The default is that sessions **share** a board: one server, one `aboard.json`, one
browser tab, both sessions reading and writing. That is usually what you want —
the user sees one board regardless of which session drew on it.

Three mechanisms keep that from turning into a mess.

## 1. One server per project, and it is not yours to restart

The port is derived from the project's absolute path, so a checkout always gets
the same port and two checkouts never collide. Whichever session starts the
server, the other finds it.

```sh
aboard status          # running? where? whose?
./restart.sh             # starts it, or prints the URL of the one already running
./restart.sh -force      # actually restart — only when you mean it
```

`./restart.sh` on a healthy board **deliberately does nothing but print the URL**.
That is the guard: without it, a second session's restart would kill the first
session's server out from under it. If you see

```
board already running at http://localhost:46624 (pid 577548)
```

that is success, not an obstacle. Use that URL.

Reach for `-force` only after changing Go code or embedded assets, and prefer
saying so, since it briefly drops the other session's browser connection (the
page reconnects on its own — `retry: 1000` on the SSE stream — but it is still a
visible blip).

## 2. Never write aboard.json directly

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
were thinking. Re-read `aboard.json`, redo your edit on the fresh copy, apply
again.

## 3. Say who you are

`--by` lands in `lastEditedBy` and on every tab you touched. Use `agent-1`,
`agent-2`, or `agent-<role>` — never `claude`, which reads as one participant
when several may be writing:

```sh
aboard apply --by "agent-1" < /tmp/next-aboard.json
```

Why it matters: it is what the browser's change banner shows ("agent-1 changed
this tab") and the only way the other session (or the user, later) can tell which of two
agents moved something. `lastEditedBy: "human"` means there are user edits you
have not read yet.

## Discovery

`.aboard/instance.json` is the source of truth for "what is running here":

```json
{ "app": "aboard", "project": "/home/you/proj", "port": 46624,
  "url": "http://localhost:46624", "state": "aboard.json", "pid": 577548 }
```

Read it rather than assuming a port. `GET /health` returns the same record, which
is how the binary tells its own board apart from an unrelated process squatting on
the port. A record whose `pid` is dead is stale — `./restart.sh` clears it.

Named boards get their own record: `.aboard/instance.<name>.json`.

## When sessions should NOT share

A side investigation that must not disturb the main board:

```sh
./restart.sh -name review
```

That gives a different derived port, `aboard.review.json` as its state file, and
its own instance record. The two boards cannot interfere. Tell the user which URL
belongs to which, since they now have two tabs to keep straight.

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

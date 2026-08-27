# How aboard runs

Most of what aboard does is invisible while it works, and every question people ask about
it — *why is my URL different today?*, *why did my write get refused?*, *which of these
files can I delete?* — is really a question about one of the moving parts below.

This page is the mental model. It is deliberately not a list of fields: the facts live in
[the `.aboard/` layout](../reference/layout.md), [the state file](../reference/state-file.md)
and [the HTTP API](../reference/http-api.md), and this page links to them rather than
copying them, because a copied fact is a fact that will disagree.

> **Why this is an explanation page and not reference.** On the
> [Diátaxis compass](https://diataxis.fr/compass/): it serves *cognition* rather than
> action, and *study* rather than work — you read it once to build a model, not while
> your hands are on the keyboard. A reader who already has the model and needs a specific
> field goes to reference instead. The three pages named above are the reference; this
> one is the thing that makes them navigable.

## One binary, nothing beside it

Everything is compiled in: the shell, the stylesheet, every renderer, the mermaid bundle,
the built-in recipes and the example board. There is no Node, no `node_modules`, no asset
directory to ship alongside, and nothing fetched from the network at runtime. That is why
`aboard capabilities` can tell you what a board can do **in a directory that has never
held one** — the answer travels inside the binary rather than beside it.

It also means an installed binary is a complete answer to "what version of the UI is
this?". There is no way for the served interface and the serving binary to disagree,
because there is only one file. (`serve --dev` is the deliberate exception, and exists so
that people working on aboard itself do not have to rebuild to see a stylesheet change.)

## `init` makes a place; `serve` opens a door

`aboard init` writes `.aboard/` at the directory you are standing in — the one command
that does **not** walk up, because there is nothing to find yet and climbing would mean
`init` in a subdirectory quietly doing nothing while reporting success. It does look up
far enough to refuse: run it inside a project that already has a board and it names the
root it found rather than making a second, invisible one beneath it.

Every other command walks up from `--cwd` (or the working directory) looking for the
nearest ancestor that *contains* a `.aboard/`. If it finds none it fails, naming what it
looked for. It never falls back to the working directory: acting on a board in whatever
directory you happened to be in is exactly how a project ends up with two of them.

`aboard capabilities` is the deliberate exception, because it describes the **binary**
rather than a board and must answer where no board has ever existed.

That single resolved root is the thing everything else hangs off — the state file, the
uploads, the journal, and the port.

## The port comes from the path

```
port = 41000 + ( first 4 bytes of sha256(root + "\0" + name), big-endian ) % 8000
```

Two properties follow, and both matter more than they sound.

**The URL is the same every run.** A docked editor tab and a bookmark stay valid across
restarts, reboots and weeks away — and stay valid from any subdirectory, because the hash
is over the *discovered root* rather than the working directory. (The spike hashed the
working directory, so `cd views && board -status` reported a different port than the board
it was looking at.)

**Two checkouts never collide.** Different roots, different ports, no coordination and no
registry.

The range, 41000–48999, sits above the crowded 3000–9000 development band and below the
ephemeral range the kernel hands out for outbound connections — so it collides with
neither your own dev servers nor the OS.

### What if something else already has the port

Two different answers, because the two cases are genuinely different.

**A stranger on the derived port** is somebody else's business. The server steps forward
one port at a time, up to 24 tries, and says so:

```
port 46731 busy, trying 46732
aboard  ->  http://localhost:46732   (embedded UI, 0.1.0)
```

Your URL has moved for as long as that stranger is there, which is the one case where
"the URL is stable" does not hold. **Read it from `aboard status` or from
`.aboard/run/instance.json`; never assume a port.** Every aboard command already does.

**This project's own board on the port** is refused, not walked past:

```
Error: this project's board is already running at http://localhost:44195 (pid 123843)
```

That is deliberate: a second session must not be able to yank the server out from under
the first, and two servers on one state file would be two write locks that cannot see
each other.

An **explicit** `--port` that is busy with something else is a plain error rather than a
walk. You asked for that port; quietly using a different one would be worse than failing.

**The check is about the board, not about the port.** Before it binds anything, `serve`
reads this project's instance record for this name and asks the process it names over
`/health` whether it is this project's board of this name. If it is, the refusal above
happens whatever port was requested — `--port` and `PORT` included. It has to work that
way round, because a board's port is not a fact: a stranger on the derived port moves it,
`--port` moves it, so "is this project already serving?" cannot be asked of any one port.

Two cases are deliberately not refusals. A record that does **not** answer is stale — the
commonest one being a board that was killed — so `serve` proceeds and overwrites it;
refusing because of the corpse of the last board would be the worst possible reading of
the record. And the per-port probe stays in the walk beside the record check, for the
opposite case: a live board whose record was deleted underneath it is invisible to the
record and still very much listening.

If you want two boards in one project, use `--name`, which gives each one its own
document, its own record, its own derived port and its own journal, receipts and logs.
`--port` is for moving one board, not for having two.

## The instance record is how everything finds everything

On start, `serve` writes `.aboard/run/instance.json`: the port, the URL, the pid, the
state file, the project root, the board name, the serving binary's identity and version,
and the URL prefix if one was given. `GET /health` returns the same record, so one board
can identify another over the wire.

This is the discovery authority, and it is why nothing in aboard — or in any client
written against it — has a hardcoded port. It is also how a client tells *this* project's
board from an unrelated process squatting on a port it guessed at: the record carries the
absolute project root, and a client compares it.

## Two ways in, one way through

A human writes through the **browser**. An agent writes through **`aboard apply`**, which
reads a whole document on stdin and `POST`s it to the running server rather than touching
the file. The distinction is not cosmetic: five things — deleting a tab, clearing a change
marker, un-acking a chat message, clearing another actor's read state, writing the human's
own `requests` — are refused from an agent, which is why `--by human` is refused from the
CLI. See
[why the guarantees are server-enforced](why-the-guarantees-are-server-enforced.md). That is the important part: `apply` posting means an agent's write goes into the
same queue as the browser's, ordered by the same lock. A direct `Edit` of the state file
has no compare-and-set at all, so a concurrent change from the browser or another session
is destroyed with no error.

The token that orders them is **`rev`**, a counter the server increments on every accepted
write. You read a document, you change it, you post it back with the `rev` you read; if
the live board has moved on, the write is refused with a `409` naming how far behind you
are, and you re-read, redo and apply again. `apply` will do that merge for you **once** —
re-reading, asking the journal which tabs moved, and re-applying its own tabs where the
server did not touch them — and still refuses a genuine same-tab collision by name rather
than silently picking a winner.

`rev` replaced a millisecond timestamp, and the reason is worth carrying: two writes
inside one millisecond share a string, so a base built from the first still matched after
the second had landed — measured at 4 collisions in 60 sequential writes, each an accepted
write that destroyed another. The argument in full is
[why writes are serialised](why-writes-are-serialised.md).

A write is also **checked** on the way in. The server runs the manifest's own declarations
over the tabs the write touched and reports anything no renderer reads — an unknown
component, an unknown prop, a colour name this board does not have. It **warns and does
not refuse**, because a declaration can legitimately lag its renderer, and the warnings
ride the reply, the journal entry, the event stream and the tab's own banner. So a mistake
an agent made on its terminal reaches the human's screen, and a mistake the human would
otherwise find first reaches the agent that is still holding the context to fix it.

## What a write costs

Compare-and-set is whole-document, so it is fair to ask what a whole document costs. The
answer is deliberately **the edit, not the board**, and it was measured rather than
asserted — twice, because the first pass changed the algorithm and the second changed the
codec.

The harness is `pkg/aboard/bench_test.go`, and rerunning it is one command:

```bash
go test -run xxx -bench . -benchmem ./pkg/aboard/
```

It synthesises N tabs with **three 1 MiB html states at every size** — a constant, not a
fraction of N. That is what makes the rows comparable: 15 tabs is 3.00 MiB and 500 tabs is
3.05 MiB, so those two differ by 485 unchanged tabs and almost no bytes, and "does a POST
scale with the tabs it did not touch" becomes a question the table can answer. 5 000 tabs
is 3.54 MiB.

| when | tabs | POST one small tab | GET | watcher tick |
| --- | --- | --- | --- | --- |
| before | 15 | 197 ms · 83 MB · 2 686 allocs | 0.64 ms | 2.14 ms |
| before | 500 | 210 ms · 88 MB · 74 962 allocs | 0.66 ms | 2.18 ms |
| before | 5 000 | 279 ms · 140 MB · 745 603 allocs | 0.83 ms | 2.55 ms |
| before | 10 MiB board | — | — | 7.71 ms |
| after the structural fixes | 15 | 61.8 ms · 40 MB · 478 allocs | 1.43 µs | 0.50 µs |
| after the structural fixes | 500 | 63.7 ms · 42 MB · 2 784 allocs | 1.39 µs | 0.50 µs |
| after the structural fixes | 5 000 | 78.5 ms · 50 MB · 25 121 allocs | 0.83 µs | 0.50 µs |
| after the structural fixes | 10 MiB board | — | — | 0.52 µs |
| after the codec | 15 | **17.2 ms** · 31 MB · 374 allocs | 1.41 µs | 0.50 µs |
| after the codec | 500 | **17.3 ms** · 32 MB · 1 918 allocs | 1.44 µs | 0.50 µs |
| after the codec | 5 000 | **28.5 ms** · 44 MB · 16 487 allocs | 1.36 µs | 0.50 µs |
| after the codec | 10 MiB board | — | — | 0.52 µs |

Measured on an Intel Core Ultra 5 125U at Go 1.26.6, best of two runs. Absolute
milliseconds are a fact about that machine; the shape below is not.

| | marginal cost of one UNCHANGED tab | 15 → 500 tabs |
| --- | --- | --- |
| before | 15.8 µs | 197 ms → 210 ms (+6.6 %) |
| after the structural fixes | 3.6 µs | 61.8 ms → 63.7 ms (+3.1 %) |
| after the codec | 2.5 µs | 17.2 ms → 17.3 ms (+0.6 %) |

A POST now scales with the **bytes it has to read, parse and write back**, which is
irreducible, and no longer with the tabs it left alone. What is left of the per-tab cost is
the struct decode and encode of the tab list itself.

The `GET` figure is the server's own work per request — a stat and a write to a discarding
sink. It no longer includes reading the file; copying bytes to a real socket is unchanged
and is not what moved. The watcher figure is the one worth reading twice: the tick used to
read and SHA-256 the whole file five times a second unconditionally, so a 10 MiB board cost
about 50 MB/s of sustained I/O at idle. It is now one `stat`, and hashes only when the size
or the modification time has moved — while the signature stays a **content** hash, so a
save that rewrites identical bytes still wakes nobody.

### The codecs that were rejected

The codec that shipped is the Go team's own published mirror of `encoding/json/v2`, which
Go 1.27 makes the default `encoding/json` — so the adoption question closes itself when
the toolchain moves. The *other* question does not close, because "let us swap in a faster
third-party JSON library" is a proposal somebody can make on any Tuesday. The survey was
run once, on 2026-08-25, and its verdicts are kept here so it is not run again:

| library | why not |
| --- | --- |
| **json-iterator/go** | Archived 2025-12-15. |
| **minio/simdjson-go** | No release since 2023; AVX2 amd64 only, with no fallback. |
| **goccy/go-json** | Pre-1.0, heavy `unsafe`, open memory-safety and panic issues, no declared Go-version policy — for 1.3–1.8× over v2 on struct unmarshal. |
| **bytedance/sonic** | JIT plus `linkname`, amd64/arm64 only with a **silent** stdlib fallback elsewhere, and marshal slower than v2. A tool whose containment story is "no network, no auth, few dependencies" is the wrong home for a JIT. |
| **segmentio/encoding** | Alive, and it edges v2 on raw-value unmarshal — but not worth a dependency against a codec that is about to be the standard library. |
| **tidwall/gjson + sjson**, **buger/jsonparser** | Healthy, and irrelevant: they matter only for patching one path of a document in place, which is the per-tab write below, which is not being built. |

The measurement that decides this is in the table above rather than in a library
comparison: a POST already costs the bytes it must read, parse and write back, and a
faster parser makes each step faster without changing how many steps there are.

### Per-tab writes are not going to be built

The obvious next step — `GET`/`POST /tab/<id>` with per-tab compare-and-set, so the browser
and agents move one tab instead of the document — is a **closed question, not a backlog
item**. It would put a second write path through the one choke point off which every tab
guarantee, every journal `before` record and every wait predicate hangs, which is the most
expensive place on the server to be subtly wrong.

The trigger that would reopen it is written down so nobody has to argue about it: **a real
board, not a synthetic one, where a single-tab write measured by the harness above exceeds
about 200 ms, or where the document exceeds `maxBodyBytes`.** The worst single-tab write
measured so far is 28.5 ms on a 3.54 MiB board of 5 000 tabs, against a body ceiling of
32 MiB. Until somebody records a measurement past one of those two numbers, this stays
closed for the same reason [the diff renderer](why-no-diff-renderer.md) is.

## The file is watched, so nothing has to be told

The server keeps the document parsed in memory and re-reads only when the file's size or
modification time has moved — a stat every 200 ms rather than a hash of the whole
document, so a large board costs one syscall per tick at idle. A change it did not make
itself — you edited the file, `git checkout` moved it, another tool wrote it — is picked
up and pushed to every open page over `GET /events`.

That stream is also how an open page notices that its own **code** changed: the first
frame on connect is a signature of the UI the server is serving, so after a rebuild the
stream drops, the browser reconnects on its own, the signature does not match what the
page loaded, and the page reloads itself.

The stream never closes, which is a fact about tooling as much as about the board: a
headless browser never reaches network-idle, so scripted screenshots need `?nosse=1`.

## The journal is the undo

Every accepted write appends a line to `.aboard/run/journal.jsonl` recording when, who,
which tabs changed, why (if the caller passed `--label`), what the checks said — and **each
changed tab as it was before the write**. `aboard journal` prints the log, `aboard watch`
streams this board's writes as they land, the `trace` renderer draws them, and
`aboard history <tab>` reads one tab's past out of the file: `--at N` prints a whole
document with that version put back, ready to pipe into `apply`.

It is a whole document rather than the one tab on purpose. A single-tab document is a
document that *deletes every other tab*, which the server would answer with a removal
request on each one in front of the human.

Two limits are structural rather than accidental. The record keeps **one rotated
generation**, so a tab's past is bounded and the listing says where it ends — an empty
list would otherwise be indistinguishable from "everything about this tab rotated away".
And a restore never puts back `touched`, `pendingRemoval` or `seen`: re-raising a dot the
human dismissed, or re-opening a removal request they answered, is not an undo.

The journal is **local**. It records who changed what on this machine, and it is not a
project audit trail; do not cite it as one.

## Content, and machine-local runtime

The split inside `.aboard/` is the one thing worth knowing about the tree:

- **Content** — `aboard.json`, `uploads/`, `recipes/`. A `markup` tab references an upload by name and would break without it.
- **Machine-local runtime** — everything under `run/`: the instance record, the journal, per-tab sidecar logs, mount receipts, screenshots. True for this machine and this moment only.

There is a second axis, and it only shows up once a project has two boards: a **named
board owns everything it writes for itself** — its document, its instance record, its
journal, its receipts and its logs are all qualified by the name. `uploads/` and
`recipes/` are the two the project keeps, and neither is keyed by tab id, which is what
makes sharing them safe: tab ids are allocated per board, so every record that *is* keyed
by one had to be split or it would hold two boards' `bb1` under one key.

`run/` is nested inside `.aboard/` rather than sitting beside it so that a project ignores
**one** path and loses nothing it wanted to keep. And the whole directory *is* ignored: a
board is local, per-developer and unversioned, for reasons argued out in
[why a local, non-authoritative channel](why-a-local-non-authoritative-channel.md).

Two things deliberately stay out of the board document. **Streaming log output** goes to a
sidecar file, because the document is rewritten whole on every write and an appending log
inside a tab's state would mean rewriting the entire board once per line. **Per-viewer
state** — selection, zoom, collapsed blocks, which chrome a host asked for — lives in the
URL, because two viewers can look at one board in the same second and must disagree about
all of it while agreeing about content.

## Two identities, one board

The same cobra tree ships twice: as the standalone `aboard`, and mounted inside another
CLI as `ape aboard <command>`. They share **one `.aboard/` per project** — the same state
file, the same derived port, the same instance record — and differ by exactly one string,
the `app` field in `/health` and the instance record.

The difference exists so an error message can name the command you actually have; nothing
in discovery cares, and the capability manifest's app name is neither of them, which is
what keeps `capsHash` identical under both hosts. The full argument is
[why two identities](why-two-identities.md).

## What the server will not do

Two things are worth stating as properties of how it runs, not as features.

**It has no authentication, and it binds loopback only.** Anything that can reach the port
can read and rewrite the whole board. Two checks stop a *browser* from being that thing on
somebody else's behalf — a `Host` allow-list, and a same-origin rule on every mutating
request — and neither is authentication. That is also why `html` tabs get no network
egress and why nothing in the browser executes anything: a `gate` verdict, a `ui` button
and an action strip all **record** an intent, and the agent that asked acts on it.

**Nothing in the UI starts an agent session.** The board may ask, and a session may choose
to listen by blocking on `aboard wait`; the human's notify button releases a session that
had *already* decided to wait. A board with nobody waiting is simply not listening, and it
says so rather than looking live. The reasoning, and the two times the opposite was
proposed and refused, is in
[why nothing in the UI starts a session](why-nothing-in-the-ui-starts-a-session.md).

## See also

- [The `.aboard/` layout](../reference/layout.md) — every path, root discovery and port derivation as reference.
- [The state file](../reference/state-file.md) — the document `apply` and the browser both write.
- [HTTP API](../reference/http-api.md) — every route, with the compare-and-set contract in full.
- [Your first board](../tutorials/first-board.md) — the same loop, walked once with your hands on it.

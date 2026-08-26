# How to run a second board in one project

You have a board going and something else comes up — a side investigation, a review of
one file, a second agent session on an unrelated question — and you do not want it
scrolling past the work already on the board.

A **named board** is a second, independent board in the same project. It has its own
document and its own URL, and the two documents never touch each other. What it does
*not* have is its own journal, its own uploads or its own logs, and that is the part
worth reading before you start.

## Do you need one at all?

Most of the time you do not. A tab is the cheap unit here: opening one is a write, and
closing it is a removal request the human answers. Reach for a **tab** when the side
question shares the same audience and the same afternoon as everything else on the
board — a diagram, a form, a pick-one.

Reach for a **second board** when one of these is true:

- **A different reader.** The main board is a shared queue and this is your own scratch space, or the other way round.
- **A different lifetime.** The main board is long-running and this is a two-hour investigation you will delete whole.
- **A different session that must not collide.** Two agents writing different tabs on one board is fine — compare-and-set orders them — but two agents whose *whole* working set is separate get less noise from each other on separate boards.

If you are reaching for a second board because the first one is cluttered, close some
tabs instead. A board with tabs nobody is using is the thing to fix.

## Make one

```bash
aboard init --name review
aboard serve --name review
```

`init --name` writes a second document beside the first:

```
.aboard/
  aboard.json              the default board
  aboard.review.json       the named one
  run/
    instance.json          the default board's record
    instance.review.json   the named one's
```

`serve --name` starts it on its own port and prints its own URL, with the name shown
beside it:

```
aboard  ->  http://localhost:48033  [review]   (embedded UI, 0.1.0)
state  ->  /home/you/work/your-project/.aboard/aboard.review.json
project->  /home/you/work/your-project
```

Both servers can run at once. They are two processes on two ports serving two files.

## Talk to it

`--name` is a root flag, so **every** command takes it:

```bash
aboard status --name review
aboard apply  --name review --by agent-2 < next.json
aboard export --name review bb7
```

If a whole session is working on the named board, set the environment variable once
instead of repeating the flag:

```bash
export ABOARD_NAME=review
aboard status          # the review board
```

## What a name changes, and what it does not

Exactly three things get their own copy: **the document**, **the instance record**, and
**the port** (derived from the project root *plus* the name, so the URL is as stable as
the default board's). The rest of `.aboard/` is per **project**, and both boards share
it:

| shared by both boards | consequence |
| --------------------- | ----------- |
| `run/journal.jsonl`   | `aboard journal` and `aboard history` show the other board's writes too |
| `run/logs/<tab>.log`  | a `log` tab on either board writes into the same directory, keyed by tab id |
| `run/rendered.json`   | one mount-receipt file, keyed by tab id |
| `uploads/`            | one image store for the project |
| `recipes/`            | one recipe directory |

Three consequences follow, and none of them is obvious from the outside.

### Tab ids are allocated per board, so an id can mean two things

Each document has its own `nextId`, so a fresh named board starts at `bb1` exactly as
the default one did. Write to both and the shared journal holds two entries that name
the same id and mean different tabs:

```
2026-08-26T13:51:27.510Z  agent-1          bb1 (Plan)
                          docs probe: default board
2026-08-26T13:51:27.546Z  agent-2          bb1 (Side note)
                          docs probe: review board
```

The `names` on the entry are the only thing separating them. `aboard journal` prints
them, which is why the tab name is worth reading and the id alone is not enough. `rev`
does not help either: it counts per board, so both of those entries are `rev 1`.

**`aboard watch` is the exception, and it is the useful one.** `journal` and `history`
read the shared file on disk; `watch` is a live stream from one running server, so it
carries only that board's writes. If you are following one board while another is busy,
watch it — that is the view that does not mix them.

### `aboard history` finds a tab id, not a board

`history` reads the same project-wide journal, so on a named board it can hand you the
*other* board's version of an id both boards happen to use — and `history <id> --at N`
turns that into a document you could apply. Before restoring, check the name in the
listing is the tab you meant:

```bash
aboard history bb1 --name review      # read the listing first
```

```
bb1 — 1 recorded version, newest first
   1  2026-08-26T13:52:04.583Z  replaced by agent-1              24 bytes (Plan)

restore one with:  aboard history bb1 --at 1 | aboard apply --by agent-1
```

That listing is the review board's, and the version it is offering you is **Plan** — the
*default* board's tab. Two things to notice before you act on it: the name in the
listing is the only thing that would have told you, and **the printed `restore one
with:` line does not carry `--name`**, so copying it verbatim reads and writes the
default board.

If the two boards have overlapping ids and you need an undo, the safe move is to read
the version out and re-apply the tab by hand rather than piping `--at` straight into
`apply`.

### `aboard uploads --prune` sees one board's tabs

The reference scan reads the tabs of **the board you asked about**, and the uploads
directory belongs to the project. So an image used only by a tab on the review board is
"unreferenced" as far as the default board is concerned:

```bash
aboard uploads                  # default board: "no tab mentions it"
aboard uploads --name review    # review board:  "bb1"
```

`--prune` on its own prints and refuses, which is the guard that matters here. **Check
both boards before `--prune --yes`**, because deletion is irreversible and `.aboard/` is
gitignored, so there is no copy to go back to.

## Find them again

Per project, `aboard status` answers for one board at a time (`--name` selects which).
The machine-wide question is `aboard boards`, which prints one row per (project, name):

```bash
aboard boards
```

```
2 boards (662 processes inspected)

  /home/you/work/your-project  [default]
    http://localhost:44195           pid 123843   15 tabs
    aboard 0.1.0, up since 2026-08-26T13:51:18Z
    last write by agent-1 at 2026-08-26T13:51:27.510Z

  /home/you/work/your-project  [review]
    http://localhost:48033           pid 123844   1 tab
    aboard 0.1.0, up since 2026-08-26T13:51:18Z
    last write by agent-2 at 2026-08-26T13:51:55.585Z

(this is the process table, so a board that is not running does not appear here —
 run `aboard status` inside each project you want the answer for)
```

The header's count is part of the answer, not decoration: a board owned by another user
is a process this one cannot read, and when it meets any it adds a line saying how many
rather than quietly handing you a shorter list.

It reads the process table rather than a registry, so it needs no project of its own and
nothing has to be cleaned up when a board dies. **`/proc` is Linux only**: on macOS and
Windows the command exists, exits `2`, and says so — the per-project answer there is
`aboard status` inside each project.

## Get rid of one

Stop the server, then delete its document:

```bash
rm .aboard/aboard.review.json
```

The instance record goes on its own: a server removes `run/instance.<name>.json` as it
shuts down, and only if the record is still its own — so a restart that already replaced
it is not undone by the old process exiting afterwards. If a board was killed hard the
file may be left behind; `aboard status --name <name>` verifies a record over `/health`
before believing it, so a stale one is reported rather than trusted.

Nothing else knows about the board. Its journal entries stay in the shared log until
rotation takes them, and any uploads only it referenced are now genuinely unreferenced —
which is the one moment `aboard uploads --prune` is doing what you want.

## See also

- [The `.aboard/` layout](../reference/layout.md#named-boards) — the same split, stated as reference, plus port derivation.
- [Why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md) — why a board is cheap enough to throw away.
- [How aboard runs](../explanation/how-aboard-runs.md) — the moving parts a second board duplicates and the ones it shares.

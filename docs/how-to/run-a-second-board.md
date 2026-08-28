# How to run a second board in one project

You have a board going and something else comes up — a side investigation, a review of
one file, a second agent session on an unrelated question — and you do not want it
scrolling past the work already on the board.

A **named board** is a second, independent board in the same project. It has its own
document, its own URL, its own journal, its own mount receipts and its own sidecar
logs. Two things it shares with every other board in the project, deliberately:
`uploads/` and `recipes/`.

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

Its runtime files appear as it is used — `run/journal.review.jsonl`,
`run/rendered.review.json`, `run/logs/review/` — each beside the default board's and
never inside it.

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
aboard export --name review ab7
```

If a whole session is working on the named board, set the environment variable once
instead of repeating the flag:

```bash
export ABOARD_NAME=review
aboard status          # the review board
```

## What a name changes, and what it does not

Everything a board writes for **itself** gets its own copy — the document, the instance
record, the port (derived from the project root *plus* the name, so the URL is as stable
as the default board's), the journal, the mount receipts and the sidecar logs:

| the default board                   | the board named `review`     |
| ----------------------------------- | ---------------------------- |
| `aboard.json`                       | `aboard.review.json`         |
| `run/instance.json`                 | `run/instance.review.json`   |
| `run/journal.jsonl`                 | `run/journal.review.jsonl`   |
| `run/rendered.json`                 | `run/rendered.review.json`   |
| `run/logs/<tab>.log`                | `run/logs/review/<tab>.log`  |

The reason all five are per board is one fact: **tab ids are allocated per board.** Each
document has its own `nextId`, so a fresh named board starts at `ab1` exactly as the
default one did, and every record above is keyed by tab id. One shared file meant two
different tabs under one key — a journal that needed the tab's *name* read to say which
board an entry came from, and an `aboard history ab1` that could offer you the other
board's version of that id as a document to restore.

Two things stay shared, and both are shared on purpose:

| shared by every board in the project | why |
| ------------------------------------ | --- |
| `uploads/`                           | an image is content a human pasted, and either board may put it on a tab |
| `recipes/`                           | a recipe is a document about the project, not about one of its boards |

Neither is keyed by tab id, which is what makes sharing them safe. The one consequence
worth knowing is the uploads accounting, below.

### Journal entries written before the split stay where they are

There is no migration, and none is possible: an entry already in `journal.jsonl` does
not record which document the write went to, and guessing from the tab id is exactly the
ambiguity being removed. Old entries stay readable and count as the **default** board's
history from now on. If you had two boards before this landed, a `bb<n>` in that file
may belong to either — read the `by` and the tab name, as you had to then.

### `aboard uploads` reads every board in the project

`uploads/` is shared, so its accounting has to be: the scan reads the default document
and every `aboard.<name>.json`, and prints a named board's tab id qualified.

```bash
aboard uploads          # --name does not narrow it; the directory is the project's
```

```
  only-on-review.png                                 1 B  review:ab1
* orphan.png                                         1 B  no tab mentions it

2 files, 2 B in /home/you/work/your-project/.aboard/uploads
references scanned in 2 board documents: aboard.json, aboard.review.json  (a tab id is prefixed with its board name)
```

That is what stops `--prune --yes` from deleting a picture the other board is showing.
`--prune` on its own still prints and refuses, because deletion is irreversible and
`.aboard/` is gitignored, so there is no copy to go back to.

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

Stop the server, then delete its document and its runtime files:

```bash
rm .aboard/aboard.review.json
rm -f .aboard/run/journal.review.jsonl* .aboard/run/rendered.review.json
rm -rf .aboard/run/logs/review
```

The instance record goes on its own: a server removes `run/instance.<name>.json` as it
shuts down, and only if the record is still its own — so a restart that already replaced
it is not undone by the old process exiting afterwards. If a board was killed hard the
file may be left behind; `aboard status --name <name>` verifies a record over `/health`
before believing it, so a stale one is reported rather than trusted.

Nothing else knows about the board. Everything above is under `.aboard/`, which the
project ignores whole, so leaving it costs nothing but disk. Any uploads only that board
referenced are now genuinely unreferenced — which is the one moment
`aboard uploads --prune` is doing what you want, and the reason to delete the document
**first**: the accounting reads every board's document, so an image is only an orphan
once the board that named it is gone.

## See also

- [The `.aboard/` layout](../reference/layout.md#named-boards) — the same split, stated as reference, plus port derivation.
- [Why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md) — why a board is cheap enough to throw away.
- [How aboard runs](../explanation/how-aboard-runs.md) — the moving parts a second board duplicates and the two it shares.

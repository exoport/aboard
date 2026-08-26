# The `.aboard/` layout

Everything a board owns lives under one directory at the project root. This page
describes that tree, how the root is found, and how the port is derived from it.

Paths are joined in exactly one place in the source (`pkg/aboard/layout.go`) and nowhere
else, which is what makes this page a description of the code rather than a parallel
opinion about it.

## The tree

```
<project root>/
  .aboard/
    aboard.json            the board document — the thing a human curates
    aboard.<name>.json     a second, named board in the same project
    uploads/               images the human pasted or dropped
    recipes/               this checkout's own recipes (project scope)
    run/                   machine-local runtime; true for this machine, now
      instance.json        port, pid, url, state file — the discovery record
      instance.<name>.json the same, for a named board
      journal.jsonl        the append-only log of accepted writes (+ .1 rotation)
      rendered.json        mount receipts: what a browser reported it drew, per tab
      logs/<tab>.log       sidecar output for a `log` tab
      shots/               screenshots from test/shot.sh
```

The split is between **content** and **machine-local runtime**. `aboard.json`,
`uploads/` and `recipes/` are content: a `markup` tab references an upload by name and
would break without it. Everything under `run/` is true only for this machine and this
moment — including `rendered.json`, which is per-VIEWER as well: it says what a browser
drew, which is the same class of fact as selection, zoom and a chat draft, and is
therefore exactly as unwelcome in the board document.

`run/` is nested inside `.aboard/` rather than sitting beside it so that a project
ignores **one** path and loses nothing it wanted to keep:

```gitignore
.aboard/
```

`aboard init --gitignore` adds that line for you. The reasoning behind ignoring it at
all is [why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md).

## Finding the project root

The root is the nearest ancestor directory that **contains** a `.aboard/` directory.
Discovery walks up from the starting directory and stops at the filesystem's fixed
point:

- the starting directory is `--cwd DIR` if given, otherwise the process's working directory;
- the walk climbs until it finds `.aboard/`, or until `filepath.Dir` stops changing (a volume root, on every platform);
- if nothing is found, commands that act on a board **fail** with a message naming what was looked for. They do not fall back to the working directory: writing a board into whatever directory you happened to be in is exactly how a project ends up with two of them.

A pre-rename `.board/` directory — from the `board` spike this tool grew out of — is
**not** recognised, deliberately: nothing under it is read, migrated or reported, and a
project holding one looks to `aboard` exactly like a project with no board at all. There
is no automatic upgrade because there is nothing worth upgrading. A board is a local,
non-authoritative channel whose conclusions were supposed to have been promoted already;
a migration path would be code carried forever so that one directory of somebody's
finished afternoon could survive. Run `aboard init` and start a board.

The one deliberate exception is the commands that describe the **binary** rather than a
board. `aboard capabilities` must answer in a directory that has never held a board —
that is the property that lets an agent ask what a copied binary can do before deciding
to use it — so a failed walk falls back to the starting directory instead of refusing.

This mirrors ape's own config discovery on purpose: a developer with both tools in one
tree should not have to hold two different rules in their head.

## Port derivation

Each project gets its own port, derived from the **discovered root**:

```
port = 41000 + ( first 4 bytes of sha256(root + "\0" + name), big-endian ) % 8000
```

so the range is 41000–48999 — above the crowded 3000–9000 development band and below
the ephemeral range the kernel hands out for outbound connections.

Two properties follow, and both matter more than they sound:

- **Two checkouts never collide.** Different roots, different ports, no coordination.
- **The URL is the same every run**, so a docked editor tab and a bookmark stay valid — and stay valid from any subdirectory, because the hash is over the discovered root rather than the working directory.

If the derived port is taken by something that is not a board, the server probes forward
(up to 24 ports) and records where it actually landed. If it is taken by **this
project's own** board, it refuses to start a duplicate and points at the running one.

Overrides, in precedence order: `--port N`, then the `PORT` environment variable, then
the derived port. An explicit port that is busy is a plain error rather than a walk —
you asked for that port.

The duplicate refusal is anchored to the **port**, not to the project, so an explicit
port that is *free* has no occupant to recognise: it starts a second server on the same
state file, and the newcomer overwrites `run/instance.json` with its own details.
`--port` moves one board; it is not the way to have two. Use
[a named board](#named-boards) for that, and see
[the compare-and-set contract](http-api.md#post-aboardjson) for what two servers on
one file costs.

## Named boards

A second board in the same project — a side investigation that must not disturb the main
one — is selected by name:

```bash
aboard serve --name review
```

A name changes three things and nothing else: the state file becomes
`.aboard/aboard.<name>.json`, the instance record becomes
`run/instance.<name>.json`, and the port is derived from root **plus** name. The two
boards' **documents** never interfere.

`ABOARD_NAME` sets the name for a session that is working on one, so it does not have to
be repeated on every command.

### What a named board does NOT get its own copy of

"three things and nothing else" is exact, and the rest of `.aboard/` is per **project**.
Two boards in one project share every one of these:

| path                          | shared by both boards |
| ----------------------------- | --------------------- |
| `run/journal.jsonl`           | one write log for the project |
| `run/logs/<tab>.log`          | one sidecar log directory |
| `run/rendered.json`           | one mount-receipt file |
| `uploads/`                    | one image store |
| `recipes/`                    | one recipe directory |

The journal is the one that will surprise you, so say the consequence out loud:
**`aboard journal` and `aboard history` in a named board show the other board's entries
too**, and tab ids are allocated **per board**, so a `bb12` in the journal may belong to
either one. A row is not enough to tell them apart — read the `by` and the timestamp, or
watch the board you mean with `aboard watch` while you work.

The journal being project-wide is deliberate: it answers "who changed what in this
project", and a second board in the same project is part of the same conversation. The
other four are shared because they were never qualified by name in the first place;
whether they should be is the human's call and is not settled here.

## Finding boards in other projects

Everything above is per project, and so is `aboard status`. The machine-wide question —
what is running anywhere — is `aboard boards`:

```bash
aboard boards                          # every running board, grouped by project
aboard boards --output-format json     # the same, as a document
```

It asks the **process table**, not a registry: it walks `/proc` for an `aboard serve` or
an `ape aboard serve`, resolves each one's project root, and then does per project
exactly what `status` does — read the instance record, verify it over `/health`. One row
per (project, name), which is why the two boards of the section above are two rows.

There is no registry file anywhere, and that is the design rather than an omission: a
process either exists or it does not, so nothing has to be written on startup and
nothing has to be cleaned up on a crash.

`/proc` is Linux only. On macOS and Windows the command exists, exits **2**, and says so
in one line — the per-project answer is `aboard status` inside each project.

## The instance record

`run/instance.json` is the discovery authority. `GET /health` returns the same record,
so one board can identify another over the wire.

| field                          | what it is                                                                     |
| ------------------------------ | -------------------------------------------------------------------------------- |
| `app`                          | Which binary is serving: `aboard`, or `ape-aboard` when hosted by ape.         |
| `host`, `argv0`                | The same identity, plus the command the user actually typed.                   |
| `version`, `built`             | The build identity of the serving binary.                                      |
| `project`                      | The absolute project root.                                                     |
| `name`                         | The board name, absent for the default board.                                  |
| `port`, `url`, `base`          | Where it is listening, and the URL prefix if `--base-path` was given.          |
| `state`                        | The state file being served.                                                   |
| `pid`, `started`               | Which process, since when.                                                     |

Never assume a port: read it from `aboard status` or from this file.

## Development paths

Two paths exist only inside aboard's own checkout, and their absence elsewhere is
meaningful rather than an error:

- `pkg/aboard/web/` — the web tree `serve --dev` serves from disk instead of the embedded copy. `--dev-dir` overrides it.
- `.claude/skills/aboard/references/reference.generated.md` — the generated half of the committed skill. `aboard capabilities --check` treats a **missing** reference as "nothing to check": a project that never copied the skill has nothing to be out of date.

## See also

- [The state file](state-file.md) — what is inside `aboard.json`.
- [HTTP API](http-api.md) — the routes that read and write these paths.
- [How aboard runs](../explanation/how-aboard-runs.md) — the same parts as a mental model rather than a tree.
- [How to run a second board in one project](../how-to/run-a-second-board.md) — the named-board table above, with its consequences worked through.
- [How to run aboard inside VS Code](../how-to/run-in-vscode.md) — using the stable URL.

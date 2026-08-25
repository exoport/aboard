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
      logs/<tab>.log       sidecar output for a `log` tab
      shots/               screenshots from test/shot.sh
```

The split is between **content** and **machine-local runtime**. `aboard.json`,
`uploads/` and `recipes/` are content: a `markup` tab references an upload by name and
would break without it. Everything under `run/` is true only for this machine and this
moment.

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
the derived port.

## Named boards

A second board in the same project — a side investigation that must not disturb the main
one — is selected by name:

```bash
aboard serve --name review
```

A name changes three things and nothing else: the state file becomes
`.aboard/aboard.<name>.json`, the instance record becomes
`run/instance.<name>.json`, and the port is derived from root **plus** name. The two
boards never interfere.

`ABOARD_NAME` sets the name for a session that is working on one, so it does not have to
be repeated on every command.

The journal is deliberately **not** qualified by name. It answers "who changed what in
this project", and a second board in the same project is part of the same conversation.

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
- [How to run aboard inside VS Code](../how-to/run-in-vscode.md) — using the stable URL.

# aboard CLI reference

> Generated from the command tree by `make docs-cli` (which runs the hidden
> `aboard gen-docs`). Do not edit by hand — change the command definitions in
> `pkg/aboard/cli/` and regenerate.

## aboard

A shared visual board for a human and one or more agent sessions

```
aboard [flags]
```

aboard serves a browser UI for a project and keeps its state in a file both
sides read and write. Tabs are DATA, not code: an agent opens one for whatever it
needs to show — a graph, a chart, a question form, an annotated screenshot, a
channel to another session, a bespoke widget — and reads back what the human
changed.

State lives under .aboard/ at the project root, which is found by walking up from
--cwd. Each project gets its own port, derived from that root, so the URL is the
same every time and two checkouts never collide.

Start with:

  aboard serve            run the server for this project
  aboard status           what is running here, and on which port
  aboard capabilities     what this board can do (no server needed)

Subcommands:

- `apply` — Write a board document from stdin, through the running board
- `boards` — List every board running on this machine, from the process table (Linux only)
- `capabilities` — Print what this board can do: types, state fields, controls, endpoints, commands
- `export` — Print one tab as text, for pasting into the project's own documents
- `history` — List what a tab said before, from the journal
- `init` — Create .aboard/ in this directory and write an empty board
- `journal` — Print recent accepted writes: when, who, which tabs
- `log` — Read stdin and append it to a tab's sidecar log, line by line
- `poke` — Release every session waiting on this board
- `recipes` — List the recipes available here, or print one
- `rendered` — Print what the browser reported it drew
- `serve` — Run the board server for this project
- `status` — Report this project's running board, if any, and the caps beacon
- `uploads` — List the files under .aboard/uploads/ and the tabs that mention them
- `version` — Print the build identity of this binary
- `wait` — Block until the board is poked, or until a predicate matches
- `watch` — Follow every change as JSON lines until interrupted

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard apply

Write a board document from stdin, through the running board

```
aboard apply [flags]
```

Read a whole board document on stdin and POST it to the running board, which
applies it under compare-and-set.

Never edit the state file directly while a board is running: a direct write has
no compare-and-set, so a concurrent change from the browser or another session is
destroyed with no error. A 409 here means somebody got there first — re-read,
redo the edit, apply again.

Warnings print on stderr before the write: a schema version the board does not
write, state no renderer reads, an unknown ui component or prop, a {bind} that
resolves nowhere, a colour name this board no longer has. They warn rather than
refuse, because a spec can lag its renderer — but read them, because "applied"
is not evidence that anything rendered. The same warnings are recorded on the
journal entry and shown to the human on the tab, so a write that warns is no
longer something only the terminal knows about.

--check runs those checks and stops: nothing is posted, no board need be
running, and the document needs no `rev`. --strict turns any warning into a
refusal (exit 1, nothing written) — the guard for a loop that must stop rather
than ship a wrong tab. Together they are "tell me and exit non-zero".

--label records WHY this write is happening on the journal entry, where the
journal, watch and trace commands show it. It is navigation inside a local,
rotating file — never a record to cite anywhere permanent.

The compare-and-set base is the `rev` inside the document you submit — the one
you read. A document with no `rev` is refused (exit 2) rather than written
unconditionally; --force writes it anyway and says so on stderr.

Examples:

```
  aboard apply --by agent-1 < next.json
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--by` | string | `agent-1` | actor recorded in lastEditedBy and on every tab this write touched |
| `--check` | bool | `false` | run the write warnings and stop: nothing is posted, and no board need be running |
| `--force` | bool | `false` | write without compare-and-set, overwriting anything since you read the document |
| `--label` | string | `—` | why this write is happening; recorded on the journal entry, not in the board |
| `--strict` | bool | `false` | refuse the write if anything warns (exit 1, nothing written) |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard boards

List every board running on this machine, from the process table (Linux only)

```
aboard boards [flags]
```

Every running board on this machine, whichever project it belongs to.

This is the cross-project half of `aboard status`. It needs no project of its
own — it works from a directory that has never held a board — because it asks
the PROCESS TABLE rather than a registry: it walks /proc for an `aboard serve`
or an `ape aboard serve`, resolves each one's project root, and then does exactly
what status does for one project — read the instance record, verify it over
/health, and report what the board says about itself.

One row per (project, name), because two boards in one project are two boards.
The project path is printed in full: the point of a machine-wide listing is that
you are not standing in the project it names.

Nothing is trusted that a live board did not just confirm. A record whose process
has gone is listed as "recorded but not answering" rather than dropped — a stale
record is information — and the count of processes inspected is printed with the
result, because "no board found" after 3 processes and after 400 mean different
things.

/proc is Linux only. Everywhere else this command exits 2 and says so, and the
per-project answer is `aboard status` inside each project.

Examples:

```
  aboard boards
  aboard boards --output-format json
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard capabilities

Print what this board can do: types, state fields, controls, endpoints, commands

```
aboard capabilities [type] [flags]
```

Ask the BINARY what it can do, rather than reconstructing it from a document.

Every renderer declares its own surface in views/<type>.spec.json beside the code
it describes, and this aggregates those with the declared command table and the
route list. It needs no running server and no project: a fresh checkout, a copied
binary, or another session holding the port all still answer.

  aboard capabilities            the whole manifest, as JSON
  aboard capabilities kanban     one type — cheap, for a mid-task lookup
  aboard capabilities --format md    the markdown reference the skill commits
  aboard capabilities --check    exit 1 if that committed reference is stale

--check treats a MISSING reference as "nothing to check": a project that never
copied the skill has nothing to be out of date.

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--check` | bool | `false` | exit 1 if the committed skill reference is stale |
| `--format` | string | `json` | json, md, or js (the generated control module) |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard export

Print one tab as text, for pasting into the project's own documents

```
aboard export <tab|key> [flags]
```

Turn a tab into markdown (or CSV, where it has rows) so its conclusions can be
promoted into a spec, an ADR, or whatever this project's own documents are.

Reads the board document from disk, so it works with no server running — for the
same reason `capabilities` does: an agent should never have to start a server to
read out a conclusion.

The strategy is not to promote early. It is to make LATE promotion cheap, and
retyping was the cost that made it expensive.

Examples:

```
  aboard export bb128
  aboard export table-example --format csv
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--format` | string | `md` | md or csv |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard history

List what a tab said before, from the journal

```
aboard history <tab> [flags]
```

Per-tab history, read out of the journal the board already keeps.

Every accepted write records each changed tab AS IT WAS, which is the unit
somebody undoing a bad write actually wants. This lists those versions newest
first, naming who replaced each one — and says plainly where the record ends,
because rotation keeps one older generation and a listing that just stopped would
read as "this tab has only ever been written twice".

  aboard history bb133                          what it said, and when
  aboard history bb133 --at 1 | aboard apply --by agent-1     put version 1 back

--at prints a WHOLE document with that one tab put back, not the tab on its own:
a single-tab document is a document that deletes every other tab, and the server
would answer it with a removal request on each one. It carries the board's
current `rev`, so a restore built while somebody else was writing is refused
rather than clobbering.

What a restore carries depends on which generation of the record it came from,
and the listing marks each version with it. An entry stamped `schema: 2` holds
the whole tab, so the name, type, note, stateFrom and key come back with the state; an
older entry holds a bare state, and then only the state moves. Neither ever puts
back `touched`, `pendingRemoval` or `seen` — re-raising a dot the human
dismissed, or re-opening a removal request they answered, is not an undo.

Reads from the running board when there is one and from
.aboard/run/journal.jsonl when there is not.

Examples:

```
  aboard history bb133
  aboard history bb133 --at 1 | aboard apply --by agent-1
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--at` | int | `0` | print the document that restores version N instead of listing (1 is the most recent) |
| `--limit` | int | `20` | how many versions to list |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard init

Create .aboard/ in this directory and write an empty board

```
aboard init [flags]
```

Create a board for the project you are standing in: `.aboard/` with an empty
document, an uploads directory, a recipes directory and the run directory.

This is the ONE command that does not walk up. Every other command finds the
project root by climbing from --cwd, because a board belongs to a project rather
than to whichever subdirectory you happened to be in — but there is nothing to
find yet, and climbing would mean `aboard init` in a subdirectory quietly doing
nothing while reporting success. So it creates a root where you stand, and
refuses when that would make a second one, naming the root it found.

It never overwrites an existing board document. --name opens a SECOND board in
the same project, with its own state file and its own port.

--example seeds the board compiled into this binary: fifteen tabs, one per
renderer, each noted with what it demonstrates. It is a worked example, not your
work — delete what you do not want.

--gitignore adds `.aboard/` to the project's .gitignore. A board is a
LOCAL, persistent, non-authoritative channel: several developers on one repo each
get their own, and a committed one is a whole-file JSON conflict on every merge
over a conversation that was never theirs.

Examples:

```
  aboard init
  aboard init --example --gitignore
  aboard init --name review
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--example` | bool | `false` | seed the board with the example tabs compiled into this binary |
| `--gitignore` | bool | `false` | append .aboard/ to the project's .gitignore if it is not already ignored |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard journal

Print recent accepted writes: when, who, which tabs

```
aboard journal [flags]
```

Per-write history of this board: the time, the actor, and the tabs that changed.

Reads from the running board when there is one and from .aboard/run/journal.jsonl
when there is not, so this works in a project whose board is stopped — it is the
third command of the resume protocol, and a session that has just cleared its
context has no reason to start a server before asking what happened.

With two sessions and a human writing one document, "who changed the plan while I
was thinking?" otherwise has no answer except git archaeology over a file that
moves constantly. Every accepted write funnels through one function, so this
cannot be bypassed by an agent that forgot to record something.

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--limit` | int | `40` | how many entries to print |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard log

Read stdin and append it to a tab's sidecar log, line by line

```
aboard log <tab>
```

Pipe a long-running command's output onto the board, so the human can watch it
happen rather than waiting for it to finish.

The stream lives in a sidecar file under .aboard/run/logs/, NOT inside the board
document: that document is rewritten whole on every write, so an appending log
inside a tab's state would mean rewriting the entire board once per line. The
tab's state holds only a pointer.

Lines are echoed to stdout as well — piping output to the board should not mean
losing it from the terminal you are watching.

Examples:

```
  go test ./... 2>&1 | aboard log bb126
```

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard poke

Release every session waiting on this board

```
aboard poke [flags]
```

Do what the human's notify button does: release every session currently blocked
on `aboard wait`, and tell them who released them and why.

Nothing here starts an agent. A session is released only if it had already
decided to listen; a board with nobody waiting is simply not listening, and this
command says so rather than pretending otherwise.

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--by` | string | `agent-1` | who is releasing them |
| `--note` | string | `—` | a message for the waiting sessions |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard recipes

List the recipes available here, or print one

```
aboard recipes
```

A recipe is a worked method for one shape of board work — put a structure in
front of the human, ask for a decision, annotate a screenshot, coordinate with
another session. The bodies live in files rather than in the skill, so the skill
stays small and can never disagree with the recipe it describes.

Four tiers, first wins by name:

  _apex/aboard/recipes/   the wider workspace's house style
  _aboard/recipes/        committed, shared with the team
  .aboard/recipes/        this checkout only, gitignored with the rest
  built-in                compiled into this binary

Shadowing is allowed and always reported: a project that overrides a built-in
recipe is doing something deliberate, and the row says what it replaced. A file
that does not parse is not skipped either — it is listed as invalid with the
reason, because a recipe the author is looking at and the tool pretends does not
exist is the worst of the three outcomes.

Subcommands:

- `list` — List every recipe available in this project, and where each came from
- `show` — Print one recipe's body, or just its tab skeleton

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard recipes list

List every recipe available in this project, and where each came from

```
aboard recipes list [flags]
```

Every recipe this project can use, one row each: the name, the tier it came
from, and what it is for.

This is the only complete answer. The skill's generated index lists what is
compiled into the binary and cannot know what a project added, so an agent that
reads only that index is incomplete.

Rows carry what is wrong with them rather than being dropped: a shadowed file is
named under the recipe that won, a file that does not parse is marked INVALID
with the reason, and a recipe needing a newer board schema says so.

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard recipes show

Print one recipe's body, or just its tab skeleton

```
aboard recipes show <name> [flags]
```

Print the recipe an agent should follow: a title line naming it and saying what
it is for, then the body. The frontmatter is stripped — it is metadata for the
list, and YAML at the top of something meant to be read as prose is noise.

--template prints ONLY the JSON tab skeleton the recipe carries, so it pipes
straight into an edit and then into `aboard apply`. A recipe with no skeleton
exits 1 saying so, rather than printing an empty document that would be applied
as an empty tab.

Examples:

```
  aboard recipes show apply-a-write
  aboard recipes show my-recipe --template | jq .
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--template` | bool | `false` | print only the recipe's JSON tab skeleton |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard rendered

Print what the browser reported it drew

```
aboard rendered [tab] [flags]
```

What a real browser actually put on screen for a tab, as it reported it.

`aboard apply` printing "applied" is evidence a write was accepted, not that
anything renders — an unknown `ui` component draws a marker and an unknown PROP
draws nothing at all. After every mount the shell posts the control ids it drew,
the ones somebody pressed, and any unknown-component markers, and this prints
them.

This is NOT a DOM sweep. Every id here is already declared in
views/<type>.spec.json; nothing is scraped and nothing is matched against prose.

Two things it is deliberately not evidence of, printed with the output so they
travel with it: no receipt means nobody had the tab OPEN, and a control listed
here was REACHED — never that it behaved correctly.

Reads .aboard/run/rendered.json, so it needs no server. With no argument it
prints every tab that has a receipt.

Examples:

```
  aboard rendered bb133
  aboard rendered
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard serve

Run the board server for this project

```
aboard serve [flags]
```

Serve this project's board over HTTP and watch its state file for changes.

The port is derived from the discovered project root, so the URL is the same
every run and two checkouts never collide; --port or PORT overrides it. The
running instance is recorded in .aboard/run/instance.json, which is how every
other command finds the board and how restart.sh stops the right process.

--base-path serves the whole board under a URL prefix, for putting it behind a
reverse proxy or inside another tool's routing. The prefix is injected into the
shell, so every fetch, the SSE stream and an html tab's iframe all build from it.
Because it is injected, it is also validated: one or more /segments of letters,
digits, dot, underscore, tilde or hyphen. Anything else is a usage error.

Examples:

```
  aboard serve
  aboard serve --dev
  aboard serve --base-path /aboard
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--base-path` | string | `—` | serve under a URL prefix, e.g. /aboard (default: the server root) |
| `--dev` | bool | `false` | serve the web tree from disk instead of the embedded copy |
| `--dev-dir` | string | `—` | with --dev, the web tree to serve (default: pkg/aboard/web under the root) |
| `--port` | int | `0` | port to listen on (0 derives one from the project root; env PORT) |
| `--state` | string | `—` | state file to serve (default: .aboard/aboard.json under the root) |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard status

Report this project's running board, if any, and the caps beacon

```
aboard status [flags]
```

Say whether a board is running for this project, where, and since when — and
whether the committed skill reference still matches this binary.

The caps line is the beacon: an agent runs status as its first act, so a skill
that was generated for a different capsHash is reported in a command it was
going to run anyway. A MISSING reference is not staleness; a project that never
copied the skill has nothing to be out of date.

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard uploads

List the files under .aboard/uploads/ and the tabs that mention them

```
aboard uploads [flags]
```

Every image the human pasted or dropped, with its size and the tabs that name it.

The reference scan reads each tab's RAW state text, plus its name and note — not
its declared fields. An html widget's markup can name a file no spec knows
about, and a scan over declared fields would call that file an orphan and offer
to delete an image somebody is looking at.

  aboard uploads                    list them, unreferenced ones marked *
  aboard uploads --prune            show exactly what deleting them would remove
  aboard uploads --prune --yes      delete them

--prune on its own prints and REFUSES: deletion is irreversible and .aboard/ is
gitignored, so there is no copy anywhere to go back to.

Reads the state file directly, so it needs no server.

Examples:

```
  aboard uploads
  aboard uploads --prune --yes
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--output-format` | string | `human` | human, json or yaml |
| `--prune` | bool | `false` | show which unreferenced files would be deleted |
| `--yes` | bool | `false` | with --prune, actually delete them |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard version

Print the build identity of this binary

```
aboard version [flags]
```

Which binary is actually serving — never a constant somebody has to remember to
bump, because those lie eventually. Go stamps the VCS revision into a plain
build, so a local binary reports the commit it came from, with "+dirty" when the
tree had uncommitted changes.

A release build carries all three stamps (version, build date, commit) through
ldflags; --output-format json prints them whether or not they were stamped, so a
build that reports "dev" says so plainly instead of looking finished.

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--output-format` | string | `human` | human, json or yaml |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard wait

Block until the board is poked, or until a predicate matches

```
aboard wait [flags]
```

Block on one long-poll request until the human presses the notify button, until
another session pokes, or until the write you named arrives.

The predicate vocabulary is deliberately tiny, and an unknown one is refused up
front rather than accepted and never fired:

  poke                 the human pressed Notify (or another session poked)
  change               any accepted write at all
  tab bb71             that tab changed
  answer bb15          that tab changed AND a human made the change
  node bb58=done       that node reached that status
  rendered bb133       a browser MOUNTED that tab and posted a receipt

The rendered form is the one that is not about a write: a mount changes nothing
on the board, so it is released by the browser reporting one. Waiting on it is
waiting for a HUMAN to have the tab open, and nothing here can cause that.

While you are waiting the human's button says who is waiting, why, and for how
long — so fill in --note. A waiter is an open connection, so the count cannot go
stale: if this process dies, the button stops claiming anyone is listening.

Exit 0 means released. Exit 3 means the timeout ran out and nobody came.

Examples:

```
  aboard wait --for "answer bb128" --note "waiting on the gate"
```

Flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--by` | string | `agent-1` | who is waiting; shown on the human's notify button |
| `--for` | string | `poke` | what to wait for: poke \| change \| "tab <id>" \| "answer <id>" \| "node <id>=<status>" \| "rendered <id>" |
| `--note` | string | `—` | why you are waiting; shown on the button beside your name |
| `--timeout` | duration | `10m0s` | how long to block before giving up |

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |

## aboard watch

Follow every change as JSON lines until interrupted

```
aboard watch
```

Stream each accepted write as one JSON object per line, as it happens.

Not SSE: the consumer here is a shell pipeline, and `data: ` prefixes would just
be something for jq to strip. Each line says THAT something changed and which
tabs; re-read the board for the contents.

Global flags:

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `--cwd` | string | `—` | directory to resolve the project root from (default: the working directory) |
| `--name` | string | `—` | board name, for a second isolated board in the same project (env ABOARD_NAME) |


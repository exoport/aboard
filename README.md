# aboard — a shared visual board for a human and their agents

`aboard` is a single-binary Go CLI that serves a browser board for one project. A human
opens it in a docked VS Code tab; one or more Claude Code sessions read and write the
same state file from the terminal. **Tabs are data, not code** — an agent opens one for
whatever it needs to show (a dependency graph, a kanban, a mermaid diagram, a question
form, an annotated screenshot, a channel to another session, a bespoke sandboxed
widget) and reads back what the human changed.

> **Status:** pre-1.0. The command surface and the state schema may change between
> minor releases until `v1.0.0`. See [CHANGELOG.md](CHANGELOG.md).

## Why aboard

Terminal prose is a narrow channel for the two things human-in-the-loop work actually
needs: **showing structure** and **asking for a decision**. A dependency graph argued
with by dragging nodes, a form with three typed questions, a screenshot with two
circles on it, a gate with allow / deny / edit-then-allow — each of those is a better
interface to the same exchange than a wall of text, and they are what make staying in
the loop bearable rather than exhausting.

So the board's job is **bandwidth, not storage**. It is a local, persistent,
non-authoritative channel: `.aboard/` is gitignored, the conclusions get promoted into
the project's own documents, and if the board and a committed document disagree the
document wins. That posture is argued out in
[why a local, non-authoritative channel](docs/explanation/why-a-local-non-authoritative-channel.md),
and it is the half of this tool that is not code.

Everything is one file: the shell, the stylesheet, every renderer and the mermaid
bundle are embedded with `//go:embed`. No Node, no `node_modules`, no asset directory
to ship alongside, and nothing on the network at runtime.

## Install

With a Go toolchain (1.27 or later — the JSON codec is `encoding/json/v2`, which is stdlib from 1.27):

```bash
go install github.com/exoport/aboard/cmd/aboard@latest
```

The binary lands at `$(go env GOPATH)/bin/aboard`. It reports the module version
it was installed at — `aboard version` prints `0.1.0` for `@v0.1.0`, and a
VCS-derived pseudo-version like `0.0.0-20260826031230-f67e682b8f8a` for `@latest` on
an untagged commit. Only a binary built with no module and no VCS information at
all calls itself `dev`.

Or take the release archive for your platform:

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/exoport/aboard/releases/latest | jq -r .tag_name)
curl -fsSL "https://github.com/exoport/aboard/releases/download/${VERSION}/aboard_linux_amd64.tar.gz" \
  | sudo tar -xz -C /usr/local/bin aboard
aboard version
```

Releases publish `aboard_<os>_<arch>.tar.gz` (`.zip` on Windows), `aboard_checksums.txt`,
and `aboard_checksums.txt.bundle` — a Sigstore bundle over the checksums file (Fulcio
certificate, signature, SCT and Rekor proof in one file, verifiable **fully offline**)
signed by keyless cosign from this repo's release workflow:

```bash
cosign verify-blob \
  --bundle aboard_checksums.txt.bundle \
  --new-bundle-format \
  --certificate-identity-regexp \
    "^https://github\.com/exoport/aboard/\.github/workflows/release\.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  aboard_checksums.txt
sha256sum -c aboard_checksums.txt --ignore-missing
```

Full recipes: [How to install aboard](docs/how-to/install.md) ·
[How to verify a release artifact](docs/how-to/verify.md).

## Quick start

```bash
cd ~/work/your-project
aboard init --example --gitignore   # create .aboard/, seed the example board, ignore it
aboard serve                        # prints the URL — the port is derived from this project
```

Then, in VS Code: `Ctrl/Cmd+Shift+P` → **Simple Browser: Show** → paste that URL. It
docks as an editor tab, so the board sits beside your code. The port is derived from
the project root, so the URL is the same every run and two checkouts never collide —
the tab stays valid. Walk the whole loop in
[Your first board](docs/tutorials/first-board.md); the docking details and their
gotchas are in [How to run aboard inside VS Code](docs/how-to/run-in-vscode.md).

A board with nothing on it is the normal starting point: `aboard init` without
`--example` gives you an empty one, and agents open the tabs they need.

There is also a **VS Code extension** — a sidebar tree of the board's tabs beside the
board in a panel, following the editor's theme, with a New Tab button and a nudge for a
waiting session — in a separate repository. It is packaged as a `.vsix` and in daily use
here, and it is not published to any marketplace: you build and install it yourself. The
Simple Browser above still needs nothing installed:
[How to use the VS Code extension](docs/how-to/use-the-vscode-extension.md).

Forgotten which boards you left running? **`aboard boards`** lists every one on the
machine, with its project path, from the process table rather than a registry (Linux
only; it exits 2 elsewhere and points you at `aboard status`).

## The tab types

Fifteen renderers. A tab picks one with its `type`, owns its own `state`, and may set
`stateFrom` to render another tab's state — so a `dag` and a `kanban` can be two
readings of one dataset.

| type      | renders                                          | what the human can do in it                                                        |
| --------- | ------------------------------------------------ | ---------------------------------------------------------------------------------- |
| `dag`     | nodes and parent links as a tidy tree            | drag to move, drop onto another node to reparent, rename, add, delete, pan, zoom    |
| `kanban`  | the same nodes grouped by `status`               | drag between columns, reorder, rename, reparent; `readOnly` makes it agent-owned    |
| `diagram` | mermaid — 23 diagram and chart types             | edit the source, hover a node for its key                                           |
| `form`    | typed questions                                  | range, select, checkbox, text, textarea; reset                                      |
| `markup`  | images with a drawing layer                      | region / ellipse / pen / move / resize, per-mark colours and notes, hide marks      |
| `chat`    | a work channel between sessions                  | send and interject; each speaker coloured                                           |
| `notes`   | free text, optionally markdown                   | edit freely, Read/Edit toggle                                                       |
| `table`   | typed rows                                       | edit cells in place, sort by header, add / duplicate / delete rows, copy CSV or md  |
| `gate`    | approval requests                                | allow / deny / edit-then-allow, each with a reason                                  |
| `log`     | command output as it happens                     | follow, filter, ANSI colour — lines live in a sidecar file, not in the state file   |
| `trace`   | who wrote what, when                             | one lane per actor, a dot per write, click for detail; reads the journal            |
| `vote`    | scored options                                   | click to score, click again to clear; a wide split is called out, not averaged      |
| `ui`      | a component tree from a trusted catalog          | buttons that record intent — no iframe, no script                                   |
| `html`    | agent-authored HTML/CSS/JS                       | anything you write — canvas, drag-and-drop, WebGL — sandboxed, no network           |
| `stack`   | several of the above in one tab                  | collapsible blocks, top to bottom                                                   |

Three rules of thumb: `dag` when you want the shape argued with, `diagram` when the
shape is yours to assert; `ui` when the layout is ordinary and `html` when the
interaction itself is the point; `table` the moment you find yourself writing rows into
`notes`. The complete per-type inventory is what `aboard capabilities` prints — see
[the capability manifest](docs/reference/capabilities.md).

## The CLI at a glance

| Command                     | What it does                                                                        |
| --------------------------- | ------------------------------------------------------------------------------------- |
| `aboard init`               | Create `.aboard/`; `--example` seeds the example board, `--gitignore` adds the ignore. |
| `aboard serve`              | Run the board server for this project. `--dev`, `--port`, `--name`, `--base-path`.    |
| `aboard status`             | Is a board running, where, since when — plus the caps beacon and skill staleness.     |
| `aboard boards`             | Every board running on this machine, from the process table. Linux only; exit 2 elsewhere. |
| `aboard apply`              | Read a document on stdin and write it through the running board (compare-and-set).    |
| `aboard wait`               | Block until the human pokes, or until a predicate matches. Exit 3 means nobody came.  |
| `aboard poke`               | Release every session waiting on this board, as the human's notify button does.       |
| `aboard journal`            | Recent accepted writes: when, who, which tabs.                                         |
| `aboard watch`              | The same, as JSON lines, until interrupted.                                            |
| `aboard history <tab>`      | What one tab said before, out of the journal. `--at N` prints a document that restores it. |
| `aboard log <tab>`          | Pipe a command's output into a `log` tab.                                              |
| `aboard export <tab\|key>`  | One tab as markdown (or `--format csv`), for pasting into the project's own documents. |
| `aboard uploads`            | The images the human pasted, and which tabs mention them. `--prune --yes` deletes the rest. |
| `aboard rendered [tab]`     | What a browser reported it actually drew — not proof that it behaved.                  |
| `aboard capabilities [type]`| What this board can do. No server needed; `--check` gates the committed skill copy.    |
| `aboard recipes list\|show` | The recipes available here, and one recipe's body or its `--template`.                 |
| `aboard version`            | Build identity of this binary.                                                         |

`--cwd DIR` on the root resolves the project from somewhere other than the working
directory, and commands that emit structured data take `--output-format human|json|yaml`.
Run `aboard <command> --help`, or read the generated
[CLI reference](docs/reference/cli.md) for every command, flag and default.

## The skill

`.claude/skills/aboard/` teaches a Claude Code session how to use a board: which tab
type suits which kind of explanation, the state schema, how to read the human's edits
back, and how two sessions share one board without losing each other's writes. **Copy
that directory into your own project** — it is not embedded in the binary and there is
no install command:

```bash
cp -r .claude/skills/aboard /path/to/your-project/.claude/skills/
```

It is auto-discovered from there. Its facts half is generated (`make caps`), so
`aboard status` can tell you when a copied skill has gone stale against the binary,
and `aboard capabilities` answers in a project that never copied it at all.

## Recipes

A recipe is a short markdown method for one board move — ask for a decision, show a
structure, react to the human's edits — with frontmatter an agent can match against and
an optional JSON tab skeleton it can apply.

```bash
aboard recipes list                   # every recipe available here, and what shadows what
aboard recipes show ask-for-a-decision
aboard recipes show ask-for-a-decision --template   # just the tab skeleton, ready to edit
```

Nine recipes ship with the binary and a project may add or override its own in
`_apex/aboard/recipes/`, `_aboard/recipes/` or `.aboard/recipes/` — first match by name
wins, and a shadowed recipe is always reported rather than hidden. A few more are
collected in this repository's [`recipes/`](recipes/README.md) library: not compiled in,
used by copying the file into one of those directories. Writing one:
[How to write a recipe](docs/how-to/write-a-recipe.md); the format itself — folders,
precedence, the frontmatter schema, the template block — is
[the recipes reference](docs/reference/recipes.md).

## Inside ape

The same cobra tree mounts inside another CLI: `ape aboard <command>` is this command
set, hosted by [ape](https://github.com/exoport/apex_process_ape), sharing **one
`.aboard/` per project** — the same state file, the same derived port, the same
instance record. A board started by `ape aboard serve` is the board `aboard status`
reports, and either binary can drive it. The two hosts are distinguishable where it
matters (`/health` and the instance file carry `app: "ape-aboard"` rather than
`app: "aboard"`, so an error message can name the command you actually have) and
identical everywhere else, including `capsHash`. See
[How to embed aboard in ape](docs/how-to/embed-in-ape.md) and
[why two identities](docs/explanation/why-two-identities.md).

## Documentation

**[docs/README.md](docs/README.md) is the index.** Everything below is linked from
there, and it follows [Diátaxis](https://diataxis.fr/) — pick the quadrant that matches
what you need:

- **[Tutorials](docs/tutorials/)** — [Your first board](docs/tutorials/first-board.md): install, seed, serve, dock, write, watch, export.
- **[How-to guides](docs/how-to/)** — [install](docs/how-to/install.md), [run it in VS Code](docs/how-to/run-in-vscode.md), [run a second board](docs/how-to/run-a-second-board.md), [put it behind a reverse proxy](docs/how-to/serve-under-a-path-prefix.md), [promote a tab into a document](docs/how-to/promote-a-tab-into-a-document.md), write a recipe, embed it in ape, verify a release.
- **[Reference](docs/reference/)** — the [CLI](docs/reference/cli.md), the [`.aboard/` layout](docs/reference/layout.md), the [state file](docs/reference/state-file.md), the [HTTP API](docs/reference/http-api.md), the [capability manifest](docs/reference/capabilities.md), [recipes](docs/reference/recipes.md).
- **[Explanation](docs/explanation/)** — [how aboard runs](docs/explanation/how-aboard-runs.md), and the design rationale, including the decisions that are closed.

If you are new, three pages get you furthest:
[Your first board](docs/tutorials/first-board.md),
[How aboard runs](docs/explanation/how-aboard-runs.md), and
[Why a local, non-authoritative channel](docs/explanation/why-a-local-non-authoritative-channel.md)
— what the board is FOR, which is the half of this tool that is not code.

## Development

```bash
git clone https://github.com/exoport/aboard.git
cd aboard
make help          # available targets
make tools         # build the pinned dev tools under $GOBIN
make build         # → ./aboard
make test          # go test -race ./...
make ci-local      # the full pre-push gate
```

CI runs build + test on both Linux and Windows, and lint + govulncheck on Linux, for
every push to `main` and every pull request. Windows is not a formality: root discovery
walks upward through path separators and the state file is written through a
same-directory temp + rename, and both behave differently there. The browser suite (`make e2e`) and the screenshot tool
(`make shot`) are **local only** — they drive a real Chromium, so a CI run could only
skip them, and a gate that always skips reads as a pass. `make e2e` needs nothing
started and no project of its own: it seeds a temporary board and serves the engine
in-process, so it cannot reach a board you care about. `make shot` needs a running
server and takes `PROJECT=<dir>` to say which one, defaulting to this checkout.
Working conventions, hard rules and the gotcha list are in [CLAUDE.md](CLAUDE.md).

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution — the embedded UI
carries the mermaid bundle (MIT).

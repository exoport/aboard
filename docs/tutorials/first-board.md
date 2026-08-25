# Your first board

By the end of this tutorial you will have a board open in your editor, you will have
changed it from the terminal and watched the change arrive, you will have blocked a
terminal session until you released it from the browser, and you will have pulled a tab
back out as markdown. That round trip — browser ↔ disk ↔ terminal — is the whole tool.

It takes about ten minutes. You need a Go toolchain (1.26 or later) or a release
archive, a project directory to put a board in, VS Code, and `python3` for the two
places where we edit JSON.

## 1. Install aboard

```bash
go install github.com/exoport/aboard/cmd/aboard@latest
aboard version
```

`aboard version` should print a version line. If your shell reports
`aboard: command not found`, `$(go env GOPATH)/bin` is not on your `PATH` — fix that
before continuing. Other install paths (release archive, build from source) are in
[How to install aboard](../how-to/install.md).

## 2. Create a board

Go to a project you actually work in — a scratch directory is fine, but the board is
more interesting next to real code.

```bash
cd ~/work/your-project
aboard init --example --gitignore
```

Three things just happened:

- `.aboard/` was created, with `aboard.json` seeded from the **example board** — 15 tabs covering every renderer, each carrying a `note` saying what it demonstrates. `kanban` appears twice (one borrows the dag's nodes through `stateFrom`, the other is a read-only agent-owned queue) and `notes` appears as a block inside the `stack` tab rather than as a tab of its own;
- `.aboard/run/` was created for the machine-local runtime files (the instance record, the journal, per-tab logs, screenshots);
- `.aboard/` was appended to your `.gitignore`, because a board is local and per-developer. If you would rather do that yourself, drop `--gitignore` and `init` just prints the line for you to add.

Look at what you have:

```bash
ls .aboard
```

`aboard.json` is the board — the only file here you would ever curate. Everything under
`run/` is true for this machine and this moment only. The full tree is described in
[The `.aboard/` layout](../reference/layout.md).

> `aboard init` refuses to overwrite an existing state file. Running it twice is safe
> and does nothing the second time.

## 3. Serve it

```bash
aboard serve
```

It prints where it is listening and stays in the foreground:

```
aboard ->  http://localhost:43211   (embedded UI, v0.1.0)
state  ->  /home/you/work/your-project/.aboard/aboard.json
project->  /home/you/work/your-project
In VS Code: Ctrl/Cmd+Shift+P -> "Simple Browser: Show" -> paste the URL above.
```

Your port will differ — it is derived from your project's path, not from mine.

Leave that terminal running and open a second one for the rest of the tutorial.

The port is **derived from the project root**, not allocated at random: two checkouts
never collide, and this project's URL is the same every run — so the editor tab you are
about to open stays valid tomorrow. `aboard serve` also refuses to start a second board
for the same project; it prints the URL of the one already running instead.

In the second terminal, confirm it:

```bash
aboard status
```

You should see the URL, the pid, the state file, and a `caps` line — the hash of what
this board can do. That command is the first thing an agent session runs; the caps line
is how it learns whether the skill it is carrying still describes this binary.

## 4. Dock it in VS Code

In VS Code, with the same project open:

1. `Ctrl/Cmd+Shift+P`
2. **Simple Browser: Show**
3. paste the URL from `aboard status`

The board opens as an editor tab, so you can drag it into a split beside your code.
Click through a few example tabs — the graph, the kanban, the mermaid diagram, the
screenshot with marks on it, the sketch pad. Each one's `note` says what it is
demonstrating.

If a widget tab comes up blank, or you want a keyboard-only route back to the board,
see [How to run aboard inside VS Code](../how-to/run-in-vscode.md).

## 5. Change the board from the terminal

This is the half you cannot see from the browser. Pick the first tab and give it a
note, in the human's sense of the word — what the tab is FOR:

```bash
python3 -c "
import json
doc = json.load(open('.aboard/aboard.json'))
doc['tabs'][0]['note'] = 'Touched from the terminal, in the first-board tutorial.'
print(json.dumps(doc, indent=2))
" > /tmp/next-aboard.json

aboard apply --by "agent-1" < /tmp/next-aboard.json
```

It prints `applied to <url> as "agent-1"`. **Watch the browser**: the tab you touched now carries a dot, and
opening it shows a banner saying `agent-1` changed it. Only you, dismissing it, clears
that marker — a later write cannot take it down, which is what stops one agent's change
hiding another's.

Three things about that command, because they are the shape every board write takes:

- **You submit the whole document, not a patch.** Read it, change it, apply it.
- **The `updatedAt` inside the document you read is the compare-and-set base.** If the browser or another session wrote in between, `apply` refuses rather than clobbering them — re-read, redo, apply again.
- **`--by` is not decoration.** It lands in `lastEditedBy` and on every tab the write touched, and it is what the human sees. `agent-1`, `agent-2`, `agent-<role>` — and `--by human` is refused from the CLI, because the CLI is not the human.

Never edit `.aboard/aboard.json` in an editor while the server is running: that path
has no compare-and-set, so a concurrent change is destroyed with no error.

Now ask the board who has been writing:

```bash
aboard journal --limit 5
```

Your write is the newest line, stamped `agent-1`. Dismiss the banner in the browser and
run it again — the dismissal is a write too, stamped `human`.

## 6. Block until the human releases you

An agent that needs an answer should block for it rather than poll. In your second
terminal:

```bash
aboard wait --by "agent-1" --note "finished the tutorial write" --timeout 2m
```

It hangs. Look at the browser: the header now shows a **notify agent-1** button with a
lit dot, your note beside it, and a countdown. Press it.

The terminal returns immediately, exit 0. Had nobody pressed it within two minutes, it
would have exited **3** — "nobody came" and "something broke" are different outcomes and
a script should be able to tell them apart. From another terminal, `aboard poke` does
the same thing as the button.

Nothing here started an agent; a session that had not decided to listen would not have
been released. That asymmetry is deliberate — see
[why nothing in the UI starts a session](../explanation/why-nothing-in-the-ui-starts-a-session.md).

## 7. Take the text back out

The board is a channel, not a record. When a tab has settled something, get the text
out and put it where the project keeps its decisions:

```bash
TAB=$(python3 -c "import json;print(json.load(open('.aboard/aboard.json'))['tabs'][0]['id'])")
aboard export "$TAB"
```

That prints the tab as markdown on stdout — ready to paste into a spec, an ADR, or a
commit message. `--format csv` gives you the rows of a tab that has rows. `export` reads
the state file from disk, so it works with no server running at all.

Adapt what it gives you rather than pasting it whole: a board tab and a document have
different jobs, and a diagram you argued with still carries the branches you rejected.
The reasoning behind that is
[why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md).

## 8. Stop

`Ctrl-C` the `aboard serve` terminal. The board is a file on disk; everything you did
is still in `.aboard/aboard.json`, and `aboard serve` picks it up exactly where you left
it.

## What you have

- A board in your project, gitignored, seeded from the example set.
- A URL that will be the same next week, docked in your editor.
- The write shape: read the document, change it, `aboard apply --by`.
- The notify channel: `aboard wait` on one side, a button on the other.
- A way to get the text out: `aboard export`.

## Where next

- Teach a Claude Code session to use it: copy `.claude/skills/aboard/` into this project. The session then discovers it on its own, and `aboard capabilities` answers its questions about the surface even if you never copy the skill.
- `aboard capabilities` — every tab type, every state field a renderer actually reads, every control, every route. Reach for it before guessing at a field name.
- `aboard recipes list` — the built-in methods for the common board moves, and how a project overrides them ([How to write a recipe](../how-to/write-a-recipe.md)).
- [The state file](../reference/state-file.md) — what you were editing in step 5, field by field.

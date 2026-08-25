# board

A shared visual board for a human and one or more Claude Code sessions. A Go
binary serves a browser UI that docks inside VS Code; `board.json` on disk is live
state that both sides read and write. Tabs are *data*, not code: an agent opens
one for whatever it needs to show — a graph, a chart, a question form, an
annotated screenshot, a channel to another agent, a bespoke HTML widget.

**Load the `board` skill before touching `board.json` or the UI.** It carries how
to choose a tab type, the full capability inventory, the schema, worked recipes,
and multi-session etiquette. This file is orientation, hard rules, and current
status only.

## Resuming after a context clear

Three commands, in this order, before anything else:

```sh
./board -status         # running? which URL? and the caps beacon
./board -capabilities   # what this board can actually do — 15 types, every state
                        # field, every CONTROL in toolbar order, every colour name,
                        # every gesture, endpoint and flag, and every `ui`
                        # component with the props it reads
./board -journal -limit 20   # who changed what recently, including other sessions
```

**Then read the skill's first section, `## What the board is FOR`, before touching
a tab.** It is the half of this project that is not code: the board is a local,
persistent, NON-AUTHORITATIVE channel; three tiers matched by lifetime; the
promotion target and boundary belong to the project, not to the skill; and work is
ordered by the cost of being wrong — put the cheapest rejectable thing in front of
the human first, which is the decision a document depends on, never a draft of the
document. Those were argued out with the human over a long session and every one of
them corrected something I had got wrong; do not re-derive them from scratch.

`-capabilities` is the point of the manifest: **do not reconstruct the surface
from memory or from this file — ask the binary.** `./board -capabilities kanban`
is the cheap per-type version. If `-status` warns that the skill reference was
generated for a different `capsHash`, the skill is describing a board that no
longer exists: run `make caps`.

Then read the board itself — it is the working record, not decoration. It was
cleared on 2026-08-23 to hold only live work and one example per renderer:

**Working**

| tab | what it holds |
|---|---|
| `bb71` Build queue | where work lands, read-only, agent-maintained. **Empty = nothing outstanding** |
| `bb128` Decisions | the gate: what needs the human's yes/no. Empty pending = nothing blocked |
| `bb132` What next | scored options for where a session should go; the human's ballot decides |
| `bb26` Coordination | the channel between sessions. `@mention` to address one |
| `bb127` Trace · `bb126` Output | who wrote what when; piped command output |

**Examples — one per renderer, each with a `note` saying what it demonstrates**

`bb133` UI gallery (all 25 `ui` components, rendered) · `bb1` Plan (dag) ·
`bb13` Progress (kanban borrowing bb1 through `stateFrom`) · `bb14` Architecture
(diagram) · `bb15` Questions (form) · `bb22` Screen (markup, paste and annotate) ·
`bb32` Migration review (stack; its notes block is the markdown example, and its
fifth block is an `html` widget — the proof that one renders inside a stack) ·
`bb111` Table example · `bb72` HTML example (a canvas sketch pad — pointer
capture, token-coloured strokes, persisted through `board.set`; the human has
drawn on it, so the whole bridge round trip is proven).
`bb143` is the human's own scratch tab — leave it alone.

Everything else that was here — 24 shipped cards, 36 verification rows, the
renderer research — is finished work whose conclusions are in this file. Do not
recreate it on the board.

## Run

```sh
./restart.sh          # start, or print the URL of the one already running
./restart.sh -force   # actually restart (after changing Go code or embedded assets)
./restart.sh -dev     # serve views/ and app.css from disk while iterating
./board -status       # URL, pid, state file
./board -capabilities            # what this board can do (no server needed)
./board -capabilities <type>      # one type, cheap
./board -wait -for "answer bb128" # block until the human decides (0 answered, 3 timed out)
./board -poke                     # release every waiting session, as the button does
./board -export <tab|key>         # a tab as markdown, for pasting into a spec (no server needed)
./board -export <tab> -format csv # its rows
./board -journal -limit 20        # who changed what, recently
./board -watch                    # the same, as it happens (JSON lines)
<cmd> 2>&1 | ./board -log bb126   # stream output into the Output tab
make caps                         # regenerate the skill's generated reference
./test/smoke.sh                   # 98 checks: mounts, notify, caps drift, uploads, read-only
./test/shot.sh [tab]              # screenshots into .shots/
```

`./board -apply` prints a warning on stderr when a write sets state no renderer
reads — the fastest way to catch a guessed field name.

The port is **derived from this directory's path** — currently `46624`, but read
`.board/instance.json` rather than assuming.

## Two hard rules

**1. Never write `board.json` with `Edit`/`Write` while a board is running.**

```sh
./board -apply -by "agent-1" < next.json
```

`Edit` bypasses compare-and-set, so a concurrent change from the browser or
another session is destroyed with no error. Measured, not theoretical. A `409`
means someone got there first: re-read, redo, apply again. Direct `Edit` is fine
only when `-status` reports nothing running. Use `agent-1` / `agent-2` /
`agent-<role>` for `-by`, never `claude` — it is shown to the user on every
changed tab.

**2. Never restart a healthy server.** Plain `./restart.sh` deliberately leaves
one running and just prints the URL, so a second session cannot yank the server
from the first. `-force` only when you mean it.

## Where things are

| path | role |
|---|---|
| `board.json` | all state; the only file not compiled into the binary |
| `main.go` | server: routes, compare-and-set POST, SSE, port derivation, `-apply` |
| `tabs.go` | the four tab guarantees (see below) |
| `ids.go` | the id allocator invariant |
| `htmltab.go` | serves `html` tabs sandboxed, with the `board.*` bridge |
| `wait.go` | the notify channel: `/wait` long poll, `/poke`, `/waiters`, `-wait`/`-poke`, predicates |
| `reload.go` | the UI signature that makes an open page reload itself on a code change |
| `export.go` | `-export`: a tab as text to promote into the project's own documents |
| `caps.go` | the board describes itself: `views/*.spec.json` → `-capabilities`, `/capabilities`, the generated skill reference, and `-apply`'s undeclared-key warning |
| `journal.go` | every accepted write recorded and streamed: `/journal`, `/watch`, `-journal`, `-watch` |
| `logs.go` | sidecar log files per tab: `/log`, `-log` — deliberately NOT in `board.json` |
| `upload.go` | `/upload` — the human pastes or drops an image; lands in `uploads/`, not `assets/` |
| `board.html` | shell: tab strip, dots, notices, `TYPES` registry, id allocator |
| `app.css` | the single token set everything is coloured from |
| `views/*.js` | one renderer per tab type, `mount<Name>(root, ctx)` → `{refresh(), focus?(), onKey?(), destroy?()}` |
| `views/controls.js` | the only way a view makes a button: `controlsFor(type)(id)` for declared controls, `button()` for agent content and shared chrome |
| `views/controls.generated.js` | every declared control, emitted from the specs by `make caps`. Never edit; the suite fails if it drifts |
| `views/menu.js` | the shared right-click menu, `copyText`, `referenceFor` |
| `views/markdown.js` | a markdown subset rendered to DOM nodes (never `innerHTML`) |
| `views/export.js` | tab → markdown / CSV / SVG, and the download attempt |
| `views/heartbeat.js` | "is the agent minding this tab still there" strip, with age decay |
| `views/inline.js` | the one inline editor: visible Save/Cancel, Enter/Esc, and a "saved" flash |
| `views/*.spec.json` | what each renderer accepts, every control it draws **in toolbar order**, and what the human can do in it — the canonical declaration, rendered FROM rather than merely describing |
| `.claude/skills/board/` | the skill: SKILL.md + 5 references, one of them generated |
| `_output/` | gitignored scratch. Holds `handoff-capability-manifest.md` — **will not survive a clone** |

## Current status

- Schema **v3**, 16 tabs, `nextId` past 197. Both duplicate examples (`bb41`,
  `bb43`) were removed by the human answering the requests. `board.html` declares
  `SCHEMA_VERSION = 3`; a mismatch shows the user a "reload" notice instead of
  breaking silently.
- **15 renderers**, all mounting in the suite: `dag`, `kanban`, `diagram`, `form`,
  `markup`, `chat`, `notes`, `html`, `stack`, plus `table`, `gate`, `log`, `trace`,
  `vote`, `ui`. A new one needs a line in `TYPES` **and** a line in
  `test/smoke.html`'s `MODULES`, or it mounts nowhere and is tested nowhere.
- **The build queue is empty and verified**: 24 cards shipped, and all 36 rows of
  `bb111` ticked by the human on 2026-08-23. Nothing is outstanding and nothing
  from these two days is unconfirmed.
- **Prefer `ui` over `html`** whenever `ui` can express it: a component tree
  cannot get the theme, contrast or type sizes wrong, has nothing to contain, and
  the next session can change one node of it instead of reading a page of someone
  else's JavaScript. `html` is for when the INTERACTION is the point (canvas,
  drag-and-drop, simulation). The `ui` catalog is 25 components.
- **`kanban` takes `state.readOnly`** — agent-owned, human reads. Affordances are
  removed rather than disabled (a card that drags then snaps back reads as a bug)
  and a badge says why. It shapes the interface; it enforces nothing, since the
  server does not distinguish a browser write from an agent one. `bb71`
  ("Build queue") is the first user: the work list, three columns.
- **The notify channel is live** (`wait.go`): a session blocks on `./board -wait`,
  the header button says *notify agent-1 · 12m* with a lit dot and a countdown,
  pressing it releases every waiter. Nobody waiting → the button is disabled. A
  waiter is an open connection, so the count cannot go stale. Predicates all
  work: `poke`, `change`, `tab <id>`, `answer <id>` (that tab changed AND a human
  did it), `node <id>=<status>`; an unknown one is refused up front rather than
  blocking on something that will never fire. `-watch` streams every change as
  JSON lines; `-journal` prints them.
- Marks are badged **with their id** (`bb168`) on the image, not a per-image
  counter: the counters restarted on every image, so each one had a "1" and none
  agreed with the list. One identifier on the image, in the table, and in a
  sentence.
- `./test/smoke.sh` → 98 checks, all passing. `go vet` clean, `gofmt` clean.
- **The skill cannot silently disagree with the code.** Each renderer declares its
  surface in `views/<type>.spec.json`; the binary aggregates that into
  `./board -capabilities` (no server needed), `GET /capabilities`, and the
  generated `references/reference.generated.md`. `./board -status` prints a
  `capsHash` and warns when the committed reference was generated for a different
  one; `make caps` regenerates it; the suite fails if it drifts. `./board -apply`
  warns on stderr when a write sets state no renderer reads — the one check no
  document can perform. Facts are generated, judgment stays authored.
- **The write path detects what used to land on the human's screen instead**
  (2026-08-24, all four found by a session using the binary in another project —
  the portability claim earning its keep). The shape they shared was one thing:
  **`ui` fails silently AND successfully.** `-apply` printed `applied`, exit 0,
  whatever you wrote, so every mistake was found by the human looking at the
  board rather than by the agent that made it — which is backwards, because the
  agent is the only one of the two still holding the context to fix it.
  - **`version` is now server-managed** (`main.go`), stamped alongside `nextId`,
    `updatedAt` and `lastEditedBy`. It was the one such field left to the caller,
    and `schema.md` showed `"version": 2` for a schema that had been 3 since
    v3 shipped — so an agent copied the example it was reading, `-apply` wrote it
    through, and `board.html` blanked the whole board in front of the human one
    round trip after being told it was ready. **Stamped rather than refused** (the
    content was fine; failing a good write over a field the caller should not set
    is the worse trade), **plus a stderr warning** from `-apply` so the stale
    source still gets fixed. Both halves matter: the stamp saves the human, the
    warning reaches the agent.
  - **`writeWarnings` descends** (`caps.go`, was `unknownStateKeys`). It read each
    tab's state as a flat map and checked only top-level keys, so it covered
    *none* of the surface where a `ui` tree or a `stack` block's state lives —
    exactly where mistakes are most likely. It now walks a `ui` component tree
    against a declared catalog and recurses into stack blocks, warning on an
    unknown component, an unknown prop on a known component, a wrong item shape,
    a bad block field, and a `{bind}` that resolves nowhere.
  - **`views/ui.spec.json` declares all 25 components and their props**, so
    `./board -capabilities ui` finally answers "what does `kv` take?" — previously
    only `views/ui.js` knew. Declaration stays canonical; the checker reads it.
    A prop list is not a lint rule invented in Go, and that is the point.
  - **`kv` resolves its values** (`views/ui.js`). It resolved the `pairs` array but
    used `String()` on each key and value where every sibling uses `asText()`, so
    the one component whose entire job is "label: value" rendered
    `[object Object]` for a `{bind}`. A live summary is a main reason to reach for
    `ui`, and the obvious component for it was the one that could not do it.
- **An `html` block inside a `stack` renders** (`htmltab.go`). `views/html.js` asks
  for `/tab/${ctx.tab.id}/html` and inside a stack that id is `"<tab>/<block>"`,
  which matched no tab — so the frame 404'd and the block was BLANK, with no
  marker and nothing on any console. `serveTabHTML` now resolves a compound path
  through the parent's `state.blocks`, serving the block's own html, data and
  title. **The CSP and sandbox are byte-identical to a tab's** (asserted), and the
  bridge needed no change at all: `stack.js`'s `ctxForBlock` already hands down a
  live `state` getter, so `board.set()` was always writing to the right place. Every
  wrong path now names what was wrong instead of 404ing blankly. `bb32`'s fifth
  block is the working example.
- **Controls are declared, not described** (2026-08-24, a four-commit series). 50
  buttons across 12 renderers are declared in `views/<type>.spec.json` and drawn
  FROM that declaration by `views/controls.js`. The problem it solves was never
  matching prose — it was that **`gestures` had no consumer.** State fields never
  drift far because `-apply` READS their declaration, so a wrong one produces a
  wrong warning and somebody fixes it; `gestures` only fed prose, so nothing broke
  when it went stale, and `table` shipped a delete-row button documented nowhere
  while `SKILL.md` advertised the feature.
  - **Two functions, on purpose.** `controlsFor('dag')('relayout')` for a control
    the renderer owns; `button(label, title)` for agent-authored content (a `ui`
    button's label is the agent's) and chrome belonging to no renderer (context
    menu, inline editor, a dialog's Cancel). Whether a button is a CAPABILITY or
    merely an affordance is a judgement no rule makes, so it is two calls and
    visible in review. The suite prints which files still use plain `button()` —
    currently `dag`, `markup`, `inline`, `menu`, `ui`, and all five are correct.
    **`board.html`'s eight shell buttons go through it too** — they are chrome
    with no spec, so they use plain `button()`, but routing them through the
    helper is what makes the lint total instead of "total except one file nobody
    remembers".
  - **`controls` is a LIST, not a map**, because unlike state fields these have an
    order and it is meaningful: it is the order they sit in the toolbar, and it is
    what the help panel shows. Alphabetical by id, markup's twelve read
    "Colour ▾, Clear marks, ✕, Ellipse…" — deterministic and useless. Reordering a
    spec therefore moves `capsHash`, which is correct: the order is part of the
    surface. The generated JS module stays an object keyed by id, since that one is
    only ever a lookup.
  - **Generated, not fetched.** The shell already pulls `/capabilities` at boot for
    the help panel, and async is fine there. Button LABELS are not: they would
    render from a fallback and visibly re-label. `make caps` emits
    `views/controls.generated.js` and **builds twice** — `views/` is embedded, so
    the first binary writes the module and the second embeds it.
  - **Four checks, none of them fuzzy**: every renderer button goes through the
    helper (a grep); every declared control is used by its renderer (catches a
    declaration left behind after its button was deleted); every rendered control
    resolves to a declaration (an id could be built at runtime); and an undeclared
    one renders `?id` with `data-undeclared` rather than a blank button. All four
    were verified by deliberately breaking them.
  - **`gestures` now means the remainder** — drag, drop, wheel, double-click,
    right-click, type-and-it-saves. **Nothing can verify it**, deliberately and
    stated in `caps.go`: no sweep can confirm a sentence still describes a set of
    pointer handlers. Fifteen entries that merely restated a button moved into
    that button's `doc`, so the unverifiable half is as small as the truth allows.
    The help panel gained a **Buttons** section so the human lost nothing.
- **A 409 no longer discards the human's edit** (`board.html`). Compare-and-set is
  whole-document, so any concurrent write conflicts with any other; the browser
  used to reload and lose whatever had just been typed, and the human is the only
  actor whose work cannot be reconstructed. It now merges: fetch fresh, re-apply
  the tabs the human touched where the server has not touched them, retry once,
  and stash a genuine same-tab collision behind a "Restore mine" notice.
- **The top bar shows which binary is serving** — the VCS revision Go stamps into
  the build, `+dirty` when the tree had uncommitted changes, from `/health`. Never
  a hand-maintained constant: those lie eventually.
- **A tab carries an optional `note`** — what it is FOR, in the human's words.
  Read it before acting on a tab. They set it in the New tab dialog, the strip
  under the tab strip, or the tab's right-click menu.
- **`git log` is a real source here, not noise.** Every commit message states the
  reasoning and the mistakes, including the ones found while fixing something else
  — read it before re-deriving a decision, and keep writing them that way.
  `board.json` moves constantly, so commit it deliberately rather than expecting a
  clean tree.
- **Under git**, so a bad edit is recoverable.
  `board.json` is tracked, which means it shows as modified constantly — that is
  expected, it is live state. `.board/`, `.shots/`, `dist/` and the `board` binary
  are ignored.

## This repo commits `board.json`. A normal project must not.

The skill now states the posture: the board is a **local, persistent,
non-authoritative channel** and `board.json` belongs in `.gitignore`. It also
carries the judgement that goes with it — **three tiers, matched by lifetime**:
the agent's context dies at a context clear; the board survives clears and
restarts but is local and unversioned; the repo survives everything and is
reviewed. Most of what happens on a board belongs in the middle tier and is spent
there — a design being reviewed, a pick-one, a diagram being checked. Only what a
future session would be WRONG without goes to the repo, and promoting everything
is its own failure: stale documents mislead, which is worse than absent ones.

Three further calls the human made on 2026-08-23, now in the skill: the promotion
target is **the project's own documents, found not invented** — `CLAUDE.md` is one
possibility among many and does not exist in most projects; the **boundary** at
which you promote (a commit, clearing a tab, a PR, the next spec edit) is the
project's to choose and to record; and **order the work by the cost of being
wrong**: put the cheapest rejectable thing in front of them first — the decision
the document depends on, not a draft of the document — because an unagreed document
anchors them to your framing and then sunk cost defends it. Document-first is the
derived case, for when the document is cheap enough to throw away. Once a document
is the record, the tab must be demoted. Several developers on one repo each get their own board; a
committed one would mean a whole-file JSON conflict on every merge, over a
conversation that was never theirs.

**This repo is the exception, deliberately**: here the board IS the product, so
`board.json` is tracked because its tabs are the demo content and the working
record of building it. That is why it shows as modified constantly. Do not
"fix" that by ignoring it here, and do not copy the habit into a real project.

The distinction that makes the rule bite: not versioned is not the same as
disposable. A gate request waiting on the human, or a session parked on `-wait`,
has to survive a restart and a week away.

## What travels to another project, and what does not

Asked directly, and worth being precise about, because three layers look alike
from the browser:

| layer | lives in | travels? |
|---|---|---|
| **behaviour** — every renderer, gesture, hover, menu | `views/*.js`, compiled in by `//go:embed` | **yes**, in the binary. Copy `board` into an empty directory with a `board.json` and every gesture works |
| **its documentation for agents** | `views/*.spec.json`, also embedded | **yes** — `./board -capabilities` answers in a bare project with no skill, no server and no repo |
| **content** — tabs, marks, images, cards | `board.json`, `uploads/`, `.board/` | **no**, and that is the point: it is that project's state |
| **judgment** — when to use what, etiquette | `.claude/skills/board/` | only if you copy the skill in. `make caps` regenerates its generated half; `./board -status` warns when the copy is stale |

So a fix made in a renderer is a fix everywhere the binary goes, and it documents
itself on arrival. A fix made in `board.json` is a fix for this project only.
`./board -capabilities -check` treats a MISSING skill reference as "nothing to
check" rather than stale — a project that never copied the skill has nothing to
be out of date.

## Decisions already made — do not relitigate

- **Go with `//go:embed`, not Node.** One binary, stdlib only, no `go.sum`.
  `server.js` remains as a no-Go fallback. The file watcher polls a content hash
  rather than using fsnotify: keeps dependencies at zero and survives a
  rename-based save, which a single-file OS watch does not.
- **Tabs are data.** A tab is a name + a `type` + its own `state`. Adding a
  renderer means three lines, and all three are load-bearing: `TYPES` in
  `board.html` (or it mounts nowhere), `MODULES` in `test/smoke.html` (or it is
  tested nowhere), and `views/<type>.spec.json` (or no agent ever learns it
  exists, and the suite's spec/mount parity check fails).
- **Four guarantees are server-enforced, not conventions** (`tabs.go`): an agent
  cannot delete a tab (a dropped tab is restored as a `pendingRemoval` request the
  human answers); cannot clear a `touched` marker (only the human's dismiss does);
  cannot un-ack a chat message (that would reopen the human's edit window on
  something already acted on); and cannot clear another actor's `seen` stamp (it
  may set its own key and nobody else's). Each exists because an agent that forgot
  would destroy the user's work or hide its own tracks.
- **Nothing in the UI may START an agent session.** Stated by the human on
  2026-08-23: *"we don't want to trigger agent sessions from the ui."* So the
  board may ASK (a gate request, a form, an action strip, an intent) and a session
  may choose to WAIT (`./board -wait`) — the notify button only releases a session
  that already decided to listen. What is ruled out: a server hook spawning
  `claude -p`, a cron or loop that wakes agents on a timer, and any "start an
  agent" affordance. I proposed the hook twice; do not propose it a third time.
  A consequence worth stating: a board with nothing waiting is simply not
  listening, and the honest thing is to say so rather than to make it look live.
- **A diff renderer is rejected. Closed, not deferred.** Asked twice; the second
  answer was *"forget it, no future for it"*. Keep the reason so nobody re-derives
  the idea and re-proposes it: a diff tab makes asking cheap, and anything cheap
  gets overused, so it becomes a way to spam the human about every change. The
  option has been taken off the What next ballot, because an option on a ballot is
  an open question by definition. Do not build it, do not propose it, do not add
  "unless…" to this entry.
- **Nothing in the browser executes anything.** `state.actions` buttons, `gate`
  verdicts and `ui` buttons all RECORD — an intent, a decision — and the agent that
  asked acts on it. That is the MCP-UI/A2UI posture and it is what makes a stray
  click harmless on a server with no auth.
- **Ids are board-wide monotonic, tagged `bb`, with no TYPE prefix** — `bb49`,
  `bb105`. Two separate decisions, and only the second was ever rejected. The
  single namespace tag ("bulletin board") exists because an id has to survive
  being written in a sentence: `bb49` is unmistakably this board's object, `49`
  is any number at all — and this channel is mostly sentences. A per-kind
  vocabulary (`node-7`, `tab-3`) stays rejected: it is a closed set in a system
  where agents invent new object kinds, so it gets guessed ad hoc, and the kind
  is already implied by where the object sits. Numbers were never renumbered in
  the migration (`49` → `bb49`), so older references still resolve. Bare and
  legacy `n49` ids are still *read* everywhere — every parser uses
  `/^[a-z]*(\d+)$/` — only writes carry the tag. Form *field* ids stay semantic.
- **An id is enough coming FROM the human, and not enough going TO them.** The
  asymmetry is the point and it is easy to get backwards. They say `bb32` and you
  are fine — you read `board.json` and know exactly what it is. You say `bb32` and
  they may have nothing: they cannot grep the board from memory, and an id from
  yesterday, or from before a context clear, is a meaningless token they now have
  to go and look up. So **name the thing and put the id beside it** — "the
  Migration review tab (`bb32`)", not "the button in bb32". The id is a HANDLE,
  not a NAME. Recorded on 2026-08-24 because I had just written "press the button
  in bb32" and the human's next message was "what is bb32?" — the rule was earned,
  not deduced. It is the same instinct as never writing "as we agreed in bb42" in
  a commit message; only the window differs, minutes in conversation against
  forever in a commit.
- **Colours only from `app.css` tokens**, single dark theme from FireFly Pro's
  neutral-black family. No hex in any view. Text is pinned to WCAG AAA because
  most type here is small.
- **The periwinkle token is `--agent`, renamed from `--claude`** on 2026-08-24.
  It is not only a CSS variable: the same word is a NAME agents write into data,
  in three separate vocabularies — a `ui` node's `tone`, a `markup` mark's
  `color`, and a `diagram` mermaid `classDef`. All three moved together.
  **A clean break, no alias**, on the human's call: `claude` resolves to nothing
  anywhere now. The accepted consequence, recorded so nobody treats it as a bug —
  **a board in another project that used the old name loses that colour silently**,
  falling back to the default, because `markup`'s `colorVar` builds `var(--<token>)`
  from whatever it is given and `ui`'s `toneOf` ignores a tone it does not know.
  **The visibility fix was then built** rather than left as a fallback: the
  palette each renderer accepts is declared (`tones` in `ui.spec.json`, `colors`
  in `markup.spec.json`), the renderers build their tone map and swatch row FROM
  that declaration, and `-apply` warns when a write names a colour the board does
  not have. So the old word now produces a warning naming the available ones,
  which is the honest version of a rename: the break is clean AND it says so.
  The one place `claude` is still matched is `views/chat.js`, and deliberately:
  that is an ACTOR name, so transcripts stamped `by: "claude"` before the rename
  keep their distinct colour. It reads history; it does not endorse the name.
- **`html` tabs are sandboxed with `connect-src 'none'`.** The server has no auth,
  so anything that can reach it can rewrite the board — blocking network egress
  from the frame is what actually contains it. Do not relax that to "just fetch".
- **Per-viewer UI state never goes in `board.json`** — selection, zoom, collapsed
  blocks, marks-hidden, chat drafts.

## How the work has actually been organised

- **A Haiku subagent keeps `bb71`.** It owns that one tab, writes with
  `-by "agent-scribe"`, and never touches code. The main session ships and then
  messages it with the card ids and the notes to set. Worth continuing: it keeps
  the queue honest without the implementing session stopping to bookkeep.
- **It stamps a heartbeat** on `bb71.state.heartbeat` — `working` when it starts,
  `idle` when it finishes, with a real UTC timestamp both times. The strip pulses
  under 90s, then ages, then goes dashed-grey after ten minutes whatever the phase
  claims. **A subagent only exists while it runs**, so "last seen 16m ago" is the
  truth between messages, not a bug — the human asked about exactly this. Do not
  add a cron to make the dot green; they scored that idea 1 out of 5.
- **A subagent cannot be resumed from a new session.** Spawn a fresh one with the
  same brief (own `bb71` only, `-by agent-scribe`, heartbeat first and last).
- **More than one session has written here.** Two cards (`bb144`, `bb145`) were
  authored by a different session stamped `agent-research`. `./board -journal` and
  the Trace tab are how you tell who did what; `lastEditedBy` only names the last.
- **The human uses the gate for real approvals.** Both commits were authorised
  there, and they tested `undo` on one. Ask through `bb128` rather than assuming —
  and `./board -wait -for "answer bb128"` if you need the answer before continuing.

## Nothing is open — and the three things that look like they are

**There is no backlog.** `bb71` is empty, `bb128` has no pending decisions, no tab
has a `pendingRemoval`, no session is waiting, and the tree is clean. If you are
resuming and looking for the next task, there isn't one queued — ask the human.

Everything below is CLOSED work, kept because each one looks open until you read
why it isn't:

- **Phase 4 of the capability manifest is DONE, but not as written.** The plan was
  a DOM sweep: collect every `button[title]` and assert it appears in that type's
  `gestures`. Measured before building it, on the dag tab that is 23 candidate
  titles — 17 of them tab-strip chrome — to surface about 4 real gaps. A check with
  that ratio gets muted, and a muted check is worse than none. So the fuzziness was
  removed at the source instead (see "Controls are declared, not described" below),
  which left three static checks and one advisory. **Do not resurrect the sweep.**
- **`bb132` (What next)**: `ui-more` scored 5 and is done; `cron` scored 1 and is
  refused as policy (see the UI-may-not-start-a-session decision). `commit` and
  `harden` were mine at 5 and are done. The `diff` option was REMOVED from the
  ballot, not just zeroed — an option on a ballot is an open question, and that
  one is closed.
- **The `--claude` to `--agent` rename is complete and its fallback is built.** The
  clean break was the human's call; the "make an unknown colour visible" fix that
  was offered as a contingency exists now, so there is nothing held in reserve.

## Gotchas that cost real time

- **Assets are compiled into the binary.** After editing `views/`, `app.css`,
  `board.html` or `assets/`, run `./restart.sh -force` or use `-dev`, or your
  change appears to do nothing. The restart is still yours to run; only the
  browser-side reload is automatic now.
- **An open page now reloads itself when its code changes** (`reload.go`). The
  server hands every SSE client a signature of the UI it is serving, as the first
  frame on connect, so a `-force` restart is self-healing: the stream drops, the
  browser reconnects on its own, the signature does not match what the page
  loaded, the page reloads. In `-dev` the watcher pushes the new signature as you
  save. A CSS-only change re-links `app.css` instead of reloading, keeping scroll
  and selection, and a reload waits for `blur` if focus is in an editable — losing
  a sentence to a stylesheet edit would be worse than the staleness. So do NOT
  tell the user to run "Developer: Reload Webviews" any more. It is still the
  manual fallback for the one case left: a page that could not reconnect (server
  down when the stream dropped), or `?nosse=1`, which has no stream by design.
- **Headless screenshots do not reliably paint iframe content.** Verify an `html`
  tab by shooting `/tab/<id>/html` directly. And `--virtual-time-budget` starves
  cross-process `postMessage`, so frame auto-sizing looks broken under it when it
  is fine in a real browser.
- **Snap-confined chromium cannot write outside `$HOME`** — that is why
  `test/shot.sh` writes into `.shots/` rather than `/tmp`.
- **`pkill -f "node server.js"` matches its own shell** and kills the caller. Kill
  by pid from the instance file.
- **Grepping `--dump-dom` output matches the page's own inline script.** Several
  "failures" were my own source text. Assert on rendered DOM, and strip
  comments/strings before grepping source for calls. Bitten again on 2026-08-23:
  a read-only assertion that counted closing `</div>`s slid into the shell's HELP
  table, which contains the same `▲▼` glyphs it was asserting were absent — and the
  obvious fix (match `<section …data-view>`) UNDER-captured, because a kanban
  column is a `<section>` too. Assert on structural counts (`class="card"` vs
  `class="id-chip"`), not on container extraction.
- **Look at a screenshot before claiming a visual change works.** A DAG that never
  auto-fit and mermaid labels clipped by a font-weight override both passed every
  DOM and colour assertion and were obvious in a picture.
- **A CLI warning can only reach the actor who runs the CLI.** Obvious once said,
  and it killed a mechanism I had almost built: a `-apply` warning on a `gate`
  verdict with no reason would have fired only on an AGENT's write, while the human
  records verdicts in the BROWSER, where no terminal is listening. If the human
  does it in the UI, the affordance belongs in the UI. (That is why decided rows
  gained "add why" instead.)
- **`set -e` aborts a subshell the moment a command inside it fails**, so
  `"$(cmd; echo $?)"` reads EMPTY rather than the exit code, and an assertion built
  that way is silently vacuous — mine passed for one run while testing nothing. Use
  `"$(cmd && echo 0 || echo 1)"`.
- **`./test/smoke.sh` pokes the board**, because poking is what it tests — so it
  releases any session genuinely blocked on `-wait`. Don't run the suite while an
  agent is waiting for something that matters. It notes what it released rather
  than failing, and measures from that baseline.
- **Do not run `./test/smoke.sh` twice in one shell call, and never run
  `./restart.sh` in the foreground of a call that might time out.** The suite takes
  ~50s, so two runs exceed a 2-minute tool timeout — and when the call is killed,
  it takes the backgrounded server with it (exit 143, board down, three times in
  one session). Start the server detached and run the suite once per call.
- **A headless screenshot needs `?nosse=1`.** The SSE stream never closes, so
  chromium never reaches network-idle and writes no file at all (exit 2, no
  error message). `test/shot.sh` already appends it; a hand-rolled chromium
  command does not.
- **`frame-ancestors` is checked against the WHOLE ancestor chain**, not the
  immediate parent. `htmlTabCSP` said `'self'`, and the board is normally viewed
  inside VS Code's Simple Browser, which renders it in a `vscode-webview://`
  document — so every html tab came up **blank in the docked browser** while
  `board.html` (no framing header) loaded fine. Reproduced headlessly with a
  cross-origin wrapper: the nested case is refused exactly like the direct one,
  and Chromium reports the blocked frame's ORIGIN, which makes it look as though
  the shell were the thing being blocked. The list is now
  `'self' vscode-webview: vscode-file: https://*.vscode-cdn.net` with the
  reasoning in `htmltab.go`. **Do not tighten it back**: frame-ancestors was never
  the containment — `connect-src 'none'` plus `sandbox="allow-scripts"` without
  `allow-same-origin` is. If another host still shows a blank tab, its webview
  console names the origin; add that origin rather than widening to `*`.
- **A literal `<\/script>` inside a widget swallows the entire script.** The
  escape is only correct when the closing tag sits *inside a JS string literal*
  (`'<\/script>'` in JS source evaluates to `</script>`). Written into raw HTML —
  which is what happens when you build the widget in a Python raw string — the
  browser never sees the closing tag, the script block runs to the end of the
  document, and **nothing executes**. The static markup still renders, so it looks
  like a styling problem rather than a dead script. The suite now asserts a real
  `</script>` and the absence of the escaped form.
- **A CLI flag in a doc is a claim, and this file got one wrong.** The resume
  section said `./board -journal -l 20`; the flag is `-limit`, so the third
  command a post-context-clear session runs exited 2. Nothing tests the commands
  in this file — if you write one here, run it.
- **`-apply` succeeding is not evidence that anything renders.** It prints
  `applied` and exits 0 for a document that draws an empty box; `ui` is the worst
  offender, because an unknown component type shows a marker but an unknown PROP
  shows nothing at all. So: **read the stderr warnings, then shoot the tab and
  look at the picture.** Three sessions in a row ended with "it's ready" followed
  by the human finding it wrong — that is what the write-path checks above are
  for, and they still cannot see a layout that is legal and unreadable.
- **A warning on a deliberately-invalid example is a true positive.** `bb133`'s
  "Unknown" panel contains a `sparkline` on purpose, to demonstrate the
  unknown-component marker, so every write touching that tab warns about it. That
  is the checker working. Do not "fix" it by deleting the demonstration, and do
  not add a suppression mechanism for one node.
- **"Was the key found" and "is the value non-nil" are different questions.** The
  bind checker asked the second and flagged `demo.n` — an empty number field,
  initialised to JSON `null` on purpose — as a broken path, on the first real tree
  it ran against. A checker that calls correct state a mistake is worse than no
  checker: it is the noise that teaches people to skip stderr, which is where the
  real warnings are.
- **A component demonstrated only in the case that works is untested.** `bb133`'s
  `kv` used literal values, so nothing on the board ever exercised the bound case
  — which is how `kv` shipped unable to render a `{bind}`, the thing it is mainly
  for. The gallery now has both, and `test/smoke.html` asserts the resolved text.
- **`test/` is embedded too.** `//go:embed … test` — so editing `test/smoke.html`
  and re-running the suite tests the OLD copy, silently, and a new probe just
  never appears in the log. Rebuild before running the suite, not after.
- **`make caps` builds twice, and neither build is redundant.** `views/` is
  embedded, so the first binary emits `controls.generated.js` from the current
  specs and the second embeds the module it just wrote. Drop one and the server
  serves the previous controls while your spec edit appears to do nothing.
- **A mechanical refactor is exactly where hoisting bites.** Turning markup's
  `function makeIconBtn` into a `const` arrow broke the whole tab — it is called
  from the toolbar setup ABOVE its own definition, and only the declaration form
  hoists. "Cannot access 'makeIconBtn' before initialization", from an edit that
  looks like pure substitution. Caught by the suite, which is the argument for
  running it after even the boring changes.
- **The help panel CAN be screenshotted — `#help` opens it.** It has since
  `47552e1`, with a comment on the line saying so. I asserted the opposite,
  shipped the Buttons section unverified on that basis, and the human found it
  broken on first sight. The lesson is not about the panel: **an "I can't verify
  this" claim needs a grep before it is made**, because it is the one kind of
  claim that licenses skipping the check. `./test/shot.sh bb22#help` now shoots
  it.

## Verified, and what is not

**Everything from the last two days is confirmed by the human**, 2026-08-23: all
36 rows of `bb111` ticked, covering every renderer shipped (`table`, `gate`, `log`,
`trace`, `vote`, `ui`), the notify channel and its predicates, the journal, the
capability manifest, the 409 merge, uploads, deep links, export, right-click menus,
`@mentions` and read-state, the help panel and its Escape, the heartbeat strip, the
inline editors, the mark ids and list alignment, image rename/remove, panel memory,
and the portability claim. Do not re-verify these; do not describe them as
unproven.

**html tabs render in the docked Simple Browser** — confirmed by the human on
2026-08-23, who then drew on the sketch pad. `vscode-webview:` was the right
origin, and their stroke is in `bb72`'s `state.data`: 141 normalised points,
which proves the whole bridge round trip (canvas → `board.set()` → postMessage →
parent → compare-and-set → disk) as a side effect.

**The 2026-08-24 work is machine-verified, not human-confirmed.** That now covers
two batches: the four write-path defects, and the four-commit control series. For
the latter, everything is asserted by the suite and by screenshots of dag, table,
gate, vote and markup reading back correct, including the help panel's Buttons section
(`./test/shot.sh bb22#help`). That section shipped broken once — its labels sat
in a 1.2ch column meant for a `·` bullet and printed through their own
descriptions — because I claimed it was unverifiable instead of checking whether
it was.

**The 2026-08-24 write-path work specifically.** Be precise about
which, because they are different claims. Asserted by the suite and by me looking
at a screenshot: the version stamp and its warning; all seven `ui`/`stack` write
warnings, plus the negative case (a `field`'s write path must NOT warn); the block
html path, its containment, and its error messages; the `kv` fix.

**An `html` block inside a `stack` round-trips its state — confirmed by the human
on 2026-08-25.** They pressed the tick button in the Migration review tab
(`bb32`), and `blocks[].state.data` holds `{"ticks": 1}` with the journal showing
`human` as the writer. That is the whole nested path proven end to end: click →
`board.set()` → postMessage → parent → `ctxForBlock`'s live state getter →
`blocks[].state.data` → compare-and-set → disk. Worth recording WHY it needed a
human: the block's write path was the part I expected to be broken when fixing the
blank frame, and it turned out to need no change at all — the defect was one
lookup in `serveTabHTML`. A prediction that something is fine is not evidence that
it is, which is why this stayed listed as a gap until someone clicked it.

**The rename and what followed it** (`--claude` to `--agent`, the declared
palettes, `board.html`'s buttons, controls-as-a-list) are asserted by the suite AND
looked at, because a colour that stops resolving renders as NO colour and no DOM
assertion catches that. Confirmed by eye: the theme probe reports `--agent`
`#a7adf4`; the `tone: agent` notice, the change banner, the chat author palette
and the READ-ONLY badge all come up periwinkle; markup's five swatches still draw,
which they would not if the generated palette failed to load; and the help panel
lists markup's twelve controls in toolbar order. The tab strip was the risky part
of routing `board.html` through the helper, so it was screenshotted rather than
trusted to the suite.

What is still genuinely unproven is first-build residue, never touched since: the
DAG delete-confirm modal and inline rename by double-click; markup's move and
resize tools and its bulk-recolour and clear-marks modals.

Two earlier entries are now moot rather than unproven: the `html` drag sorter's
persistence path went with `bb31`/`bb44` when the human answered those removal
requests, and the browser half of auto-reload is confirmed indirectly but firmly —
they reported UI bugs in code that had shipped minutes earlier, which can only
reach an open page through the self-reload.


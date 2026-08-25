---
name: aboard
description: Show the user something on this project's shared board — a diagram, dependency graph, kanban, chart, annotated screenshot, question form, a channel for talking to another agent, or a bespoke HTML widget — and read back what they changed. Tabs are data you create and name, not a fixed set. Also covers writing aboard.json safely when a board is live or another session is editing it, and what the board is for: a local, non-authoritative channel whose conclusions must be promoted into committed artifacts.
when_to_use: Whenever an explanation would land better as a picture or an interactive thing than a paragraph; when a plan or dependency set has structure worth seeing; when you need structured input rather than asking several questions in prose; when you want the user to point at part of an image; when you need to coordinate with another agent session somewhere the user can watch; when the user says "show me", "draw this", "put it on the board", "make me a tab", "what does the board say", "I moved things around", or asks you to react to their edits; before editing aboard.json for any reason; and whenever another Claude Code session may be editing the same project's board.
---

# The shared board

A local server in this project serves a board that the user and you both edit.
`aboard.json` on disk is the single source of truth. You read it with `Read`, write
it with `aboard apply`; the user works in a browser tab docked inside VS Code.

**Tabs are data, not a fixed menu.** A tab is a name you choose, a `type` that
picks a renderer, and its own state. Open one for whatever the moment needs and
remove it when it stops earning its place. The types below are capabilities, not
a list of tabs that exist.

## What the board is FOR — and what it must never be

**A local, persistent, non-authoritative channel.** Read those three words
separately, because they are three different claims and only two of them are
obvious:

- **Local.** `aboard.json` is NOT committed in a normal project. The port is
  derived from the checkout path, `.aboard/` and `uploads/` are per-machine, and
  several developers on one repo each have their own board with their own agents.
  Committing it means a whole-file JSON conflict on every merge, in a blob nobody
  can review, over a conversation that was never theirs. In a real project add
  `aboard.json`, `.aboard/`, `uploads/` to `.gitignore`. (The board's OWN repo is
  the exception: there, the board is the product.)
- **Persistent.** Not versioned is not the same as scratch. A gate request waiting
  on the human, a form half answered, a session parked on `aboard wait` — those must
  survive a restart and a week away. Do not treat the file as disposable.
- **Non-authoritative.** If the board and a committed document disagree, **the
  document wins.** The board is where a thing is worked out; the repo is where it
  lands.

Its job is bandwidth, not storage: a picture, a form, a gate and an approval queue
are better interfaces to a decision than a wall of terminal prose, and they are
what make human-in-the-loop bearable rather than exhausting. That is the whole
value. Nothing about that requires the exchange to be preserved.

### Three tiers, and matching the thing to its lifetime

Most of what happens on the board is NOT destined for the repo, and an
indiscriminate "write it all down" rule produces the opposite failure: a project
full of "which of these three?" transcripts and half-decisions that nobody reads
and that go stale. A stale document misleads, which is worse than a missing one.

So there are three places a thing can live, and the skill is choosing correctly
between them:

| tier | lifetime | what belongs there |
|---|---|---|
| **the agent's context** | dies at a context clear | the reasoning in flight |
| **the board** | survives clears, restarts, a week away — local, unversioned | the exchange itself |
| **the repo** | survives everything, shared, reviewed | what someone else would be WRONG without |

**The board is the middle tier, and that is the point of it.** It is working memory
that outlives your context: a design you are getting feedback on, a diagram you are
checking against their mental model, a "pick one of these three pending tasks", a
screenshot they are pointing at, a form half answered. None of that belongs in a
spec document. Its value is consumed by the exchange — it changes what you build
next, and then it is spent. Let it go, or clear it, without ceremony.

The discriminator, when you are unsure:

> **Would a future session, or another developer, be wrong without this?**

- **Yes** → it belongs in the project's own documents. A rule with a reason
  ("never trigger agent sessions from the UI", "the diff renderer is rejected,
  because asking becomes cheap") outlives everything, and a session that does not
  have it will re-derive the idea and propose it again.
- **No, but I need it to finish this** → the board. Say so in the tab's `note`, so
  a session arriving after a context clear knows the tab is live work rather than
  a leftover.
- **Neither** → spend it. A clarifying question that only changed the next hour is
  not documentation. Do not archive it, do not summarise it into a file nobody
  asked for.

A preference expressed once is transient. A decision with a reason is durable.
That difference is usually the whole judgement.

### Where a promoted thing goes — find it, do not invent it

**Every project keeps its decisions somewhere already. Go and look.** Likely homes,
roughly in order: an ADR directory (`docs/adr/`, `docs/decisions/`), a spec or
design document for the area you are working in, `ARCHITECTURE.md`,
`DECISIONS.md`, `CONTRIBUTING.md`, `CLAUDE.md` if the project has one, or the
commit message and PR description of the change that acts on the decision.

Two rules about the target:

- **Prefer the document the decision is ABOUT.** A decision about the cutover
  belongs in the cutover spec, not in a general decisions file, because that is
  where the next person reads before touching it.
- **Do not create a new decisions file when the project has one**, and do not
  create one at all without asking. A second place to look is worse than an
  imperfect first place — and `CLAUDE.md` is not the answer everywhere; in most
  projects it does not exist, and the specs live elsewhere entirely.

### The boundary — when to promote, decided per project

You cannot know in advance which exchanges will turn out durable, so do not try to
notice continuously. Promote at a **boundary**: a named moment where you ask, once,
*"did anything here become a rule?"*

Which boundary is the project's to choose, not the skill's:

- **the commit that acts on it** — natural where work lands in small commits, and
  the message is already answering "what and why";
- **before a tab is cleared or repurposed** — natural where the board carries a
  long-running exchange;
- **the end of a work session, or a PR description** — natural where changes are
  batched;
- **when a spec document is next edited** — natural in spec-led projects.

**Establish which one this project uses, and record that where the project records
decisions.** Then honour it. If nobody has decided, ask — it is a one-line question
and it prevents both failure modes at once.

Two things make a late promotion cheap enough to actually happen.

**The text is one command away.** `aboard export <tab-id-or-key>` prints the tab
as markdown for pasting into whatever document the project uses — decisions with
their reasons, answers beside their questions, a node tree, a chat transcript.
`-format csv` for rows. It reads `aboard.json` from disk, so it needs no running
server. Adapt what it gives you; do not paste it whole into a spec, because a
board tab and a document have different jobs.

**And the reason is still there to copy.** So capture the reason in the structure at the moment of the
exchange — the `gate` verdict's `reason`, a `vote` option's `comments`, the tab
`note`, the `chat` message where they explained it. The decision usually survives
on its own; the reason is what evaporates, and it is the half that stops the
argument recurring.

When you find a verdict with no reason and it looks durable, **ask for it and have
them write it on the decision** — a `gate` row takes a reason after the fact, and
records that it was added late. Do not invent the reason on their behalf and do not
promote a naked decision as though it had one: "allow" with no why is exactly what
gets re-litigated. And weigh a late reason as what it is — reconstructed, not
recorded — which is why the export marks it.

### Which comes first, the board or the document

**Put the cheapest rejectable thing in front of them first.** That is the whole
rule, and it decides the order without you having to pick a mode.

Three questions, in this order:

1. **What is the expensive commitment here?** The spec, the schema, the migration,
   the refactor — whatever costs real effort to produce.
2. **What assumption does it rest on that they could overturn?** Usually the
   *approach*, sometimes a constraint or a priority. Not the details; the thing
   that makes the whole artifact the wrong artifact if it is wrong.
3. **Put THAT on the board, in the cheapest form that can carry a rejection** —
   three bullets and a pick-one, a diagram, a `form` with the two open questions,
   a `gate` request. Then pay for the artifact.

The distinction that does the work: **what goes up first is the decision the
document depends on, not a draft of the document.** "Event-sourced or CRUD — here
is the trade-off in four lines" costs nothing to reject. "Here is my architecture
spec, thoughts?" costs both of you, and costs more than the writing:

- an unagreed document **anchors** them to your framing, so they end up editing
  your structure instead of stating their own;
- and once it exists, **sunk cost** pulls both of you toward keeping it — you
  defend a shape you only chose because something had to be chosen.

That is why "we can always throw it away and rewrite" understates the damage.

**Document first is a derived case, not a different mode.** When the document is
cheap enough to throw away, it IS the cheapest rejectable thing, so write it: a
short design note, a spec where the writing is how you work out the shape, or a
document that already exists and only needs confirming. The expense is the test,
not the format. A one-page note: write it. Forty pages of architecture: agree the
approach first.

Rejected as a test: *"has a shared understanding been reached?"* That is a fact
about their head, and asking yourself to judge it produces confident guesses. The
question above is about **what you are most likely to be wrong about and what it
would cost** — your own uncertainty and your own effort, both of which you can
actually see.

Two more things, whichever order you end up in:

- **Promotion is a rewrite, not an export.** A diagram you argued with carries
  rejected branches, variants and question marks; committed as documentation, the
  next reader cannot tell what was decided from what was merely considered.
  `aboard export` gives you the material, not the document.
- **When a document becomes the record, demote the tab**: clear it, or set its
  `note` to say it is superseded and by what. An editable, authoritative-looking
  working copy sitting beside the committed truth, with nothing marking which is
  which, is the shadow-record failure created on purpose.

Keep the argument as well as the outcome only when a rejected option is tempting
enough that someone will propose it again — the same instinct as keeping the
reason with a decision, and why "alternatives considered" earns its place in some
documents and is padding in others.

### The failure it guards against, in both directions

The board LOOKS authoritative — things have ids, an agent maintains it, there is a
heartbeat and a change banner — so both sides start treating it as the record, and
then it is cleared. Watch for:

- a decision whose only trace is a `gate` verdict → put the outcome in the commit
  that acts on it;
- a plan the human corrected by dragging nodes → restate the corrected model in
  prose where the code can see it;
- "as we agreed in bb42" in a commit message or a PR → **never**. That id means
  nothing to anyone else, or to you next month. Cite the artifact, not the tab.

And in the other direction, just as real: do not turn every exchange into a file.
If you find yourself writing a document whose only reader would be you, five
minutes ago, you are promoting something that should have been spent.

Two failure modes to watch for in yourself while the work is still moving, one per
direction:

- **Sprawl** — exchanges accumulating on the board, nothing landing anywhere. The
  tell is a tab you keep adding to without ever asking what it settled.
- **False authority** — a confident document about something nobody agreed to. The
  tell is the human editing your structure rather than telling you their own, or
  correcting details in a shape they never chose.

### What follows from it

- **Assume nothing exists.** Find a tab by `key` and upsert it; tolerate an empty
  board. A board can be cleared between two turns of the same task.
- **Do not build a shadow tracker.** If the project has issues, a `TODO`, or a doc
  that already holds the work list, the board reflects it — it does not replace
  it.
- **To show someone else**, export the tab (markdown, CSV, SVG from its
  right-click menu) and commit that. A genuinely shared board would need auth and
  hosting, and this server deliberately has neither.
- **The journal is local too.** `.aboard/journal.jsonl` tells you who changed what
  on THIS machine. It is not a project audit trail; do not cite it as one.

## Is it running?

```sh
aboard status
```

Running (use it), stale record, or nothing (`./restart.sh`). It also prints a
`caps` line: the hash of what this board can actually do. **If it warns that the
skill reference was generated for a different hash, this skill is describing a
board that no longer exists** — run `aboard capabilities dag` (or any type) for
the truth, and `make caps` to regenerate
[references/reference.generated.md](references/reference.generated.md).

`aboard capabilities` needs no running server, and answers on a fresh checkout:
every type, every state field it actually reads, every gesture, every endpoint,
every flag. Reach for it when you are about to guess at a field name — and note
that `aboard apply` warns on stderr when a write sets state no renderer reads,
which is the failure that otherwise looks like success.

**Never assume a port** — it is derived per project. Take the URL from `aboard status` or
`.aboard/instance.json`, and give it to the user the first time:

> Board is at http://localhost:46624 — `Ctrl/Cmd+Shift+P` → "Simple Browser: Show" → paste that URL.

## Writing

**Never `Edit` or `Write` `aboard.json` while a board is running.** Those bypass
compare-and-set, so a concurrent change from the browser or another session is
silently destroyed. Instead:

```sh
aboard apply --by "agent-1" < /tmp/next-aboard.json
```

Read `aboard.json`, build the whole new document, apply it. `aboard apply` uses the
`updatedAt` inside the submitted document as its base, so read-edit-apply is safe
with no extra bookkeeping. A refusal means someone got there first — re-read, redo
the edit, apply again. Do not fall back to `Edit`.

**Asking is only half a question.** If you need the answer before you can go on,
block for it instead of polling — `aboard wait --by "agent-1" -timeout 15m`.
The board's header then shows *notify agent-1* with a lit dot, and the human
pressing it releases you (exit 0; exit 3 means nobody came). Say what you are
waiting for when you start waiting. Full contract in
[references/capabilities.md](references/capabilities.md#waiting-for-the-human).

`--by` is not decoration: it names you in `lastEditedBy` and on every tab you
touched, which is how the user and any other session tell who did what. Use
**`agent-1`, `agent-2`, `agent-<role>`** — not `claude`, which reads as a single
participant when there may be several. It is what the change banner shows.

## Choosing a type

| you want to | type | what the human can do in it |
|---|---|---|
| show a hierarchy the user can restructure | `dag` | drag to move, **drop onto another node to reparent**, double-click to rename, edit title/note/status/parent, add, delete, pan, zoom |
| track work through states | `kanban` | drag between columns (yours: any names, any count), reorder, rename inline, reparent, add, delete, `j/k/h/l` keys; `readOnly: true` makes it yours to write and theirs to read |
| assert a shape: sequence, state machine, ER, gantt, chart | `diagram` | mermaid, 23 types; edit source, hover a node for its key |
| get typed answers | `form` | range, select, checkbox, text, textarea; reset |
| have them point at part of an image | `markup` | several images side by side, **region / ellipse / pen / move / resize**, per-mark colours and notes, hide marks |
| talk to another agent where the user can see it | `chat` | send and interject; each speaker coloured |
| say something no structure fits | `notes` | free text, edit freely; `markdown: true` renders it with a Read/Edit toggle |
| put many typed rows in front of them | `table` | edit cells in place (text, number, select, checkbox, longtext), sort by header, add / duplicate / delete rows, copy CSV or markdown |
| get a yes, a no, or a "not like that" | `gate` | allow / deny / edit-then-allow, each with a reason; pair it with `-wait -for "answer <tab>"` |
| show output as it happens | `log` | follow, filter, ANSI colour — lines live in a sidecar file, fed by `aboard log <id>` |
| show who did what, when | `trace` | one lane per actor, a dot per write, click for detail; reads the journal, not `aboard.json` |
| let several participants score options | `vote` | click to score, click again to clear; a wide split is called out rather than averaged |
| lay something out without writing code | `ui` | a component tree drawn by trusted components — no iframe, no script; buttons record intent |
| build a bespoke interactive thing | `html` | anything you write — canvas, drag-and-drop, WebGL — sandboxed, no network |
| show several of the above together | `stack` | collapsible blocks top to bottom |

That table is a summary. **[references/capabilities.md](references/capabilities.md)
is the complete inventory** — every field of every type, every command, every
guarantee. Read it before concluding the board cannot do something.

Three rules of thumb: **`dag` when you want the shape argued with, `diagram` when
the shape is yours to assert**; **`ui` when the layout is ordinary and `html` when
the interaction itself is the point**; and **`table` the moment you find yourself
writing rows into `notes`.** And **`stack` whenever the answer is really
"look at this, then decide this"** — that is most of the time, and one composite
tab beats three tabs the user has to correlate.

**Say who is minding a tab you own.** On a `readOnly` kanban, set
`state.heartbeat = {by, at, phase: 'working'|'idle', note}` when you start and
finish. The strip pulses while `working` and under 90s old, and goes visibly stale
after ten minutes whatever the phase claims — so use real timestamps, or the
indicator lies, which is worse than not having one.

**Hand a decision back without asking them to type.** `state.actions:
[{id,label,intent}]` renders a button strip on any tab; a press appends to
`state.intents` and nothing executes. Read the intents next turn.

**How much room a view takes is your call, not the renderer's:** `state.height`
accepts any CSS length on `dag`, `chat`, `html` and `log`. Kanban columns are yours too —
`state.columns` takes any names, any count.

**Prefer `ui` over `html` whenever `ui` can express it.** A `ui` tree is drawn by
trusted components, so it cannot get the theme, the contrast or the type sizes
wrong; there is no iframe or CSP to reason about; and the next session can change
one node of it instead of reading a page of your JavaScript. Reports, summaries,
dashboards, small forms, stats, comparison tables: `ui`.

**Ask for the component's props rather than guessing them** — `./aboard
-capabilities ui` lists all 25 with what each one reads, including the fixed item
shapes (`kv` takes `pairs[{key, value}]`, not `items[{k, v}]`). Guessing is the
expensive mistake here: an unknown component TYPE draws a visible marker, but an
unknown PROP draws nothing at all, so the tab renders as a titled card wrapping an
empty box and looks like a styling problem. `aboard apply` warns about both now, and
about a `{bind}` that resolves nowhere.

Reach for `html` only when the INTERACTION is the point and no arrangement of
components gets there — a canvas, a drag-and-drop sorter, a simulation, WebGL. It
gets no network access, so it cannot fetch or exfiltrate; it persists state by
calling `aboard.set()`. If you are reaching for `html` to lay out text and numbers,
use `ui`. An `html` block inside a `stack` works like any other block.

## What travels between projects

The renderers and their spec files are compiled into the binary, so every gesture
and every state field works — and documents itself via `aboard capabilities` —
in any project you drop the binary into, with no skill and no server. `aboard.json`
is that project's content and travels with nothing. This skill travels only if
copied in, which is why `aboard status` warns when its generated half no longer
matches the binary.

## Ids

**Never reuse an id, and never compute one from "highest in this container + 1".**
Take the board-wide counter:

```js
const id = 'bb' + doc.nextId; doc.nextId += 1;
```

Ids are how you and the user refer to things across turns. A reused id silently
re-points an instruction at a different object — which is why the counter is
board-wide and the server refuses to let it regress. An id is unique board-wide,
so it never needs qualifying by tab.

**Write `bb<n>`; read anything.** The `bb` tag ("bulletin board") is there so an
id survives being written in a sentence: `bb49` is unmistakably a board object
where `49` is any number at all, and most of what passes between you and the user
is sentences. Say `bb49`, not `49`, whenever you mean the object. Bare `49` and
legacy `n49` still parse everywhere, so old references keep working — the
migration prefixed ids without renumbering them.

There is still no TYPE prefix (`node-7`, `tab-3`): that is a closed vocabulary in
a system where you invent new kinds of object, so it would be guessed ad hoc and
stop meaning anything, and the kind is already implied by where the object sits.

### Ids do not travel in both directions

**An id is enough coming FROM the human. It is not enough going TO them.**

That asymmetry is real and it is not about politeness:

- **They say "bb32" → you are fine.** You can read `aboard.json` in a second and
  find out exactly what it is. Take the id and act.
- **You say "bb32" → they may have no idea.** They cannot grep the board from
  their head. An id they saw ten minutes ago is still live; an id from yesterday,
  or from before a context clear, is a meaningless token — and they will have to
  go and look it up, which is work you just handed them.

So **when you address the human, name the thing and put the id beside it**:

> the Migration review tab (`bb32`) — its html block still needs a click

not "press the button in bb32". Same for nodes, marks and cards: *"the 'auth'
node (`bb58`)"*, *"the mark on the login screen (`bb168`)"*.

The id still earns its place — it is unambiguous, it survives being pasted, and it
is what you both point at once they know which object it is. It is a HANDLE, not a
NAME, and a handle needs a name attached the first time it appears in a message,
and again after any real gap.

This is the same instinct as never writing "as we agreed in bb42" in a commit
message, and it fails the same way: a reference that means nothing to the reader
is not a reference. The difference is only how long the window lasts — minutes in
conversation, forever in a commit.

Form *field* ids stay semantic and author-chosen (`strategy`, `window`); the
form itself gets a generated id.

## Creating, naming, removing

Give a tab a `key` when it has an ongoing purpose (`"plan"`, `"review"`). Next
turn, find that key and update the same tab instead of opening a second one. Tab
sprawl is the failure mode here.

**Read `tab.note` before you act on a tab, and write one when you make it.** It is
free text saying what the tab is FOR — the intent that its contents cannot carry.
A kanban of eight cards does not say whether it is a wish list or a commitment; a
markup tab does not say what the human was hoping you would notice. The human can
write and edit it (a strip under the tab strip, or the tab's right-click menu, or
the field in the New tab dialog), so treat it as an instruction, not decoration.

**You cannot delete a tab.** A write that drops one has it restored with a
`pendingRemoval` request the user answers in the UI. So do not try to delete —
*ask*, by setting `pendingRemoval: { by, at, reason }` with a reason worth
reading, and tell the user you have asked.

**You cannot clear a `touched` marker.** That marker raises the dot on the tab
and the banner inside it, and only the user dismissing it takes it down. Do not
try; the server carries it forward regardless.

## Reading their edits

Read `aboard.json` and diff against what you last applied. Highest signal first:

- `nodes[].parent` changed — they restructured your hierarchy. Treat it as a
  correction to your model, not noise.
- `form.fields[].value` — their answers.
- `markup` regions and strokes, plus each mark's `note` — where they pointed.
- any `note` field — often a direct message to you. Read them all.
- `chat` messages with `by: "human"` — they interjected.
- `lastEditedBy: "human"` — there are edits you have not read.
- a tab with no `touched` that you marked earlier — they read and dismissed it.

Say what changed before acting on it:

> You moved "auth" under "platform" and set the downtime slider to 5 minutes — so I'll treat auth as a platform concern and plan for a 5-minute window.

## Reference

- [references/capabilities.md](references/capabilities.md) — **the full surface**:
  every command, endpoint, tab type, field, and what the human can do in each.
  Read this when you want to know what is possible, not just what you remember.
- [references/schema.md](references/schema.md) — `aboard.json` and every type's state.
- [references/recipes.md](references/recipes.md) — worked examples for each type.
- [references/multi-session.md](references/multi-session.md) — two sessions, ports, etiquette.

## Pitfalls

- **Assets are embedded in the binary.** After editing `views/*.js`, `app.css`,
  `aboard.html`, or `assets/`, run `./restart.sh -force`, or run `-dev` to serve
  from disk. Otherwise your change appears to do nothing.
- **`./restart.sh` on a healthy board does nothing by design.** It prints the URL,
  so a second session cannot kill the first one's server. `-force` when you mean it.
- **A new renderer type needs a line in the `TYPES` registry in `aboard.html`.**
  A tab whose `type` has no renderer shows a "no renderer" notice.
- **Pen strokes serialise as one `"x,y x,y"` string**, not nested arrays. One
  scribble as arrays was 75% of the file's lines.
- **Per-viewer UI state stays in the browser** — selection, zoom, collapsed
  blocks, marks-hidden. Never write it into `aboard.json`.
- **Look at a screenshot before claiming a visual change works.**
  `./test/shot.sh` then read the image. Several real bugs here passed every DOM
  and colour assertion and were obvious in a picture.
- **Render it and look before you say it is ready.** That applies to what you
  WRITE, not only to renderer code — a tab is a thing on someone's screen, and
  "I put it on the board" is a claim about how it looks. Three sessions in a row
  ended the same way: the agent said it was ready, the human looked, it was wrong.
  `ui` is where this bites, because it fails silently AND successfully: `aboard apply`
  prints `applied` and exits 0 whether the tree renders or draws an empty box.
  Read the stderr warnings (they now descend into a `ui` tree and into `stack`
  blocks, so a mistyped prop or a `{bind}` pointing nowhere is named at the write),
  then shoot the tab and read the picture. Neither step is optional, because the
  warnings cannot see a layout that is legal and still unreadable.

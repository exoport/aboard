# The state file

`.aboard/aboard.json` is the board: one JSON document that the browser and every agent
session read and write. This page is the essentials — the document's own fields, the id
invariant, and the shape of a tab.

> **The canonical, exhaustive description is the skill.**
> `.claude/skills/aboard/references/schema.md` carries every type's state, field by
> field, and `aboard capabilities <type>` prints what a renderer *actually reads*,
> generated from its own declaration. Where this page and those disagree, they are
> right: this one is written, theirs is generated or maintained beside the code.

## The document

```jsonc
{
  "version": 3,                          // server-managed; the schema the renderers read
  "rev": 41,                             // server-managed; the compare-and-set base
  "updatedAt": "2026-08-25T11:03:09Z",   // server-managed; when, for a human reading this
  "lastEditedBy": "agent-1",             // "human", or whatever --by was passed
  "nextId": 147,                         // server-managed; the id allocator
  "tabs": [ /* … */ ]
}
```

**Never set `version`, `rev`, `updatedAt`, `lastEditedBy` or `nextId` yourself.** The
server manages all five and stamps them on every accepted write. Leaving `rev` exactly as
you read it is what makes the conflict check work; a write replaces the whole document,
so always build from a fresh read.

`rev` is a counter, incremented once per accepted write, and it is the **compare-and-set
base** — see [`POST /aboard.json`](http-api.md#post-aboardjson). `updatedAt` used to be
that base and is not any more: it is a millisecond clock, and two writes inside one
millisecond produce the same string, so a base built from the first still matched after
the second had landed. Measured at 4 collisions in 60 sequential writes, each one a
provably stale write accepted with a `200`. `updatedAt` stays because it answers a
different question — *when* — and nothing keys off it now.

`version` is in that list because it once was not, and it cost a whole board: a schema
example showed `"version": 2` long after the board had moved to 3, an agent copied the
example it was reading, the write went through with exit 0, and the browser — which
refuses to render a version it does not know — showed the human a blank board one round
trip after being told it was ready. The server now stamps the field, and the write path
**warns** when a document names the wrong one, so the stale source still gets fixed. A
hand-written `version` is never right for longer than one schema change.

The minimum valid document is:

```json
{ "version": 3, "nextId": 1, "tabs": [] }
```

which is what `aboard init` writes.

## Ids

**Ids are board-wide monotonic, tagged `bb`, and never reused.** Allocate one by taking
`nextId`, using it, and incrementing:

```js
const id = 'bb' + doc.nextId; doc.nextId += 1;
```

Renderers used to allocate "highest suffix in this container + 1". Delete every mark on
an image and the next one was `r1` again; delete the last kanban card and the new one
took its id — so any instruction that referenced the old object silently pointed at a
different one. Since ids are how a human and an agent refer to things across turns, that
is a correctness bug, not a cosmetic one.

Two consequences of a single counter:

- **An id is unique board-wide, so it never needs qualifying by tab.** That is the only thing that works inside a `stack` tab holding two kanbans or two images, where the tab cannot disambiguate at all.
- **One namespace tag, no type prefix.** `bb` ("bulletin board") exists so an id survives being written in a sentence: `bb147` is unmistakably a board object where `147` is any number at all. It says nothing about kind, so it cannot be guessed wrong. A per-kind vocabulary (`node-7`, `tab-3`) is a closed set in a system where agents invent new kinds of object, so it gets guessed ad hoc and stops meaning anything — and the kind is already implied by where the object sits.

Ids are strings, so DOM attributes and map keys agree with the document. Bare (`147`) and
legacy (`n7`) ids are still *read* everywhere — every parser matches `^[a-z]*(\d+)$` —
only writes carry the tag. The server enforces the invariant: `nextId` never decreases
and is always above every numeric id present, so a hand-edited document still allocates
safely.

**Form field ids are the deliberate exception.** Those are semantic keys you choose
(`strategy`, `window`) and read answers back by — do not generate them. The form itself
still carries a generated `id`.

## A tab

```jsonc
{
  "id": "bb3",                // "bb<number>", continuing from doc.nextId
  "key": "architecture",      // OPTIONAL stable handle — find by this to update the same
                              // tab next turn instead of opening another
  "name": "Architecture",     // what the human sees
  "type": "diagram",          // picks the renderer
  "state": { /* type-specific */ },

  "note": "why this exists",  // OPTIONAL: what the tab is FOR — the agent's brief
                              // statement of the tab's purpose, which the human may
                              // edit. Read it before acting on the tab; it carries
                              // intent the contents cannot.

  "requests": [               // OPTIONAL: the human's notes TO an agent about this tab.
    { "id": "bb199",          // from the board allocator, like everything else
      "at": "2026-08-26T09:12:00Z",
      "by": "human",
      "text": "the arrow points the wrong way",
      "done": { "by": "agent-1", "at": "…", "note": "flipped it" } }
  ],

  "stateFrom": "bb1",         // OPTIONAL: render another tab's state with this type —
                              // a kanban and a dag over one dataset

  "touched": { "by": "agent-1", "at": "…", "note": "…" },      // server-set
  "pendingRemoval": { "by": "agent-1", "at": "…", "reason": "…" },
  "seen": { "human": "…", "agent-2": "…" }                     // per-actor read state
}
```

Five fields are not fully yours, and the server enforces that rather than trusting a
convention — see
[why the guarantees are server-enforced](../explanation/why-the-guarantees-are-server-enforced.md):

- **`touched`** is stamped on any tab an agent's write changed, and cleared only by the human dismissing it. An attempt to remove it in a write is ignored and the previous marker carried forward. It drives the dot on the tab.
- **`pendingRemoval`** is how a tab goes away. A write that simply drops a tab has it restored with this set; you may also set it deliberately, with a reason worth reading. Only the human's answer deletes or keeps.
- **`seen`** is per-actor read state. An actor may stamp its own key and nobody else's.
- **Chat acknowledgements** inside a `chat` tab's state are carried forward; a write cannot un-ack a message.
- **`requests`** are the human's notes to an agent, and the one thing on a tab that flows their way. An agent write may only *add* a `done` stamp to a request that already exists; anything else it does to the list — dropping it, editing the text, reordering, inventing one, clearing or replacing a stamp — is undone and the previous list restored, exactly as `touched` is. The `by` on a stamp is rewritten to the actual writer, and a missing `at` is stamped, so an attribution the human reads is never one nobody checked. Only their deleting the whole request makes it go away, which is also the only way a `done` is ever cleared.

  Read them from a terminal rather than by grepping the document: `aboard requests` lists
  what is pending, oldest first, naming the tab; `aboard status` prints the count; and
  `aboard requests done <id> --by agent-1 --note "…"` is the stamp. `aboard wait --for
  request` blocks until one exists or arrives — and answers at once if one is already
  waiting, because a note left an hour ago is not an event still to come.

## Per-type state

Every renderer declares what it reads in `pkg/aboard/web/views/<type>.spec.json`, and
that declaration is what the manifest reports. Ask the binary rather than a document:

```bash
aboard capabilities kanban     # one type: its state fields, controls, gestures, palettes
aboard capabilities            # all fifteen, plus routes and the command table
```

Two shapes are worth knowing before you read a spec:

- **`dag` and `kanban` share a node list.** `parent` and `status` are independent axes: kanban groups by `status`, the dag reads `parent`, and moving a card between columns never changes the tree. Two tabs — one `dag`, one `kanban` with `stateFrom` pointing at it — give both readings of one dataset.
- **`ui` and `stack` are trees, not flat bags.** A `ui` tab's state is a component tree from a declared catalog; a `stack` tab's state is a list of blocks, each with its own type and state. The write-path checker walks both.

## Writes are validated, and the validation warns

`aboard apply` writes on stderr when a document sets state no renderer reads: an unknown
component, an unknown prop on a known component, a wrong item shape, a bad block field, a
`{bind}` that resolves nowhere, a colour name the board does not have, or a `version`
this board does not write.

**None of them refuse the write by default.** A spec can lag its renderer, and a board
that rejected writes because its own documentation was behind would be worse than one
that documents late. Two flags opt out of that default: `apply --check` runs the checks
and posts nothing (no server need be running, and the document needs no `rev`), and
`apply --strict` turns any warning into a refusal — exit 1, nothing written — which is
the guard for a loop that must stop rather than ship a wrong tab.

Read the warnings either way: `apply` printing `applied` and exiting 0 is not evidence
that anything renders. The failure this catches is `ui` failing *silently and successfully* —
the human finding the empty panel before the agent that made it hears anything, which is
backwards, because the agent is the one still holding the context to fix it.

## See also

- [The `.aboard/` layout](layout.md) — where this file lives, and what lives beside it.
- [HTTP API](http-api.md) — the compare-and-set contract that writes it.
- [The capability manifest](capabilities.md) — the per-type declarations, and how to ask for them.
- [How aboard runs](../explanation/how-aboard-runs.md) — where this file sits in the loop, and why `apply` posts rather than writing it.

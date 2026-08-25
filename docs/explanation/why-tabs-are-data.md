# Why tabs are data

A tab is a **name**, a **`type`** that picks a renderer, and its **own `state`**. The
browser has no fixed list of tabs, and neither does the server: the state file carries a
`tabs` array, and whatever is in it is the board.

The types are **capabilities, not a menu of tabs that exist.** "Open a kanban" is not a
feature request; it is a write.

## What that buys

**An agent opens a board for whatever it needs to show.** A plan, a chart, a question, a
screenshot to point at, a conversation with another session, a bespoke widget — named for
the moment rather than chosen from five. The alternative design, where each tab is a
screen someone built, forces every new kind of explanation through a release.

**Tabs are cheap enough to spend.** That is what makes the board the middle tier of
[three](why-a-local-non-authoritative-channel.md): a tab is worth opening for one
exchange and removing when it stops earning its place. A tab that took a feature to create
would never be disposable, and a board of undisposable tabs becomes a shadow tracker.

**Two readings of one dataset.** A tab may set `stateFrom` to render another tab's state
with a different type — a `dag` and a `kanban` over the same nodes, where `parent` and
`status` are independent axes. That is a one-line write, not a synchronisation problem.

**Composition.** A `stack` tab holds several renderers top to bottom, so "look at this,
then decide that" is one tab instead of three the human has to correlate. That is usually
the right shape.

**Idempotence.** A tab can carry a `key` — a stable handle an agent finds it by — so the
next turn updates the same tab instead of opening another. Assume nothing exists: find by
key, upsert, tolerate an empty board.

## What a renderer costs

Adding a type is three declarations, and **all three are load-bearing**:

| declaration                                | omit it and…                                        |
| ------------------------------------------ | ---------------------------------------------------- |
| the type registry in the shell             | it mounts nowhere                                   |
| the module list in the browser suite       | it is tested nowhere                                |
| `views/<type>.spec.json`                   | no agent ever learns it exists, and the spec/mount parity check fails |

The third one is the interesting one. A renderer nobody can discover is a renderer nobody
uses, so the declaration is not paperwork — it is how the capability reaches the agents
that would reach for it. And because the manifest is generated from that declaration
rather than from prose, the documentation of a new type cannot be forgotten separately
from the type: see [why the manifest is declared](why-the-manifest-is-declared.md).

## The lines that make it work

**Ids are board-wide monotonic.** A single counter, so an id is unique across the whole
document and never needs qualifying by tab — which is the only thing that works inside a
`stack` holding two kanbans. Details in [the state file](../reference/state-file.md#ids).

**Per-viewer UI state never goes in the document.** Selection, zoom, collapsed blocks,
marks-hidden, chat drafts: those belong to one viewer's browser. Putting them in shared
state means one person's scroll position is a write everyone else's board reacts to.

**A tab carries a `note` — the human's words for what it is FOR.** Read it before acting
on the tab. A kanban of eight cards does not say whether it is a wish list or a
commitment; a screenshot does not say what the human was looking for. That intent cannot
be inferred from the contents, which is why there is a field for it.

**Nothing in the browser executes anything.** A `state.actions` button, a `gate` verdict
and a `ui` button all *record* — an intent, a decision — and the agent that asked acts on
it. Tabs being data is what makes that possible: an interface built from data cannot do
anything the data does not describe.

## See also

- [The state file](../reference/state-file.md) — the tab shape, field by field.
- [The capability manifest](../reference/capabilities.md) — how a type declares itself.
- [Why a local, non-authoritative channel](why-a-local-non-authoritative-channel.md) — why cheap-to-open matters more than it sounds.

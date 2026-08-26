# Why the guarantees are server-enforced

Five things an agent cannot do to a board, no matter what it writes:

1. **Delete a tab.** A write that drops one has the tab restored, carrying a removal *request* the human answers with *Keep* or *Remove tab*.
2. **Clear a `touched` marker.** That marker raises the dot on the tab and the banner inside it. Only the human dismissing it takes it down; a write that omits it has the previous marker carried forward.
3. **Un-acknowledge a chat message.** Once a session has acked a message, the human's edit/delete window on it has closed. Acks are carried forward.
4. **Clear another actor's read state.** An actor may stamp its own `seen` key and nobody else's.
5. **Write the human's requests.** A `requests` entry is the human asking for something to be fixed on that tab. An agent may only *add* a `done` stamp to one that already exists, under its own name — never create, edit, reorder, delete or un-stamp one.

They are enforced in the server, on every accepted write, rather than written down as
conventions for agents to honour.

## Why enforcement and not documentation

Each of these exists because **an agent that forgot would destroy the user's work or hide
its own tracks** — and both failures are silent. Nothing errors. The human just finds
their tab gone, or never finds out that something changed.

A convention is a request that the party with the most to gain from ignoring it should
not. Consider the second guarantee from the inside: an agent writing a document it read a
moment ago will naturally write back the whole thing, including a `touched` field it
never looked at. If that clears the marker, then **a later agent write hides an earlier
one** — not maliciously, just as a side effect of read-modify-write. No amount of prose
in a skill fixes a failure mode that is the default behaviour of the obvious
implementation.

The rule of thumb they share: *if forgetting it destroys something that cannot be
reconstructed, it is not a convention.* The human's attention is the scarcest resource in
this system, and four of the five guarantees are about not spending it silently.

## What each one buys

**Removal is a request, not an act.** Tabs are cheap and agents open them freely, so
agents will also want to tidy them away — and the human is the one who knows whether a
tab was still being used. Restoring a dropped tab with a `pendingRemoval` (its `by`, its
`at`, and a `reason` worth reading) turns a destructive action into a question that costs
one click to answer. Agents ask; the human decides. Set it deliberately, with a real
reason: a tab dropped from the array is restored with a generic one, which is worse for
the human than the sentence you would have written.

**The change marker belongs to the reader, not the writer.** "Has the human seen this?"
is a fact about the human. Only the human's dismissal can answer it, so only the human's
dismissal clears it.

**An ack closes a window.** In a `chat` tab the human can edit or delete a message until a
session has acted on it. Dropping an ack would reopen that window on something already
acted on — the transcript would then be editable after the fact, which is exactly the
property a transcript must not have.

**A request only travels one way.** Everything else on a board is an agent showing the
human something and reading back what they changed. A `requests` entry is the reverse —
they point at a tab and say "this is wrong" — and it is the only content on the board an
agent has no business authoring. Two failures were possible and only one of them needs
bad intent. The ordinary one is the same read-modify-write that used to eat `touched`: an
agent hands the whole document back without a field it never looked at, and the note is
gone with nothing to say it existed. The other is an agent *creating* one, which would
put words in the human's mouth in the one place they are entitled to assume the words are
theirs. The stamp is the exception because it is the only half an agent can answer: "has
this been dealt with" is a fact about the agent, and "do I still want this" is a fact
about the human, so each owns its half. And a stamp is never cleared by an agent — the
human deleting the whole note is how one goes away, which is also the only way a stamp is
ever removed.

**Read state is per actor.** `touched` answers "has the human looked", which is all one
marker can answer. With two sessions and a human on one board, one agent dismissing
something erased the signal another had left. Per-actor `seen` keys make "changed since I
last looked" answerable for everybody, and the may-only-stamp-your-own rule is what stops
the same collision happening one layer down.

## What the guarantees are not

**They are not authentication.** The server does not distinguish a browser write from an
agent write by any means stronger than the `__by` field the caller sends. A determined
client can claim to be the human. That is fine, because the threat model here is a
mistake, not an adversary: the server is loopback-only and unauthenticated by design, so
the guarantees exist to make the *easy* path safe, not to resist an attacker who is
already inside.

One consequence, worth stating: the CLI **refuses `--by human`**. The CLI is not the
human, and an agent that could claim to be would be able to clear its own change markers.
An absent actor defaults to `unknown`, which gets agent-level powers only — the safe end
of an ambiguity.

**They are not a permission system.** `kanban`'s `readOnly` is the neighbouring idea and
it is deliberately weaker: it removes editing affordances (a card that drags then snaps
back reads as a bug) and shows a badge saying an agent maintains this board. It shapes the
interface; it enforces nothing. The five guarantees are the opposite bargain — invisible
in the interface, absolute in the server.

## See also

- [The state file](../reference/state-file.md) — the fields these guarantees govern.
- [`aboard requests`](../reference/cli.md#aboard-requests) — reading and answering the fifth one from a terminal.
- [HTTP API](../reference/http-api.md) — where they are applied on the write path.
- [Why nothing in the UI starts a session](why-nothing-in-the-ui-starts-a-session.md) — the same instinct about who is allowed to act.

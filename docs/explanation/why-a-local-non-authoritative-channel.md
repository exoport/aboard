# Why a local, non-authoritative channel

The board looks authoritative. Things have ids, an agent maintains them, there is a
heartbeat and a change banner and a journal. That appearance is the danger, so this page
states what the board is FOR — and what it must never be.

It was argued out with a human over a long session, and every claim here corrected
something that had been got wrong first. Do not re-derive it from scratch.

## Three words, three separate claims

**A local, persistent, non-authoritative channel.** Read those separately, because only
two of them are obvious.

**Local.** The state file is not committed in a normal project. The port is derived from
the checkout path, `.aboard/` is per-machine, and several developers on one repo each
have their own board with their own agents. Committing it means a whole-file JSON
conflict on every merge, in a blob nobody can review, over a conversation that was never
theirs. In a real project, `.gitignore` the whole `.aboard/` directory —
`aboard init --gitignore` does it for you.

**Persistent.** Not versioned is not the same as scratch. A gate request waiting on the
human, a form half answered, a session parked on `aboard wait` — those must survive a
restart and a week away. Do not treat the file as disposable.

**Non-authoritative.** If the board and a committed document disagree, **the document
wins.** The board is where a thing is worked out; the repo is where it lands.

Its job is bandwidth, not storage: a picture, a form, a gate and an approval queue are
better interfaces to a decision than a wall of terminal prose, and they are what make
human-in-the-loop bearable rather than exhausting. That is the whole value. Nothing about
it requires the exchange to be preserved.

> **aboard's own repository ignores its board too.** The example tabs are a fixture
> compiled into the binary at `pkg/aboard/example/`, and `aboard init --example` seeds
> from them. A committed board would be a log of somebody's afternoon in a place
> reviewers read.

## Three tiers, matched by lifetime

Most of what happens on a board is not destined for the repo, and an indiscriminate
"write it all down" rule produces the opposite failure: a project full of "which of these
three?" transcripts and half-decisions nobody reads and that go stale. **A stale document
misleads, which is worse than a missing one.**

So there are three places a thing can live, and the skill is choosing correctly between
them:

| tier                   | lifetime                                                     | what belongs there                     |
| ---------------------- | ------------------------------------------------------------- | -------------------------------------- |
| **the agent's context**| dies at a context clear                                      | the reasoning in flight                |
| **the board**          | survives clears, restarts, a week away — local, unversioned  | the exchange itself                    |
| **the repo**           | survives everything, shared, reviewed                        | what someone else would be WRONG without |

**The board is the middle tier, and that is the point of it.** It is working memory that
outlives a context: a design being reviewed, a diagram checked against someone's mental
model, a "pick one of these three", a screenshot being pointed at, a form half answered.
None of that belongs in a spec document. Its value is consumed by the exchange — it
changes what gets built next, and then it is spent.

The discriminator, when you are unsure:

> **Would a future session, or another developer, be wrong without this?**

- **Yes** → it belongs in the project's own documents. A rule with a reason ("never trigger agent sessions from the UI"; "the diff renderer is rejected, because asking becomes cheap") outlives everything, and a session that does not have it will re-derive the idea and propose it again.
- **No, but I need it to finish this** → the board. Say so in the tab's `note`, so a session arriving after a context clear knows the tab is live work rather than a leftover.
- **Neither** → spend it. A clarifying question that only changed the next hour is not documentation. Do not archive it; do not summarise it into a file nobody asked for.

A preference expressed once is transient. A decision with a reason is durable. That
difference is usually the whole judgement.

## Where a promoted thing goes — find it, do not invent it

**Every project keeps its decisions somewhere already. Go and look.** Likely homes,
roughly in order: an ADR directory, a spec or design document for the area being worked
on, `ARCHITECTURE.md`, `DECISIONS.md`, `CONTRIBUTING.md`, a `CLAUDE.md` if the project
has one, or the commit message and PR description of the change that acts on the
decision.

Two rules about the target:

- **Prefer the document the decision is ABOUT.** A decision about the cutover belongs in the cutover spec, not in a general decisions file, because that is where the next person reads before touching it.
- **Do not create a new decisions file when the project has one**, and do not create one at all without asking. A second place to look is worse than an imperfect first place — and `CLAUDE.md` is not the answer everywhere; in most projects it does not exist and the specs live elsewhere entirely.

## The boundary — when to promote, decided per project

You cannot know in advance which exchanges will turn out durable, so do not try to notice
continuously. Promote at a **boundary**: a named moment where you ask, once, *"did
anything here become a rule?"*

Which boundary is the project's to choose, not the tool's:

- **the commit that acts on it** — natural where work lands in small commits, and the message is already answering "what and why";
- **before a tab is cleared or repurposed** — natural where the board carries a long-running exchange;
- **the end of a work session, or a PR description** — natural where changes are batched;
- **when a spec document is next edited** — natural in spec-led projects.

**Establish which one this project uses, and record that where the project records
decisions.** Then honour it. If nobody has decided, ask — it is a one-line question and it
prevents both failure modes at once.

Two things make a late promotion cheap enough to actually happen.

**The text is one command away.** `aboard export <tab|key>` prints the tab as markdown for
pasting into whatever document the project uses — decisions with their reasons, answers
beside their questions, a node tree, a chat transcript. `--format csv` for rows. It reads
the state file from disk, so it needs no running server.

**And the reason is still there to copy** — if it was captured in the structure at the
moment of the exchange: the `gate` verdict's reason, a `vote` option's comments, the tab
`note`, the `chat` message where they explained it. The decision usually survives on its
own; **the reason is what evaporates**, and it is the half that stops the argument
recurring.

When you find a verdict with no reason and it looks durable, ask for it and have the human
write it on the decision — a `gate` row takes a reason after the fact, and records that it
was added late. Do not invent the reason on their behalf, and do not promote a naked
decision as though it had one: "allow" with no why is exactly what gets relitigated. Weigh
a late reason as what it is — reconstructed, not recorded — which is why the export marks
it.

## Which comes first, the board or the document

**Put the cheapest rejectable thing in front of them first.** That is the whole rule, and
it decides the order without anyone having to pick a mode.

Three questions, in this order:

1. **What is the expensive commitment here?** The spec, the schema, the migration, the refactor — whatever costs real effort to produce.
2. **What assumption does it rest on that they could overturn?** Usually the *approach*, sometimes a constraint or a priority. Not the details; the thing that makes the whole artifact the wrong artifact if it is wrong.
3. **Put THAT on the board, in the cheapest form that can carry a rejection** — three bullets and a pick-one, a diagram, a `form` with the two open questions, a `gate` request. Then pay for the artifact.

The distinction that does the work: **what goes up first is the decision the document
depends on, not a draft of the document.** "Event-sourced or CRUD — here is the trade-off
in four lines" costs nothing to reject. "Here is my architecture spec, thoughts?" costs
both of you, and costs more than the writing:

- an unagreed document **anchors** them to your framing, so they end up editing your structure instead of stating their own;
- and once it exists, **sunk cost** pulls both of you toward keeping it — you defend a shape you only chose because something had to be chosen.

That is why "we can always throw it away and rewrite" understates the damage.

**Document first is a derived case, not a different mode.** When the document is cheap
enough to throw away, it *is* the cheapest rejectable thing, so write it: a short design
note, a spec where the writing is how you work out the shape, or a document that already
exists and only needs confirming. The expense is the test, not the format. A one-page
note: write it. Forty pages of architecture: agree the approach first.

Rejected as a test: *"has a shared understanding been reached?"* That is a fact about
someone else's head, and asking yourself to judge it produces confident guesses. The
question above is about **what you are most likely to be wrong about and what it would
cost** — your own uncertainty and your own effort, both of which you can actually see.

Two more things, whichever order you end up in:

- **Promotion is a rewrite, not an export.** A diagram you argued with carries rejected branches, variants and question marks; committed as documentation, the next reader cannot tell what was decided from what was merely considered. `aboard export` gives you the material, not the document.
- **When a document becomes the record, demote the tab**: clear it, or set its `note` to say it is superseded and by what. An editable, authoritative-looking working copy sitting beside the committed truth, with nothing marking which is which, is the shadow-record failure created on purpose.

Keep the argument as well as the outcome only when a rejected option is tempting enough
that someone will propose it again — the same instinct as keeping the reason with a
decision, and why "alternatives considered" earns its place in some documents and is
padding in others.

## The failure it guards against, in both directions

Because the board looks authoritative, both sides start treating it as the record, and
then it is cleared. Watch for:

- a decision whose only trace is a `gate` verdict → put the outcome in the commit that acts on it;
- a plan the human corrected by dragging nodes → restate the corrected model in prose where the code can see it;
- **a tab id cited in a commit message or a PR → never.** That id means nothing to anyone else, or to you next month. Cite the artifact, not the tab.

And in the other direction, just as real: do not turn every exchange into a file. If you
find yourself writing a document whose only reader would be you five minutes ago, you are
promoting something that should have been spent.

Two failure modes to watch for in yourself while the work is still moving, one per
direction:

- **Sprawl** — exchanges accumulating on the board, nothing landing anywhere. The tell is a tab you keep adding to without ever asking what it settled.
- **False authority** — a confident document about something nobody agreed to. The tell is the human editing your structure rather than telling you their own, or correcting details in a shape they never chose.

## What follows from it

- **Assume nothing exists.** Find a tab by `key` and upsert it; tolerate an empty board. A board can be cleared between two turns of the same task.
- **Do not build a shadow tracker.** If the project has issues, a `TODO`, or a document that already holds the work list, the board reflects it — it does not replace it.
- **To show someone else**, export the tab and commit that. A genuinely shared board would need auth and hosting, and this server deliberately has neither.
- **The journal is local too.** `.aboard/run/journal.jsonl` records who changed what on *this machine*. It is not a project audit trail; do not cite it as one.

## See also

- [The `.aboard/` layout](../reference/layout.md) — what exactly is ignored, and why `run/` is nested inside it.
- [Why nothing in the UI starts a session](why-nothing-in-the-ui-starts-a-session.md) — the same posture applied to control.
- [Why tabs are data](why-tabs-are-data.md) — what makes a tab cheap enough to spend.

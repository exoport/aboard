# How to promote a board tab into a document

A tab has settled something — a decision was made, a shape was agreed, a list of rows
was corrected — and it now needs to live somewhere a future reader will find it. The
board is not that place: it is gitignored, per-machine, and non-authoritative by design.

Promotion is four steps, and only the first one is a command.

## 1. Get the text out

```bash
aboard export ab128
aboard export decisions            # the same tab, by its `key`
```

`export` prints the tab as markdown on stdout. It reads the state file from disk, so it
works with **no server running** — you do not have to start a board to read a conclusion
out of one.

A `gate` tab comes out looking like this:

```markdown
# Decisions

Where an agent asks for a decision. Empty pending means nothing is blocked. …

## Decisions

- **Seed this project's board with the example content** — allow, 2026-08-25
  - Why: A worked example per renderer is cheaper to delete than to reconstruct.
```

The heading is the tab's name, the paragraph under it is the tab's `note`, and then the
type's own rendering: verdicts with their reasons for a `gate`, answers beside their
questions for a `form`, an indented tree for a `dag`, one section per block for a
`stack`, an outline of the component tree for a `ui`.

A tab with nothing to say in text says so instead of pretending:

```
_A log tab has no useful text form — look at it instead._
```

You get that line in two different situations, and they are worth telling apart:

- **Three types have no text form at all** — `log`, whose lines live in a sidecar file rather than in the document; `html`, which is a page, so what you would promote is a screenshot; and `trace`, which is the journal, and `aboard journal` already prints that. No amount of content changes the answer for these three.
- **Any other type, when the tab is empty of the thing it holds.** A `markup` tab with no image, a `chat` with no messages, a `vote` with nobody's scores on it. These export perfectly well once they have content: `markup` gives you the image and one line per mark, badged with the mark's own id; `chat` gives you the transcript; `vote` gives you each option with the scores beside it. The example board ships all three empty, so trying them there is misleading.

Where the line really is the answer — a widget, a live log — take a picture of it, or
write what you concluded from it in your own words. That is step 2, which you were going
to do anyway.

**`export` reads the tab's OWN state.** A tab that borrows another's through
`stateFrom` — the example board's Progress kanban reads the Plan tab's nodes — has no
state of its own, so it exports as "no useful text form" however full it looks on
screen. Export the tab that owns the data.

**Rows come out as CSV** for anything whose own state has rows or nodes — `table`,
`dag`, `kanban`:

```bash
aboard export table-example --format csv
```

```csv
id,cell type,number,select,checkbox,longtext
ab188,text,1,low,yes,"A single line. Saves as you type; the cell flashes ""saved"" on blur."
```

On a tab with neither, `--format csv` exits `1` and tells you to use `--format md`. A tab
id or key that does not exist exits `1` and prints the board's whole tab list, so a typo
costs you nothing.

## 2. Rewrite it — do not paste it

**`aboard export` gives you material, not a document.** The two have different jobs, and
the export carries things a document must not:

- **Rejected branches.** A diagram you argued with still holds the options you turned down, and a reader cannot tell what was decided from what was merely considered.
- **Process.** "Waiting on the human", "agent-1 has this", a `doing` status — true on a board, meaningless in a spec.
- **Ids.** `ab128` means nothing to anyone else, or to you next month. Never carry a tab id into a document, a commit message or a PR description. Cite the artifact, not the tab.

Keep the **reason**, always. The decision usually survives on its own; the reason is what
evaporates, and it is the half that stops the argument recurring six weeks later. If a
`gate` verdict has no reason and the decision looks durable, ask the human for one and
have them add it to the row rather than inventing it on their behalf — the board records
that a reason was added late, which is information a reader should have.

Keep the **argument** only when a rejected option is tempting enough that someone will
propose it again. "Alternatives considered" earns its place in some documents and is
padding in others.

## 3. Put it where this project already keeps decisions

**Find the home; do not invent one.** Every project keeps its decisions somewhere
already. Likely places, roughly in order: the spec or design document for the area being
worked on, an ADR directory, `ARCHITECTURE.md`, `DECISIONS.md`, `CONTRIBUTING.md`, a
`CLAUDE.md` if the project has one, or the commit message and PR description of the
change that acts on the decision.

Two rules:

- **Prefer the document the decision is ABOUT.** A decision about a cutover belongs in the cutover spec, because that is what the next person reads before touching it — not in a general decisions file.
- **Do not create a new decisions file when the project has one**, and do not create one at all without asking. A second place to look is worse than an imperfect first place.

## 4. Demote the tab

Once the document is the record, **the tab must stop looking like one**. An editable,
authoritative-looking working copy sitting beside the committed truth, with nothing
marking which is which, is a shadow record — and you have just created it on purpose.

Pick one:

- **Clear it** if the exchange is finished. Dropping a tab from a write does not delete it: the server restores it with a `pendingRemoval` request for the human to answer, which is the confirmation you want here.
- **Set its `note`** to say it is superseded and by what — the note is the first thing a session arriving after a context clear reads. *"Superseded by `docs/cutover.md`; kept for the diagram only."*
- **Leave it** only if it is still live work.

## When to do all this

Not continuously. You cannot tell in advance which exchanges turn out durable, so
promote at a **boundary** — a named moment where you ask, once, *"did anything here
become a rule?"*. Which boundary is the project's to choose: the commit that acts on the
decision, the moment a tab is cleared, the end of a session, a PR description, or the
next edit of a spec.

**Establish which one this project uses and record that where the project records
decisions.** If nobody has decided, ask — it is a one-line question and it prevents both
failure modes at once: a board that never lands anything, and a repo full of
half-decisions nobody reads.

The test for whether a thing should be promoted at all:

> **Would a future session, or another developer, be wrong without this?**

- **Yes** → promote it.
- **No, but I need it to finish this** → leave it on the board, and say so in the `note`.
- **Neither** → spend it. A clarifying question that only changed the next hour is not documentation.

## See also

- [Why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md) — the three tiers this page is the mechanics of, and the argument for each rule above.
- [CLI reference](../reference/cli.md#aboard-export) — every flag `export` takes.
- [Your first board](../tutorials/first-board.md) — the same command as the last step of the tutorial.

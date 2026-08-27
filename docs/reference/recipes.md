# Recipes

A **recipe** is a markdown file with YAML frontmatter: the frontmatter is what a machine
reads (`name`, `description`, `when_to_use`), the body is the method an agent follows,
and an optional fenced block carries a JSON tab skeleton. Nine ship inside the binary; a
project adds or overrides its own by putting files in one of three directories.

```bash
aboard recipes list                        # every recipe available here, and where it came from
aboard recipes show <name>                 # the body, which is what an agent follows
aboard recipes show <name> --template      # only the JSON tab skeleton, so it pipes
```

This page is the technical description: the folders, the precedence, the file format, the
frontmatter schema and the commands. For writing one, see
[How to write a recipe](../how-to/write-a-recipe.md); for why the bodies live in files
rather than in the skill, see
[why the manifest is declared](../explanation/why-the-manifest-is-declared.md).

Every transcript below was produced by running the command against a scratch project
seeded with `aboard init --example --gitignore` — plus, where the transcript needs one,
a library recipe copied into `.aboard/recipes/`, a `_aboard/recipes/` file deliberately
shadowing a built-in, and a deliberately broken file. Paths are printed **absolute**;
`<project>/` stands in for the project root here, long listings are elided at a `…`
line, and trailing spaces are trimmed. Nothing else is edited.

## The two kinds, and the four places

Two kinds, split by weight rather than quality: the **embedded** recipes ship in every
binary, and every other recipe is a **file somebody wrote or copied**. The library is
where the second kind is curated, not a third kind and not a place anything is read from.

| kind | where it lives | discovered? |
| --- | --- | --- |
| **built-in** — embedded | compiled into the binary — nine of them, authored under `pkg/aboard/recipes/builtin/` in aboard's own checkout. Not a folder a project has. | yes, always, in every project the binary reaches |
| **external** — a file | one of the project's three recipe directories: `_apex/aboard/recipes/`, `_aboard/recipes/`, `.aboard/recipes/` under the project root | yes |
| …the **library** it is usually copied from | `recipes/` at the top of the aboard repository | **no** — copy the file into one of the three directories above |

The [library](../../recipes/README.md) is not a discovery tier and nothing reads it at
runtime. It is not embedded and cannot be: `//go:embed` does not reach above the package
directory, and a built-in has to earn its place in every binary the tool ever installs.
It holds two files today — `human-checklist.md` and
`decision-wizard-with-live-summary.md` — and a library recipe becomes a built-in only
when it turns out to be needed everywhere.

### The discovery order

Four tiers, **most specific first**. The three directory names are literal strings and
are not configurable.

| order | directory | `scope` in JSON | what `recipes list` shows | what it is for |
| --- | --- | --- | --- | --- |
| 1 | `_apex/aboard/recipes/` | `apex` | `_apex/aboard/recipes` | the wider workspace's house style |
| 2 | `_aboard/recipes/` | `aboard` | `_aboard/recipes` | committed, shared with the team |
| 3 | `.aboard/recipes/` | `dot-aboard` | `.aboard/recipes` | this checkout only, gitignored with the rest of `.aboard/` |
| 4 | — | `builtin` | `built-in` | compiled in; ships everywhere the binary goes |

The human listing shows the **directory** rather than the scope name, because a row
reading `apex` does not tell anybody where to go and edit the file.

Two of the three directories sit **beside** `.aboard/`, not inside it: they are meant to
be committed, while `.aboard/` is gitignored wholesale. The third is inside it and is
therefore private to the checkout.

Recipes are per **project**, not per board: `--name` gives a second board its own state
file, journal, receipts and logs, but `uploads/` and `recipes/` stay shared — a recipe is
a document about the project. See [the `.aboard/` layout](layout.md).

`recipes list` and `recipes show` answer **without a project root**. A directory that has
never held a board still gets the nine built-ins, and exit 0; that is what lets an agent
ask what a copied binary knows before deciding to use it.

### Missing and unreadable directories

- A tier directory that **does not exist** is the normal case — most projects have none
  of the three — and is silently skipped. So is a path of that name that exists but is
  not a directory: the tier is tested with a stat, and anything that is not a directory
  is not a tier.
- A tier directory that exists and **cannot be read** fails the whole command, exit 1:

  ```console
  $ aboard recipes list
  Error: reading recipes in apex: open .: permission denied
  ```

  (The message names the scope, not the directory.)

- A single **file** that cannot be read does not fail the command. It becomes one row
  carrying its reason, and every other recipe is still listed — see
  [Why a file is rejected](#why-a-file-is-rejected).

## Precedence

**First tier wins, by name.** A recipe in `_apex/aboard/recipes/` shadows one of the same
name in `_aboard/recipes/`, which shadows `.aboard/recipes/`, which shadows the built-in.
`recipes show <name>` resolves through the identical order.

The loser is **never hidden**. It is reported on the row of the recipe that won, as
`shadowedBy` in the structured output and as an indented `shadows <path>` line in the
human one — because that is the row a reader is looking at when they wonder why their
edit did nothing:

```console
$ aboard recipes list
name                           scope            description
-----------------------------  ---------------  ----------------------------------------------------------------
apply-a-write                  built-in         The read-modify-apply shape every board write takes, plus the…
…
human-checklist                .aboard/recipes  A list of things only the human can do, each item's…
                               <project>/.aboard/recipes/human-checklist.md
…
show-a-structure               _aboard/recipes  House style: a dag plus the kanban that mirrors it, with our…
                               shadows recipes/builtin/show-a-structure.md
                               <project>/_aboard/recipes/show-a-structure.md

10 recipe(s), 1 shadowed file(s). `aboard recipes show <name>` prints one.
```

Three details the table cannot carry:

- **Shadowing is by name, not by validity.** A file that does not parse still occupies the
  name and still shadows the valid recipes below it. It is listed as `INVALID`, with the
  files it shadowed named under it, and `recipes show` on that name fails rather than
  falling through to the built-in. That is deliberate: the alternative is a broken file
  the author is looking at while the tool quietly runs a different recipe.

  ```console
  $ aboard recipes list
  show-a-structure               _aboard/recipes
                                 INVALID: no YAML frontmatter block (a recipe opens with a `---` line and closes with another)
                                 shadows <project>/.aboard/recipes/show-a-structure.md
                                 shadows recipes/builtin/show-a-structure.md
                                 <project>/_aboard/recipes/show-a-structure.md

  $ aboard recipes show show-a-structure
  Error: recipe "show-a-structure" (<project>/_aboard/recipes/show-a-structure.md) does not parse: no YAML frontmatter block (a recipe opens with a `---` line and closes with another)
  ```

- **`shadowedBy` is ordered most specific first**, in tier order below the winner.
- A file whose frontmatter has **no `name`** is keyed by its filename stem instead, so two
  broken files never collapse into one row.

## What is read, and what is skipped

Within one directory, each entry is treated like this — nothing is recursed into and
nothing else is read:

| file | treatment |
| --- | --- |
| `*.md` | parsed as a recipe. The suffix test is exact and lower-case: `NOTES.MD` is not one |
| `README.md` — the name is matched case-insensitively, but only among the `*.md` above, so `ReadMe.md` is skipped by name and `README.MD` by the suffix test | **skipped**, not reported. It is the note `aboard init` leaves saying what the directory is for |
| anything else (`.txt`, `.yaml`, no extension) | ignored silently |
| a subdirectory holding at least one `.md` file that is not a `README.md`, counted one level down and not recursively | listed as an `INVALID` row saying recipe directories are flat |
| a subdirectory holding none — including one holding only a `README.md` | ignored silently |
| a file that cannot be opened — a `chmod 000`, a dangling symlink | listed as an `INVALID` row with the OS error |

**A recipe directory is flat: subdirectories are reported, never recursed.** Nesting would
add a fifth axis to a precedence order that is four fixed tiers, with no rule for what
shadows what. The row exists so that "my recipe is not in the listing" is never answered
by silence:

```console
team                           .aboard/recipes
                               INVALID: is a directory holding 1 .md file(s) — recipe directories are flat, so move them up one level; nothing inside it is loaded
                               <project>/.aboard/recipes/team
```

## The file format

One recipe per file, named `<name>.md`, UTF-8. **The filename stem is the recipe's name**,
and the frontmatter must agree with it: the name is what `recipes show` takes and what a
shadow report prints.

```markdown
---
name: ask-to-remove-a-tab
description: "You cannot delete a tab. …"
when_to_use: "When a tab is superseded or spent and you want it gone. …"
tags: [guarantees, cleanup, pendingRemoval]
---

# Ask to remove a tab

…the body an agent reads and follows…
```

### The frontmatter block

The block opens on the **first line** of the file and closes on the next line that is a
delimiter. These are tolerated:

| tolerated | detail |
| --- | --- |
| a UTF-8 byte-order mark | stripped before the first line is examined |
| CRLF line endings | the delimiter test ignores a trailing `\r` |
| trailing spaces or tabs after `---` | the delimiter test ignores trailing spaces, tabs and CR |
| a closing delimiter at EOF with no trailing newline | the scan is line-oriented, not a search for `\n---\n` |

Everything else is a parse failure, reported rather than guessed at. In particular the
opening `---` must be the **first** line (no blank line, no leading spaces), and the
delimiter is exactly three hyphens — `----` is not one.

The body is everything below the closing delimiter, with leading newlines trimmed. It is
markdown and nothing parses it further, except for the one template fence below.

### The frontmatter schema

| field | type | required | meaning |
| --- | --- | --- | --- |
| `name` | string | **yes** | Must equal the filename stem. What `aboard recipes show <name>` takes. A mismatch is an error, not a silent rename. |
| `description` | string | **yes** | One line, what the recipe does. Never truncated except in the human listing, which cuts it to 64 bytes — on a rune boundary, backing off to the last word boundary if there is one past the halfway mark — and appends `…`. In the generated index it is collapsed to one line with any `\|` escaped — as this cell has to be, since a pipe splits a table cell even inside backticks. |
| `when_to_use` | string | **yes** | The matching signal: the situation, in words an agent would recognise. May be written across several lines; every place that prints it — the tables, and `show`'s `**When to use:**` line — collapses runs of whitespace to single spaces first. |
| `tags` | list of strings | no | For grouping and filtering. Carried through to the structured output; nothing in the binary interprets them. |
| `requires.min_schema` | integer | no | The lowest state-file schema version this recipe is written against. `min_schema` is the only key `requires` has. |

The three required fields are checked in the table's order and the first failure is the
one reported, with the stem comparison sitting between `name` and `description`.
`description` and `when_to_use` are trimmed before the test, so a whitespace-only value
counts as missing; `name` only has to be non-empty, because a name of spaces then fails
the stem comparison instead.

**Unknown keys are ignored, not refused.** A key the schema does not define — a typo, or a
field somebody invented — parses and disappears. So does an unknown key inside `requires`.
There is no strict-field mode, and a misspelled `when_to_used` therefore reports the
missing `when_to_use` rather than the typo.

A recipe whose `min_schema` is higher than the schema this binary writes is **marked, not
hidden** — the reader can still open it and see what they are missing:

```console
$ aboard recipes list
future                         .aboard/recipes  needs a newer schema
                               needs schema 99; this board is v3
                               <project>/.aboard/recipes/future.md

$ aboard recipes show future
# future — needs a newer schema

**When to use:** y

> This recipe wants board schema v99; this binary writes v3. Parts of it may not render.
```

### A complete minimal recipe

Every required field, a body, no template. This is `ask-to-remove-a-tab`, a built-in, in
full — `tags` is optional and is shown because every built-in carries them:

````markdown
---
name: ask-to-remove-a-tab
description: "You cannot delete a tab. Set pendingRemoval with a reason worth reading and let the human answer it."
when_to_use: "When a tab is superseded or spent and you want it gone. Never drop it from the array — the server restores it with a generic reason, which is worse for the human than the one you would have written."
tags: [guarantees, cleanup, pendingRemoval]
---

# Ask to remove a tab

You cannot delete one. Ask, with a reason worth reading:

```js
const stale = b.tabs.find((t) => t.key === 'old-plan');
if (stale) stale.pendingRemoval = {
  by: 'agent-1',
  at: new Date().toISOString(),
  reason: 'Superseded by the Migration review tab. Nothing here is referenced any more.',
};
```

Then tell the user you have asked, so they know to answer it. If you simply drop
the tab from the array instead, the server restores it with a generic reason —
worse for them, so write the reason.
````

### A complete recipe with every field

`requires` and a template block. This is the frontmatter of `human-checklist`, a
[library](../../recipes/README.md) file, with its template abbreviated:

````markdown
---
name: human-checklist
description: "A list of things only the human can do, each item's explanation, tick and notes box together in one card so nothing has to be scrolled between."
when_to_use: "When you are handing over steps only a person can carry out — verify this by hand, install that, look at it in the real app — and you need to read back which ones they actually did. Not for work you could do yourself, and not for an approval on the record."
tags: [ui, checklist, handover, verification]
requires:
  min_schema: 3
---

# A checklist the human works through

…the body…

```aboard-template
{
  "key": "handover-checks",
  "name": "Your checks",
  "type": "ui",
  "note": "Things only you can do. Tick each one and say what you saw; I read this tab back.",
  "state": { "…": "…" }
}
```
````

`ask-for-a-decision` is the worked example inside the binary, and the only built-in that
carries a template: run `aboard recipes show ask-for-a-decision` to read a complete file
with a `form` skeleton in it. Both library files carry one, and both are `ui` trees.

## The template block

A recipe may carry **at most one** fenced block holding a tab skeleton. The fence is
tagged rather than positional, because a recipe is full of code blocks and a convention
about position — "the first JSON fence" — would break the day somebody documented a
payload above the skeleton.

````markdown
```aboard-template
{
  "key": "cutover",
  "name": "Cutover",
  "type": "form",
  "state": { "fields": [] }
}
```
````

The rules, exactly:

| rule | detail |
| --- | --- |
| the info string is `aboard-template` **alone** | The line must open with at least three backticks and everything after the run of backticks, trimmed, must equal `aboard-template`. `` ```json aboard-template `` does **not** match — it is a plain `json` block and the recipe reports no template. |
| a decoy fence does not match | A ```` ```json ```` block anywhere in the body is ordinary markdown; only the tagged fence is extracted. |
| leading whitespace is allowed | The fence line is trimmed before it is tested, so an indented block still counts. |
| the closing fence is backticks only | A line that is nothing but backticks (three or more) closes the block. A line with an info string on it does **not** close it, so a nested ```` ```js ```` fence swallows what follows into the block and the JSON check below rejects the result. |
| an unclosed fence at EOF | Takes what there is; the JSON check below then decides whether that is usable. |
| **at most one** | Two blocks is a validation error naming the count, not a silent choice of which one wins. |
| CRLF is normalised | A trailing `\r` is stripped from every line of the block, and the result is trimmed of surrounding whitespace. |
| it must be valid JSON | Any valid JSON value passes — the test is validity, not shape, so a bare array or number is accepted here and refused later by whatever consumes it. |

**The JSON check is the only validation, deliberately.** It catches a trailing comma or an
unquoted key — the mistake `aboard apply` would otherwise reject after the agent had
already told the human it was done. The block is **not** checked against the tab schema: a
template is a *skeleton*, edited before it is applied, and many are deliberately partial.

What a skeleton must and must not carry is a convention, enforced for the recipes in this
repository by the Go suite (`TestBuiltinTemplatesAreCleanTabSkeletons`) rather than by the
parser:

- it must have a `type`, or nothing can render it;
- it must not set `id`, `rev`, `updatedAt`, `version`, `lastEditedBy` or `touched` — the
  document allocates the first and the server owns the rest;
- wrapped in a document, it must raise zero write warnings from the same checker
  `aboard apply` runs.

`aboard recipes show <name> --template` prints the block and nothing else, so it pipes. A
recipe with no block exits 1 naming itself, rather than printing an empty document that
would be applied as an empty tab:

```console
$ aboard recipes show apply-a-write --template
Error: recipe "apply-a-write" has no aboard-template block — run `aboard recipes show apply-a-write` and follow the body
```

## Why a file is rejected

A file the tool tried to use and could not is **never dropped**. It appears in `list`
marked `INVALID` with the reason, and `show` on that name fails with the same text.

The qualifier matters: this section is about entries the tool *read*. Something that is
not a recipe at all — a name that does not end in a lower-case `.md`, a `README.md`, a
subdirectory with no recipes in it — is skipped in silence and appears nowhere. So a
file missing from the listing **entirely**, rather than sitting in it marked `INVALID`,
is a different question, and its answer is in
[What is read, and what is skipped](#what-is-read-and-what-is-skipped) rather than here.

Two of the reasons come from reading the directory, before anything is parsed:

| message | cause |
| --- | --- |
| `cannot be read: …` | the entry is listed but could not be opened: a `chmod 000`, a dangling symlink. The OS error names the file's **base name**, because the directory is read as its own filesystem root |
| `is a directory holding N .md file(s) — recipe directories are flat, so move them up one level; nothing inside it is loaded` | a subdirectory with recipes in it |

The rest are the parser's, and they run **in this order, stopping at the first failure**:

| message | cause |
| --- | --- |
| ``no YAML frontmatter block (a recipe opens with a `---` line and closes with another)`` | no opening delimiter on line 1, or no closing one |
| `frontmatter is not valid YAML: …` | the block is not YAML, or a value has the wrong shape (`tags` as a string, an unterminated list). The YAML library's own message follows, and it may run to several lines |
| ``frontmatter has no `name` `` | `name` absent or empty |
| ``frontmatter name "x" does not match the file stem "y" — `aboard recipes show` takes the name, so the two must agree`` | the two disagree. The row is filed under the **stem**, because that is the file the reader is looking at |
| ``frontmatter has no `description` `` | absent, empty or whitespace |
| ``frontmatter has no `when_to_use` `` | absent, empty or whitespace |
| ``N `aboard-template` blocks — a recipe carries at most one`` | two or more tagged fences |
| ``the `aboard-template` block is not valid JSON`` | the skeleton would not parse |

The footer counts them, and counts shadowed files beside them — `11 recipe(s), 2 invalid,
2 shadowed file(s).` `recipes list` still exits **0**: an invalid file is a fact about
the project, not a failure of the command. Only an unreadable tier **directory** fails
it.

## What the structured output carries

`--output-format json` and `--output-format yaml` print the same fields under the same
keys, one object per recipe, in the same name order as the human listing.

| field | type | always present | meaning |
| --- | --- | --- | --- |
| `name` | string | yes | The recipe's name — the file stem |
| `description` | string | yes | Full text, never truncated |
| `whenToUse` | string | yes | Full text. Note the key: the frontmatter is `when_to_use`, the output is `whenToUse` |
| `tags` | list | omitted when empty | As written in the frontmatter |
| `requires` | object | omitted when zero | `{ "minSchema": N }`. Note the key: `min_schema` in, `minSchema` out |
| `scope` | string | yes | `apex`, `aboard`, `dot-aboard` or `builtin` |
| `path` | string | yes | Absolute for a file on disk; the **embedded** path (`recipes/builtin/<name>.md`) for a built-in, so an error message can still name it |
| `hasTemplate` | bool | yes | Whether `--template` will produce anything |
| `shadowedBy` | list | omitted when empty | The paths this recipe won over, most specific first |
| `error` | string | omitted when the file parsed | Why the file cannot be used |

The body is **not** in the listing: a listing carrying every body would be the problem
that keeping the bodies out of the skill was meant to solve. Use `recipes show`.

Three of the ten rows of the listing above, elided at the `…` lines — a plain built-in,
the copied library file, and the recipe that shadows a built-in:

```console
$ aboard recipes list --output-format json
[
  {
    "name": "apply-a-write",
    "description": "The read-modify-apply shape every board write takes, plus the id allocator and the upsertTab helper.",
    "whenToUse": "Before any write to the board — this is the shape all the other recipes assume. Read it first if you are about to change a tab, and re-read it if a write came back 409.",
    "tags": [
      "core",
      "write",
      "ids",
      "compare-and-set"
    ],
    "scope": "builtin",
    "path": "recipes/builtin/apply-a-write.md",
    "hasTemplate": false
  },
  …
  {
    "name": "human-checklist",
    "description": "A list of things only the human can do, each item's explanation, tick and notes box together in one card so nothing has to be scrolled between.",
    "whenToUse": "When you are handing over steps only a person can carry out — verify this by hand, install that, look at it in the real app — and you need to read back which ones they actually did. Not for work you could do yourself, and not for an approval on the record.",
    "tags": [
      "ui",
      "checklist",
      "handover",
      "verification"
    ],
    "requires": {
      "minSchema": 3
    },
    "scope": "dot-aboard",
    "path": "<project>/.aboard/recipes/human-checklist.md",
    "hasTemplate": true
  },
  …
  {
    "name": "show-a-structure",
    "description": "House style: a dag plus the kanban that mirrors it, with our column names.",
    "whenToUse": "When you have inferred a plan and want it argued with. This project's version of the built-in.",
    "tags": [
      "dag",
      "kanban",
      "plan",
      "stateFrom"
    ],
    "scope": "aboard",
    "path": "<project>/_aboard/recipes/show-a-structure.md",
    "hasTemplate": false,
    "shadowedBy": [
      "recipes/builtin/show-a-structure.md"
    ]
  }
]
```

`--output-format yaml` is the same record under the same keys, one document, list items
in the same order.

## The commands

Full flag and default listing: [the CLI reference](cli.md).

### `aboard recipes`

Prints help, exit 0. A bare group does not guess at `list`, because then `recipes` and
`recipes list` would be two spellings of one thing until a third subcommand arrived. An
unknown subcommand exits 2.

### `aboard recipes list [--output-format human|json|yaml]`

Every recipe available in this project, one row each. Default `human`: three columns —
name, scope, description — with everything else on indented lines under the row. A clean
**built-in** is one line; a recipe on disk is at least two, because its path is always
printed under it. The indented lines are, in order: `INVALID: <reason>`,
`needs schema N; this board is vN`, one `shadows <path>` per shadowed file, and the
file's own path — the last of which is omitted for a built-in, whose path is inside the
binary and is nowhere a reader can go and edit.

The description is cut in **this view only**; `show` and the structured formats carry it
whole.

| exit | when |
| --- | --- |
| 0 | printed — including when rows are invalid |
| 1 | a recipe directory exists and could not be read |
| 2 | an unknown `--output-format`, or an argument — it takes none |

### `aboard recipes show <name> [--template]`

The body an agent follows: a title line `# <name> — <description>`, a
`**When to use:**` line, the schema warning if the recipe needs a newer board, then the
body. The frontmatter is stripped — it is metadata for the listing, and YAML at the top of
something meant to be read as prose is noise. If the recipe has a template, a closing line
says so.

`--template` prints only the JSON skeleton.

| exit | when |
| --- | --- |
| 0 | printed |
| 1 | no such name; or the file does not parse; or `--template` on a recipe that has none; or a recipe directory exists and could not be read — `show` walks the same tiers `list` does |
| 2 | a missing or extra argument |

The two argument errors are cobra's — `accepts 1 arg(s), received 0` and
`accepts 1 arg(s), received 2`. An unknown name lists what **is** available rather than
answering with a bare "not found", because the commonest cause is a near miss:

```console
$ aboard recipes show nope
Error: no recipe named "nope" — available: apply-a-write, ask-for-a-decision, ask-to-remove-a-tab, build-something-interactive, composite-review-tab, coordinate-with-another-agent, human-checklist, point-at-part-of-an-image, react-to-their-edits, show-a-structure
```

### `aboard recipes index`

**Hidden; repo maintenance.** Prints the markdown index that
`.claude/skills/aboard/references/recipes.md` is generated from, to stdout — it writes
nothing itself. `make caps` redirects it into that file, and
`aboard capabilities --check` fails when the committed copy no longer matches the
binary.

**Built-in recipes only**, and deliberately: the file it generates ships inside a skill
directory that gets copied between projects, so a table listing one project's own recipes
would be wrong everywhere else it was copied. The paragraph under the table is what makes
that harmless — it leads with `aboard recipes list`, which is the only complete answer for
any given project. The output is deterministic: sorted by name, no timestamps, no moving
counts. A built-in that does not parse is a build defect and is refused rather than
emitted as a row of empty cells.

Because it is hidden, it is absent from the generated CLI reference and from the declared
command table, so adding a maintainer command never moves `capsHash`.

| exit | when |
| --- | --- |
| 0 | printed |
| 1 | a built-in recipe does not parse — a defect in this binary, refused rather than emitted as a row of empty cells |
| 2 | an argument was passed; it takes none |

`aboard capabilities --check` compares the committed file against what this binary would
emit. A file that is **absent** is not drift — a project that never copied the skill has
nothing to be stale — but one that exists and disagrees exits non-zero naming the
regeneration command.

## How an agent uses one

Three lines:

1. `aboard recipes list` — the complete answer for this project. The generated index in
   the skill lists only what is compiled into the binary, so an agent that reads only that
   index is incomplete.
2. `aboard recipes show <name>` — read the body and follow it. A non-zero exit means the
   name is unknown or the file does not parse, and the message says which; run `list`
   rather than guessing at a near miss.
3. `aboard recipes show <name> --template` — if it carries a skeleton, fill it in, give it
   an id from the document's `nextId`, and apply it as part of the whole document.

The long form, including the slash-command spelling (`/aboard --show-a-structure <prompt>`
is the recipe name plus a subject, not a separate code path), is in
[How to write a recipe](../how-to/write-a-recipe.md).

**There is no `--template | apply` one-liner, and there should not be.** `--template`
prints a **tab**; `apply` takes a **document**. A document composed from one skeleton
would drop every tab you were not touching, so the shape is read-modify-apply —
`aboard recipes show apply-a-write` is that shape written out.

```console
$ aboard recipes show human-checklist --template | aboard apply --check
Error: stdin json has no tabs array
```

Checking a skeleton on its own is a different question, and that one does have a
one-liner. It needs no board and writes nothing:

```console
$ aboard recipes show human-checklist --template \
    | python3 -c 'import json,sys; json.dump({"tabs":[json.load(sys.stdin)]}, sys.stdout)' \
    | aboard apply --check
checked: no warnings — nothing was written (drop --check to apply)
```

## See also

- [How to write a recipe](../how-to/write-a-recipe.md) — the task version of this page.
- [The recipe library](../../recipes/README.md) — the recipes collected in this repository but not compiled into the binary.
- [The CLI reference](cli.md) — generated flags, defaults and help text for `recipes list|show`.
- [The `.aboard/` layout](layout.md) — where `recipes/` sits among the rest, and what two boards in one project share.
- [The capability manifest](capabilities.md) — what a recipe body should consult rather than copy.
- [The state file](state-file.md) — the document a template block becomes part of.

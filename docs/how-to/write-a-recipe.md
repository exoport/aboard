# How to write a recipe

A **recipe** is a short markdown method for one board move — ask for a decision, show a
structure, react to the human's edits — written for an agent to follow. It has
frontmatter an agent can match against, a body it reads, and optionally a JSON tab
skeleton it can fill in and apply.

Recipes ship with the binary, and a project can add or override its own. Write one when
your project has a board move that keeps recurring and keeps being done slightly
differently.

## Where a recipe goes

Four scopes, most specific first. The directory names are literal — they are not
configurable:

| directory (what `recipes list` shows) | `scope` in `--output-format json` | intended lifetime                                          |
| ------------------------------------- | --------------------------------- | ---------------------------------------------------------- |
| `_apex/aboard/recipes/`               | `apex`                            | the wider workspace's house style                          |
| `_aboard/recipes/`                    | `aboard`                          | committed, shared with the team                            |
| `.aboard/recipes/`                    | `dot-aboard`                      | this checkout only; gitignored with the rest of `.aboard/` |
| `built-in`                            | `builtin`                         | compiled in; ships everywhere the binary goes              |

The human column shows the **directory**, not the scope name, and deliberately: a
row reading `apex` does not tell anybody where to go and edit the file.

**Lookup is first-wins by `name`, in that order.** A recipe in `_apex/aboard/recipes/`
shadows one with the same name in `_aboard/recipes/`, which shadows `.aboard/recipes/`,
which shadows the built-in. Shadowing is allowed and **always reported** — a project
that overrides a built-in is doing something deliberate, and you should be able to see
what it replaced:

```bash
aboard recipes list
aboard recipes list --output-format json
```

That prints one line per recipe — name, scope and description — with the file's path
and anything it shadows indented underneath, and the footer counts the shadowed files.
A clean built-in is one line; a recipe with something worth knowing about it is two.
The JSON form carries the whole record (`whenToUse`, `tags`, `requires`, `hasTemplate`,
`shadowedBy`). It is the only complete answer about what is available: the generated
index in the skill lists what is compiled into the binary and cannot know about a
project's own files.

Pick the scope by who should have it. Something only you want, in one checkout →
`.aboard/recipes/` (it is ignored, so it stays yours). Something the team should share
→ `_aboard/recipes/`, committed. Something every project in a workspace should inherit →
`_apex/aboard/recipes/`.

## The file

One recipe per file. **The filename stem is the recipe's name** and the frontmatter must
agree with it — a mismatch is a validation error, not a silent rename, because the name
is what `aboard recipes show` takes and what a shadow report names.

```markdown
---
name: ask-for-a-decision
description: Ask several questions at once as a form with typed fields, then read the answers back by field id.
when_to_use: When you would otherwise write a paragraph containing three questions. Use it for typed input — a choice, a number, a bit of free text — not for an approval you need on the record.
tags: [form, decision]
requires:
  min_schema: 3
---

# Ask for a decision

...the body an agent reads and follows...
```

| field         | required | what it is                                                                                                          |
| ------------- | -------- | --------------------------------------------------------------------------------------------------------------------- |
| `name`        | yes      | Must equal the file stem. What `aboard recipes show <name>` takes.                                                  |
| `description` | yes      | One line, what the recipe does. It goes in the index table verbatim, so write it as a sentence, not a title.         |
| `when_to_use` | yes      | The matching signal: the situation, in the words an agent would recognise. May be two lines in the file; it is collapsed to one in the table. |
| `tags`        | no       | A list, for grouping and filtering.                                                                                 |
| `requires`    | no       | `min_schema: N` — the lowest state-file schema this recipe's skeleton is valid against.                              |

Two rules that come from the index being generated: a `|` in any value is escaped rather
than breaking the table, and **nothing is ever truncated**. A description too long for a
table is a description to rewrite.

Write `when_to_use` as the situation, not the mechanism. "When you would otherwise write
a paragraph containing three questions" is matchable; "for forms" is not.

## The body

The body is prose for an agent. Keep it to the shape of the move: what to look at first,
what to build, what to say to the human, and what to do with the answer. State the
failure mode if there is one — the reason a recipe exists is usually that somebody got
it wrong once.

Do not restate the schema or the capability surface in a recipe body. `aboard
capabilities` is the canonical answer and it cannot go stale; a recipe that copies field
names into prose will disagree with the binary eventually.

## The template block

If the recipe produces a tab, give it a skeleton in a fenced block tagged
`aboard-template`:

````markdown
```aboard-template
{
  "name": "Open questions",
  "type": "form",
  "note": "Three questions blocking the migration.",
  "state": {
    "fields": [
      { "id": "strategy", "label": "Cutover strategy", "type": "select",
        "options": ["big-bang", "dual-write", "shadow-read"] }
    ]
  }
}
```
````

One block per recipe. It holds a **tab skeleton** — no ids, no `updatedAt`, nothing the
server manages — so an agent can fill in the content, allocate an id from the document's
`nextId`, and apply it. Extract it on its own:

```bash
aboard recipes show ask-for-a-decision --template
```

That prints the JSON and nothing else, so it pipes. A recipe with no template block
exits non-zero and names the recipe, rather than printing an empty document that would
be applied as an empty tab.

Three built-ins carry one, and they are the worked examples of this format:
`ask-for-a-decision`, a `form`; and the two `ui` tabs,
`decision-wizard-with-live-summary` and `human-checklist`. Read one before writing your
first — what their bodies carry is the judgement `aboard capabilities` cannot, such as
why a summary has to live in the same tab as the fields it summarises, and why a literal
"3 of 8 done" is the one thing not to put on a checklist when nothing on the board
computes. This page deliberately keeps no list of the full set: `aboard recipes list` is
the complete answer, and a hand-maintained copy of what ships is a copy that drifts.

## Try it

```bash
mkdir -p .aboard/recipes
$EDITOR .aboard/recipes/my-move.md

aboard recipes list                    # my-move should appear, scope ".aboard/recipes"
aboard recipes show my-move            # the body, as an agent will read it
aboard recipes show my-move --template # the skeleton, if it has one
```

If `list` does not show it, the frontmatter did not validate — the row is not silently
dropped, so read what `list` says about it. The usual cause is `name` disagreeing with
the filename.

**A recipe directory is flat.** `.aboard/recipes/team/my-move.md` is not loaded: the
precedence order is four fixed tiers, and nesting would add a fifth axis with no rule
for what shadows what. A subdirectory holding `.md` files is listed as an invalid entry
saying so, rather than dropped — the same rule as every other file it cannot use.

## How an agent reaches it

Two routes, and they are the same route:

- **Directly.** The agent runs `aboard recipes show <name>` and follows the body. It learns which names exist from `aboard recipes list`, or from the generated index in the skill.
- **From a slash command.** `/aboard --show-a-structure the auth migration` is not a separate code path: the token after `--` is the recipe **name**, everything after it is the **prompt**. The agent runs `aboard recipes show show-a-structure`, reads the body, and follows it against "the auth migration". If `show` exits non-zero it says so and runs `aboard recipes list` rather than guessing at a near-miss name.

So the recipe is the method and the rest of the line is the subject. Nothing about the
slash form is privileged, which is the point of keeping the bodies out of the skill: an
agent that was never given a slash command gets the identical result.

## Changing a built-in

Built-in recipes live in the binary and are authored in the same format under
`pkg/aboard/recipes/builtin/`. If you are working in aboard's own checkout: edit the
file, then regenerate the skill's index, which is generated from that frontmatter:

```bash
make caps
```

Commit what it writes. From outside the repo you do not need to fork anything — put a
file with the same `name` in one of the three project directories and it shadows the
built-in, visibly.

## See also

- [The capability manifest](../reference/capabilities.md) — what a recipe body should consult rather than copy.
- [The state file](../reference/state-file.md) — the document your template block becomes part of.
- [Why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md) — why `.aboard/recipes/` is the private scope and `_aboard/recipes/` the shared one.

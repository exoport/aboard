# How to write a recipe

A **recipe** is a short markdown method for one board move — ask for a decision, show a
structure, react to the human's edits — written for an agent to follow. It has
frontmatter an agent can match against, a body it reads, and optionally a JSON tab
skeleton it can fill in and apply.

Nine recipes ship with the binary, and a project can add or override its own. Write one
when your project has a board move that keeps recurring and keeps being done slightly
differently.

This page is the task. **[Recipes](../reference/recipes.md) is the reference** — every
folder and its precedence, the frontmatter schema field by field, the exact rules for the
template fence, every reason a file is rejected, and the commands with their exit codes.
Look things up there.

**Look in the library first.** aboard's own repository collects further recipes in a
top-level [`recipes/`](../../recipes/README.md) folder — worth sharing, not worth
shipping in every binary. They are not compiled in and not discovered: you get one by
copying the file into one of your project's recipe directories. The move you are about to
write may already be in there, and one that is already written and already run beats one
you are about to draft.

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

Three carry one, and they are the worked examples of this format: the built-in
`ask-for-a-decision`, a `form`; and the two `ui` tabs in the
[library](../../recipes/README.md), `decision-wizard-with-live-summary` and
`human-checklist`. Read one before writing your first — what their bodies carry is the
judgement `aboard capabilities` cannot, such as why a summary has to live in the same tab
as the fields it summarises, and why a literal "3 of 8 done" is the one thing not to put
on a checklist when nothing on the board computes. This page deliberately keeps no list
of the full set: `aboard recipes list` is the complete answer, and a hand-maintained copy
of what ships is a copy that drifts.

## Try it

```bash
mkdir -p .aboard/recipes
$EDITOR .aboard/recipes/my-move.md

aboard recipes list                    # my-move should appear, scope ".aboard/recipes"
aboard recipes show my-move            # the body, as an agent will read it
aboard recipes show my-move --template # the skeleton, if it has one
```

If `list` does not show it under the name you expected, look under its **filename
stem**: a file that was read and could not be used is listed there, marked `INVALID`
with the reason, rather than dropped. The usual cause is `name` disagreeing with the
filename. Every reason a file is rejected is tabulated in
[Recipes](../reference/recipes.md#why-a-file-is-rejected).

If it is missing from the listing **entirely** it was never read as a recipe, and that
is a different question: the suffix test is exact and lower-case, so `my-move.MD` and
`my-move.markdown` are not recipes, and a `README.md` is skipped on purpose. See
[what is read, and what is skipped](../reference/recipes.md#what-is-read-and-what-is-skipped).

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

## Contributing one to the library

If the recipe is general — useful in projects that are nothing like yours — it belongs in
aboard's [`recipes/`](../../recipes/README.md) rather than only in your own
`_aboard/recipes/`. Same format, same frontmatter, same one template block; the
difference is only where it lives and how it travels.

**Built-in or library?** A built-in has to earn its place in every binary the tool ever
installs, and it can never be edited by the people it reaches — a project can only shadow
it. The library asks for one `cp` and gives you a file you can then change for the project
you copied it into. So: put it in the library unless a session would be *wrong* without
it. Nine are built in and the bar for a tenth is high.

Run it once before proposing it, against a scratch project rather than a board anyone is
using:

```bash
mkdir -p /tmp/scratch && cd /tmp/scratch && aboard init   # makes .aboard/recipes/
cp <aboard-checkout>/recipes/<name>.md .aboard/recipes/
aboard recipes list
aboard recipes show <name> --template \
  | python3 -c 'import json,sys; json.dump({"tabs":[json.load(sys.stdin)]}, sys.stdout)' \
  | aboard apply --check
```

The wrapping is not ceremony: `--template` prints a **tab** and `apply` takes a
**document**, so a bare `--template | apply` exits 1 with *stdin json has no tabs array*.
That is the right refusal — a document composed from one skeleton would drop every tab
you were not touching, which is why the real write is read-modify-apply
(`aboard recipes show apply-a-write`). `--check` needs no board and writes nothing, so it
is the cheap half you can run on a skeleton alone.

The Go suite walks `recipes/` with the same assertions it applies to the built-ins — every
file parses, its frontmatter is complete, and any template it carries is a clean tab
skeleton raising zero write warnings — so a broken library file fails the build. The
generated index in the skill, though, lists **built-ins only**: it is copied between
projects, where a path into aboard's own checkout names nothing. `recipes/README.md` is
the library's index, so **add a row to its table** as part of the same change — nothing
generates that file, and the suite holds it to the folder rather than trusting it: the
rows and the recipes must be the same set, and a row's "when to use" must still be the
recipe's own `when_to_use`.

## See also

- [Recipes](../reference/recipes.md) — the reference: folders, precedence, the frontmatter schema, the template block, and the commands.
- [The capability manifest](../reference/capabilities.md) — what a recipe body should consult rather than copy.
- [The state file](../reference/state-file.md) — the document your template block becomes part of.
- [Why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md) — why `.aboard/recipes/` is the private scope and `_aboard/recipes/` the shared one.
- [The recipe library](../../recipes/README.md) — the recipes collected in this repository but not compiled into the binary.

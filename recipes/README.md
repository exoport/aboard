# The recipe library

Recipes that are worth sharing but not worth shipping in every binary.

> **The decision, in the human's words (2026-08-26):** the embedded recipes are small and are
> basic functionality; the external recipe files here are the curated library; any user can
> create their own recipes or copy one from this curated folder into their project.

A **recipe** is a short markdown method for one board move — ask for a decision, show a
structure, react to the human's edits — written for an agent to follow. Nine of them are
*built in*: compiled into the `aboard` binary, available in any project it reaches, with
no file to copy. This folder is the other kind. Nothing here is embedded, nothing here is
discovered automatically, and a project gets one of these by **copying the file into one
of its own recipe directories**.

The split is a judgement about weight, not quality. A built-in has to earn its place in
every binary the tool ever installs; a library recipe only has to be worth someone's
`cp`. A recipe that turns out to be needed everywhere can be promoted into
`pkg/aboard/recipes/builtin/` later, and one that has stopped earning its place can move
the other way — which is what happened to both files in here.

## What is in it

| recipe | when to use it |
| --- | --- |
| [`decision-wizard-with-live-summary`](decision-wizard-with-live-summary.md) | When you have put a pile of findings in front of the human and need a verdict on each, and they want to see what they have chosen so far without hunting through tabs. This shape is for DECIDING; a `gate` tab is for committing. |
| [`human-checklist`](human-checklist.md) | When you are handing over steps only a person can carry out — verify this by hand, install that, look at it in the real app — and you need to read back which ones they actually did. Not for work you could do yourself, and not for an approval on the record. |

Both are `ui` tabs and both carry an `aboard-template` block, so both can be applied
rather than retyped.

## Using one

Copy the file into whichever of the project's three recipe directories matches who should
have it — **`.aboard/recipes/`** for this checkout only (it is gitignored, so it stays
yours), **`_aboard/recipes/`** committed for the team, **`_apex/aboard/recipes/`** for
every project in a workspace:

```bash
cp recipes/human-checklist.md <project>/.aboard/recipes/
```

Then it is a recipe like any other — discovered by name, listed with the directory it
came from, and shadowing the built-in of the same name if there is one:

```bash
cd <project>
aboard recipes list                         # human-checklist, scope ".aboard/recipes"
aboard recipes show human-checklist         # the body, as an agent reads it
aboard recipes show human-checklist --template   # just the tab skeleton
```

Read the body first — a template applied without reading what it is for is a tab nobody
asked for.

**The template is a TAB and `aboard apply` takes a DOCUMENT**, so there is no
`--template | apply` one-liner and there should not be: composing a document from a
skeleton would drop every tab you are not touching. The shape is read-modify-apply, and
`aboard recipes show apply-a-write` is that shape written out — read the document, splice
the filled-in tab in with an id from its `nextId`, apply the whole thing under
compare-and-set.

Checking the skeleton on its own is a different question and does have a one-liner. It
needs no board and writes nothing:

```bash
aboard recipes show human-checklist --template \
  | python3 -c 'import json,sys; json.dump({"tabs":[json.load(sys.stdin)]}, sys.stdout)' \
  | aboard apply --check
```

Copying is the mechanism on purpose. A library that a project could point at remotely
would make every board depend on a directory outside it, and a recipe you have copied is
one you can edit for the project you copied it into — which is usually the first thing
worth doing.

## Contributing one

Same format as a built-in: **[How to write a recipe](../docs/how-to/write-a-recipe.md)**
is the full guide, and the short version is

1. **Frontmatter.** `name` (which must equal the file stem), `description`, `when_to_use`
   are required; `tags` and `requires.min_schema` are optional. Write `when_to_use` as
   the *situation*, in words an agent would recognise, not the mechanism.
2. **A body an agent can follow.** What to look at first, what to build, what to say to
   the human, what to do with their answer — and the failure mode, because the reason a
   recipe exists is usually that somebody got it wrong once. Do not restate the
   capability surface; `aboard capabilities` is the answer that cannot go stale.
3. **At most one `aboard-template` block**, if the recipe produces a tab. It holds a tab
   skeleton — no `id`, no `rev`, no `updatedAt`, nothing the server manages.
4. **Run it once**, against a scratch project rather than a board anyone is using:

   ```bash
   mkdir -p /tmp/scratch && cd /tmp/scratch && aboard init   # makes .aboard/recipes/
   cp <aboard-checkout>/recipes/<name>.md .aboard/recipes/
   aboard recipes list      # it appears, with its scope
   aboard recipes show <name> --template \
     | python3 -c 'import json,sys; json.dump({"tabs":[json.load(sys.stdin)]}, sys.stdout)' \
     | aboard apply --check                            # exits 0, no warnings
   ```

   A recipe that has never been run is a recipe that has never been checked: `ui` fails
   silently *and* successfully — an unknown prop renders nothing at all while `apply`
   still prints `applied` — which is why the template goes through the write path's own
   checker before it is committed.

5. **Add a row to the table above**, with the recipe's `when_to_use` copied across. This
   file is the library's index and nothing generates it, so the suite gates it instead:
   the rows and the folder must be the same set, and a row's "when to use" must still be
   the recipe's own.

The Go suite walks this folder with the same assertions it applies to the built-ins
(`TestBuiltinTemplatesAreCleanTabSkeletons`): every file parses, its frontmatter is
complete, and any template it carries is a clean tab skeleton that raises zero write
warnings. A broken file here fails the build, the same as a broken built-in. A second
test (`TestTheLibraryReadmeIsAnIndexOfTheLibrary`) holds this README to the folder, for
the same reason `aboard capabilities --check` holds the generated index to the binary: a
hand-maintained list of what ships is a list that drifts, and the one already gated is
the half that could be regenerated.

## Why the skill's index does not list these

`.claude/skills/aboard/references/recipes.md` is generated by `aboard recipes index`,
which reads the recipes **compiled into the binary** — that file ships inside a skill
directory copied from project to project, and a table of files that live only in aboard's
own checkout would be a list of recipes the reader cannot open. The index says so in its
own words and points at `aboard recipes list`, which is the only complete answer for any
given project. This README is the index for the library.

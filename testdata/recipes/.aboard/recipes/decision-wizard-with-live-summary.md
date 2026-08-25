---
name: decision-wizard-with-live-summary
description: "One ui tab of internal panels — N decision panels plus a Summary panel that reads the same state.data the fields write, so it cannot go stale."
when_to_use: "When you have put a pile of findings in front of the human and need a verdict on each, and they want to see what they have chosen so far without hunting through tabs. Use this shape for deciding and a gate tab for committing."
tags: [ui, tabs, bind, decisions, summary, wizard]
requires: { min_schema: 3 }
---

# A decision wizard with a live summary

**One `ui` tab. Internal panels. A read-only Summary panel that cannot go stale.**

Use this when you have put a pile of findings in front of the human and need a
verdict on each — options, a recommendation, a place to say "not like that" —
and they want to see what they have chosen so far without hunting through tabs.

> Written from a real session (2026-08-24): ten decisions about a validation
> harness, first built as five tabs plus a `gate`, then rebuilt into this shape
> when the human asked "could this be done with a single tab with internal tabs?"
> It could, and the rebuild made something possible that had been impossible.

---

## The constraint that decides the shape

**A `bind` path never reaches outside its own tab.** Every mechanism on the board
is tab-scoped, and all three dead ends are worth knowing before you design:

| you might try                          | what actually happens                                                                                                                         |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| a `ui` tab summarising four other tabs | `resolve()` reads `path.split('.').reduce(…, data())`, and `data()` is `ctx.state.data` — this tab's own (`views/ui.js:194-202`)              |
| an `html` tab reading `/aboard.json`   | the frame is posted only `ctx.state.data` (`views/html.js:250`) and runs `sandbox="allow-scripts"` with no network — it cannot fetch anything |
| `stateFrom` pointing at the others     | resolves to exactly ONE source tab via `tabById` (`aboard.html:187-188`), not an aggregate                                                     |

So a summary spread across N tabs can only ever be **a copy you refresh** — stale
the moment the human changes a pick, and looking authoritative while it lags.
That is the shadow-record failure, built on purpose.

**Put every bound value in one tab's `state.data` and the problem disappears.**
The Summary panel reads the same object the fields write. It is not synchronised
with the answers; it _is_ the answers.

That is the whole reason to prefer one tab here. Not tidiness — capability.

---

## The shape

```aboard-template
{
  "id": "bb1",
  "key": "decisions",
  "name": "Port decisions",
  "type": "ui",
  "note": "One tab, N panels. Panels 2..N-1 decide; the Summary panel is read-only and live.",
  "state": {
    "data": { "b1": "— not decided —", "b1_note": "", "hk_a": false },
    "intents": [],
    "root": {
      "type": "tabs",
      "panels": [
        {
          "label": "Overview",
          "children": [
            { "type": "heading", "value": "What this tab decides" },
            { "type": "text", "value": "Framing: what was investigated, and what each panel below is for." },
            { "type": "stat", "value": 2, "label": "decisions waiting" }
          ]
        },
        {
          "label": "B1",
          "children": [
            {
              "type": "card",
              "title": "B1 — the model pin",
              "accent": true,
              "children": [
                { "type": "text", "value": "What is actually true, with file:line." },
                {
                  "type": "field",
                  "field": "select",
                  "label": "Granularity",
                  "bind": "b1",
                  "options": [
                    "— not decided —",
                    "(b) per fixture, with a per-step override  (recommended)",
                    "(a) per step, as the schema stands",
                    "(c) one repo-wide pin",
                    "none of these — see my note"
                  ]
                },
                { "type": "caption", "value": "Why the recommendation is the recommendation." },
                { "type": "field", "field": "longtext", "label": "Changes you want", "bind": "b1_note" }
              ]
            }
          ]
        },
        {
          "label": "Housekeeping",
          "children": [
            {
              "type": "checklist",
              "items": [{ "label": "Delete the dead fixture while you are in there", "bind": "hk_a" }]
            }
          ]
        },
        {
          "label": "Summary",
          "children": [
            {
              "type": "card",
              "title": "Live — reads the same values the panels write",
              "accent": true,
              "children": [
                { "type": "caption", "value": "Not a copy and never stale. Read-only by construction." },
                {
                  "type": "row",
                  "children": [
                    { "type": "caption", "value": "B1 granularity" },
                    { "type": "text", "value": { "bind": "b1" } }
                  ]
                },
                {
                  "type": "row",
                  "children": [
                    { "type": "caption", "value": "B1 note" },
                    { "type": "text", "value": { "bind": "b1_note" } }
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  }
}
```

`tabs` takes `panels: [{label, children}]`, remembers the open panel **per
viewer** (never written to state — it would flip the panel under everyone else),
and rebuilds a panel's children on every switch. That rebuild is what makes the
Summary live: switching to it re-resolves every bind from current data.

### One decision, as a card

```jsonc
{
  "type": "card",
  "title": "B1 — the model pin",
  "accent": true,
  "children": [
    { "type": "text", "value": "What is actually true, with file:line." },
    {
      "type": "field",
      "field": "select",
      "label": "Granularity",
      "bind": "b1",
      "options": [
        "— not decided —",
        "(b) per fixture, with a per-step override  (recommended)",
        "(a) per step, as the schema stands",
        "(c) one repo-wide pin",
        "none of these — see my note",
      ],
    },
    {
      "type": "caption",
      "value": "Why the recommendation is the recommendation.",
    },
    {
      "type": "field",
      "field": "longtext",
      "label": "Changes you want",
      "bind": "b1_note",
    },
  ],
}
```

Four rules that earn their place:

- **A sentinel for undecided, never `""`.** `input.value = current ?? ''` selects
  the blank option, so an unanswered select and an answered-with-blank one are
  indistinguishable. `"— not decided —"` as both the default value and the first
  option makes "they have not reached this yet" readable in the summary and in
  your read-back.
- **Put `(recommended)` in the option label.** The human should not have to
  cross-reference prose to find which one you meant.
- **Always offer `none of these — see my note`.** A wizard with no exit trains
  people to pick the nearest wrong answer.
- **A `longtext` note per decision, not one at the end.** A note attached to a
  decision survives promotion into the repo beside it; a note in a general box
  loses its subject.

### The Summary panel

**Do not reach for `kv` here.** It is the obvious choice and it does not work —
see the next section.

> **Correction, carried in at the port (2026-08-25).** `kv` resolves a `{bind}`
> now — `views/ui.js` renders both sides of a pair through `asText`, and
> `ui.spec.json` says so. The recommendation below still stands, but on layout
> grounds rather than capability: `caption` + `text` in a `row` gives you
> control over the label column that a `<dl>` does not. Read the Gotchas section
> as a record of what it cost to find out, not as current behaviour.

Use `caption` + `text` in a `row`, one per value:

```jsonc
{
  "label": "Summary",
  "children": [
    {
      "type": "card",
      "title": "Live — reads the same values the panels write",
      "accent": true,
      "children": [
        {
          "type": "caption",
          "value": "Not a copy and never stale. Read-only by construction.",
        },
        {
          "type": "row",
          "children": [
            { "type": "caption", "value": "B1 granularity" },
            { "type": "text", "value": { "bind": "b1" } },
          ],
        },
        {
          "type": "row",
          "children": [
            { "type": "caption", "value": "B1 note" },
            { "type": "text", "value": { "bind": "b1_note" } },
          ],
        },
      ],
    },
  ],
}
```

Read-only because there is nothing there to edit — no `field`, no `checklist`, no
`button`. That is a stronger guarantee than a `readOnly` flag: the components
that write simply are not present.

#### Which components can display a bound value

Only the ones that render through `asText()`, which calls `resolve()`:
**`text`, `caption`, `title`, `heading`, `stat`, `notice`, `code`, `badge`.**
Anything else renders `{bind:…}` as the object it is.

---

## Reading it back

One command, no parsing of prose:

```sh
curl -s http://localhost:<port>/aboard.json | python3 -c "
import json,sys
d=json.load(sys.stdin)
t=next(t for t in d['tabs'] if t.get('key')=='decisions')
print(json.dumps(t['state']['data'], indent=1, ensure_ascii=False))"
```

Then act on it, and **promote the outcome into whatever the project uses for
decisions**. The board is where a thing is worked out; it is not the record. A
verdict whose only trace is `state.data` dies with the machine.

Carry the **reason** across, not just the verdict — that is what the per-decision
`longtext` is for, and it is the half that stops the argument recurring.

---

## Gotchas that each cost a round

> **Three of the bullets below were fixed after this recipe was written**, on
> 2026-08-24, and are carried through unedited because the reasoning is still
> worth reading. What changed: `kv` resolves a `{bind}` on both sides of a pair
> (`views/ui.js`); `aboard apply`'s write warnings descend into a `ui` tree and a
> stack's blocks rather than checking only top-level state keys; and `version` is
> stamped by the server on every accepted write, so a caller cannot write a wrong
> one. The failure mode the bullets describe — **`ui` fails silently and
> successfully** — has not changed, and neither has the instruction that follows
> from it: render it and look.

- **`kv` cannot show a bound value — it is the one component that does not
  resolve.** `views/ui.js:289-299` calls `resolve(node.pairs)` on the **array**,
  then renders each value with bare `String(pair.value)` — never `asText`. So
  `{"bind":"b1"}` in a pair comes out as the literal string `[object Object]`.
  Every other display component goes through `asText`, which resolves. `kv` is
  fine for static label/value prose and useless in a live summary. **This bit me
  twice in one session**: first with the wrong prop name, then, having fixed
  that, with a Summary panel full of `[object Object]`.
- **`kv` takes `pairs` with `{key, value}`** — not `items` with `{k, v}`
  (`views/ui.js:289-299`). Get it wrong and you get a titled card wrapping an
  empty `<dl>`: no error, no warning, just nothing. **An unknown component
  `type` renders a visible marker; an unknown _prop_ on a known type renders
  silently empty**, and `aboard apply`'s stderr warning only covers top-level tab
  state fields, so it does not reach inside a `ui` tree. Look at the screen.
- **The general rule behind both of the above: `ui` fails silently and
  successfully.** `aboard apply` prints `applied`, exit 0, whatever you wrote.
  Nothing type-checks a component tree. The only instrument is your eyes on the
  rendered page — so render it and look before telling the human it is ready.
- **`"version": 3`.** `.claude/skills/aboard/references/schema.md:5` still shows
  `2`; `caps.go:92` and `capabilities.md:69` say `3`. `aboard apply` accepts a
  wrong version silently, exit 0 — the only thing that notices is
  `aboard.html:98`, which blanks the whole board in the human's face one
  round-trip later.
- **`ui`'s `table` is read-only by design** — the `table` _tab type_ is the
  editable one. Consolidating into `ui` costs you editable cells, click-to-sort
  and right-click copy-as-markdown. Replace notes with `field longtext`.
- **`checklist` writes booleans**, and `asText` renders a non-string as
  `JSON.stringify` — so a ticked box reads `true` in the Summary. Legible, but if
  you want words in the summary use a two-option `select` instead.
- **One tab means one `note` and one `touched` marker**, and `aboard export` gives
  one blob rather than per-topic exports. Real costs; worth it for a summary that
  cannot lie.
- **Nested bind paths auto-create** (`writeBound` walks and fills), so
  `"b1.choice"` is safe. Flat keys (`b1_choice`) are easier to eyeball in a
  read-back dump.

---

## When NOT to use this shape

- **You want the structure argued with** → `dag` or `kanban`. This shape asks
  "which of these?", not "is this the right set?". The human can only pick from
  options you wrote.
- **You need an approval _record_, with reasons, undo, and "who decided what"** →
  `gate`. It keeps `decided[]` with `verdict`, `reason`, `reasonAddedAt`, `by`,
  `at`, and an undo that stays on the record. This shape keeps only the current
  value: change a select and the previous answer is gone.
- **The decisions are genuinely independent and worked in different sittings** →
  separate tabs, each with its own `note` and `touched` marker, and accept that
  there can be no live cross-tab summary.

**The honest pairing:** this shape for _deciding_, `gate` for _committing_. If
both matter, run the wizard first and generate the gate queue from the answers —
but say plainly that the generation is you reading and rewriting, because nothing
on the board computes or executes.

---

> This lives in `.aboard/recipes/` — the project's own recipe directory, which is
> gitignored along with the rest of `.aboard/`. If it earns its place, promote it
> into `_aboard/recipes/` (committed, shared with the team) or into
> `pkg/aboard/recipes/builtin/` (shipped in the binary, indexed in the skill),
> where the next session will actually find it.

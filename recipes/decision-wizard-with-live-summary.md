---
name: decision-wizard-with-live-summary
description: "One ui tab of internal panels — N decision panels plus a Summary panel that reads the same state.data the fields write, so it cannot go stale."
when_to_use: "When you have put a pile of findings in front of the human and need a verdict on each, and they want to see what they have chosen so far without hunting through tabs. This shape is for DECIDING; a gate tab is for committing."
tags: [ui, tabs, bind, decisions, summary, wizard]
requires:
  min_schema: 1
---

# A decision wizard with a live summary

**One `ui` tab. Internal panels. A read-only Summary panel that cannot go stale.**

Use this when you have put a pile of findings in front of the human and need a
verdict on each — options, a recommendation, a place to say "not like that" —
and they want to see what they have chosen so far without hunting through tabs.

## The constraint that decides the shape

**A `bind` path never reaches outside its own tab.** Every mechanism here is
tab-scoped, and all three dead ends are worth knowing before you design:

| you might try | what actually happens |
| --- | --- |
| a `ui` tab summarising four other tabs | `resolve()` in `views/ui.js` walks the path over `data()`, and `data()` is `ctx.state.data` — this tab's own |
| an `html` tab reading the board document | `views/html.js` posts the frame `ctx.state.data` and nothing else, and the frame runs `sandbox="allow-scripts"` under `connect-src 'none'` — it cannot fetch anything |
| `stateFrom` pointing at the others | `stateOf()` in `aboard.html` resolves ONE hop through `tabById`, not an aggregate |

So a summary spread across N tabs can only ever be **a copy you refresh** — stale
the moment the human changes a pick, and looking authoritative while it lags.

**Put every bound value in one tab's `state.data` and the problem disappears.**
The Summary panel reads the same object the fields write. It is not synchronised
with the answers; it _is_ the answers. That is the reason to prefer one tab here.
Not tidiness — capability.

## The shape

`tabs` takes `panels: [{label, children}]`, remembers the open panel **per
viewer** (never written to state — it would flip the panel under everyone else),
and rebuilds a panel's children on every switch. That rebuild is what makes the
Summary live: switching to it re-resolves every bind from current data.

`aboard recipes show decision-wizard-with-live-summary --template` prints the
skeleton below on its own. It carries no `id`: allocate one from the document's
`nextId` as `apply-a-write` describes.

**Give the `key` a subject.** `port-decisions`, not `decisions`: `key` is what
`upsertTab` matches on, so a generic one silently takes over a tab that already
exists under that name — and `decisions` is exactly the key the example board's
`gate` tab ships with, which an `upsertTab` would overwrite in place, turning the
human's approval record into your wizard.

```aboard-template
{
  "key": "port-decisions",
  "name": "Port decisions",
  "type": "ui",
  "note": "One tab, N panels. The middle panels decide; the Summary panel is read-only and live.",
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
            { "type": "text", "value": "What was investigated, and what each panel below is for." },
            { "type": "stat", "value": 1, "label": "decisions on this tab" }
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
                { "type": "text", "value": "What is actually true, and where you read it." },
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
                  "type": "kv",
                  "pairs": [
                    { "key": "B1 granularity", "value": { "bind": "b1" } },
                    { "key": "B1 note", "value": { "bind": "b1_note" } },
                    { "key": "Delete the dead fixture", "value": { "bind": "hk_a" } }
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

### One decision, as a card

The `B1` panel in the template above is one. Four rules earn their place in it:

- **A sentinel for undecided, never `""`.** A select whose bound value is missing
  or empty draws blank rather than any option, so "they have not reached this
  yet" and "they picked the empty one" look identical on screen and read
  identically in your read-back. `"— not decided —"` as both the default value in
  `data` and the first option says which it is.
- **Put `(recommended)` in the option label.** The human should not have to
  cross-reference prose to find which one you meant.
- **Always offer `none of these — see my note`.** A wizard with no exit trains
  people to pick the nearest wrong answer.
- **A `longtext` note per decision, not one at the end.** A note attached to a
  decision survives promotion into the repo beside it; a note in a general box
  loses its subject.

### The Summary panel

Two shapes work, and the choice is layout, not capability:

- **`kv` with `pairs: [{key, value}]`** — both sides resolve a `{bind}`, so
  `{"bind": "b1"}` as a value prints the answer. Shortest to write, and it lines
  the labels up in a column for you.
- **`caption` + `text` in a `row`, one per value** — more to write, and worth it
  when a label needs to be long or a value needs to sit under rather than beside
  it.

Read-only because there is nothing there to edit — no `field`, no `checklist`, no
`button`. That is a stronger guarantee than a `readOnly` flag: the components
that write simply are not present.

**A typed count is the one thing not to put here.** Nothing on the board
computes, so `"3 of 8 decided"` written into the tree is right until the first
answer and looks authoritative for the rest of the session — the shadow record
this whole shape exists to avoid, rebuilt inside the Summary itself. The `stat`
in the template counts how many decisions the tab HOLDS, which is your own data
and moves only when you rewrite the tab; everything that tracks the human is a
`{bind}`. Count what has been answered when you read the tab back, where you are
the one who can be correct about it.

`aboard capabilities ui` lists every component and the props it reads, including
which of them carry display text. Consult it rather than guessing; do not expect
markdown in any of them — every `ui` component sets text, never markup, so a
`**bold**` in a `text` value renders as four literal asterisks. Use `code` for a
command and `notice` for a called-out line.

## Reading it back

Two routes, both with no server running:

```sh
aboard export port-decisions
```

That prints the whole tree as an outline with every bind RESOLVED — the panels,
each field's label and the answer sitting at its bind path, and each checklist
item as `- [x]` or `- [ ]`. It is the form you paste into the project's own
document.

```sh
python3 -c "
import json
d = json.load(open('.aboard/aboard.json'))
t = next(t for t in d['tabs'] if t.get('key') == 'port-decisions')
print(json.dumps(t['state']['data'], indent=1, ensure_ascii=False))"
```

That is the raw record, keyed the way you wrote the binds — the form you act on.

Then **promote the outcome into whatever the project uses for decisions**. The
board is where a thing is worked out; it is not the record. A verdict whose only
trace is `state.data` dies with the machine. Carry the **reason** across, not just
the verdict — that is what the per-decision `longtext` is for, and it is the half
that stops the argument recurring.

If you need the answers before you can continue, block rather than poll:

```sh
aboard wait --for "answer <tab-id>" --note "waiting on the port decisions"
```

## Gotchas

- **`ui` fails silently and successfully.** `apply` prints `applied` and exits 0
  for a tree that draws an empty box. The write-time checks do descend into a
  `ui` tree — an unknown component, an unknown prop, a `{bind}` that resolves
  nowhere and a colour name this board does not have all warn — so read stderr,
  and use `apply --check` to run them without writing, or `apply --strict` to
  refuse on any warning. Then render it and **look**: nothing here can see a
  layout that is legal and unreadable.
- **An unknown component type draws a visible marker; an unknown PROP draws
  nothing at all.** The second is the one that reads as a styling problem.
- **`checklist` writes booleans**, and a non-string renders as its JSON — so a
  ticked box reads `true` in the Summary. Legible; if you want words there, use a
  two-option `select` bound to the same path instead.
- **`ui`'s `table` is read-only by design** — the `table` TAB type is the editable
  one. Consolidating into `ui` costs you editable cells, click-to-sort and
  right-click copy-as-markdown. Replace notes with a `longtext` field.
- **One tab means one `note`, one `touched` marker and one `export`.** Real costs,
  worth it for a summary that cannot lie.
- **Nested bind paths auto-create**, so `"b1.choice"` is safe to write into an
  empty `data`. Flat keys (`b1_choice`) are easier to eyeball in a raw read-back.

## When NOT to use this shape

- **You want the structure argued with** → `dag` or `kanban`. This shape asks
  "which of these?", not "is this the right set?": the human can only pick from
  options you wrote.
- **You need an approval RECORD, with reasons, undo and who decided what** →
  `gate`. It keeps each decision with its verdict, reason, author and time, and an
  undo that stays on the record. This shape keeps only the current value: change a
  select and the previous answer is gone.
- **The decisions are genuinely independent and worked in different sittings** →
  separate tabs, each with its own `note` and `touched` marker, and accept that
  there can be no live cross-tab summary.

**The honest pairing:** this shape for _deciding_, `gate` for _committing_. If
both matter, run the wizard first and write the gate queue from the answers — but
say plainly that the writing is you reading and rewriting, because nothing on the
board computes or executes.

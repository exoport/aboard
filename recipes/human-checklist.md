---
name: human-checklist
description: "A list of things only the human can do, each item's explanation, tick and notes box together in one card so nothing has to be scrolled between."
when_to_use: "When you are handing over steps only a person can carry out — verify this by hand, install that, look at it in the real app — and you need to read back which ones they actually did. Not for work you could do yourself, and not for an approval on the record."
tags: [ui, checklist, handover, verification]
requires:
  min_schema: 3
---

# A checklist the human works through

**One `ui` tab. One card per item: the explanation, the tick and the notes box,
in that order, together.**

Use it for the list you would otherwise write as a numbered paragraph and then
have to chase — a hand-verification list, an install checklist, the five things
in the release ritual that no test can perform. The items are the human's to do
and yours to read back.

## Why one card per item

The obvious shape is a `stack`: a markdown block of instructions on top, a table
of ticks below. It was built that way once, and the human's verdict was the
reason this recipe exists:

> "I had to scroll top to bottom to read the instruction and then go down for
> the check, up for the next instruction."

Instructions and the place to answer them are one thing per item, not two lists
that must be kept aligned by the reader. So: one `card` per item, holding

1. the **title** — what to do, in a few words;
2. the **explanation** — why, and how to tell it worked. A `text`, plus a `code`
   node if there is a command to run;
3. the **tick** — a one-item `checklist` bound to `items.<id>.done`;
4. the **notes** — a `longtext` field bound to `items.<id>.notes`, for what they
   saw, or why they could not.

The notes box is not optional furniture. "Did not work" with nothing beside it
costs you a whole round trip to find out what happened.

## The header that cannot go stale

Put a `kv` at the top with one row per item, each value bound to that item's
`done` path. It reads the same values the ticks write, so it is not a copy and
cannot lag — the same property the `decision-wizard-with-live-summary` Summary
panel has, for the same reason.

A bound boolean prints as `true` or `false`: legible, if plain. If you want words
up there, bind a two-option `select` (`not yet` / `done`) to `items.<id>.status`
instead of a tick and read that path back — the tick is the friendlier gesture,
the select is the friendlier summary, and it is a real trade rather than an
oversight.

**A literal "3 of 8 done" is the one thing not to write there.** Nothing on the
board computes: a number you type into the tree is right until the first tick and
wrong afterwards, and it looks authoritative the whole time. The honest header is
a static `stat` of how many items there ARE — which is your own data and cannot
drift — beside the live per-item rows. Count the ticks yourself when you read the
tab back, where you are the one who can be correct about it.

## The template

`aboard recipes show human-checklist --template` prints the skeleton on its own.
It has two example items; make one card per real item and one `data.items` entry
to match. Allocate the tab's `id` from the document's `nextId` as
`apply-a-write` describes.

**Give the tab's `key` a subject.** `upsertTab` matches on `key`, so a generic
one takes over whatever already answers to it — `handover-checks` rather than
`checks`.

**The item ids are yours and they are not board ids.** `chromium`, `panel`,
`smoke-run` — semantic keys are right inside `data`, where they make the raw
read-back legible. The `bb<n>` rule is for objects the board hands out ids for;
a key in your own data blob is not one of them.

```aboard-template
{
  "key": "handover-checks",
  "name": "Your checks",
  "type": "ui",
  "note": "Things only you can do. Tick each one and say what you saw; I read this tab back.",
  "state": {
    "data": {
      "items": {
        "chromium": { "done": false, "notes": "" },
        "panel": { "done": false, "notes": "" }
      }
    },
    "intents": [],
    "root": {
      "type": "col",
      "children": [
        {
          "type": "card",
          "title": "Where this stands",
          "accent": true,
          "children": [
            { "type": "stat", "value": 2, "label": "items to confirm" },
            {
              "type": "kv",
              "pairs": [
                { "key": "Chromium runs", "value": { "bind": "items.chromium.done" } },
                { "key": "The panel opens", "value": { "bind": "items.panel.done" } }
              ]
            },
            { "type": "caption", "value": "These rows read the same values the ticks below write, so they cannot go stale." }
          ]
        },
        {
          "type": "card",
          "title": "1 · Chromium runs on this machine",
          "children": [
            { "type": "text", "value": "The browser suite drives a real Chromium, so it has to be installed and on PATH before any of it means anything. It should print a version and exit." },
            { "type": "code", "value": "chromium --version" },
            { "type": "checklist", "items": [{ "label": "Done — it printed a version", "bind": "items.chromium.done" }] },
            { "type": "field", "field": "longtext", "label": "What you saw", "bind": "items.chromium.notes" }
          ]
        },
        {
          "type": "card",
          "title": "2 · The panel opens in the editor",
          "children": [
            { "type": "text", "value": "Open the board in the editor's panel and switch between two tabs. Only a real editor window shows whether the panel hosts it correctly, which is why this one is yours and not mine." },
            { "type": "checklist", "items": [{ "label": "Done — the board drew and the tabs switched", "bind": "items.panel.done" }] },
            { "type": "field", "field": "longtext", "label": "What you saw, or where it stopped", "bind": "items.panel.notes" }
          ]
        }
      ]
    }
  }
}
```

## Writing the items

- **One card per item, in the order they must be done.** Number the titles, so a
  conversation about "the third one" has something to land on.
- **Say how they will know it worked**, not just what to do. "It should print a
  version and exit" is the half that lets them tick it without asking you.
- **Keep the explanation to a sentence or two**, and put a command in a `code`
  node rather than in the middle of a sentence. Every `ui` component renders text
  and never markup, so backticks and `**bold**` arrive as literal characters —
  `aboard capabilities ui` lists which prop of each component carries its text.
- **Do not ask for anything you could do yourself.** A list of things you were too
  lazy to check is how a checklist stops being read.
- **The tick label is the claim they are making** — "Done — it printed a version",
  not "OK". You will read that label back next to their answer.

## Reading it back

```sh
aboard export handover-checks
```

The outline prints each card, each item as `- [x]` or `- [ ]` read from the bind,
and each notes field with the human's words resolved from `state.data`. That is
the form to paste into the handover document.

```sh
python3 -c "
import json
d = json.load(open('.aboard/aboard.json'))
t = next(t for t in d['tabs'] if t.get('key') == 'handover-checks')
items = t['state']['data']['items']
done = sum(1 for i in items.values() if i.get('done'))
print(f'{done} of {len(items)} done')
for key, item in items.items():
    print(('[x] ' if item.get('done') else '[ ] ') + key, '—', item.get('notes', '') or '(no note)')"
```

`state.data.items` is the record. That is also where the count belongs: computed
at read time by you, never written into the tab.

An unticked item is not a failure — it is "not yet". Say which is which when you
report back, and quote the notes rather than summarising them; the note is the
only part of this the human typed.

If you need the answers before you can continue, block rather than poll:

```sh
aboard wait --for "answer <tab-id>" --note "waiting on the handover checks"
```

`answer` fires when a HUMAN changed that tab, which is exactly one tick or one
note. It releases on the first one, so re-read and decide whether to wait again
rather than assuming the whole list came back.

## When NOT to use this shape

- **You need the approval on the record, with reasons and undo** → `gate`. A tick
  is a state, not a decision anybody signed.
- **You are asking questions rather than handing over work** → a `form` tab, or
  `decision-wizard-with-live-summary` if there are enough of them to want a
  summary.
- **The list is yours, not theirs** → a `kanban` with `readOnly`, which says at a
  glance that the human is reading and you are the one moving the cards.

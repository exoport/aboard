---
name: react-to-their-edits
description: "Read what the human asked for, then diff the board against the copy you last applied to find what they changed, dismissed or deleted."
when_to_use: "At the start of a turn after the human has had the board, or whenever they say \"I moved things around\". A request is a direct ask; a dismissed marker means they read it; a deleted tab is an answer."
tags: [read-back, diff, requests, touched, pendingRemoval]
---

# React to their edits

Their words first. `requests` is the one channel that runs their way — a note on a
tab saying what is wrong with it — and it is a command rather than a diff because
it was written while nobody was watching:

```sh
aboard requests                       # pending, oldest first, naming the tab
aboard requests done bb199 --by agent-1 --note "redrew the arrow"
```

Stamp one only when you have actually done it: the strike-through and your note
are the only feedback they get. You may not create, edit or delete one — the
server restores the list if you try, including when you simply hand the document
back without the field. Their deleting a request is how it goes away, and a
request you stamped that has since vanished means they consider it settled.

Then everything they changed by hand:

```js
const now  = JSON.parse(require('fs').readFileSync('.aboard/aboard.json','utf8'));
const before = JSON.parse(require('fs').readFileSync('/tmp/next-aboard.json','utf8'));
const map = (d) => Object.fromEntries(d.tabs.map((t) => [t.id, t]));
const a = map(before), z = map(now);

for (const id of Object.keys(z)) {
  const p = a[id], q = z[id];
  if (!p) { console.log('new tab ' + id + ' ' + q.name); continue; }
  if (JSON.stringify(p.state) !== JSON.stringify(q.state)) console.log('changed ' + id + ' ' + q.name);
  if (p.touched && !q.touched) console.log('read+dismissed ' + id + ' ' + q.name);
  const asked = (t) => (t.requests || []).filter((r) => !r.done).map((r) => r.id).join();
  if (asked(p) !== asked(q)) console.log('requests changed on ' + id + ' — run `aboard requests`');
  if (!p.pendingRemoval && q.pendingRemoval) console.log('removal pending ' + id);
}
for (const id of Object.keys(a)) if (!z[id]) console.log('the user deleted ' + id);
```

A dismissed marker means they read it. A tab they deleted is an answer to a
removal request — do not re-create it.

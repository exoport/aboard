---
name: react-to-their-edits
description: "Diff the board against the copy you last applied to find what the human changed, dismissed or deleted."
when_to_use: "At the start of a turn after the human has had the board, or whenever they say \"I moved things around\". A dismissed marker means they read it; a deleted tab is an answer."
tags: [read-back, diff, touched, pendingRemoval]
---

# React to their edits

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
  if (!p.pendingRemoval && q.pendingRemoval) console.log('removal pending ' + id);
}
for (const id of Object.keys(a)) if (!z[id]) console.log('the user deleted ' + id);
```

A dismissed marker means they read it. A tab they deleted is an answer to a
request — do not re-create it.

---
name: apply-a-write
description: "The read-modify-apply shape every board write takes, plus the id allocator and the upsertTab helper."
when_to_use: "Before any write to the board — this is the shape all the other recipes assume. Read it first if you are about to change a tab, and re-read it if a write came back 409."
tags: [core, write, ids, compare-and-set]
---

# Apply a write

Every write is the same shape: read the whole document, modify it, apply it.
Never compose a document from scratch — you would drop the tabs you are not
touching.

```sh
aboard status    # confirm it is running, get the URL
```

```sh
node -e "
const fs = require('fs');
const b = JSON.parse(fs.readFileSync('.aboard/aboard.json', 'utf8'));
// … mutate b.tabs …
fs.writeFileSync('/tmp/next-aboard.json', JSON.stringify(b, null, 2));
"
aboard apply --by "agent-1" < /tmp/next-aboard.json
```

If it refuses, re-read and redo. Do not reach for `Edit`.

## Helpers worth pasting

```js
// The board-wide counter — the ONLY correct allocator. Never "highest in this
// container + 1": that hands out an id twice as soon as anything is deleted,
// silently re-pointing every instruction that referenced the old object.
const newId = (b) => 'bb' + (b.nextId++);

// Reuse a tab that already exists for this purpose instead of opening another.
function upsertTab(b, key, make) {
  const found = b.tabs.find((t) => t.key === key);
  if (found) { Object.assign(found, make(found)); return found; }
  const tab = make(null);
  tab.id = newId(b);
  tab.key = key;
  b.tabs.push(tab);
  return tab;
}
```

`key` is the guard against tab sprawl: one key per ongoing purpose, updated in
place turn after turn.

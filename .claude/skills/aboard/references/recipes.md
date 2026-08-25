# Recipes

Every write is the same shape: read the whole document, modify it, apply it.
Never compose a document from scratch — you would drop the tabs you are not
touching.

```sh
aboard status    # confirm it is running, get the URL
```

```sh
node -e "
const fs = require('fs');
const b = JSON.parse(fs.readFileSync('aboard.json', 'utf8'));
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

## Show a structure the user can argue with

```js
upsertTab(b, 'plan', () => ({
  name: 'Migration plan', type: 'dag',
  state: { columns: ['todo', 'doing', 'done'], nodes: [
    { id: 'bb51', title: 'Ingest',    parent: null,   status: 'done',  order: 0, note: 'S3 -> queue' },
    { id: 'bb52', title: 'Normalise', parent: 'bb51', status: 'doing', order: 0, note: 'schema drift is the risk' },
    { id: 'bb53', title: 'Enrich',    parent: 'bb52', status: 'todo',  order: 0, note: '' },
  ] },
}));
```

Then say what to do with it:

> Plan's on the board. If I've got the dependencies wrong, drag a node onto its
> real parent and I'll pick it up.

Add a second tab reading the same data as a kanban:

```js
const plan = b.tabs.find((t) => t.key === 'plan');
upsertTab(b, 'progress', () => ({ name: 'Progress', type: 'kanban', stateFrom: plan.id }));
```

## Ask for a decision

```js
upsertTab(b, 'cutover', () => ({
  name: 'Cutover', type: 'form',
  state: { title: 'Cutover', intro: 'Answer and I will act.', fields: [
    { id: 'strategy', type: 'select',   label: 'Strategy', options: ['big bang', 'dual write', 'shadow read'], value: 'dual write' },
    { id: 'window',   type: 'range',    label: 'Downtime (min)', min: 0, max: 60, step: 5, value: 10 },
    { id: 'notes',    type: 'textarea', label: 'Anything I am missing', value: '' },
  ] },
}));
```

Stop and let them answer. Read `state.fields[].value` back and restate their
answers before acting.

## Look at this, then decide that — one composite tab

This is usually the right shape, and it beats three tabs the user must correlate.

```js
upsertTab(b, 'review', () => ({
  name: 'Migration review', type: 'stack',
  state: { blocks: [
    { id: 'bb61', type: 'dag',    title: 'Dependencies', state: { nodes: [ /* … */ ], columns: ['todo','doing','done'] } },
    { id: 'bb62', type: 'form',   title: 'Decide',       state: { title: 'Cutover', fields: [ /* … */ ] } },
    { id: 'bb63', type: 'markup', title: 'On screen',    state: { images: [ /* … */ ] } },
    { id: 'bb64', type: 'notes',  title: 'Open questions', state: { text: '- who owns rollback?' } },
  ] },
}));
```

Blocks are collapsible and each keeps its own state. Any type except `stack`.

## Point at part of an image

Put the image in `assets/`, then:

```js
upsertTab(b, 'layout', () => ({
  name: 'Layout review', type: 'markup',
  state: {
    layout: 'side-by-side',
    images: [
      { id: newId(b), src: 'assets/before.png', caption: 'Before', annotatable: true, regions: [], strokes: [] },
      { id: newId(b), src: 'assets/after.png',  caption: 'After',  annotatable: false },
    ],
  },
}));
```

`annotatable: false` makes the second image a reference only. Reading marks back,
converted to pixels and named:

```js
const tab = b.tabs.find((t) => t.key === 'layout');
const W = 1440, H = 900;                       // that image's real dimensions
for (const img of tab.state.images || []) {
  for (const r of img.regions || []) {
    console.log(`${img.id}/${r.id}: (${Math.round(r.x*W)},${Math.round(r.y*H)}) `
      + `${Math.round(r.w*W)}x${Math.round(r.h*H)} — ${r.note || 'no note'}`);
  }
  for (const s of img.strokes || []) {
    const pts = String(s.points || '').split(' ').map((p) => p.split(',').map(Number));
    const xs = pts.map((p) => p[0]*W), ys = pts.map((p) => p[1]*H);
    console.log(`${img.id}/${s.id}: scribble near `
      + `(${Math.round(Math.min(...xs))},${Math.round(Math.min(...ys))}) — ${s.note || 'no note'}`);
  }
}
```

Then name the element each mark landed on, so they can see you understood.

## Coordinate with another agent, visibly

```js
const chat = upsertTab(b, 'coord', () => ({ name: 'Coordination', type: 'chat', state: { messages: [] } }));
chat.state.messages ||= [];
chat.state.messages.push({
  id: newId(b),                 // never length + 1: deleting one hands the id out twice
  at: new Date().toISOString(),
  by: 'agent-1',
  text: 'Taking the schema work. Leaving the API surface to you.',
});
```

Use `agent-1` / `agent-2` / `agent-<role>` for `by` so several agents read as
distinct actors. Messages with
`by: "human"` are the user interjecting — answer them.

## Build something interactive that no type covers

```js
upsertTab(b, 'sorter', () => ({
  name: 'Risk sorter', type: 'html',
  state: {
    html: [
      '<h3>Rank by risk</h3><ul id="l" style="list-style:none;padding:0"></ul>',
      '<script>',
      '  var items = aboard.get().order || ["schema drift","auth rewrite"];',
      '  var l = document.getElementById("l");',
      '  function draw(){ l.textContent="";',
      '    items.forEach(function(t,i){',
      '      var li=document.createElement("li");',
      '      var up=document.createElement("button"); up.textContent="↑";',
      '      up.onclick=function(){ if(i>0){ var x=items[i-1]; items[i-1]=items[i]; items[i]=x;',
      '        aboard.set({order:items}); draw(); } };',
      '      var s=document.createElement("span"); s.textContent=" "+(i+1)+". "+t;',
      '      li.appendChild(up); li.appendChild(s); l.appendChild(li); });',
      '    aboard.fit(); }',
      '  draw();',
      '<\/script>',
    ].join('\n'),
    data: {},
  },
}));
```

The frame is sandboxed with no network access. It persists through
`aboard.set(value)`, which lands in `state.data` — read it back from there.

## Ask to remove a tab

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

## React to their edits

```js
const now  = JSON.parse(require('fs').readFileSync('aboard.json','utf8'));
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

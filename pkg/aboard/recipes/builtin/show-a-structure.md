---
name: show-a-structure
description: "Put a plan or a dependency set on the board as a dag the human can drag into the right shape, and mirror it as a kanban."
when_to_use: "When you have inferred a structure — a plan, a dependency graph, an order of work — and want it argued with rather than approved in prose. Also when the same nodes want a second reading by status."
tags: [dag, kanban, plan, stateFrom]
---

# Show a structure the user can argue with

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

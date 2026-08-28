---
name: composite-review-tab
description: "One stack tab holding several renderers — look at this, then decide that — instead of three tabs the human must correlate."
when_to_use: "Whenever the evidence and the decision belong together: a graph plus the form that acts on it, a screenshot plus the open questions. This is usually the right shape."
tags: [stack, review, composite]
---

# Look at this, then decide that — one composite tab

This is usually the right shape, and it beats three tabs the user must correlate.

```js
upsertTab(b, 'review', () => ({
  name: 'Migration review', type: 'stack',
  state: { blocks: [
    { id: 'ab61', type: 'dag',    title: 'Dependencies', state: { nodes: [ /* … */ ], columns: ['todo','doing','done'] } },
    { id: 'ab62', type: 'form',   title: 'Decide',       state: { title: 'Cutover', fields: [ /* … */ ] } },
    { id: 'ab63', type: 'markup', title: 'On screen',    state: { images: [ /* … */ ] } },
    { id: 'ab64', type: 'notes',  title: 'Open questions', state: { text: '- who owns rollback?' } },
  ] },
}));
```

Blocks are collapsible and each keeps its own state. Any type except `stack`.

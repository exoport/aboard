---
name: ask-for-a-decision
description: "Ask several questions at once as a form with typed fields, then read the answers back by field id."
when_to_use: "When you would otherwise write a paragraph containing three questions. Use it for typed input — a choice, a number, a bit of free text — not for an approval you need on the record."
tags: [form, questions, input]
---

# Ask for a decision

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

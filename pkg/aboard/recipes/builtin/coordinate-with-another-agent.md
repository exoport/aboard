---
name: coordinate-with-another-agent
description: "Open a chat tab as the channel between sessions, where the human can watch the handoff and interject."
when_to_use: "When more than one session is working the same project and they need to divide the work in the open. Also the place to answer a human who has interjected in the transcript."
tags: [chat, multi-session, coordination]
---

# Coordinate with another agent, visibly

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

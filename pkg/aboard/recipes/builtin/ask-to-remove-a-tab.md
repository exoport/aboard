---
name: ask-to-remove-a-tab
description: "You cannot delete a tab. Set pendingRemoval with a reason worth reading and let the human answer it."
when_to_use: "When a tab is superseded or spent and you want it gone. Never drop it from the array — the server restores it with a generic reason, which is worse for the human than the one you would have written."
tags: [guarantees, cleanup, pendingRemoval]
---

# Ask to remove a tab

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

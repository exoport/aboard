# Development

Working documents for the people — and the sessions — building `aboard`. Nothing here
is user-facing: the docs a user reads live in [`../docs/`](../docs/), and this directory
is the record of how the thing got made and what is still owed.

Two folders, split by **who the document is addressed to**:

- **[`planning/`](planning/)** — plans. A plan is the authoritative brief for a body of
  work: the decisions taken with the human, marked DECIDED where they are not to be
  relitigated, and the shape of the commits that carry them out. A plan is written
  before the work and edited during it; its Status line says how far it has got.
- **[`handoffs/`](handoffs/)** — briefs for a later session. A handoff is what one
  session leaves for the next when the work is real but not next: the context it would
  otherwise have to rediscover, what was measured, and what is proposed rather than
  done. Each one says at the top whether it is live, superseded, or kept only as design
  rationale — read that line first, because a superseded handoff still holds the
  reasoning and none of the instructions.

**Order of the live handoffs**, decided with the human on 2026-08-25:

1. the review fixes — `development/research/review-d6c2f84-20260825.md`, the two highs and the
   behaviour mediums, each with a test that fails before and passes after;
2. [`handoffs/handoff-e2e-browser-suite.md`](handoffs/handoff-e2e-browser-suite.md) — the real
   browser suite, which is what makes everything after it verifiable;
3. [`handoffs/handoff-json-hot-paths.md`](handoffs/handoff-json-hot-paths.md) — structural JSON
   fixes, then json/v2, no per-tab resources unless a measurement reopens it;
4. `handoff-13-features.md`, then `handoff-board-for-vscode-panel.md`.

`handoff-phase-e-finish.md` is the record of the port and is done.

`git log` is a real source here too. Commit messages in this repo carry the reasoning
and the mistakes found on the way, so a decision you are about to re-derive is often
already argued out in the commit that made it.

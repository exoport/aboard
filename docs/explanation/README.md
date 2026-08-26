# Explanation

Explanation docs answer "why", and the "how does this actually work" that is asked in
order to understand rather than in order to act — design rationale, conceptual
background, the shape of the problem aboard solves. Unlike [Tutorials](../tutorials/) and
[How-to guides](../how-to/), explanation is not action-oriented; unlike
[Reference](../reference/), it is not exhaustive description. It is discursive. A
reader of explanation is trying to deepen their understanding, not get something done
right now.

Several of these pages record **closed** decisions. They are here so that a future
session — or a future reader with a good idea — finds the reason before re-deriving the
proposal. A rule with a reason outlives everything; a rule without one gets relitigated
every few months.

## Available explanation

- [how-aboard-runs.md](how-aboard-runs.md) — the moving parts, in one pass: one binary, the port derived from the path, the instance record, the two ways in and the one way through, the watcher, the journal as the undo, and what the server will not do.
- [why-a-local-non-authoritative-channel.md](why-a-local-non-authoritative-channel.md) — what the board is FOR and what it must never be: three words that are three separate claims, the three tiers matched by lifetime, where a promoted thing goes, and which comes first — the board or the document.
- [why-tabs-are-data.md](why-tabs-are-data.md) — a tab is a name, a `type` and its own `state`; what that buys, and the three declarations a new renderer needs.
- [why-the-guarantees-are-server-enforced.md](why-the-guarantees-are-server-enforced.md) — deleting a tab, clearing a change marker, un-acking a message, clearing another actor's read state, writing over the human's own requests: five things an agent must not be able to do even by accident.
- [why-nothing-in-the-ui-starts-a-session.md](why-nothing-in-the-ui-starts-a-session.md) — the board may ask and a session may choose to wait; no affordance starts an agent. Proposed twice, refused twice.
- [why-no-diff-renderer.md](why-no-diff-renderer.md) — closed, not deferred, and the reason kept: a diff tab makes asking cheap, and anything cheap gets overused.
- [why-html-tabs-are-sandboxed.md](why-html-tabs-are-sandboxed.md) — `connect-src 'none'` plus an opaque origin is the containment; `frame-ancestors` is not, and why it lists VS Code's webview origins.
- [why-two-identities.md](why-two-identities.md) — `aboard` and `ape-aboard` sharing one `.aboard/`: what differs, what must not, and why the manifest's app name is neither.
- [why-writes-are-serialised.md](why-writes-are-serialised.md) — one lock across read → compare-and-set → write, what it deliberately leaves out, and the fanout race that killed the process.
- [why-the-manifest-is-declared.md](why-the-manifest-is-declared.md) — declaration as the canonical source rather than a scrape; why controls are a list; why the command table replaced walking the flag registry; and why `gestures` stays deliberately unverifiable.

## Planned explanation

- **Why compare-and-set on the whole document.** Whole-file CAS is coarse; the alternative — per-tab merge — was tried and what it cost. Still worth its own page: [why writes are serialised](why-writes-are-serialised.md) argues the lock and the token, not the granularity.

Port derivation used to be listed here. It is answered in
[how-aboard-runs.md](how-aboard-runs.md) — stable URLs, no collisions between checkouts,
and what happens when a stranger holds the port — with the formula itself in
[the layout reference](../reference/layout.md#port-derivation).

## Writing explanation

- Take a position. If an alternative was considered and rejected, name it and say why.
- Discuss; don't instruct. The reader is here to think alongside you.
- Set context generously: "before we had X, things looked like Y" belongs here.
- Record the mistake that produced the rule. A rule whose failure mode is written down survives a rewrite; one that reads as taste does not.
- Link to [Reference](../reference/) for facts and [How-to](../how-to/) for action.

See the [Diátaxis explanation rubric](https://diataxis.fr/explanation/).

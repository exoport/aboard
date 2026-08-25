# Why there is no diff renderer

**Closed, not deferred.** A `diff` tab type — showing the human a change and asking them
to approve it — was proposed twice and refused twice. The second answer was *"forget it,
no future for it"*.

This page exists so that the idea is not re-derived and re-proposed a third time. It is a
good-looking idea; that is exactly why the reason has to outlive the conversation.

## The reason

**A diff tab makes asking cheap, and anything cheap gets overused.** It would become a
way to spam the human with every change.

That is the whole argument, and it is worth unpacking one level, because the failure is
not the tab — it is what having the tab does to the agent's judgement. Right now, showing
someone a change costs something to prepare: you have to decide what is worth their
attention, put it in a form that carries the decision, and say what you need from them. A
renderer that turns "here is a diff" into a one-line write removes that cost, and the
first thing removed friction does is remove the deciding.

Then the second-order effect: a human who is shown thirty diffs stops reading diffs. The
approval becomes a reflex, and the one change that genuinely needed their eyes goes
through with the same click as the twenty-nine that did not. **The tool that asks for
attention most cheaply is the tool that destroys attention fastest.**

## What to do instead

The need behind the request is real. Two things already cover it, and both keep the cost
in the right place:

- **`gate`** — for a change that needs a decision. Allow, deny, or edit-then-allow, each with a reason, and the reason is recorded beside the verdict where a later promotion can copy it. Pair it with `aboard wait --for "answer <tab>"` when you cannot continue without the answer.
- **The diff that already exists** — `git diff`, in the terminal, in the review tool the project already uses, in the PR. Code review is a solved problem with better tools than a board tab, and putting a worse copy of it on the board fragments where review happens.

If what you want is to show *structure* rather than a textual change — this plan versus
that one, the shape before and after — that is a `dag`, a `diagram`, or a `stack` holding
both plus the question. Those are shapes to argue with, not changes to rubber-stamp.

## The option was removed, not zeroed

When the board carried a ballot of possible next work, `diff` was **taken off it** rather
than scored zero. **An option on a ballot is an open question by definition**, and leaving
it there would have re-opened the decision every time someone read the list.

The same instinct applies to this page: do not add an "unless…" to it. A closed decision
with a hedge is an open decision wearing a disguise.

## See also

- [Why a local, non-authoritative channel](why-a-local-non-authoritative-channel.md) — the board's job is bandwidth, and bandwidth spent badly is worse than none.
- [Why nothing in the UI starts a session](why-nothing-in-the-ui-starts-a-session.md) — the other refusal, with the same shape: cheap actions get taken.

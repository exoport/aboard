# Why nothing in the UI starts a session

Stated by the human who owns this design, in these words: *"we don't want to trigger
agent sessions from the ui."*

So the board may **ask** — a gate request, a form, an action strip, an intent — and a
session may choose to **wait** (`aboard wait`). The notify button only releases a session
that had already decided to listen. What is ruled out:

- a server hook that spawns an agent process;
- a cron or a loop that wakes agents on a timer;
- any "start an agent" affordance anywhere in the interface.

The hook was proposed twice. Do not propose it a third time.

## The asymmetry is the whole design

Look at what the notify channel actually is. A session runs `aboard wait`, which holds an
open connection. The header shows *notify agent-1* with a lit dot, that session's note and
a countdown. The human presses it; every waiter is released. Nobody waiting → the button
is disabled.

The waiter is an open connection, so the count cannot go stale, and — the part that
matters here — **there is nothing to press when nobody is listening.** The board cannot
manufacture attention. It can only return it to a session that parked itself and said
why.

That is a deliberate inversion of the obvious design. The obvious design has the human
press a button and something starts working. It feels responsive and it is wrong, for
three reasons:

**A spawned session has no context.** The value of an agent session is what it is holding:
the file it just read, the failure it just reproduced, the argument it is halfway through
with you. A session started by a button starts empty, and the first thing it does is
reconstruct — badly, expensively, and with a confidence that does not match.

**Nobody chose to spend the money.** An agent run costs tokens and time. A UI affordance
that starts one moves that decision from a person who knows what they are asking for to a
button that looks like every other button. Anything cheap to press gets pressed.

**It makes the board a control plane.** The board is a *channel*: local, unversioned,
non-authoritative, with no authentication of any kind, reachable by anything on the
machine. A channel that can start processes is not a channel. The reason a stray click is
harmless here is that **nothing in the browser executes anything** — action buttons, gate
verdicts and `ui` buttons all *record* an intent or a decision, and the agent that asked
acts on it when it next looks. Keeping the board unable to start work is what keeps that
true.

## The honest consequence

A board with nothing waiting is simply not listening. There is no queue that a future
session will pick up, and no daemon minding the store.

The right response to that is to **say so**, not to make the board look live. The
heartbeat strip is the same idea applied to subagents: it pulses when a stamp is fresh,
ages, then goes dashed-grey after ten minutes whatever the phase claims. A subagent only
exists while it runs, so "last seen 16m ago" is the truth between messages, not a bug. A
cron to keep the dot green was proposed and scored one out of five: it would replace a
true statement with a comforting one.

If you want an agent to act on something the human just recorded, the sequence is: the
agent decides to wait, says what it is waiting for, and blocks. Then the button means
something.

## What this does not rule out

- **Blocking on a predicate.** `aboard wait --for "answer <tab>"` returns when that tab changed *and* a human did it. The session still chose to wait.
- **Long timeouts.** A session can park for as long as it likes; the connection is the record.
- **A human starting a session themselves**, in their terminal, with their context, having read what the board is asking. That is not an affordance in the UI — it is a person deciding.

## See also

- [HTTP API](../reference/http-api.md) — `/wait`, `/poke` and `/waiters`, the whole notify channel.
- [Why a local, non-authoritative channel](why-a-local-non-authoritative-channel.md) — the posture this follows from.
- [Why html tabs are sandboxed](why-html-tabs-are-sandboxed.md) — the other half of "a stray click is harmless".

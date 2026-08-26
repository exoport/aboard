# Why writes are serialised

A board has more than one writer by design. The human types in the browser, one session
applies a document from the terminal, another one is halfway through its own edit, and
none of them coordinates with the others. The whole point of the thing is that they do
not have to.

That only works if the server has an answer to "two of you at once". For a long time it
did not, and the answer it gave instead was the worst available one: it told both
writers they had succeeded.

## The failure this fixes

`POST /aboard.json` reads the current document, compares the caller's `__base` against
the live revision, reconciles the tab list against the four guarantees, and renames a
new file into place. Four steps, and until this change no lock spanned them.

So two overlapping posts each read the same document, each compared against the same
base, each passed, and each wrote. The last rename won. The loser got `200 OK`
and an `updatedAt` of its own, and the journal — the record a session reads to answer
"what changed while I was thinking?" — carried an entry for a write that is not in the
file. Under a barrier-synchronised reproduction it happened in 40 trials out of 40.

That is not "a race that occasionally loses an edit". It is compare-and-set not
working, in the one situation compare-and-set exists for. A check and the write it
guards have to be indivisible, or the check is advice.

The fix is a `sync.Mutex` held across all four steps. It is not clever and it is not
supposed to be.

The comparison itself was the second half of the same problem. The base was `updatedAt`,
a millisecond timestamp — so even under a perfect lock, two writes inside one millisecond
shared a token and a provably stale base passed the check. Measured at 4 collisions in 60
sequential writes. The token is a **revision counter** now (`rev`), stamped beside
`updatedAt` on every accepted write: it cannot collide, it cannot run backwards when the
clock does, and it is comparable, so a refusal can say *your base is rev 41 and the board
is at rev 43* rather than handing back two hashes that differ. A content hash — the
`stateSignature` the SSE path already computes — would have been just as correct as an
equality token and says nothing a reader can act on, which is why it is not the one.

## What the lock covers, and what it deliberately does not

Inside: the read of the state file, the comparison, the tab reconciliation, the id and
version stamps, the atomic write, and the journal append.

The journal append is the one worth arguing about, because it is the step that could
have gone outside. It stays in. Two writers could otherwise rename in one order and
append in the other, and a journal that reports an order that never happened is worse
than a journal with a gap: the gap is visibly a gap.

Outside: everything that tells somebody a write happened — the `/watch` stream, the
sessions blocked on `aboard wait`, the SSE frame that reaches every open page. Those are
notifications, they can fan out to a lot of listeners, and the record they refer to is
already on disk before any of them runs, so nobody is ever told about a write they could
then fail to find.

That is not free, and the cost is worth naming rather than implying there is none. A
writer releases the lock and then notifies, so two writes landing back to back can in
principle reach a `/watch` consumer in the opposite order to the one the journal file
records — the second writer has to complete an entire disk write inside the few
instructions between the first one's unlock and its notification, which is narrow but not
impossible under load. The trade is deliberate: the FILE is the record and it is ordered;
the stream is a nudge that says "go and look". A consumer reconstructing history reads
`aboard journal`, not a tail of `aboard watch`.

Pulling `/watch` alone back inside the lock would close that window and cost almost
nothing — the sends are non-blocking. It was not done for a reason that is about
evidence rather than performance: nothing here can write a test that fails before and
passes after for an inversion that needs a disk write to land inside a handful of
instructions, and an untested reordering of the write path is a worse trade than a
window that is written down. If the ordering of the live stream ever has to be a
guarantee, that is the change, and it needs a way to prove itself first.

## What the lock does not protect

**Two server processes on one state file.** A mutex is process-local, so this is a real
limit, not an oversight. It is handled a level up rather than here: when `serve` derives
its port it probes whoever holds it, recognises this project's own board, and refuses to
start beside it, naming the URL and pid of the one already running. One board, one
server, one lock.

"Handled a level up" is doing real work in that sentence, and the level above has a hole
in it: an explicit `--port` (or `PORT`) is taken literally and binds without probing, so
a second server for the same project is still something you can ask for by name. Neither
lock would see the other, and the two of them would race exactly as two goroutines used
to. Closing that is a separate change in the same review; it is recorded here because a
page about what serialisation guarantees should say where the guarantee is anchored and
how firmly.

**Anything that edits `.aboard/aboard.json` behind the server's back.** An `Edit` from a
tool, a text editor, a script with a redirect — none of them can be serialised by a lock
they never take. That is why the rule is "never write the state file directly" and why
`aboard apply` posts to the running board rather than writing the file: going through
the server is what puts an agent's write in the same queue as the human's.

**The base token's own resolution.** Serialisation guarantees the comparison happens
against a document nobody is mid-way through changing. What the comparison is worth
still depends on the value being compared — see [`__base`](../reference/http-api.md).

## The other half: broadcasting to a subscriber that just left

The same class of bug lived in the fanout. The server keeps a map of subscriber
channels, one per open page, and a second map for `/watch` consumers. Both used to copy
the channels under the lock, release it, and then send.

A client that disconnected inside that window unsubscribed the ordinary way: delete the
channel from the map, close it, return. The sender was holding a copy of a channel that
was now closed, and a send on a closed channel is a panic — on the state watcher's
goroutine, which is a bare `go f()` with no HTTP handler above it to recover. The whole
server died. The board went blank, every session parked on `wait` was released with
nothing, and `aboard status` reported a stale record for something that had been healthy
seconds earlier.

The sends now happen under the lock. The alternative — delete the channel from the map
without closing it, since nothing reads it afterwards — works equally well and was
rejected for one reason: it makes correctness rest on an invariant ("never close a
subscriber channel") that is spread across two files, enforced by nothing, and quietly
undone by the next reader who thinks a missing `close` is a leak. Sending under the
lock puts the rule where the code that has to obey it is. It costs nothing measurable:
every send is already non-blocking, so a wedged client cannot hold the lock, and the
critical section is a walk over a handful of channels with no I/O in it.

The two background goroutines — the state watcher and the UI watcher — now also run
under a recover that logs through the configured logger. That is defence in depth and
should stay that way: after the fix nothing in the fanout path panics, and if the
recover ever fires it means a new bug, which is exactly when a server dying silently is
most expensive. It does not restart the goroutine. A poll loop that panicked once will
panic again on the next tick, and a restart loop is how a bug turns into a log nobody
reads.

## What a caller sees now

Exactly what the API always promised. Of N simultaneous writes off one base, one gets
`200` and the rest get `409` with the live `rev`. A `409` is not an error to route
around: it means a real change landed while you were thinking. Re-read, redo the edit on
the fresh copy, apply again.

The browser does not simply retry, because the human is the one actor whose work cannot
be reconstructed: it fetches fresh, re-applies the tabs the human touched where the
server has not touched them, retries once, and stashes a genuine same-tab collision
behind a "Restore mine" notice rather than discarding what was just typed.

"The server has not touched this tab" is decided by VALUE, not by the bytes. Key order is
not part of a tab's meaning and no writer is asked to agree on one: `aboard init` writes
the example board as authored JSON and a `GET` serves those bytes unchanged, while the
server re-marshals through its own structs on the first accepted write — so a tab's
`note` is its third key before and its last key after, identical throughout. Comparing
the JSON text made that reordering look like an edit on both sides at once, which is the
definition of a collision, so on a freshly initialised board the first concurrent write
sent the human's edit to the stash instead of merging it.

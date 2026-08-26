# Brief 1 — the two races (plan-2 item 1)

Read `development/planning/plan-2-briefs/COMMON.md` first. Source of truth for the findings:
`development/research/review-d6c2f84-20260825.md`, section **High**. Code:
`pkg/aboard/server.go` (`postState`, `fanout`, `events`, `watch`, `watchUI`, `writeAtomic`) and
`pkg/aboard/journal.go` (`notifyWatchers`). Existing tests: `pkg/aboard/server_test.go`,
`pkg/aboard/poststate_test.go`.

## Scope — exactly this

1. **`postState` is serialised.** A dedicated mutex (not `s.mu`, which guards fanout) held from
   the `os.ReadFile` of the state file through `writeAtomic` and the journal append, so the
   read → CAS → reconcile → write span is one critical section. Consider whether any other
   writer of the state file (`/upload`? `/log`? the `apply` path? `init`?) must take the same
   lock — audit every call site that writes `aboard.json` and say what you found.
2. **Fanout never sends on a closed channel.** `fanout` copies subscriber channels under `s.mu`,
   releases, then sends; a client that unsubscribes in the window closes the channel first and
   the send panics. Fix `fanout` AND `notifyWatchers` (`journal.go`) with the same shape: either
   delete from the map without closing (nothing else reads it), or hold the lock across the
   non-blocking sends. Pick one, say why. Add `recover()`-style protection to `watch` and
   `watchUI` so a future panic in those goroutines is logged through `Options.Logger` rather
   than taking the process down — and say in the report whether that recover is defence in
   depth or load-bearing after your fix (it should be the former).
3. **Tests, both must fail on the current HEAD (verify by stashing nothing — copy the test in,
   run it against the unfixed code first, record the failure, then fix):**
   - N (≥ 8) concurrent `POST /aboard.json` off one base document, barrier-synchronised (e.g.
     a `sync.WaitGroup` + a start channel), assert exactly one 200 and the rest 409, that the
     document on disk equals the winner's, and that the journal has exactly one entry for it.
   - An SSE client that disconnects while broadcasts are in flight, under
     `go test -race -run <name> -count=20`, no panic, no data race. Do the same for the journal
     watcher (`/watch`).
4. **Write the concurrency story down**: a short section in `docs/explanation/` (find the right
   existing page or add one and link it from `docs/README.md`) and one paragraph in
   `docs/reference/http-api.md` saying that writes are serialised and what a 409 means under
   concurrency. Keep it to what is true after your change.

## Done when

Both reproductions from the review fail to reproduce; the tests above exist and are in
`go test -race ./...`; the full ladder is green (`make smoke` included, against a scratch
project on a detached server); the review file's two **High** entries have
`— fixed <describe>` appended (the orchestrator will add the commit hash).

## Out of scope

Everything in the review's Medium/Low lists (those are items 2 and 3). Do not change the CAS
token, origin checks, or anything in `aboard.html`.

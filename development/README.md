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

**The order of everything still owed is in
[`planning/plan-2_finish-line.md`](planning/plan-2_finish-line.md)** — decided with the human on
2026-08-25: review fixes, the end-to-end browser suite, the JSON hot paths, the eleven reviewed
features, the panel prerequisites, the VS Code extension (implemented, not installed), then close
the books. Its Status line says which item is next; each handoff's own Status line says whether
it is live, done, or superseded. `handoff-phase-e-finish.md` is the record of the port and is done.

## Small things queued, with the reason they were not done

Findings from the 2026-08-25 review that are REAL and were judged out of
proportion to fix now. Each says what it would take, so nobody has to re-measure.
Everything else in that review is dispositioned in
[`research/review-d6c2f84-20260825.md`](research/review-d6c2f84-20260825.md).

- **`serve --dev` follows a symlink out of the web tree.** `os.DirFS` does, and
  `static()`'s allow-list gates the URL PATH, not the file it resolves to — so a
  symlink at `views/anything.js` is served. Reproduced. Not fixed because the
  threat model is a hostile file in the developer's **own source tree**, reached
  through a flag they typed, on a loopback-only server. The fix is `os.OpenRoot`
  (Go 1.24+) whose `FS()` refuses symlinks — but it refuses **all** of them, so it
  would silently break a developer who symlinks part of their own web tree, which
  is a real workflow and the likelier outcome. Worth doing alongside anything that
  makes `--dev` reachable by someone other than its operator.
- **`POST /log?tab=bb999` creates a sidecar log for a tab that does not exist.**
  Each file is capped and rotated, so it is bounded in SIZE; what is unbounded is
  the NUMBER of files, one per distinct well-formed id. Not fixed because
  validating the id against the live document would break the legitimate order of
  `aboard log` — a pipeline can open before the tab it writes to is applied — and
  a cap on the count would be an arbitrary number. Worth doing with an
  uploads-style accounting pass (plan-2 item 6, `bb369`), which already has to
  count and prune files under `.aboard/`.
- **`BUILD_DATE` is the commit's date, so a dirty build is stamped with the time
  of the commit it was built from.** Deliberate and documented in the Makefile:
  `date -u` would change every second, defeating Go's build cache and paying for
  it twice in `make caps`, which builds on purpose. `+dirty` in the version
  string is what says the bytes are not the commit's. Only worth revisiting if
  the build date is ever used to decide something rather than to be read.

`git log` is a real source here too. Commit messages in this repo carry the reasoning
and the mistakes found on the way, so a decision you are about to re-derive is often
already argued out in the commit that made it.

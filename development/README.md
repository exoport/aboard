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

## What is open

**Nothing, except what is gated on the human.**
[`planning/plan-2_finish-line.md`](planning/plan-2_finish-line.md) — decided with the human on
2026-08-25 and executed over 2026-08-25/26 — is **complete**: the two races, the behaviour and
coverage review fixes, the end-to-end browser suite, the JSON hot paths, the eleven reviewed
features, the panel prerequisites, the VS Code extension (implemented, not installed), and this
bookkeeping. Every handoff in [`handoffs/`](handoffs/) says DONE or SUPERSEDED at the top;
`plan-1_port-from-spike.md` is the record of how the repo came to be and its 16 decisions still
bind.

**The one open list is [plan-2 §10](planning/plan-2_finish-line.md#10-gated-on-the-human--do-not-start-without-an-answer)** — ten
entries, all of them questions for the human rather than work to pick up. Do not start any of
them without an answer: the `boards` subcommand's registry, the remote and first tag, Go 1.27,
the `ape aboard` mount and the `aboard <cmd>` strings measured for it, installing the VS Code
extension (M6), the example board's prose, the notify confirmation's repaint, the journal record
the `apply` merge would need widening to survive a foreign rename, and a pointer to the
judgement calls in `handoffs/handoff-phase-e-finish.md`, which stand until overruled.

Below that, one more list: real findings that are nobody's blocker and nobody's question —
decisions for whoever next touches that code. They are not §10, and they are not work queued
for a next session either.

If you are resuming and looking for the next task, there is not one queued — ask the human.

## Small things queued, with the reason they were not done

Real findings, judged out of proportion to fix when they were found — three from the
2026-08-25 review, two measured on 2026-08-26 while closing the books and reviewing it.
None of them is
gated on the human (that is §10); each is a call for whoever next has a reason to be in
that code. Each says what it would take, so nobody has to re-measure.
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
- **A pinned tool and the same tool on `$PATH` are two different tools, and this
  repo runs both — measured on the linter AND on the formatter.** One decision
  covers them, which is why they are one entry.

  *The linter.* `make lint` runs the bingo-pinned `golangci-lint v2.6.0` and reports
  **0 issues**; the `golangci-lint-mod` pre-commit hook runs whatever
  `golangci-lint` is on `$PATH` — v2.11.1 on the machine this was measured on —
  and reports **11**, listed here so nobody runs it twice: `gosec` `G703`
  (path traversal via taint analysis) at `logs.go:73,74,76` — the sidecar log
  path, which is deliberate and already validated by `logTabRe`; `gosec` `G705`
  (XSS via taint analysis) at `server.go:1758`, a `w.Write(body)` on a body the
  server marshalled itself; `modernize/stringscut` at `caps.go:598` and
  `test/e2e/dag_test.go:336,341`; and `prealloc` at `bench_test.go:233`,
  `export.go:566,573` and `htmltab_palette_test.go:21`. Same `.golangci.yaml`; a
  newer analyzer set. It matters because `CLAUDE.md` says the pre-commit hooks
  must pass before a commit lands, so as written no commit can land here —
  measured 2026-08-26. Nine of the eleven are in code plan-2 never touched; the
  two in `dag_test.go` are item 4's own, and they are the cheap ones. Not fixed
  while closing the books, because the fix is a
  DECISION and not an edit: either move the bingo pin to the version the hook
  will pick up anyway (and then answer the four taint findings on their merits —
  three of them are the deliberate sidecar-log path and one is a body the server
  built itself), or make the hook use the pin so the two gates cannot drift
  again. The second is the smaller change and the first is the honest one. Worth
  doing before the first tag, since `make ci-local` says "safe to push + tag" on
  the strength of the pinned run alone.

  *The formatter, found while reviewing this item and the reason the entry is
  plural.* `make fmt` runs the bingo-pinned `gofumpt v0.10.0` and **rewrites
  `pkg/aboard/history_test.go`** (it splits `journalWith(t, root,` onto its own
  line), while the `gofumpt v0.9.2` on `$PATH` — which is what the ladder rung
  `gofumpt -l .` actually invokes — reports the whole tree clean. So the committed
  tree is clean under the ladder and dirty under the documented formatter, and a
  developer who runs `make fmt` gets a diff in a file they did not touch. This one
  WRITES rather than reports, which makes it the more likely of the two to end up
  in somebody's commit by accident. Nothing gates on it: CI and `make ci-local`
  run `make lint`, and `make check` is `gofmt`, not `gofumpt`. The reformat was
  reverted rather than committed — that is the conservative option, not the
  answer.
- **`make install INSTALL_DIR=<dir>` fails if the directory does not exist** —
  `install -m 755 aboard $(INSTALL_DIR)/aboard` (`Makefile:56`; not `cp`, which
  matters because the one-flag fix is `install -D`, not a separate `mkdir -p`).
  An override to a fresh path reports `install: cannot create regular file
  '<dir>/aboard': No such file or directory` and make exits 1 — measured
  2026-08-26 against a fresh path and against an existing one. Harmless for the
  documented default (`/usr/local/bin` exists), which is why it was left: making
  the target create its own destination would also silently create a MISTYPED
  one, and being told the path does not exist is the more useful answer more
  often. Worth knowing while you are in there: the target `rm -f`s `./aboard`
  when it finishes, so a `make install` in the middle of a ladder costs the next
  step a rebuild.
- **`BUILD_DATE` is the commit's date, so a dirty build is stamped with the time
  of the commit it was built from.** Deliberate and documented in the Makefile:
  `date -u` would change every second, defeating Go's build cache and paying for
  it twice in `make caps`, which builds on purpose. `+dirty` in the version
  string is what says the bytes are not the commit's. Only worth revisiting if
  the build date is ever used to decide something rather than to be read.

`git log` is a real source here too. Commit messages in this repo carry the reasoning
and the mistakes found on the way, so a decision you are about to re-derive is often
already argued out in the commit that made it.

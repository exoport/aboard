# Development

Working documents for the people — and the sessions — building `aboard`. Nothing here
is user-facing: the docs a user reads live in [`../docs/`](../docs/), and this directory
is the record of how the thing got made and what is still owed.

Two folders, split by **who the document is addressed to**:

- **[`planning/`](planning/)** — plans. A plan is the authoritative brief for a body of
  work: the decisions taken with the human, marked DECIDED where they are not to be
  relitigated, and the shape of the commits that carry them out. A plan is written
  before the work and edited during it; its Status line says how far it has got.
- **[`research/`](research/)** — measurements and reviews, each with a disposition beside
  every finding. Written once, not maintained.

There used to be a third folder, `handoffs/`, and its absence is deliberate. A handoff is
what one session leaves the next when the work is real but not next — and it is a
**transient implementation artifact**, so once the work lands the handoff is deleted rather
than left saying DONE at the top while everything still cites it. The six that lived here
were removed on **2026-08-27**, after what was load-bearing in each was promoted first: the
write-cost measurements into `docs/explanation/how-aboard-runs.md`, the rejected browser
drivers into `docs/how-to/run-the-browser-suite.md`, the embedding non-goals into
`docs/reference/http-api.md`, the two missing manifest arguments into
`docs/explanation/why-the-manifest-is-declared.md`, and the five porting judgement calls
into plan-2 §10 itself. `git log` holds the full text of all six. The `handoff` skill still
exists and still writes to gitignored `_output/handoffs/`; what was retired is keeping them
**in the repository**.

## What is open

**Nothing, except what is gated on the human.**
[`planning/plan-2_finish-line.md`](planning/plan-2_finish-line.md) — decided with the human on
2026-08-25 and executed over 2026-08-25/26 — is **complete**: the two races, the behaviour and
coverage review fixes, the end-to-end browser suite, the JSON hot paths, the eleven reviewed
features, the panel prerequisites, the VS Code extension (implemented, not installed), and this
bookkeeping. `plan-1_port-from-spike.md` is the record of how the repo came to be and its 16
decisions still bind.

**The one open list is [plan-2 §10](planning/plan-2_finish-line.md#10-gated-on-the-human--do-not-start-without-an-answer)** — five
entries, all of them questions for the human rather than work to pick up. Do not start any of
them without an answer: the remote and first tag, the `ape aboard` mount and the
`aboard <cmd>` strings measured for it, installing the VS Code extension (M6), and the five
porting judgement calls — `make dist` dropped, `restart.sh` kept, NOTICE in the release
archives, the `vuln` CI job, and the two hidden commands outside the declared table — which
are written out in §10 itself and stand until overruled.

Four entries left that list on 2026-08-26, when the human answered them; their dispositions are
[§10c](planning/plan-2_finish-line.md#10c-answered-on-2026-08-26--closed-not-deferred) and the
work landed with them. **`boards` (`bb372`) was DROPPED and then REVERSED, both on 2026-08-26,
and it is BUILT**: `aboard boards` is a `/proc` scan reading `cmdline` (never `comm`), behind
`//go:build linux`, exiting 2 with the reason and `aboard status` as the alternative everywhere
else. The registry file stays rejected — that half of the drop survived the reversal. The
platform objection did not: it is answered by the build tag and the refusal rather than by not
building the feature. The other three: the example board's
prose says "the agent", the notify button's acknowledgement is a toast the SSE repaint cannot
reach, and `JournalEntry.Before` carries the whole tab so the `apply` merge survives a foreign
rename.

A fifth left it on **2026-08-27**: **Go 1.27**, answered by the human once the toolchain was
installed on the machine. `go.mod` says `go 1.27.0`, the `github.com/go-json-experiment/json`
dependency is gone in favour of the stdlib `encoding/json/v2` and `encoding/json/jsontext`,
and the goreleaser pin that entry was blocking moved to **v2.18.0**. The `apex_process_ape`
condition attached to it — one toolchain across both — was overtaken rather than met: the
human moved the machine, and the `ape aboard` mount is still its own §10 entry.

Below that, one more list: real findings that are nobody's blocker and nobody's question —
decisions for whoever next touches that code. They are not §10, and they are not work queued
for a next session either.

If you are resuming and looking for the next task, there is not one queued — ask the human.

## Small things queued, with the reason they were not done

Real findings, judged out of proportion to fix when they were found — three from the
2026-08-25 review, two measured on 2026-08-26 while closing the books and reviewing it. One
of the five — the pinned-versus-`$PATH` divergence, third in the list below — is now
RESOLVED, and is kept for its measurement rather than as work. None of the rest is
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
- **RESOLVED 2026-08-26 (plan-2 item 10) — a pinned tool and the same tool on
  `$PATH` are two different tools, and this repo ran both.** Kept because the
  measurement is the argument, and because the shape recurs.

  *What it was.* `make lint` ran the bingo-pinned `golangci-lint v2.6.0` and
  reported **0 issues**; the `golangci-lint-mod` pre-commit hook ran whatever
  `golangci-lint` was on `$PATH` — v2.11.1 on the machine this was measured on —
  and reported **11**. Same `.golangci.yaml`; a newer analyzer set. The formatter
  had the mirror-image version of it: `make fmt` (pinned `gofumpt v0.10.0`)
  rewrote `pkg/aboard/history_test.go` while the `v0.9.2` on `$PATH` — which is
  what the ladder rung `gofumpt -l .` actually invoked — called the whole tree
  clean. So the committed tree was clean under the ladder and dirty under the
  documented formatter, and `CLAUDE.md` said the pre-commit hooks must pass before
  a commit lands, which as written meant no commit could land here.

  *The decision, and it was the human's.* "We must use the make so we use the
  bingo go tooling, we must update the tools" — both halves, not either. The pins
  moved (golangci-lint **v2.13.1**, gofumpt **v0.11.0**, govulncheck **v1.7.0**,
  goreleaser **v2.17.1**), and the bingo pin reached through `make` became
  authoritative everywhere: `.pre-commit-config.yaml` is two `local` hooks running
  `make lint` and `make fmt-check`, `ci.yml` runs the same targets, and the ladder
  rung is `make fmt-check` rather than a bare `gofumpt -l .`. `make check` — `go
  vet` plus the stdlib `gofmt` — stays as the gate a bare checkout can run with
  nothing fetched, which is a different job and the only place a tool outside
  `.bingo` is still used on purpose.

  *What the newer linter then found, and what happened to it.* ~110 findings, all
  fixed in code except one config line. The one config change: `exhaustruct_v5`
  added to the disable list beside `exhaustruct`, which was already disabled — a
  linter renaming itself must not be a way to re-enable it, the same shape as the
  `wsl`/`wsl_v5` pair. In code: the two `gosec` taint findings, four `modernize`,
  four `prealloc`, and every `goconst` — the repeated wire keys are now named
  constants in `pkg/aboard/wire.go`, one block per vocabulary, with `key*` (the
  board document) and `wire*` (the HTTP API) deliberately kept apart even where
  they spell a word the same way.

  *And the one thing the upgrade left behind, closed 2026-08-26.* v2.13.1 printed a
  deprecation warning on every `make lint`: `default: all` enables every linter
  golangci knows, which since v2.12.0 is both `gomodguard` and its successor
  `gomodguard_v2`. `.golangci.yaml` now disables the old NAME — the opposite of the
  `exhaustruct`/`exhaustruct_v5` and `wsl`/`wsl_v5` pairs beside it, which disable
  both because this repo decided against those linters, where this one is wanted.
  Neither has any settings here, so nothing about what is allowed changed; `make
  lint` is 0 issues and silent.

  *The one thing that did NOT get fixed on its merits.* `gosec` G703 on the three
  sidecar-log file operations. The validation moved into `layout.go` — `Root.LogFile`
  now refuses any id that is not `^[A-Za-z0-9_-]{1,64}$` instead of taking it on
  trust from a comment, with a test — and gosec still reports it, because its taint
  analysis only consults sanitizers inside the function holding the sink
  (`taint.valueReachableFromParams` never looks at them across a call). The note
  above `logPath` in `logs.go` records that, names the call-site "fix" that silences
  it and is a bug, and the three `//nolint:gosec` lines point at the note.

  *And one the new gate nearly repeated.* `make fmt-check` first shipped as
  `out=$(gofumpt -l .); [ -n "$out" ] && fail` — which is green whenever gofumpt
  itself fails, because `-l` lists no files when it cannot parse one. Measured: a
  file with a syntax error in it passed the gate, printed "fmt-check ok" and exited
  0. The recipe now checks the exit status as well as the output, and
  `TestFmtCheckFailsWhenGofumptItselfFails` lifts the recipe out of the Makefile and
  runs it against a stub for each of the three answers the tool can give.

  *The shape worth remembering.* Two copies of a tool is not a tidiness problem: it
  is two gates that can each be green while the other is red, over a tree that never
  changed. The fix is not "pick the newer one", it is "have one" — and then check
  that the one you have can actually go red.

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

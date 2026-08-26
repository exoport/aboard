# Brief 12 — `aboard boards` over /proc (Linux only, honest elsewhere), and gomodguard_v2

Read `COMMON.md` first. The human REVERSED the 2026-08-26 "drop `bb372`" decision the same day,
with a design: **implement `aboard boards` as a `/proc` scan, run only where `/proc` exists
(Linux), give a proper message elsewhere, and print a summary of every running board including
the FULL project path.** No registry file — that half of the old proposal stays rejected.

## 1. `aboard boards`

Discovery, on Linux:
- Walk `/proc/[0-9]*/`. A candidate is a process whose `cmdline` (NUL-separated argv) is an
  `aboard serve` or an `ape aboard serve` — match on `filepath.Base(argv[0])` being `aboard`,
  or `ape` with `argv[1] == "aboard"`, and `serve` present as the subcommand. `comm` is NOT
  enough (15 chars, the host's name under ape) — say so in a comment.
- The project root: honour `--cwd`/`-C <dir>` (and `--cwd=<dir>`) in argv if present, else
  `/proc/<pid>/cwd`; then `FindRoot` from there. A process whose `cwd` cannot be read (another
  user's) is counted and reported as "N processes could not be inspected (permission)", never
  silently skipped.
- Then do exactly what `status` does for one project: read the root's `instance*.json` records,
  keep the one whose `pid` matches, and VERIFY it with `ProbeBoard` (`/health` `project` equals
  the root, `app` is `aboard` or `ape-aboard`). A record that fails is listed as "recorded but not
  answering" rather than dropped — a stale record is information.
- One row per board (root + name): **root (full absolute path)**, name (`default` when empty),
  app, url, port, pid, started, version, number of tabs (`GET /aboard.json`, cheap) and the
  `lastEditedBy`/`updatedAt` pair. Human table sorted by root then name; `--output-format json|yaml`
  with paired tags (item 2's rule). Zero boards: "no running board found (N processes inspected)".
- Not Linux, or `/proc/self` absent: exit 2 (declared) with one line on stderr naming the
  reason and the alternative — `aboard status` inside each project. Windows cross-compile
  (`make xcompile-windows`) must stay green: put the scanner behind `//go:build linux` with a
  stub for other OSes, and keep the message path testable everywhere.
- Declare the command in `commands.go` (flags, exits); `make caps` (capsHash moves — say so);
  `make docs-cli`; the skill's SKILL.md/capabilities reference; `docs/reference/cli.md` regenerated;
  a how-to paragraph (find the right page: the second-board how-to or the multi-session
  reference); `CLAUDE.md`: REPLACE the "No `boards` command — closed, not deferred" bullet with
  the decision as it now stands (a `/proc` scan, Linux only, honest message elsewhere, no
  registry); plan-2 §10c and `handoff-13-features.md` item 11: DROPPED → DONE, this way, with the
  reversal recorded and the date; `CHANGELOG.md`.
- Tests: the scanner takes its proc root as a parameter (a `fakeProc` tree under `t.TempDir()`
  with `cmdline`/`cwd` symlinks) covering: aboard serve, ape aboard serve, `--cwd`, a non-aboard
  process named `aboard`, an unreadable cwd; a live test that starts the engine in-process and
  finds ITS OWN pid through the real `/proc` (skipped with a reason off Linux); the non-Linux
  message path; the output-format parity test picks up the new struct automatically — check.

## 2. `gomodguard` → `gomodguard_v2`

`.golangci.yaml` still names `gomodguard`, which golangci-lint v2.13 deprecates in favour of
`gomodguard_v2` with a warning on every run. Migrate (settings too, if any); `make lint` must
print 0 issues AND no deprecation warning; `make pre-commit` exit 0. `development/README.md`'s
note about the warning is closed.

## Also answer, in the how-to, what a named board shares

`Root` in `layout.go` is name-aware ONLY for `StateFile` and `InstanceFile`. `JournalFile`,
`LogsDir`, `RenderedFile`, `UploadsDir`, `RecipesDir` are per PROJECT, so two boards in one
project share one journal, one logs dir, one receipts file and one uploads dir. Do NOT change
that here (the human is deciding); write it down where `--name` is documented, plainly, including
the consequence that `aboard journal`/`history` in a named board show the other board's entries
and that tab ids are per board, so `bb12` in the journal may belong to either. `boards` output
must therefore show one row per (root, name).

## Done when

`aboard boards` works on this machine against two scratch boards (a default and a `--name`)
plus one `ape`-shaped fake in the fake-proc test; the non-Linux path is tested; ladder green
(`make lint` warning-free, `make fmt-check`, `make pre-commit`, `make e2e` once, `make ci-local`
once incl. `xcompile-windows`); every doc listed above says what is true.

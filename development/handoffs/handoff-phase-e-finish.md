# Handoff — resume the aboard port at Phase E (the finishing pass)

**Status: DONE — Phase E landed as `c331dfc` on 2026-08-25** (lint 0, smoke 101/101, ci-local
green, snapshot verified). Kept for the record of the port and for the "still unproven" and
"open judgement calls" lists below, which remain true. The live queue starts at
`handoff-json-hot-paths.md`.

Written 2026-08-25 when the human asked to pause cleanly after Phase D. Everything below is
what a session needs to pick the work up without re-deriving it.

## Where it stands

Six commits on `main` in `/home/diegos/_dev/exoport/aboard`, tree clean, no remote:

| commit | what |
|---|---|
| `9f0c7af` | verbatim port of the spike (`/home/diegos/_dev/ai/board` @ `1089f4f`) |
| `c192fb8` | split: `pkg/aboard` engine, `pkg/aboard/cli` cobra tree, `pkg/aboard/web` embed, `cmd/`; one resolved root; declared command table; `--base-path` |
| `a83c107` | rename `board` → `aboard` (binary, `.aboard/aboard.json`, `/aboard.json`, `ABOARD_NAME`, bridge `window.aboard`/`__ABOARD_DATA__`/`__aboard`, skill dir) |
| `a350127` | `init`, `recipes`, root `--name`, version symbols, hidden `recipes index`/`gen-docs`, bb359 + bb360 fixes, five executed-command defects fixed; 90 Go tests |
| `e5c698b` | ape-style tooling: `.bingo`, Makefile, golangci, goreleaser + keyless cosign, CI/release workflows, dotfiles, LICENSE/NOTICE/CHANGELOG |
| `0b508d8` | skill `.claude/skills/aboard`, lean `CLAUDE.md`, `README.md`, Diátaxis `docs/`, `development/handoffs/*` |

Also: `/home/diegos/_dev/exoport/aboard_vscode` has one commit (`70dce85`) holding the rewritten
extension handoff (`docs/handoff.md`) and a README; the extension itself is not started.
`apex_process_ape` was not modified (the `ape aboard` mount is future work, as briefed).

Verified at HEAD: `go build/vet ./...`, `gofumpt -l .` clean, `go test -race ./...` green (90
tests), `make caps` current at `capsHash 58b40b03`, `aboard capabilities --check` = 0,
`docs/reference/cli.md` regenerated. The browser suite last ran green (103/103) at the rename
commit against a converted seed; it has NOT run on the merged tree.

The governing plan with all 16 human decisions is `development/planning/plan-1_port-from-spike.md`;
the per-phase briefs are in `development/planning/plan-1-briefs/`. The raw agent reports from Phase
D and every staged artefact are in `_output/plan-1/` (gitignored — local to this machine only).

## What Phase E must do

Run `development/planning/plan-1-briefs/brief-E-finish.md` as written, with these path
substitutions (the brief was written against a session scratchpad that no longer exists):

- `$S/phase-d-reports.json` → `_output/plan-1/phase-d-reports.json`
- `$S/doc-commands.txt` → `development/planning/plan-1-briefs/doc-commands.txt`
- `$S/staging/…` → `_output/plan-1/staging/…`

In short: fix the golangci backlog under the pinned config (82 issues at last count — per-linter
numbers are in the D2a report), run `make lint test docs-check xcompile-windows snapshot` and
`make ci-local`, execute every command in `doc-commands.txt` against a scratch project seeded with
`aboard init --example`, run `make smoke` ONCE per tool call and LOOK at the screenshots, then have
an independent reviewer re-run the ladder. Commit the result.

**Historical: `make smoke` no longer exists.** `test/smoke.sh` was retired by plan-2 item 4 and
the browser suite is `make e2e`, which seeds its own temporary board. The paragraph above is
what Phase E was actually verified with, kept as the record; it is not a ladder to run today.

## After Phase E

**All of it has since landed.** The queue that started here was `handoff-json-hot-paths.md`
(decided 2026-08-25: structural JSON fixes first, then json/v2, no per-tab resources) and then
`handoff-13-features.md`; both are DONE, and everything owed after them was ordered and
executed as `development/planning/plan-2_finish-line.md`, which is complete. The only open
list left is that plan's §10 — the things gated on the human.

## Open judgement calls the agents made — the human may want to overrule

- ~~`boards` (bb372) was re-specified around an invented `~/.aboard/known-roots.json` registry written by
  `aboard serve` and verified against `/health` — `development/handoffs/handoff-13-features.md`. Not settled.~~
  **Settled 2026-08-26, twice: the human DROPPED the feature and REVERSED that the same day with a
  design.** The registry is dead either way — that is the half of the drop that survived. What shipped
  is the `/proc` scan, Linux-only behind a build tag and exiting 2 with `aboard status` as the
  alternative elsewhere — plan-2 §10c.
- `make dist` from the spike was dropped (goreleaser `snapshot` + `xcompile-windows` replace it).
- `restart.sh` was kept as a dev convenience and documented in CLAUDE.md's directory map.
- `recipes index` and `gen-docs` are hidden and excluded from the declared command table (parity test
  says so); recipe scope names are `apex` / `aboard` / `dot-aboard` / `builtin`.
- `init` in a directory that already IS a root but has no document completes instead of refusing.
- NOTICE is added to the release archives (ape ships README + LICENSE only); a `vuln` CI job was kept.
- The nine built-in recipes carry no `aboard-template` block (the spike wrote them as JS, not JSON);
  only the user example (`testdata/recipes/.aboard/recipes/decision-wizard-with-live-summary.md`) has one.

## Still unproven, on purpose

- ~~The html bridge's WRITE half after the rename (a widget's `aboard.set()` reaching `state.data`).~~
  **Closed 2026-08-26**: `test/e2e` drives the sketch pad's Undo inside the sandboxed frame and reads
  `state.data` back off the server, and does the same for an html block inside a stack
  (`TestAWidgetWritesThroughTheBridge`, `TestAWidgetInsideAStackBlockWritesThroughTheBridge`).
- ~~The 11 open features of the 13-feature review are queued, not built.~~ **Closed 2026-08-26**:
  ten of the eleven shipped as plan-2 item 6 (`d69197a`); the eleventh, `boards` (`bb372`), was put
  to the human, DROPPED, then REVERSED the same day and built as a `/proc` scan — plan-2 §10c.
  Nothing of the thirteen is open.
- The spike's own board was found DOWN mid-session and restarted with its own `restart.sh` on
  port 46624; nothing in its tree was written.

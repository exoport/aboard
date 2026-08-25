# Handoff — resume the aboard port at Phase E (the finishing pass)

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

## Open judgement calls the agents made — the human may want to overrule

- `boards` (bb372) was re-specified around an invented `~/.aboard/known-roots.json` registry written by
  `aboard serve` and verified against `/health` — `development/handoffs/handoff-13-features.md`. Not settled.
- `make dist` from the spike was dropped (goreleaser `snapshot` + `xcompile-windows` replace it).
- `restart.sh` was kept as a dev convenience and documented in CLAUDE.md's directory map.
- `recipes index` and `gen-docs` are hidden and excluded from the declared command table (parity test
  says so); recipe scope names are `apex` / `aboard` / `dot-aboard` / `builtin`.
- `init` in a directory that already IS a root but has no document completes instead of refusing.
- NOTICE is added to the release archives (ape ships README + LICENSE only); a `vuln` CI job was kept.
- The nine built-in recipes carry no `aboard-template` block (the spike wrote them as JS, not JSON);
  only the user example (`testdata/recipes/.aboard/recipes/decision-wizard-with-live-summary.md`) has one.

## Still unproven, on purpose

- The html bridge's WRITE half after the rename (a widget's `aboard.set()` reaching `state.data`)
  needs a human click in a real browser; wire-level and read-half are verified.
- The 11 open features of the 13-feature review are queued, not built.
- The spike's own board was found DOWN mid-session and restarted with its own `restart.sh` on
  port 46624; nothing in its tree was written.

# Brief 6a — features, the write-path cluster (plan-2 item 6, part a)

Read `COMMON.md` first. Source: `development/handoffs/handoff-13-features.md`, items
**1 (`bb361` warnings travel with the write), 2 (`bb362` `apply --check` / `--strict`),
10 (`bb371` write labels in the journal)**. Items 1–5 of plan-2 have landed: writes are
serialised, the CAS token is a revision, the document is cached in memory (item 5) — read
`server.go`'s `postState` as it is NOW before designing.

**You are working in a git worktree** whose path the orchestrator gave you in the prompt. Work
ONLY there. Two sibling worktrees implement the other feature clusters in parallel; a merge
agent will squash-merge all three into `main`. So: keep your changes to the files your features
need, do not reformat unrelated code, run `make caps` (it will move `capsHash` — expected), and
list every file you touched in the report. Scratch projects go under the scratchpad with your
cluster's name.

## Scope

- `bb361`: `postState` runs `writeWarnings` over the tabs the write touched (scoped — never the
  whole document); the strings land on the journal entry (`warnings` field), never on a tab; the
  browser's notice banner and `views/trace.js` show them; `aboard journal`/`watch` print them.
  The gallery's deliberate `sparkline` keeps warning — no suppression mechanism.
- `bb362`: `apply --check` runs the warnings and exits without posting (exit 0 with warnings
  printed, or exit 1 if `--strict` too); `apply --strict` refuses on any warning (non-zero,
  nothing written). Declared in `commands.go`; `make caps`; the skill's apply section.
- `bb371`: `apply --label "…"` → `__label` stripped in `postState` beside `__by`/`__base`, stored
  on the journal entry; `journal`, `watch` and `trace.js` print it. Declared; documented.
- Go tests for each; an e2e test for the banner and the trace tab showing a warning/label (the
  suite is `make e2e`, `test/e2e/`; one run per tool call, timeout 600000).
- `docs/reference/http-api.md` (journal entry shape), `docs/reference/cli.md` regenerated,
  the skill, the handoff's sections marked done with what shipped.

## Done when

Three features shipped with tests and docs; ladder green in your worktree (`make e2e`
included); report lists files touched and the `capsHash` before/after.

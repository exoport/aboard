# Brief 9 — close the books (plan-2 item 9)

Read `COMMON.md` first. Items 1–8 have landed on HEAD (check `git log`); the orchestrator has
ticked the plan as each landed. Your job is the bookkeeping that makes a fresh session find
nothing open except plan-2 §10.

## Scope

1. `CLAUDE.md`: the status/gotchas/verified sections reflect everything above (the browser
   suite is `make e2e`; `smoke` is gone; revision token; origin/host refusals; the cached
   document; the new commands `history`, `rendered`, `uploads`, `apply --check/--strict/--label/--force`;
   the panel prerequisites). Every command in it runs (execute each once).
2. The skill (`.claude/skills/aboard/`): read SKILL.md and every reference as the agent that
   will use them; every command exists; `make caps` current; `aboard capabilities --check` 0.
3. `development/README.md`: the order list replaced by "see plan-2 — complete" plus the open
   list (only §10 items and anything item 3 queued).
4. Each handoff's Status line says done (or, for `bb372` and M6, gated with the pointer);
   `handoff-capability-manifest.md` and `handoff-phase-e-finish.md` checked for stale claims.
5. The review file fully dispositioned (verify: no High/Medium/Low line without one).
6. `CHANGELOG.md` Unreleased: one line per plan-2 item.
7. The plan file's Status line says complete, with the commit hashes beside each item as the
   orchestrator recorded them (verify each hash exists).
8. `make ci-local` green (once, detached if needed, log to a file, read it) and `make e2e`
   green (once).
9. `docs/` links: `make docs-check` 0; the `docs/README.md` index lists every new page.

## Done when

`grep -rn 'TODO\|FIXME\|XXX\|not yet\|has not landed\|if it has not' --include=*.md . | grep -v _output |
grep -v lib/mermaid | grep -v node_modules` returns only lines you can justify in the report,
one by one; the ladder is green.

## Leftovers surfaced by the item reviewers — dispose of each (fix, or record for the human)

- `pkg/aboard/example/aboard.json` says "Claude" seven times in card titles/prose (e.g. "Claude
  reads and reacts"), visible in every `aboard init --example` board. The clean-break rename
  applied to the TOKEN and colour names, not to prose; whether the example's prose should name
  "the agent" instead is one decision — record it under §10 of the plan as a question for the
  human, do not edit the seven strings.
- The notify button's "notified N sessions" confirmation is repainted away by the SSE `waiters`
  frame the poke itself causes (recorded in `test/e2e/notify_test.go`). A ~1.5 s suppression of
  `refreshWaiters` after a poke would fix it; it is an interface taste call — record under §10.
- `test/shot.sh` exits 0 when every shot FAILS (a confined chromium outside `$HOME`). Fix it:
  exit non-zero when no PNG was written, keeping the existing warning. One line; run it once.
- `development/README.md` carries three findings item 3 queued (`--dev` symlink escape, sidecar
  log file count, `BUILD_DATE`); keep them listed as open, with their reasons, under the §10
  pointer.
- Comments in `views/form.js`, `views/markup.js`, `pkg/aboard/web/test/mermaid-probe.html` still
  say "Claude" — not user-facing; leave them, but say so in the report.
- The `apply`/`--check` split and the receipts endpoint from item 6 are new CLI grammar: verify
  the plan-1 CLI grammar table (`plan-1_port-from-spike.md`) lists `history`, `rendered`,
  `uploads`, `apply --check/--strict/--label/--force` and NOT `boards`.

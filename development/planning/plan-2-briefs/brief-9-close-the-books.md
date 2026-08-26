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

# Brief 3 — coverage, docs, lows (plan-2 item 3)

Read `COMMON.md` first. Source: `development/research/review-d6c2f84-20260825.md` — the
coverage/docs half of **Medium** and the whole **Low** section (15 confirmed, 18 unverified).
Items 1 and 2 have landed on HEAD.

## Scope

Coverage and tooling (Medium):
1. The CLI parity test walks subcommands: a `Subcommands` field in the declared command table
   (`pkg/aboard/commands.go`) and a recursive walk in `cli/parity_test.go`, so `recipes list`'s
   and `recipes show`'s flags are declared and asserted. `capsHash` will move — say so.
2. `reconcileNextID` table tests (`pkg/aboard/ids_test.go`, new if absent): the floor, never
   goes backwards, ids present only in the current document, an agent that reused a lower
   `nextId`. Each row must fail if the function is gutted to "return incoming nextId" — verify
   that once by gutting it and restoring.
3. `writeWarnings`: one minimal document per detector class (unknown component, unknown prop,
   undeclared state key, bad block field, dead `{bind}`, unknown tone/colour) plus the two
   negatives (a valid `field` bind path must not warn; a JSON `null` value at a found key must
   not warn). Each must fail with its detector disabled — verify one.
4. `make smoke` depends on `build`; a skipped section exits non-zero (the section names what it
   needed).
5. `smoke.html`'s render-count probes are COMPARED against `got/want` (a threshold table), not
   logged.
6. `bb71` in the example fixture (`pkg/aboard/example/aboard.json`) gets three cards across its
   columns so the read-only-kanban assertion requires `cards > 0` and the no-drag/no-edit
   negatives are real. Keep `nextId` consistent (`init --example` recomputes it, but the
   fixture should still be self-consistent). Item 4 will build its interaction fixture ON TOP
   of the example, so make this the example's own improvement, not a test-only hack.
7. The three docs claiming a `go install` build reports `dev` corrected (`README.md`,
   `docs/how-to/install.md`, `docs/how-to/verify.md`) — it reports the module version or a
   pseudo-version; `verify.md`'s argument for the signed archive must be rewritten to the true
   reason (only the signature distinguishes).

Confirmed lows — fix each, with a test where one is possible:
8. Journal rotation: `tail`/`journal` read `journal.jsonl.1` too, oldest first, so history does
   not go invisible after rotation.
9. `writeAtomic` keeps 0644 (respecting umask) — the documented policy.
10. Recipe discovery survives one unreadable or dangling `.md` (reported as an invalid entry
    with the reason, the rest still listed).
11. The two `log.Printf` calls go through `Options.Logger` (`tabs.go`, `server.go`).
12. Go tests for base-path injection (`server.go`) and the wait-predicate vocabulary (`wait.go`).
13. A drift gate for the recipe index in `make caps` / the suite (the one generated artifact
    without one).
14. The four false doc claims: `.gitattributes`' `-text` rationale; "no `filepath.Join`
    outside `layout.go`" (fix the four violations OR add a test that greps for it and fails —
    do both if the violations are cheap to move); http-api.md's "anything unmatched is 404"
    (it is 403, and `POST /` returns the shell — fix the doc or the code, say which);
    SKILL.md's `make caps` remedy in a project that only copied the skill.
15. `mergeSeen` — tab creation and a tab with no `seen` map go through the writer filter.
16. `/tab/<id>/html` standalone fetch carries a CSP `sandbox` directive (hardening, say so).
17. `init --gitignore` failing at the last step reports what WAS created.
18. The two latent hosted-mode findings (messages hardcoding `aboard`, `version`/`/health`
    reporting the host's commit) — record in the review file as "latent until the ape mount;
    tracked in plan-2 §10", do not build.

The 18 unverified lows: triage each — reproduce it; if real, fix it with a test; if not, write
`— refuted: <why>` beside it in the review file; if real but out of proportion, `— queued: <where>`
(a line in `development/README.md`'s open list). No entry may be left without a disposition.

## Done when

The review file has a disposition beside EVERY finding (High, Medium, Low, unverified);
`make ci-local` is green (it runs test, lint, govulncheck, docs-check, xcompile-windows,
snapshot — run it once, detached if it risks the tool timeout, log to a file); `make smoke`
green on a scratch project with the new non-vacuous assertions.

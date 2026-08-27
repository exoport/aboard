# Brief 24 — retire the handoffs (both repos), promote the spike's comparison matrix, carry one gotcha

Read `COMMON.md` first. Items 1–23 are landed. Three things the human decided on 2026-08-27.

## A. The handoffs were transient implementation artifacts — delete them

All six files in `development/handoffs/` say DONE at the top, and `aboard_vscode/docs/handoff.md`
is implemented through M6 step 1. The human: "the handoffs were transient implementation
artifacts; if already implemented, delete them." Do it **without losing anything still
load-bearing** — each file is read once, and what a future reader would be WRONG without moves to
a permanent home first. Known load-bearing content (verify, do not assume):
- `handoff-json-hot-paths.md`: the **Measured** table (before / after-structure / after-codec, the
  three sizes, the marginal-tab cost) and the "(3) per-tab resources — do not build; the trigger"
  rule. Move both into `docs/explanation/how-aboard-runs.md` (a "What a write costs" section, with
  how to rerun `go test -bench` in `pkg/aboard/bench_test.go`) — or say where else and why.
- `handoff-e2e-browser-suite.md`: the "rejected drivers, so nobody re-researches" list (go-rod,
  chromedp, @playwright/test, Puppeteer, Cypress, WebdriverIO) → `docs/how-to/run-the-browser-suite.md`
  or the explanation page that owns the decision. Also its "explore once, codify forever" MCP note
  if it is not already in the skill.
- `handoff-phase-e-finish.md`: the judgement calls plan-2 §10 points at (`make dist` dropped,
  `restart.sh` kept, NOTICE in archives, the `vuln` job, hidden commands outside the declared
  table) → write them INTO plan-2 §10 (or `CLAUDE.md`'s decisions) so §10 stops pointing at a file.
- `handoff-13-features.md`: the `bb372` history is already in `CLAUDE.md`; check nothing else.
- `handoff-capability-manifest.md`: check `docs/explanation/why-the-manifest-is-declared.md`
  already carries its reasoning (it was written from it); anything missing moves.
- `handoff-board-for-vscode-panel.md`: §7 "deliberately not doing" (no new endpoint, no auth
  relaxing, no vscode mode on the server, active tab never in the state file) → the embedding
  section of `docs/reference/http-api.md` or `docs/how-to/use-the-vscode-extension.md`.
Then `git rm -r development/handoffs`, and fix EVERY reference (grep the whole tree for
`handoffs/` and `handoff-`, not just `docs/` — `make docs-check` only walks `docs/`): `CLAUDE.md`
(the "every handoff says DONE" sentence and the §10 pointer), `development/README.md`,
`CHANGELOG.md` (a link to a deleted file → the new home), the plan files (historical mentions may
stay as history but must say the folder was deleted on 2026-08-27 after implementation and that
git history holds it — do not rewrite the plans' past tense), the briefs (historical, leave), and
`.claude/skills/handoff/SKILL.md` (a working skill that writes handoffs — decide: retarget it to a
scratch location / `development/planning/`, or delete it too since handoffs are no longer kept;
say which and why). Record the decision in `CLAUDE.md`'s decisions: handoffs are transient; a
finished one is deleted, its load-bearing content promoted first.

**Extension repo** (`/home/diegos/_dev/exoport/aboard_vscode`): move what `docs/handoff.md` still
carries that the README does not — the §11 verification checklist with its observed/unobserved
state (README gains a "What has been observed in a real VS Code" section, rows and dates), the §9
packaging ladder (.vsix / Open VSX steps, "never the Marketplace" reasoning), the §10 hardening
cases and their status, the §6 start-fallback preference (both binaries present → `aboard`) —
then `git rm docs/handoff.md`; fix the comments in `src/launch.ts`, `src/messages.ts`,
`src/model.ts`, `test/launch.test.ts` and every README mention to point at README sections (and at
aboard's `docs/` pages instead of aboard handoff file names). `npm ci && npm run build && npm test
&& npx tsc --noEmit` from a fresh copy (`git ls-files -z --others --exclude-standard --cached |
xargs -0 cp --parents -t <dir>`); the integration test needs `/home/diegos/_dev/exoport/aboard/aboard`
(`make build` there if absent).

## B. Promote the spike's comparison matrix

Source: the export at `/tmp/claude-1000/-home-diegos--dev-ai-board/7009f57e-89c0-4a45-b80b-5b15a6656847/scratchpad/bb244-export.md`
(the spike's *Board vs artifacts* tab, exported; the spike at `/home/diegos/_dev/ai/board` is
READ-ONLY — never write there, never touch its board on 46624). Write
`docs/explanation/why-a-board-and-not-an-artifact.md`: a lead paragraph (what an artifact is —
a hosted, versioned, shareable HTML page — and what the board is), then the **20-dimension
matrix** with the same columns (dimension · this board · HTML artifacts · edge · because). The
"this board" column was written against the spike in August and MANY rows describe gaps that are
now closed — re-verify every row against the CURRENT code and docs and state today's truth: the
`rev` token, five server-enforced guarantees, `apply`'s merge on 409, `history`/restore and
journal schema 2, warnings travelling with the write and `apply --check/--strict`, `export`
rendering `ui`, two themes and `theme.json`, `requests`, `boards`, the origin/host guards,
`uploads` accounting, the html frame reading the real palette. Keep the "HTML artifacts" column
as the human wrote it (that is their record of the other medium), keep names in backticks and
never link into source. Then the closing section **Rejected in review — kept so nobody
re-proposes them**, from the export's last table, in the voice of `CLAUDE.md`'s "closed, not
deferred" entries. Link the page from `docs/README.md` and `docs/explanation/README.md`; one line
in `CLAUDE.md`'s decisions pointing at it. Run every CLI command you write into it once.

## C. One gotcha that never travelled

The spike's `CLAUDE.md` recorded: **a CLI warning can only reach the actor who runs the CLI** —
the reason warnings now travel with the write (plan-2 item 6, `bb361`) and why a gate verdict's
"add why" is a UI affordance rather than an `apply` warning. `grep` finds no trace of it in
aboard. Add it as one gotcha bullet in `CLAUDE.md` pointing at the write-warnings decision.

## Done when

`development/handoffs/` and `aboard_vscode/docs/handoff.md` are gone with nothing load-bearing
lost (your report lists, per file, what moved where and what was judged not worth keeping);
`grep -rn "handoffs/\|handoff-" --include='*.md' .` outside the briefs and plan history returns
only deliberate historical mentions; the explanation page exists and is linked; the gotcha is in;
ladder green (`make docs-check`, `make lint`, `make fmt-check`, `make pre-commit`, `go test -race
./...`, `make e2e` once — untouched, say so); the extension green from a clean copy. The human's
boards on 47781 and 44917 must not be touched; do not push, do not commit.

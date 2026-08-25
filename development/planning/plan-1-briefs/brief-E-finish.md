# Phase E brief — make it all true at once

Repo `/home/diegos/_dev/exoport/aboard`. The tree holds UNCOMMITTED work from two parallel agents
(Go features in `pkg/ cmd/ go.mod go.sum testdata/recipes/`; tooling, skill, docs, handoffs
everywhere else). Their reports are in `S=/tmp/claude-1000/-home-diegos--dev-ai-board/0e329b78-9ff1-4b58-bd23-4efd89232c39/scratchpad/phase-d-reports.json`.
Governing plan: `development/planning/plan-1_port-from-spike.md`. Never touch the spike
(`/home/diegos/_dev/ai/board`, a board runs there on 46624). Do not commit; the orchestrator does.
You may touch ANY file now — you are the only writer.

Goal: every gate the repo declares passes, every command any document contains runs, every
generated file is current, and a release snapshot builds. In this order:

1. `go build ./... && go vet ./... && gofumpt -l . && go test -race ./...` — green first.
2. `make tools` (should be a no-op if D2b ran it) then `make lint`. Fix the backlog under the
   pinned `.golangci.yaml` (the same linter set as ape). Rules: prefer fixing code over excluding;
   a per-file `//nolint:<linter> // reason` is acceptable for a genuine false positive; a config
   exclusion is acceptable only for a class (e.g. `mnd` in `*_test.go`, `tagliatelle` for the
   state document's JSON keys which are a wire contract — say so in the config comment). Do not
   weaken the linter set. Report counts before/after by linter.
3. `make caps` (builds twice; writes `controls.generated.js`, `reference.generated.md`,
   `recipes.md`) then `./aboard capabilities --check` → 0. `make docs-cli` → `docs/reference/cli.md`.
   `make docs-check` → 0. `git diff --stat` must show only generated files changing from those.
4. Execute every command in `$S/doc-commands.txt` (from the docs/skill NOTES) against a scratch
   project under the scratchpad seeded with `aboard init --example --gitignore` and a server
   started DETACHED (never in the foreground of a call that can time out; `watch` and `wait` with
   `timeout -s INT 3` / `--timeout 2s`). Each command must exit as its doc says. Fix the code OR the
   doc — whichever is wrong — and list every mismatch. Include: `aboard recipes list` with a user
   recipe dropped into `.aboard/recipes/` AND a same-name shadow in `_aboard/recipes/`,
   `aboard recipes show <name> --template | python3 -m json.tool`, `aboard --cwd <subdir> status`
   (upward walk), `aboard --name x init` then `aboard --name x status`, `aboard serve --base-path /x`.
5. Browser ladder on that scratch project: `make smoke` ONCE per tool call (timeout 180000, log to a
   file, read the whole log — including the new bridge-name assertion and "a gate export carries the
   reasons" against the example fixture); `make shot` / `test/shot.sh` for bb133, bb1, bb128, bb32,
   `/tab/bb72/html`, `bb22#help`, and LOOK at each PNG with the Read tool; report what you see.
6. `make snapshot` (goreleaser, no publish, no sign) — must succeed: archives in `dist/` for every
   platform, `README.md`/`LICENSE`/`NOTICE` inside, `aboard version` from an extracted linux
   binary prints the snapshot version. `make xcompile-windows` must pass (the engine uses `setsid`?
   no — but check for `syscall` or unix-only calls; fix with build tags if needed).
7. `make ci-local` end to end if it exists; otherwise the union of test, lint, govulncheck,
   docs-check, xcompile-windows, snapshot.
8. `.claude/skills/aboard/`: after `make caps`, read SKILL.md and every reference once as the agent
   that will use them — every command exists, every path is `.aboard/…`, no placeholder text
   remains, `references/recipes.md` lists the built-ins and the discovery paragraph.
9. `CLAUDE.md` and `README.md`: every command runs (you ran them in step 4); the make-target table
   matches `make help`; the layout table matches `layout.go`.
10. Final sweep: `git status` (nothing unexpected: no binary, no `.aboard/`, no `dist/`); `grep -rn
    'TODO\|FIXME\|XXX\|placeholder' --include=*.md --include=*.go . | grep -v _output | grep -v
    lib/mermaid` and resolve or justify each.

Report: each step's numbers (lint before/after per linter, test count, smoke ok/fail, PNGs seen,
snapshot artefacts), every fix you made (file, what, why), every doc↔code mismatch and which side
you changed, anything left undone with the reason, and a list of files changed grouped by area
(Go / web / tooling / skill / docs / handoffs) so the orchestrator can commit in groups.

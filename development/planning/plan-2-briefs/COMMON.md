# Common rules for every plan-2 brief

Read this first, then your item's brief. These are the orchestrator's standing rules; the
plan itself is `development/planning/plan-2_finish-line.md` and the repo's `CLAUDE.md` binds.

## The repo

- Repo: `/home/diegos/_dev/exoport/aboard` (module `github.com/exoport/aboard`). Go 1.26,
  stdlib + cobra/pflag/yaml.v3 only. `gofumpt`, `golangci-lint` are on PATH (bingo-pinned copies
  come from `make tools`). Chromium is at `/home/diegos/.local/bin/chromium`. Node is on PATH.
- **The spike at `/home/diegos/_dev/ai/board` is read-only history. Never write there, never
  touch its board on port 46624.**
- **Never commit, never push, never `git stash`/`git checkout --`/`git reset` anything you did
  not create.** The orchestrator commits. Leave the tree with your changes in place.
- **Never restart a healthy server you did not start.** `aboard status` tells you.
- Scratch space (logs, temporary projects for a running board, screenshots you look at):
  `/tmp/claude-1000/-home-diegos--dev-ai-board/7009f57e-89c0-4a45-b80b-5b15a6656847/scratchpad/`
  — make a subdirectory named after your item. A board for manual probing goes in a scratch
  project seeded with `./aboard init --example --gitignore` there, NEVER in the repo root
  (`.aboard/` in the repo is gitignored but the human's own board may live there).

## The ladder (run it, in this order, before you report)

```sh
go build ./... && go vet ./... && gofumpt -l . && go test -race ./...
make lint                      # must be zero findings; never weaken .golangci.yaml
make caps                      # ONLY when a views/*.spec.json or the command table changed
./aboard capabilities --check  # must exit 0 after make caps
make docs-cli                  # when the cobra tree changed (docs/reference/cli.md is generated)
make docs-check                # always, if you touched docs/ or README.md
make smoke                     # the browser suite: ONE run per tool call, timeout 180000,
                               # output to a file, read the whole file. Needs a RUNNING server
                               # started DETACHED (setsid nohup … &), on a scratch project.
                               # (make e2e replaces it once plan-2 item 4 lands.)
```

- `make smoke` and `make e2e` take ~1 min. **Never run two in one tool call**, and never start
  the server in the foreground of a call that could time out: a killed call takes the
  backgrounded server with it.
- `test/` and `web/` are **embedded in the binary** — rebuild before running any browser check
  or you test the previous copy, silently.
- `make caps` builds twice on purpose; do not "optimise" it.
- A screenshot you took must be LOOKED at (Read the PNG). `test/shot.sh` writes under
  `.aboard/run/shots/` of the project it is pointed at.
- `set -e` + `$(cmd; echo $?)` reads empty; write `$(cmd && echo 0 || echo 1)`.

## What a change must carry

- **A behaviour change updates the docs in the same change**: `CLAUDE.md`, the skill under
  `.claude/skills/aboard/`, `docs/` (Diátaxis; keep `make docs-check` green), and
  `docs/reference/http-api.md` / the generated `cli.md` where the surface moved.
- **Every fix has a test that fails before and passes after.** Say which test, and say that you
  saw it fail.
- **A CLI command written into a doc is executed once** by you, and exits as the doc says.
- **Colours only from `app.css` tokens; nothing in the browser executes anything; the four
  tab guarantees; ids `bb<n>`; per-viewer state never in the state file.** See `CLAUDE.md`.
- No `filepath.Join` outside `pkg/aboard/layout.go`.
- Judgement calls you took are RECORDED in your report, with the alternative you rejected.
  Anything that needs the human goes in the report under "needs the human", and you pick the
  conservative option meanwhile.

## The report (your final text IS the return value — raw, structured, no preamble)

```
## Changed
- <file>: <what> — <why>
## Tests
- <name>: <fails before / passes after, how you saw it fail>
## Ladder
build/vet/gofumpt: … | go test -race: N pkgs ok | lint: 0 | caps --check: 0 | docs-check: 0 | smoke/e2e: ok/fail (log path)
## Judgement calls
- …
## Not done / needs the human
- …
## Commit message draft
<subject as the claim>

<body: reasoning and the mistakes found on the way>
```

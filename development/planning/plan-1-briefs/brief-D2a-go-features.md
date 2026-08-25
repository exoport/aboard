# Phase D2a brief — Go features (after the rename)

Repo `/home/diegos/_dev/exoport/aboard`, HEAD = the rename commit (everything is `aboard` now:
`cmd/aboard`, `pkg/aboard`, `pkg/aboard/cli`, `pkg/aboard/web`, `.aboard/aboard.json`, `.aboard/run/`).
Governing plan: `development/planning/plan-1_port-from-spike.md`. Read it, then `git log -3`, then
`pkg/aboard/commands.go`, `layout.go`, `server.go` (postState, reconcile), `tabs.go`, `caps.go`,
`version.go`, `pkg/aboard/cli/*.go`, `pkg/aboard/web/views/diagram.js`, `views/html.spec.json`,
`views/diagram.spec.json`, `views/controls.js`.

**You own**: `pkg/`, `cmd/`, `go.mod`, `go.sum`, `testdata/recipes/`. **Another agent is
concurrently writing everything else** (Makefile, .claude/, docs/, README, CLAUDE.md, tooling
dotfiles, development/, .github, .bingo, testdata/example-board is NOT used — see §3). Do not touch
files outside your set; do not commit; do not `git add`. Do not run `make` targets that regenerate
files under `.claude/` (the other agent owns that dir) — run `go run ./cmd/aboard capabilities
--format js > pkg/aboard/web/views/controls.generated.js` by hand when you change a spec, and build
twice. Never touch the spike (`/home/diegos/_dev/ai/board`, a board runs there on 46624).

## 1. Root `--name` (persistent), env `ABOARD_NAME`

Move `--name` to the root command as a persistent flag (default from `ABOARD_NAME`), remove the
per-command copies on `serve`/`apply`, and have every command that touches a board (status, apply,
wait, poke, journal, watch, log, export, init, serve) resolve it. Update `commands.go` (`rootFlags`)
and the parity test. `test/smoke.sh`/`shot.sh` read `.aboard/run/instance.json` unconditionally —
leave them (not yours) but note it.

## 2. Version symbols — MUST match the staged tooling

Read `/tmp/claude-1000/-home-diegos--dev-ai-board/0e329b78-9ff1-4b58-bd23-4efd89232c39/scratchpad/staging/tooling/Makefile`,
`.goreleaser.yaml` and `NOTES.md`. The ldflags there set exported package vars in
`github.com/exoport/aboard/pkg/aboard` — `Version`, `BuildDate`, `GitCommit` (check the exact
names and paths in those files; `-X` against a missing symbol is silently ignored). Reconcile
`version.go`: those three exported vars, plus the resolver that falls back to
`debug.ReadBuildInfo` (module version, `vcs.revision`, `vcs.modified` → `+dirty`, `vcs.time`) and
finally `"dev"`. `aboard version` prints them (`--output-format` json/yaml gives all three). `/health`
and the instance file use the same resolver.

## 3. `aboard init [--example] [--gitignore]`

- Creates `<cwd>/.aboard/` (init is the ONE command that does not walk up — it creates a root
  where you stand; refuse if a root exists above or here and say where it is, unless `--name` names
  a new board in the existing root).
- Writes `.aboard/aboard.json` (or `aboard.<name>.json`) = the minimal document the shell accepts
  (read `web/aboard.html`'s boot path and the split's "minimal document" message; `version: 3`,
  `nextId: 1`, `tabs: []`, `updatedAt` now, `lastEditedBy: "init"`). Creates `.aboard/run/`,
  `.aboard/uploads/`, `.aboard/recipes/` (empty, with a one-line README.md saying what goes there and
  the frontmatter shape). Refuses to overwrite an existing state file.
- Prints the `.aboard/` gitignore line; `--gitignore` appends it to `<root>/.gitignore` if absent.
- `--example` seeds from the EMBEDDED example board: copy
  `/tmp/…/scratchpad/staging/fixture/testdata/example-board/aboard.json` to
  `pkg/aboard/example/aboard.json` (embed it from `pkg/aboard`; `//go:embed` cannot reach
  `testdata/` at the root — that is why it lives here). Rewrite `nextId`/`updatedAt` on seed.
  Read the staged `fixture/NOTES.md` for what the fixture contains.
- `serve` without a state file now says `run 'aboard init' in <dir>` (plus `--name` hint).
- Tests: fresh dir → files exist and parse; refuses overwrite; `--gitignore` idempotent; `--example`
  document has 15 tabs, unique ids, `nextId` > max id; refuses inside an existing root.

## 4. Recipes (`pkg/aboard/recipes.go`, `recipes/builtin/*.md`, `cli/recipes.go`)

Format (DECIDED): YAML frontmatter `name` (required, must equal the file stem), `description`
(required), `when_to_use` (required), `tags: [..]` (optional), `requires: {min_schema: N}`
(optional); body markdown; optional ONE fenced block tagged `aboard-template` holding a JSON tab
skeleton. Parse with a hand-rolled splitter in the shape of ape's
`/home/diegos/_dev/exoport/apex_process_ape/internal/frontmatter/frontmatter.go` (read it) +
`yaml.v3`.

Discovery, first wins by name: `<root>/_apex/aboard/recipes/*.md` → `<root>/_aboard/recipes/*.md`
→ `<root>/.aboard/recipes/*.md` → built-in (embedded from `pkg/aboard/recipes/builtin/`; copy the
staged files from `/tmp/…/scratchpad/staging/recipes/builtin/` — read that dir's files and the
staged `INDEX-FORMAT.md`). Folder names are literal strings with a comment saying ape hard-codes
`_apex/pipelines` the same way. A same-name file in a lower tier is reported as `shadowed_by` on the
winner and listed (dimmed / marked) rather than hidden. A file that fails to parse (no frontmatter,
name ≠ stem, missing required field, two template blocks, invalid JSON in the template) is NOT
skipped silently: it appears in `list` as invalid with the error, and `show` on it fails with the
error. `requires.min_schema > SchemaVersion` → marked "needs schema N".

Types: `Recipe{Name, Description, WhenToUse, Tags, Requires{MinSchema}, Scope, Path, Body, Template,
ShadowedBy []string, Err string}`; `Scope` one of `apex`, `aboard`, `dot-aboard`, `builtin` (render
as `_apex/aboard/recipes`, `_aboard/recipes`, `.aboard/recipes`, `built-in`). `DiscoverRecipes(root)
([]Recipe, error)`; `FindRecipe(root, name)`; `(Recipe).TemplateJSON()`.

CLI:
- `aboard recipes list [--output-format human|json|yaml]` — human: a table NAME · SCOPE · DESCRIPTION,
  shadowed entries and invalid ones marked; json/yaml: everything but Body.
- `aboard recipes show <name> [--template]` — prints the body (frontmatter stripped, a first line
  `# <name> — <description>` then the body); `--template` prints only the JSON skeleton, error exit 1
  if the recipe has none. Unknown name → exit 1 listing the available names.
- `aboard recipes index` — HIDDEN (`Hidden: true`); prints the markdown index the skill's
  `references/recipes.md` is generated from: exactly the format in the staged `INDEX-FORMAT.md`
  (built-in table + the discovery paragraph). Deterministic. No user recipes in it (it documents
  the binary, not a project).
- `recipes` goes into `commands.go` (with `index` marked hidden and skipped by parity the way `help`
  is, or included — pick one and make the parity test say which).
- `writeWarnings`-style honesty: none of this may fail silently and successfully.

Tests (use `testdata/recipes/` fixtures you create: a tree with all three tiers, one name present in
all three, one only in `.aboard`, one invalid (no frontmatter), one with name ≠ stem, one with a
template, one with `requires.min_schema: 99`; plus the staged user example
`/tmp/…/scratchpad/staging/recipes/example-user/decision-wizard-with-live-summary.md` copied to
`testdata/recipes/.aboard/recipes/`): precedence, shadow report, invalid reporting, template
extraction, built-ins present and every built-in parses, min_schema flag, `FindRecipe` unknown, the
index output is stable (golden compare against itself twice; no timestamps).

## 5. `aboard gen-docs --out <file>` — HIDDEN maintainer command

Copy the shape of ape's `internal/apecmd/gendocs.go` (read it): hand-rolled, deterministic markdown
of the whole cobra tree (skips hidden/help/completion), no timestamps. The staged Makefile's
`docs-cli` target calls it — read NOTES.md for the exact invocation and match it.

## 6. bb359 — spec drift (fix, with the checker catching it)

- `pkg/aboard/web/views/html.spec.json`: delete the sentence claiming an html BLOCK inside a stack
  does not render (it does; the human clicked it). Replace with one sentence saying it does and that
  its state lives in `blocks[].state.data`.
- `pkg/aboard/web/views/diagram.js`: the four raw `<button>` tags in the template string bypass
  `controls.js`. Route them through `controlsFor('diagram')(id)` and declare the four in
  `diagram.spec.json`'s `controls` list in toolbar order, with `doc`. Regenerate
  `controls.generated.js` (by hand, see above), build twice. `capsHash` moves — expected; say so.
  Mind hoisting: do not convert function declarations to const arrows.
- Add a Go test that greps `views/*.js` for `<button` inside template literals / innerHTML
  assignments outside `controls.js` and `menu.js`/`inline.js`/`ui.js`/`dag.js`/`markup.js` (the five
  the spike lists as legitimately using plain `button()`), so this cannot recur unnoticed — mirror
  what `test/smoke.sh`'s static checks do, in Go, so CI sees it.

## 7. bb360 — the four guarantees actually enforced

- `mergeSeen` (tabs.go) has zero call sites: wire it into the reconcile path so a non-human write
  can set only its own `seen[<by>]` key and never clear another actor's. Test it.
- `postState` defaults an absent `__by` to `"human"`, so a bare POST deletes every tab and clears
  every marker. Absent `__by` → `"unknown"`, treated as an agent (all four guarantees apply). The
  browser always sends `__by: "human"`; verify in `aboard.html` and note the line. `aboard apply
  --by human` stays refused. Test: a POST with no `__by` that drops a tab gets it back as a
  `pendingRemoval`; a POST with `__by: human` that drops it is honoured.
- While there: `changeSummary` ignores `note`-only edits (a note change is written but never
  journaled). Include `note` in the comparison — the note is human-authored intent and belongs in
  the journal. Test it.

## 8. Ladder before you report

`go build ./... && go vet ./... && gofumpt -l . && go test -race ./...` (all green; count the tests);
`go run ./cmd/aboard capabilities --format js > pkg/aboard/web/views/controls.generated.js` then
build again; a scratch project in `t.TempDir()`-style dir under the scratchpad: `aboard init
--example --gitignore`, `aboard serve` DETACHED, `aboard status`, `aboard recipes list`, `aboard
recipes show apply-a-write`, `aboard recipes show decision-wizard-with-live-summary --template | python3
-m json.tool` (after dropping the user example into that project's `.aboard/recipes/`), a shadowing
case (same name in `_aboard/recipes/`), `aboard version`, `ABOARD_NAME=x aboard init` then `aboard
status --name x`; stop by pid. Report every output. Run `golangci-lint run ./...` with the staged
`.golangci.yaml` copied to the scratchpad and `-c` pointing at it — you do NOT have to fix the backlog
(the next phase does) but report the count by linter so the next phase can budget.

## Report

Files changed/added; the new capsHash; test count; every command run with its outcome; deviations;
undone; and the exact CLI contract the docs/Makefile must match (every command, flag, hidden
command, exit code).

## 9. Defects found by executing the documented commands (fix these too, with tests where cheap)

- `aboard export --format csv` on a wrong format prints "try -format md" — spike grammar. Fix the message.
- `aboard watch` does not exit on SIGINT/SIGTERM (`timeout -s INT 3 aboard watch` still alive at 5s).
  The root `Execute` already has a signal-aware context; wire it into the streaming read so the
  command returns promptly. Verify with `timeout -s INT 2 ./aboard watch; echo $?`.
- `aboard journal` exits 1 when no board is running, so the third command of the resume protocol
  fails in a project whose board is stopped. DECIDED: fall back to reading
  `.aboard/run/journal.jsonl` from disk when nothing is listening (as `export` already works
  serverless), and say `(from disk — no board running)` on stderr in human mode.
- The embedded example board: the smoke check "a gate export carries the reasons" greps
  `aboard export decisions` for `Why:`; the staged fixture resets the Decisions tab (`bb128`) to
  no rows. Give the fixture's Decisions tab ONE decided example row with a reason (look at the
  spike's board.json bb128 for the exact row shape — read only) so the example demonstrates a
  decided verdict and the assertion holds. Note it in the fixture's tab `note`.
- `HostStandalone` moved from "board" to "aboard": an old `.board/run/instance.json` is no longer
  recognised — intended; just make sure `status` in a dir that has ONLY a stale `.board/` says
  "no .aboard/ here — run aboard init" rather than something confusing.

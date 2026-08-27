# Plan 1 — build `aboard` from the `board` spike

Status: decided 2026-08-25 with the human (16 questions asked, all answered). Phases A–D landed as
commits 9f0c7af, c192fb8, a83c107, a350127, e5c698b, 0b508d8. Paused before Phase E (the finishing
pass) at the human's request — resume from `development/handoffs/handoff-phase-e-finish.md`. This is the
brief every implementing session works from. Where it says DECIDED, do not relitigate.

> **`development/handoffs/` no longer exists**, and neither does the extension repo's
> `docs/handoff.md`. Both were deleted on **2026-08-27**, once every handoff in them had been
> implemented and everything load-bearing had been promoted to a permanent home (`docs/`,
> `CLAUDE.md`, plan-2 §10, and — for the extension — `aboard_vscode/README.md`). Every
> reference in this file to a `handoff-*.md` file, or to `aboard_vscode/docs/handoff.md`, is preserved
> as **history**: it names the document this work was written from at the time, and `git log`
> in the respective repository holds the full text. Nothing in this file is a live pointer, including the Status line above.

**Phase A–C landed** as the three bisectable commits decision 2 asked for:
`9f0c7af` (port the spike verbatim), `c192fb8` (split into an embeddable engine, a cobra
tree and one resolved root), `a83c107` (rename the product, keeping every name that is a
data contract). **Phase D is in progress**: the feature commits, the build/release
tooling, the skill, the docs, the handoffs and the example board.

One deviation from §15 worth reading before you follow the paths below: the example
board is **embedded at `pkg/aboard/example/aboard.json`**, not `testdata/example-board/`,
so that a `go install` binary carries the thing `aboard init --example` seeds from.
Everywhere this plan says `testdata/example-board/`, read `pkg/aboard/example/`.

## Sources

| what | where | role |
|---|---|---|
| spike | `/home/diegos/_dev/ai/board` | the working implementation: 11 Go files (`package main`, stdlib only), `board.html`, `app.css`, `views/*.js` + `views/*.spec.json`, `vendor/mermaid.min.js`, `assets/`, `test/`, `.claude/skills/board/`, `_output/` (handoffs + one example recipe, **untracked**, backed up at `/tmp/claude-1000/-home-diegos--dev-ai-board/0e329b78-9ff1-4b58-bd23-4efd89232c39/scratchpad/output-backup/`) |
| conventions | `/home/diegos/_dev/exoport/apex_process_ape` | how a Go CLI is shipped here: cobra, `.bingo/`, Makefile, `.golangci.yaml`, `.goreleaser.yaml` (keyless cosign), `.github/workflows/{ci,release}.yml`, dotfiles, Diátaxis `docs/`, `.claude/skills/{handoff,release}`, `CLAUDE.md` shape |
| target | `/home/diegos/_dev/exoport/aboard` | the definitive project (empty at start) |
| target 2 | `/home/diegos/_dev/exoport/aboard_vscode` | receives the updated VS Code extension handoff only (`docs/handoff.md` + README); the extension is a later session's work |

A board is **running from the spike** (port 46624, pid in `.board/instance.json`). Never
`Edit`/`Write` the spike's `board.json`; read it. Never run the spike's `restart.sh -force`.
The spike is read-only for this plan except `_output/` is not touched either.

## Decisions (all DECIDED by the human on 2026-08-25)

1. **Dependencies: cobra + pflag + yaml.v3 throughout.** The spike's "stdlib only" rule is
   reversed for aboard. Module `github.com/exoport/aboard`, `go 1.26.6`. Keep the dependency
   list to exactly those (plus their transitive closure) unless a later plan adds one.
2. **Build order: port verbatim → split → rename, three separately verified commits**, then
   feature commits. Rename is LAST because the app identity sits inside the hashed caps
   manifest and the manifest machinery must be proven before it reports a new hash.
3. **CLI grammar: subcommands with ape's conventions.** `--cwd <dir>` on the root,
   `--output-format human|json|yaml` where output is structured. A **hand-declared command
   table** in caps replaces `flag.VisitAll` (which under cobra would silently return the
   wrong flags and move `capsHash`). Every command written in any doc is executed once to
   prove it runs.
4. **State file is `.aboard/aboard.json`**, route `GET/POST /aboard.json`. Content lives at
   `.aboard/{aboard.json, uploads/, recipes/}`; machine-local runtime files live under
   `.aboard/run/{instance.json, instance.<name>.json, journal.jsonl, logs/<tab>.log, shots/}`.
   A second named board is `.aboard/aboard.<name>.json`. ONE resolved root, looked up in one
   place (`layout.go`), nowhere else. Atomic-write temp prefix `.aboard-*.json` inside the
   same directory.
5. **Project root: upward walk for `.aboard/`** from `--cwd` (default `os.Getwd()`), stopping
   at the filesystem fixed point (mirror `apexcfg.Find`). The port is derived from the
   **discovered root**: `sha256(root + "\x00" + name)`, first 4 bytes big-endian,
   `41000 + n%8000`, probing forward up to 24 ports as today. `--port` and `PORT` still
   override. `ABOARD_NAME` replaces `BOARD_NAME`. The instance file remains the discovery
   authority for clients.
6. **Two identities.** `/health` and `instance.json` carry `app: "aboard"` when served by the
   `aboard` binary and `app: "ape-aboard"` when served by ape; both are stamped with `host`
   (the same string), `argv0`, `pid`. `probeBoard` accepts either identity. The identity is
   injected by the host through `aboard.Options{App: ...}` — never read from `os.Args[0]`.
   The caps manifest's `app` field is `"aboard"` regardless of host (it describes the board,
   not the process), so `capsHash` is host-independent.
7. **Base path.** The shell is served with a single injected constant (`window.ABOARD_BASE`,
   default `""`), set by `serve --base-path /prefix`; every browser→server URL, the html-tab
   iframe `src`, the SSE `EventSource` and the `<link>`/`<script>` tags build from it.
   Root-absolute literals are gone from `views/*.js` and the shell.
8. **handoff-13-features scope: split.** During the port fix the two correctness items:
   `bb359` (spec drift: `html.spec.json` line saying an html block in a stack does not render
   is FALSE — delete it; `views/diagram.js` draws four raw `<button>` tags in a template
   string — route them through `controlsFor('diagram')` and declare them in
   `diagram.spec.json`) and `bb360` (the four guarantees: `mergeSeen` has zero call sites —
   wire it into `reconcileTabs`; an absent `__by` on POST must NOT default to `"human"` —
   default to `"unknown"` which gets agent-level powers only, and `--by human` from the CLI is
   refused). The remaining eleven (`bb361`–`bb372` minus `bb370`) become the first build
   queue, recorded in `development/handoffs/handoff-13-features.md` with `bb372`
   (`boards`) re-specified against instance files instead of a `/proc` scan.
9. **`aboard init`**: creates `.aboard/aboard.json` (`{"version":3,"nextId":1,"tabs":[]}` —
   check the spike for the exact minimal document `board.html` accepts), creates
   `.aboard/run/`, prints the gitignore line; `--gitignore` appends `.aboard/` to the
   project's `.gitignore` if absent; `--example` seeds from the embedded example board
   (`pkg/aboard/example/aboard.json (embedded; //go:embed cannot reach testdata/)`, converted from the spike). Refuses to overwrite an
   existing state file. `serve` refuses to start without a state file and says to run `init`.
10. **Recipes.** Frontmatter: `name` (required, must equal the file stem), `description`
    (required), `when_to_use` (required), `tags: [..]` (optional), `requires: {min_schema: N}`
    (optional), plus an optional fenced block ` ```aboard-template ` holding a JSON tab
    skeleton extractable with `--template`. Lookup order, first wins on name:
    `_apex/aboard/recipes/` → `_aboard/recipes/` → `.aboard/recipes/` → built-in. Built-in
    recipes are authored as `pkg/aboard/recipes/builtin/<name>.md` in the same format and
    embedded; `make caps` generates the skill's `references/recipes.md` index from them.
    Shadowing is allowed and always reported. Folder names are literal strings (ape hard-codes
    `_apex/pipelines` the same way).
11. **Skill: a hand-copied directory `.claude/skills/aboard/`**, written for the `aboard`
    prefix. NOT embedded in the binary, no `skill install`. The human's skill framework
    derives the `ape aboard` variant. The generated reference stays committed in the skill
    dir and regenerated by `make caps`; `aboard status` warns when it is stale.
12. **Bridge globals renamed outright**: `window.aboard`, `window.__ABOARD_DATA__`, the
    `__aboard:` postMessage envelope. No alias. Every example widget carried into the fixture
    gets its HTML rewritten to the new names.
13. **Tests/CI: Go tests gate CI; the browser suite is local.** `go test -race ./...` on
    ubuntu + windows plus golangci-lint in `ci.yml`. `test/smoke.sh` and `test/shot.sh` are
    ported as `make smoke` / `make shot`. Go tests must cover: compare-and-set, the four tab
    guarantees, id reconciliation, `capsHash` stability, `writeWarnings`, root discovery,
    port derivation, layout paths, recipe lookup + precedence + shadow report + frontmatter
    validation, `init`, and the command table ↔ cobra tree parity.
14. **Docs: lean `CLAUDE.md` in ape's shape + Diátaxis `docs/`.** The argued-out judgment
    ("What the board is FOR", the three tiers, the four guarantees, the sandbox posture,
    "nothing in the UI starts a session", "no diff renderer, closed") becomes
    `docs/explanation/why-*.md`, substance verbatim. The dated journal, the verification
    ledger and "nothing is open" are dropped to git history. `docs/reference/cli.md` is
    generated from the cobra tree (`make docs-cli`).
15. **The repo gitignores `.aboard/`.** The spike's example tabs become
    `pkg/aboard/example/aboard.json (embedded; //go:embed cannot reach testdata/)` (bridge names rewritten, asset paths adjusted),
    used by tests and by `aboard init --example`.
16. **VS Code extension: handoff only.** `aboard_vscode/docs/handoff.md` rewritten for the
    new names plus a README pointing at it. The three aboard-side prerequisites
    (`?chrome=` param, active-tab postMessage, localStorage try/catch) go on the build queue.

Conventional calls taken without asking: keep the `bb` id tag; keep every other HTTP route
name; drop `server.js`; `vendor/` → `pkg/aboard/web/lib/` (Go treats `vendor/` specially);
copy `release.yml` + the goreleaser `signs` block verbatim with the certificate identity
changed to `https://github.com/exoport/aboard/.github/workflows/release.yml@refs/tags/vX.Y.Z`
and `goreleaser-action` pinned to the bingo version; `go.work` for local cross-module dev
(gitignored), never a committed `replace`; copy ape's `handoff` and `release` skills (release
with the ape-only price/harness gates deleted, not left to no-op); `handoff-capability-
manifest.md` kept as rationale only; git init locally and commit, **no remote, no push**;
`apex_process_ape` is not modified in this plan.

## Target layout

```
aboard/
  cmd/aboard/main.go                 thin: os.Exit(cli.Execute(cli.Options{App: "aboard"}))
  pkg/aboard/                        engine (package aboard)
    layout.go                        Root discovery, .aboard/ paths — THE only place paths are joined
    state.go                         board document types, read/write, compare-and-set, atomic write
    tabs.go ids.go                   the four guarantees, id reconciliation (from spike)
    server.go                        http server, routes, SSE, watcher (from spike main.go)
    htmltab.go wait.go reload.go journal.go logs.go upload.go export.go caps.go  (from spike)
    recipes.go                       discovery, frontmatter, precedence, template extraction
    recipes/builtin/*.md             built-in recipes (embedded)
    commands.go                      the declared command table (feeds caps + is asserted equal to the cobra tree)
    version.go                       ldflags override, else debug.ReadBuildInfo of this module
    web/                             package web: embed.FS
      aboard.html app.css views/ lib/mermaid.min.js assets/ test/
    *_test.go
  pkg/aboard/cli/                    package cli: cobra tree
    root.go                          NewRootCmd(Options) *cobra.Command; Execute(Options) int
    serve.go status.go init.go apply.go wait.go poke.go journal.go watch.go log.go export.go capabilities.go recipes.go version.go
    exit.go                          typed exit errors (copy ape internal/apecmd/report.go's exitError/ExitCode shape)
  .claude/skills/aboard/             SKILL.md + references/ (reference.generated.md, recipes.md generated)
  .claude/skills/handoff/ .claude/skills/release/   copied from ape
  docs/{README.md,tutorials,how-to,reference,explanation}
  development/planning/plan-1_port-from-spike.md   (this file)
  development/handoffs/*.md          the updated handoffs
  test/smoke.sh test/shot.sh         local browser suite
  pkg/aboard/example/aboard.json (embedded; //go:embed cannot reach testdata/)
  testdata/recipes/{_apex/aboard/recipes,_aboard/recipes,.aboard/recipes}/…   precedence fixtures
  .bingo/ Makefile .golangci.yaml .goreleaser.yaml .github/workflows/{ci,release}.yml
  .gitignore .gitattributes .pre-commit-config.yaml .markdownlint.yaml .markdownlintignore .prettierrc.yaml .prettierignore
  LICENSE NOTICE CHANGELOG.md README.md CLAUDE.md go.mod go.sum
```

`//go:embed` cannot reach upward, so the whole web tree lives inside `pkg/aboard/web/`.
The engine takes the `fs.FS` (four functions in the spike already do).

## CLI grammar (final)

```
aboard [--cwd DIR] <command>
  serve        [--name N] [--port P] [--state FILE] [--dev] [--base-path /p]      the default is NOT serve; a bare `aboard` prints help
  status       [--output-format]                                                     running? url, pid, caps beacon, skill staleness
  init         [--name N] [--example] [--gitignore]
  apply        [--by ACTOR] [--check] [--strict] [--label S] [--force] [--name N]   stdin JSON → compare-and-set through the running server
  wait         [--for PRED] [--timeout D] [--note S]                                 exit 0 released, 3 timed out
  poke         [--note S]
  journal      [--limit N] [--output-format]
  history      <tab> [--at N] [--limit N] [--output-format]                          what a tab said before; --at prints a document `apply` accepts
  watch                                                                              JSON lines until interrupted
  log          <tab>                                                                 stdin → tab log
  rendered     [tab] [--output-format]                                               what the browser reported it drew
  uploads      [--prune] [--yes] [--output-format]                                   files under .aboard/uploads/ and the tabs that mention them
  export       <tab|key> [--format md|csv]
  capabilities [type] [--format json|md|js] [--check]
  recipes list [--output-format]      | recipes show <name> [--template]
  version      [--output-format]
```

`history`, `rendered` and `uploads` were added by plan-2 item 6 (handoff-13-features
`bb363`, `bb368`, `bb369`), which is why they are here and not in the original grammar,
and so were `apply`'s `--check`, `--strict` (`bb362`), `--label` (`bb371`) and `--force`
(the review's no-base finding, plan-2 item 2). `boards` is NOT here: it is gated on the
human (plan-2 §10).

`--by human` is refused from the CLI. `-name`/`BOARD_NAME` → `--name`/`ABOARD_NAME`.
The old single-dash modes do not exist; `aboard -status` prints cobra's unknown-flag error.

## Verification ladder (each commit)

- `go build ./... && go vet ./... && gofumpt -l . && go test -race ./...`
- `make caps` (builds twice; asserts the generated reference and controls module are current)
- `make smoke` against a server started detached for the repo's own `.aboard/` (seeded with
  `aboard init --example`), never in the foreground of a call that might time out
  — **historical: `make smoke` and `test/smoke.sh` were retired by plan-2 item 4. The
  browser suite is `make e2e`, which seeds its own temporary board and needs no server
  at all. This ladder is the record of how plan 1 was verified, not a list to run today.**
- `make shot` of at least: the gallery tab, one html tab (shoot `/tab/<id>/html` directly), the
  help panel (`#help`) — and LOOK at the pictures
- for the rename commit: `grep -rn '\bboard\b' --include=*.go --include=*.js --include=*.html --include=*.css --include=*.md --include=*.json` and justify every survivor (data contract: `bb` ids, the `board.json` schema keys, `by: "claude"` in chat history handling)

## Commit messages

Same style as the spike and ape: subject is the claim, body is the reasoning and the
mistakes found on the way. Trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

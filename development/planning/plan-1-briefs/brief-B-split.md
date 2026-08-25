# Phase B brief — the split (commit 2 of 3)

Repo: `/home/diegos/_dev/exoport/aboard` (git, branch main, HEAD = the verbatim port).
Governing plan: `development/planning/plan-1_port-from-spike.md` in that repo — READ IT FIRST,
every decision in it is settled. This brief is the subset that is YOUR commit.

**This commit changes STRUCTURE, not NAMES.** Everything is still called `board`: the binary,
`AppName = "board"`, the state file `board.json`, the runtime dir `.board/`, the skill dir
`.claude/skills/board/`, the bridge globals, the UI strings. The rename is commit 3 (another
session). Do not rename anything user-visible. Do not touch `.claude/skills/`, `README.md`,
`CLAUDE.md`, `_output/`, `development/`.

Reference material you should read (read-only, never modify):
- ape's embeddable command shape: `/home/diegos/_dev/exoport/apex_process_ape/internal/apedcmd/root.go`
  (exported `NewRootCmd() *cobra.Command`, `SilenceUsage`, `SilenceErrors`, signal-aware `Execute`)
- ape's typed exit errors: `/home/diegos/_dev/exoport/apex_process_ape/internal/apecmd/report.go` (`exitError`, `ExitCode(err) (code, silent)`)
- ape's flag conventions: `/home/diegos/_dev/exoport/apex_process_ape/internal/apecmd/config.go` (`--output-format human|json|yaml`, `--cwd`)
- ape's upward root walk: `/home/diegos/_dev/exoport/apex_process_ape/internal/apexcfg/apexcfg.go` `Find(start)`
- ape's version resolution: grep `buildident` under `/home/diegos/_dev/exoport/apex_process_ape/internal/`
- cobra/pflag versions to use: the ones in ape's go.mod (`spf13/cobra v1.10.2`, `spf13/pflag v1.0.10`)

## Deliverable

### 1. Package layout

```
cmd/board/main.go                  ~10 lines: os.Exit(cli.Execute(cli.Options{Host: aboard.HostStandalone}))
pkg/aboard/                        package aboard — the engine. Every spike .go file moves here
                                   (server.go replaces main.go: the server, routes, SSE, watcher; flag parsing and
                                   mode dispatch are GONE from the engine)
pkg/aboard/layout.go               Root discovery + every path (see §3). The ONLY file that joins a path under the root.
pkg/aboard/commands.go             the declared command table (see §5)
pkg/aboard/version.go              Version() from ldflags var, else debug.ReadBuildInfo for this module, else "dev"
pkg/aboard/web/                    package web: `//go:embed board.html app.css views lib assets test` → `var FS embed.FS`
                                   (the spike's root files/dirs MOVE here; `vendor/` becomes `lib/` — Go treats vendor/ specially)
pkg/aboard/cli/                    package cli — the cobra tree. One file per subcommand. `NewRootCmd(Options) *cobra.Command`,
                                   `Execute(Options) int`. exit.go copies ape's exitError/ExitCode shape.
```

Embeddability rules for `pkg/aboard` and `pkg/aboard/cli` (ape will `AddCommand` this tree):
no package-level cobra vars, no `init()` that registers commands, no `flag.Parse`, no
`log.SetFlags`/`log.SetOutput` at package level, no `os.Exit` outside `cli.Execute` and
`cmd/board/main.go`, no reads of `os.Args[0]`. Commands return typed errors; `Execute` maps
them to exit codes (wait timeout = 3, as today). Server logging goes through a logger the
Options carry (default `log.Default()`).

Exported engine API (keep it small, document each): `Options`, `HostStandalone`/`HostApe`
constants (`"board"` / `"ape-board"` — the rename commit changes the strings, not the
shape), `AppName`, `FindRoot(start) (Root, error)`, `Root` with its path methods,
`Open`/`Serve`-style constructor for the server, the client-side helpers the CLI needs
(`RunningInstance`, `Apply`, `Wait`, `Poke`, `Journal`, `Watch`, `Log`, `Export`,
`Capabilities`, `Status`). Unexported stays unexported.

### 2. CLI grammar (exactly this; the command name is still `board`)

```
board [--cwd DIR] <command>
  serve        [--name N] [--port P] [--state FILE] [--dev] [--dev-dir DIR]  [--base-path /p]
  status       [--output-format human|json|yaml]
  apply        [--by ACTOR] [--name N]          stdin JSON → compare-and-set through the running server
  wait         [--for PRED] [--timeout D] [--note S]     exit 0 released, 3 timed out
  poke         [--note S]
  journal      [--limit N] [--output-format]
  watch
  log          <tab>
  export       <tab|key> [--format md|csv]
  capabilities [type] [--format json|md|js] [--check]
  version      [--output-format]
```
A bare `board` prints help (exit 0). `board -status` is an unknown-flag error. `--by human` is
refused from the CLI with a message (the human acts in the browser). `ABOARD_NAME` env is NOT
introduced yet — keep `BOARD_NAME` for this commit; `PORT` stays. `init` and `recipes` are
NOT in this commit (a later phase adds them; `serve` without a state file must fail with a
clear message saying the state file path it looked for).

### 3. Layout and root discovery (`layout.go`)

- `FindRoot(start)`: `filepath.Abs(start)`, walk up until a directory containing `.board/` is
  found; stop at the filesystem fixed point (`filepath.Dir(dir) == dir`) → `ErrNoRoot` naming
  the start dir. `--cwd` sets `start` (default `os.Getwd()`).
- Paths under the root (state = content, run = machine-local):
  `.board/board.json` (or `.board/board.<name>.json`), `.board/uploads/`,
  `.board/run/instance.json` (or `instance.<name>.json`), `.board/run/journal.jsonl`
  (`journal.<name>.jsonl` for a named board — check what the spike does and keep its rule),
  `.board/run/logs/<tab>.log`, `.board/run/shots/`.
  The atomic-write temp file is created in the state file's directory with prefix `.board-*.json`.
- The three divergent path roots in the spike (`instancePath()` relative to CWD; journal.go and
  logs.go relative to the state file's dir) all collapse into `Root` methods. Grep for every
  `filepath.Join`, `".board"`, `"uploads"`, `os.Getwd`, `os.DirFS(".")` and make sure none
  survive outside layout.go except through a `Root` method.
- Port derivation hashes the **discovered root** (same algorithm: `sha256(root + "\x00" + name)`,
  first 4 bytes big-endian, `41000 + n%8000`, probe forward 24). `--port`/`PORT` override.
- `--dev` serves the web tree from disk: `--dev-dir` default `pkg/aboard/web` relative to the
  root. The reload signature and the CSS-relink path must still work in dev.
- `serve` writes the instance file under `run/` and removes it on clean shutdown as today.

### 4. Identity

`Options.Host` is `HostStandalone` (`"board"`) or `HostApe` (`"ape-board"`). `/health` and the
instance file stamp `app: <host>`, plus `host`, `argv0` (from `os.Args[0]` ONLY at the cmd
layer — pass it in through Options; the engine never reads os.Args), `pid`. `probeBoard`
accepts either identity. The caps manifest's `app` is `AppName` regardless of host.

### 5. The declared command table replaces `collectFlags()`

`commands.go` declares every command, its flags (name, type, default, doc) and its exit codes
as data. `buildManifest` uses it for the manifest's `commands` section (replacing `flags`);
this is what the generated skill reference and `GET /capabilities` show. Add a Go test that
walks the cobra tree from `cli.NewRootCmd` and asserts it equals the table exactly (names,
flags, defaults) — so neither can drift. Expect `capsHash` to change in this commit (the
manifest's shape changed); regenerate with `make caps` and say so in the commit message.

### 6. Base path

Serve `board.html` through a tiny template step that injects `window.ABOARD_BASE = "<base>"`
(the name is deliberately already `ABOARD_` — it is new, not renamed) from `--base-path`
(default `""`). Then remove every root-absolute literal from `board.html` and `views/*.js`:
`/board.json`, `/health`, `/capabilities`, `/waiters`, `/poke`, `/events`, `/app.css`,
`/views/*.js`, `/log?tab=`, `/upload?name=`, `/tab/<id>/html`, `/journal?limit=`, `/uploads/…`,
image `src` for `assets/…`. Use one helper (e.g. `api(path)`) in the shell that the views import
or read from `window`. The html-tab iframe `src` and the `EventSource` go through it too. The
server, for its part, strips the base path prefix before routing. Prove it: start with
`--base-path /x`, fetch `/x/board.json` and `/x/tab/<id>/html`, and shoot the page under `/x/`.

### 7. Tooling in this commit

- `Makefile`: keep the spike's targets working against the new layout (`build` → `go build -o board ./cmd/board`,
  `caps` still builds twice and emits `pkg/aboard/web/views/controls.generated.js` and
  `.claude/skills/board/references/reference.generated.md`, `check`, `test` → `go test -race ./...`,
  `smoke` → `test/smoke.sh`, `shot` → `test/shot.sh`, `dev`). The ape-style Makefile arrives later; do not restyle it.
- `restart.sh`: update to the new binary path, `serve` subcommand and `.board/run/instance…json`.
- `test/smoke.sh` and `test/shot.sh`: port every `./board -x` invocation to the subcommand grammar,
  every `.board/instance.json` read to `.board/run/`, every `board.json` read to `.board/board.json`.
  Add ONE real state-changing write (rename a tab and revert it through `board apply`) immediately
  before the "the journal records writes" assertion — it currently passes only on history.
  `test/smoke.html` lives inside `pkg/aboard/web/test/` now (it is embedded — rebuild before running).
- `.gitignore`: `/board` binary, `/.board/`, `/dist/`, keep the rest sensible.
- `go.mod`: add cobra + pflag, `go mod tidy`, commit `go.sum`.
- Delete `server.js` (3 of 16 routes, none of the guarantees; its fallback in restart.sh goes too).

### 8. Go tests (new, in this commit)

`pkg/aboard`: FindRoot (found at start, found two levels up, not found → ErrNoRoot), every
`Root` path method, port derivation (known vector: compute one and pin it; and root-vs-cwd
independence), `capsHash` stable across two `buildManifest` calls, the command-table ↔ cobra
parity test in `pkg/aboard/cli`. Use `t.TempDir()`; no server needed for these.

### 9. Verification ladder — run ALL of it, in this order, and report each result honestly

1. `go build ./... && go vet ./... && gofumpt -l . (must print nothing) && go test -race ./...`
2. `make caps` — twice-built; then `./board capabilities --check` exits 0.
3. Seed a board for the smoke run: `mkdir -p .board && cp /home/diegos/_dev/ai/board/board.json .board/board.json`
   (READ the spike's board.json; never write to it; never touch the spike's server on 46624).
4. Start the server DETACHED: `(setsid nohup ./board serve > .board/run/server.log 2>&1 &)`; wait for
   `.board/run/instance.json`; `./board status` must show it.
5. `./test/smoke.sh > .board/run/smoke.log 2>&1` — ONCE per tool call, timeout 180000 ms, never twice in
   one call. Target: every check passes, including the journal one. Read the log, not the tail.
6. `./test/shot.sh` for the gallery tab (bb133), a dag (bb1), the html tab's frame directly
   (`/tab/bb72/html`), and the help panel (`bb22#help`). LOOK at each PNG with the Read tool. A tab
   that renders an empty box passes every DOM assertion.
7. Base path: restart with `serve --base-path /x`, curl `/x/board.json` (200) and `/x/tab/bb72/html`
   (200), shoot `/x/` and look at it.
8. Embeddability: `grep -rn 'os.Exit\|flag.Parse\|os.Args\|init()' pkg/` — justify each survivor in the report.
9. Stop the server by the pid in the instance file. Remove the binary. `git status` must show only what you intend.

### 10. Commit

`git add -A && git commit` with the repo's message style: the subject is the claim, the body is
the reasoning and every mistake found on the way (there will be some — write them down). Use
`-c user.name="Diego Szychowski" -c user.email="diegopodo@gmail.com"`. Trailer:
`Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`. One commit. Do not push (there is no remote).

## Traps recorded by this project — each cost real time once

- `//go:embed` cannot reach upward; the error reads like a path typo.
- `pkg/aboard/web/test/` is EMBEDDED: editing `smoke.html` and re-running tests the OLD copy until rebuilt.
- `make caps` must build twice; drop one and the server serves stale button labels.
- A `function` → `const` arrow rewrite breaks hoisting (markup.js `makeIconBtn` is called above its definition).
- Headless screenshots need `?nosse=1` (shot.sh appends it) and do not paint iframes — shoot `/tab/<id>/html` directly.
- Never run the smoke suite twice in one tool call; never run a server in the foreground of a call that can time out.
- `set -e` inside `$(cmd; echo $?)` makes the assertion vacuous; use `$(cmd && echo 0 || echo 1)`.
- A literal `<\/script>` in raw HTML swallows the script; only inside a JS string is the escape right.
- `-apply` printing `applied` is not evidence anything renders: read stderr warnings, then look at a picture.
- `frame-ancestors` in htmltab.go was widened deliberately for VS Code; do not tighten it.

## Report back (final text = raw data for the orchestrator)

Commit hash; the ladder results step by step with numbers (tests run, smoke ok/fail counts,
which PNGs you looked at and what you saw); the new `capsHash`; the exported API list; every
deviation from this brief with the reason; anything left undone.

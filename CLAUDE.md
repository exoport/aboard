# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Project overview

`aboard` is a single-binary Go CLI that serves a **shared visual board** for a human
and one or more agent sessions. The whole browser UI — shell, stylesheet, renderers,
the mermaid bundle — is embedded with `//go:embed`, and the board's state is one JSON
file on disk that both sides read and write. **Tabs are data, not code**: an agent
opens one for whatever it needs to show — a graph, a chart, a question form, an
annotated screenshot, a channel to another session, a bespoke widget — and reads back
what the human changed.

Two shapes ship from one tree: the `aboard` binary, and the same cobra tree mounted
inside another CLI as `ape aboard <command>` (see
[why two identities](docs/explanation/why-two-identities.md)). That is why
`pkg/aboard` is a **library** with no `os.Exit`, no `flag.Parse`, no package-level
cobra state and no reads of `os.Args`.

## Resuming after a context clear

Three commands, in this order, before touching anything:

```bash
aboard status                # running? which URL? plus the caps beacon and skill staleness
aboard capabilities          # what this board can do: every type, state field, control,
                             #   colour name, route and command — no server needed
aboard journal --limit 20    # who changed what recently, including other sessions
```

`aboard capabilities` is the point of the manifest: **do not reconstruct the surface
from memory or from this file — ask the binary.** `aboard capabilities kanban` is the
cheap per-type version. If `status` warns that the skill reference was generated for a
different `capsHash`, the skill is describing a board that no longer exists: run
`make caps`.

Then read the board itself, and each tab's `note` — that is the human's statement of
what the tab is FOR, and it carries intent the contents cannot.

### Directory map

| Path                              | Purpose                                                                                                                              |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `cmd/aboard/`                     | Binary entry point. Thin: it resolves `Options` and calls `cli.Execute`, the only place a status leaves the process.                  |
| `pkg/aboard/`                     | The engine (`package aboard`): server, routes, SSE, state document, guarantees, manifest. A library — see the constraints in `aboard.go`. |
| `pkg/aboard/layout.go`            | **Root discovery and every path under `.aboard/`. The only file that joins a path** — no `filepath.Join` outside it.                  |
| `pkg/aboard/tabs.go` · `ids.go`   | The four tab guarantees and the id allocator invariant.                                                                              |
| `pkg/aboard/caps.go`              | The board describes itself: `web/views/*.spec.json` → `aboard capabilities`, `GET /capabilities`, the generated skill reference, and `apply`'s write warnings. |
| `pkg/aboard/commands.go`          | The **declared command table**: the CLI surface as data, feeding the manifest and asserted equal to the cobra tree.                   |
| `pkg/aboard/recipes.go` + `recipes/builtin/` | Recipe discovery, frontmatter, precedence and template extraction; the built-in recipes, embedded.                         |
| `pkg/aboard/web/`                 | `package web`: the whole browser tree as an `embed.FS` — `aboard.html`, `app.css`, `views/`, `lib/`, `assets/`, `test/`.              |
| `pkg/aboard/cli/`                 | `package cli`: the cobra tree. One command per file, plus `exit.go` (typed exit errors) and `format.go` (`--output-format`).          |
| `test/e2e/`                       | The browser suite: `package e2e`, `//go:build e2e`, playwright-go. Local only, never in CI. Needs no server and no `PROJECT` — it seeds a temp board and serves the engine in-process. See [how to run it](docs/how-to/run-the-browser-suite.md). |
| `test/shot.sh`                    | The screenshot tool, and the last shell script in `test/`. Needs a RUNNING server; `PROJECT=<dir>` says which board, defaulting to this checkout, and it only reads. |
| `restart.sh`                      | Dev convenience only: start this project's board or print the URL of the one already running, and `-force` to actually replace it. `aboard serve` refuses a duplicate on its own; this adds the deliberate restart. |
| `pkg/aboard/example/`             | The example board `aboard init --example` seeds from, embedded so a `go install` binary carries it; also the fixture the Go tests read. |
| `.claude/skills/aboard/`          | The skill: SKILL.md + references. Hand-copied into a project; its generated half is rebuilt by `make caps`.                           |
| `.claude/skills/{handoff,release}/` | Working skills for this repo, carried from ape: writing a handoff, and cutting a release. The release one drops ape's harness gates. |
| `docs/`                           | User-facing docs, Diátaxis-structured — see [docs/README.md](docs/README.md).                                                         |
| `development/`                    | Plans and handoffs. `git log` is a real source here too: commit messages carry the reasoning and the mistakes.                        |

## Workflow

### Make targets

```bash
make help          # available targets
make build         # → ./aboard
make install       # → /usr/local/bin/aboard (override INSTALL_DIR=...)
make run           # build, then serve with the embedded UI
make dev           # serve pkg/aboard/web from disk, so UI edits need no rebuild
make status        # what is running for this project, and is the skill stale
make test          # go test -race ./...
make test-cover    # with coverage profile
make check         # vet + gofmt — the gate that needs no tools fetched
make lint          # golangci-lint (pinned via bingo)
make fmt           # gofumpt (pinned via bingo)
make caps          # regenerate the controls module, skill reference and recipe index
make docs-cli      # regenerate docs/reference/cli.md from the cobra tree
make docs-check    # docs/ links resolve and every doc is reachable from docs/README.md
make e2e           # LOCAL ONLY: the browser suite — a real Chromium, driven (~1 min).
                   #   No server and no PROJECT: it seeds its own temp board.
                   #   E2E_HEADED=1 · E2E_TRACE=always · E2E_KEEP=1 · E2E_RUN='TestKanban.*'
make shot          # LOCAL ONLY: screenshots (PROJECT=<dir> SHOT_TABS="<tab-id> <tab-id>#help")
make snapshot      # goreleaser snapshot (no upload, no sign)
make govulncheck   # scan for known vulnerabilities (pinned via bingo)
make xcompile-windows  # cross-compile + cross-vet for Windows
make ci-local      # the full pre-push gate
make pre-commit    # run the pre-commit hooks across all files
make tools tidy clean
```

Tooling is pinned via [bingo](https://github.com/bwplotka/bingo) — `.bingo/Variables.mk`
plus a per-tool `.mod`. Upgrade with `bingo get <module>@<version>` and commit the
regenerated files.

### Commits

- [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`).
- The subject is the claim; the body is the reasoning and the mistakes found on the way.
- Pre-commit hooks must pass before a commit lands.

## Two hard rules

**1. Never write `.aboard/aboard.json` with `Edit`/`Write` while a board is running.**

```bash
aboard apply --by "agent-1" < next.json
```

`Edit` bypasses compare-and-set, so a concurrent change from the browser or another
session is destroyed with no error. Measured, not theoretical. `apply` uses the **`rev`**
inside the document you submit as its base — a counter the server stamps on every
accepted write, not `updatedAt`, which was a millisecond clock two writes could share —
so read-edit-apply is safe by construction; a `409` means someone got there first —
re-read, redo, apply again. A document with no `rev` has no base at all and is refused
(exit 2) rather than clobbering everything since the last read; `--force` writes it
anyway, says so on stderr, and is not the way past a `409`. Use
`agent-1` / `agent-2` / `agent-<role>` for `--by`, never `claude`: it is shown to the
human on every tab the write touched. `--by human` is refused from the CLI.

**2. Never take a healthy server away from another session.** `aboard serve` refuses to
start a second board for the same project and prints the URL of the one already
running — take that URL rather than freeing the port. The board a colleague or another
session is watching is not yours to restart.

## Decisions already made — do not relitigate

- **Tabs are data, not code.** A new renderer is three declarations, and all three are load-bearing — [why](docs/explanation/why-tabs-are-data.md).
- **The board is a local, persistent, non-authoritative channel**, and `.aboard/` is gitignored — [why](docs/explanation/why-a-local-non-authoritative-channel.md).
- **Four guarantees are server-enforced, not conventions**: no agent deletes a tab, clears a `touched` marker, un-acks a chat message, or clears another actor's `seen` — [why](docs/explanation/why-four-guarantees-are-server-enforced.md).
- **Nothing in the UI may START an agent session.** The board may ask; a session may choose to wait — [why](docs/explanation/why-nothing-in-the-ui-starts-a-session.md).
- **A diff renderer is rejected. Closed, not deferred** — [why](docs/explanation/why-no-diff-renderer.md).
- **`html` tabs are sandboxed with `connect-src 'none'`**, and `frame-ancestors` deliberately admits VS Code's webview origins — [why](docs/explanation/why-html-tabs-are-sandboxed.md).
- **Two identities, one `.aboard/`**: `aboard` and `ape-aboard` are hosts; the manifest's app name is neither — [why](docs/explanation/why-two-identities.md).
- **The capability surface is declared and checked, never scraped**; controls are a list because their order is the toolbar's — [why](docs/explanation/why-the-manifest-is-declared.md).
- **Writes are serialised, and a `409` is the guarantee that nothing of yours landed.** One lock across read → compare-and-set → reconcile → write, and the token compared is a **revision counter** (`rev`), never a timestamp; the losers of a simultaneous write are refused, not queued on top — [why](docs/explanation/why-writes-are-serialised.md).
- **The board answers loopback only, and refuses a cross-site write.** A `Host` outside `localhost`/`127.0.0.1`/`[::1]` is `403` (the DNS-rebinding guard), and so is any mutating request whose `Origin` is not the board's own — [the rules](docs/reference/http-api.md#who-is-allowed-to-ask). Neither is authentication; the server has none. They stop a browser from being the thing that reaches it for somebody else.
- **Nothing in the browser executes anything.** Action buttons, `gate` verdicts and `ui` buttons RECORD an intent; the agent that asked acts on it. That is what makes a stray click harmless on a server with no auth.
- **Ids are board-wide monotonic, tagged `bb`, with no type prefix** — [reference](docs/reference/state-file.md#ids). Form *field* ids stay semantic.
- **An id is enough coming FROM the human and not enough going TO them.** They say an id and you can look it up; you say one and they may have nothing. Name the thing and put the id beside it — "the Migration review tab (`<id>`)", never "the button in `<id>`".
- **Prefer `ui` over `html`** whenever a component tree can express it: it cannot get the theme, contrast or type sizes wrong, and the next session can change one node instead of reading a page of someone else's JavaScript. `html` is for when the INTERACTION is the point.
- **Colours only from `app.css` tokens.** Single dark theme, text pinned to WCAG AAA, no hex in any view. The periwinkle token is `--agent`; `claude` resolves to nothing, and a write naming an unknown colour warns.
- **Per-viewer UI state never goes in the state file** — selection, zoom, collapsed blocks, marks-hidden, chat drafts.
- **One resolved root.** Paths are joined in `layout.go` and nowhere else — enforced by `TestNothingOutsideLayoutJoinsAPath`, an AST walk, because the rule had four violations for as long as nothing checked it. The port is derived from the discovered root, so the URL is the same from any subdirectory.
- **Dependencies are cobra + pflag + yaml.v3 and their closure.** No vendor directory; the mermaid bundle is committed at `pkg/aboard/web/lib/` because Go treats `vendor/` specially. **`playwright-go` is TEST-ONLY**: it is reached only from `test/e2e/`, every file of which is behind `//go:build e2e`, so it never enters the binary — `go list -deps ./cmd/aboard` does not mention it and neither does `go version -m ./aboard`. Its module path is `github.com/mxschmitt/playwright-go`, which is what the community fork publishes in its own `go.mod` at these tags.

## This repo gitignores `.aboard/`

The board is local, per-developer and unversioned: several developers on one repo each
get their own, and a committed one would mean a whole-file JSON conflict on every merge
over a conversation that was never theirs. So `.aboard/` — the state file, `uploads/`,
project recipes, and everything machine-local under `run/` — is ignored **here too**,
including in aboard's own checkout.

The demo content is compiled into the binary at `pkg/aboard/example/aboard.json`
instead, where it is a fixture rather than a log of somebody's afternoon, and
`aboard init --example` seeds a board from it: **15 tabs covering every renderer**, one
per type, each with a `note` saying what it demonstrates — plus `kanban` twice (once
borrowing the dag's nodes through `stateFrom`, once read-only as an agent-owned queue),
and `notes` as a block inside the `stack` tab rather than a tab of its own. That is also
how you get a board to develop against:

```bash
aboard init --example --gitignore
aboard serve
```

Not versioned is not the same as disposable: a gate request waiting on the human, or a
session parked on `aboard wait`, has to survive a restart and a week away.

## Gotchas that cost real time

- **The web tree is compiled into the binary.** After editing anything under `pkg/aboard/web/`, rebuild (`make build`) or run `aboard serve --dev`, or your change appears to do nothing.
- **`make caps` builds twice, and neither build is redundant.** `pkg/aboard/web` is embedded, so the first binary emits `views/controls.generated.js` from the current specs and the second embeds the module it just wrote. Drop one and the server serves the previous controls while your spec edit appears to do nothing.
- **`test/` is embedded too**, and it still holds two probe pages (`pkg/aboard/web/test/mermaid-probe.html`, `theme-probe.html`). Editing anything under `pkg/aboard/web/` and re-running a browser check tests the OLD copy, silently. Rebuild first, not after.
- **Do not run `make e2e` twice in one shell call.** It is ~1 min, so two runs blow a two-minute tool timeout. It needs no server, so there is nothing to start in the foreground and nothing to kill — that class of accident is gone with `test/smoke.sh`.
- **The browser suite cannot touch your board any more.** `make e2e` seeds a temp root, serves it in-process on a free port, and deletes it. The old `make smoke` had to be aimed at a real board with `PROJECT=`, WROTE to it, and poked the notify channel — releasing any session genuinely blocked on `aboard wait`. `test/shot.sh` still takes `PROJECT` and still needs a running server, because it reads the board and writes only pictures.
- **A failing `$(...)` aborts a `set -e` script with no message at all.** `sh` is `dash` here, so `BASE=$(sed … missing-file)` ended the old shell suite instantly and `make` printed nothing but its own error line — it read as "the suite is broken", not "the file is missing". `test/shot.sh` is the one shell script left where this can still bite: every command substitution whose failure is survivable needs `|| true`. Same family as `$(cmd; echo $?)` reading empty.
- **A headless screenshot needs `?nosse=1`.** The SSE stream never closes, so chromium never reaches network-idle and writes no file at all — exit 2, no message. `test/shot.sh` appends it; a hand-rolled chromium command does not.
- **Headless chromium does not reliably paint iframe content**, so verify an `html` tab by shooting `/tab/<id>/html` directly. `--virtual-time-budget` also starves cross-process `postMessage`, which makes frame auto-sizing look broken when it is fine in a real browser.
- **A browser check that cannot run must FAIL, not skip.** The retired shell suite had ten sections that printed `skip …` and let the run exit 0, so a third of the checks could be absent with nothing to say so. `test/e2e` has no skip path for a missing dependency: `TestMain` installs the driver and fails the run if it cannot, and a fixture that has gone missing is a `t.Fatal` naming what it needed. The one `t.Skip` in the suite names a genuine ambiguity (nowhere empty on the dag canvas to drop on), which is what a skip is for.
- **The whole browser suite shares ONE temporary board**, so run it shuffled now and then: `go test -tags e2e -count=1 -timeout 10m -shuffle=on ./test/e2e`. Declaration order hides order dependencies, and it hid two: a test helper allocating scratch tab ids from a private counter the board's own allocator walked into (two tabs, one id), and a real shell defect where a tab removed in the browser came back on screen if a reload landed inside the save it was awaiting. Allocate ids from the document's `nextId`, and do any agent-side setup BEFORE `open()` — a write issued after the page is up is a foreign change racing the thing under test.
- **`make e2e` writes its evidence twice on a failure** — into the temp board it drove, and into `<repo>/.aboard/run/e2e/<TestName>/`, which is gitignored and survives the run. A trace (`npx playwright show-trace`), a full-page screenshot, the board document, and that page's console. **Look at the screenshot.**
- **A CLI command in a doc is a claim. Run it.** The spike's resume section said `-journal -l 20` when the flag was `-limit`, so the third command a resuming session ran exited 2. Nothing tests the commands in prose; if you write one, execute it once.
- **`apply` succeeding is not evidence that anything renders.** It prints `applied` and exits 0 for a document that draws an empty box — `ui` is the worst offender, because an unknown component shows a marker but an unknown PROP shows nothing at all. Read the stderr warnings, then shoot the tab and **look at the picture**.
- **Screenshots land under the target project's `.aboard/run/shots/`** because a snap-confined chromium cannot write outside `$HOME`. That constraint and `PROJECT=/tmp/...` pull against each other: if `make shot` writes nothing at all against a scratch project under `/tmp`, the confinement is why, and a scratch project under `$HOME` is the way out.

## Documentation

User-facing docs live in `docs/` and follow [Diátaxis](https://diataxis.fr/):
`tutorials/`, `how-to/`, `reference/`, `explanation/`. Place a new page in the quadrant
that matches its primary user need — see [docs/README.md](docs/README.md) — and keep it
reachable from that index, which `make docs-check` enforces along with every relative
link. `docs/reference/cli.md` is generated by `make docs-cli`; never hand-edit it, and run it in the
same change as any edit to a command's help text — `TestTheGeneratedCLIReferenceIsNotStale`
fails when the committed file stops matching the cobra tree.

The repo-level [README.md](README.md) is the entry point for first-time visitors: short
intro, fast install, link into `docs/` for depth.

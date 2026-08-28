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

Four commands, in this order, before touching anything:

```bash
aboard status                # running? which URL? plus the caps beacon, the skill's
                             #   staleness, and how many requests are waiting on you
aboard requests              # what the HUMAN has asked for, oldest first, naming the tab
aboard capabilities          # what this board can do: every type, state field, control,
                             #   colour name, route and command — no server needed
aboard journal --limit 20    # who changed what recently, including other sessions
```

`aboard capabilities` is the point of the manifest: **do not reconstruct the surface
from memory or from this file — ask the binary.** `aboard capabilities kanban` is the
cheap per-type version. If `status` warns that the skill reference was generated for a
different `capsHash`, the skill is describing a board that no longer exists: run
`make caps`.

Then read the board itself, and each tab's `note` — the agent's brief statement of what
the tab is FOR, which the human may edit; it carries intent the contents cannot. What
they want DONE about a tab is a different field, `requests`, and `aboard requests` is how
you find it.

### Where the project stands

**Nothing is open.** `development/planning/plan-2_finish-line.md` is complete as of
2026-08-26: the two races, the review's behaviour/coverage/low findings, the browser
suite, the JSON hot paths, the eleven reviewed features, the VS Code panel's
server-side prerequisites, and the extension itself (in `aboard_vscode`, built,
unit-tested, and **run in a real Extension Development Host once, partially**, on
2026-08-26, then worked through a second time the same day — what those runs reached is
the status block in the extension's `README.md`, which is now the only record of it: that
repo's `docs/handoff.md` was deleted on 2026-08-27 with its load-bearing content moved
into the README). On **2026-08-27** the extension's notify bell became a **nudge button**
(`$(zap)` / `$(circle-slash)`, commands `aboard.nudge*`), a dev **`.vsix`** packages with
`npm run package`, and the human **installed it and ran it** — which found four things no
test had (plan-2 §10r): the purpose strip read as a notification, the `+` cost a row of
the panel, and the VS Code palette mapping was wrong TWICE. Both palette failures were
made of individually valid colours, which is why nothing warned: **the four depth tokens
are an ORDER and the eleven voices are a SET that must stay mutually distinguishable**,
and a host theme guarantees neither — so an embedder now sends the ten neutrals and keeps
the board's voices ([theme.md](docs/reference/theme.md)). Confirmed in a running host,
high contrast both ways. The review file has a disposition beside every finding, and
`development/handoffs/` is gone — see the decision below.

That one run is also the only reason the native-dialog defect below was ever found: it
is invisible to a suite that runs at the top level of a browser, and it had shipped.

**The one open list is that plan's §10** — five entries, and every one of them is a
question rather than work: the remote and the first tag, the `ape aboard` mount
and the `aboard <cmd>` strings that go with it, installing the extension, and the five
porting judgement calls that stand until overruled (written out in §10 itself since
2026-08-27, rather than pointing at a file). **Go 1.27 left that list on 2026-08-27**,
answered by the human: the toolchain moved on this machine, so `go.mod` says `go 1.27.0`,
`github.com/go-json-experiment/json` is gone in favour of the stdlib `encoding/json/v2`,
and the goreleaser pin it was blocking reached v2.18.0 — see the dependency decision
below. Four
more left that list on 2026-08-26 when the human answered them (§10c, and the work
landed with the answers): the example board's prose says "the agent", the notify
button's acknowledgement is a flash the repaint cannot reach, the journal entry carries
the whole tab so the `apply` merge survives a foreign rename, and `boards` — dropped
that morning and REVERSED the same day with a design — shipped as a `/proc` scan with
no registry file.
On **2026-08-28** the clipboard round trip landed and then taught the same lesson twice:
the panel runs `xclip` because a webview cannot reach the clipboard at all, and the board
now learns what its host can do from an ANNOUNCEMENT rather than by asking and timing
out — a timeout cannot tell "nothing framed me" from "an old host" from "a host that
broke", and one failure survived three rounds of reinstall-and-restart on that evidence.
Gating the ask on that announcement was then a regression within the hour, because a host
one build older announces nothing and copies perfectly well: **an announcement explains a
failure, it does not authorise the attempt.** The same day, two renames before the first
tag — **ids are tagged `ab`** (was `bb`; a rename, not a migration, since every parser
reads `^[a-z]*(\d+)$`) and the document **schema resets to 1** (it read 3, counting the
spike's two layout changes). That second one shipped a defect worth remembering: the
version is declared in Go AND in the shell, and changing one made every board come up
with an empty tab strip, no console error and a valid document — now checked by
`TestTheShellAgreesWithTheDeclaredSchemaVersion`. `capsHash` is `207b5d93`.

`development/README.md` carries a separate list — findings that are real,
are nobody's blocker and are nobody's question (`--dev` symlinks, the sidecar log file
count, `BUILD_DATE` and `make install INSTALL_DIR`), each with what it would take, so
nobody re-measures them. A fifth, the pinned-versus-`$PATH` divergence, is RESOLVED:
see "the make targets are the gate" below.

If you are resuming and looking for the next task, **there is not one queued — ask the
human.** A remote exists (`github.com/exoport/aboard`, and `…/aboard_vscode` for the
extension) and **nothing has ever been pushed to either**. `git remote -v` prints
`git@github.diegosz:exoport/…` rather than `github.com` because this machine rewrites it
with a `url.…insteadOf` rule in `git config` pointing at an SSH host alias that picks the
right key — the alias is local configuration, not the address, so **write `github.com` in
prose**; an earlier version of this file said `github.diegos_exo`, which is neither the
real host nor the alias actually in use.
Pushing waits for the human: their own manual test of both repos, and their review of
plan-2 §10.

### Directory map

| Path                              | Purpose                                                                                                                              |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `cmd/aboard/`                     | Binary entry point. Thin: it resolves `Options` and calls `cli.Execute`, the only place a status leaves the process.                  |
| `pkg/aboard/`                     | The engine (`package aboard`): server, routes, SSE, state document, guarantees, manifest. A library — see the constraints in `aboard.go`. |
| `pkg/aboard/layout.go`            | **Root discovery and every path under `.aboard/`. The only file that joins a path** — no `filepath.Join` outside it.                  |
| `pkg/aboard/tabs.go` · `ids.go`   | The five tab guarantees and the id allocator invariant.                                                                              |
| `pkg/aboard/caps.go`              | The board describes itself: `web/views/*.spec.json` → `aboard capabilities`, `GET /capabilities`, the generated skill reference, and `apply`'s write warnings. |
| `pkg/aboard/commands.go`          | The **declared command table**: the CLI surface as data, feeding the manifest and asserted equal to the cobra tree.                   |
| `pkg/aboard/recipes.go` + `recipes/builtin/` | Recipe discovery, frontmatter, precedence and template extraction; the **nine built-in** recipes, embedded in the binary.        |
| `recipes/` (top level)            | The **recipe library**: recipes worth sharing but not worth shipping in every binary. Not embedded, not a discovery tier — a project gets one by copying the file into `.aboard/recipes/` or a sibling. See [recipes/README.md](recipes/README.md). |
| `pkg/aboard/web/`                 | `package web`: the whole browser tree as an `embed.FS` — `aboard.html`, `app.css`, `views/`, `lib/`, `assets/`, `test/`.              |
| `pkg/aboard/cli/`                 | `package cli`: the cobra tree. One command per file, plus `exit.go` (typed exit errors) and `format.go` (`--output-format`).          |
| `test/e2e/`                       | The browser suite: `package e2e`, `//go:build e2e`, playwright-go. Local only, never in CI. Needs no server and no `PROJECT` — it seeds a temp board and serves the engine in-process. See [how to run it](docs/how-to/run-the-browser-suite.md). |
| `test/shot.sh`                    | The screenshot tool, and the last shell script in `test/`. Needs a RUNNING server; `PROJECT=<dir>` says which board, defaulting to this checkout, and it only reads. |
| `restart.sh`                      | Dev convenience only: start this project's board or print the URL of the one already running, and `-force` to actually replace it. `aboard serve` refuses a duplicate on its own; this adds the deliberate restart. |
| `pkg/aboard/example/`             | The example board `aboard init --example` seeds from, embedded so a `go install` binary carries it; also the fixture the Go tests read. |
| `.claude/skills/aboard/`          | The skill: SKILL.md + references. Hand-copied into a project; its generated half is rebuilt by `make caps`.                           |
| `.claude/skills/{handoff,release}/` | Working skills for this repo, carried from ape: writing a handoff, and cutting a release. The release one drops ape's harness gates. |
| `docs/`                           | User-facing docs, Diátaxis-structured — see [docs/README.md](docs/README.md).                                                         |
| `development/`                    | Plans, briefs and the review record. **No handoffs** — see the decision below. `git log` is a real source here too: commit messages carry the reasoning and the mistakes.  |

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
make lint          # golangci-lint (pinned via bingo) — the gate
make fmt           # gofumpt (pinned via bingo) — rewrites
make fmt-check     # gofumpt (pinned via bingo) — reports, and fails; the gate
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

**RESOLVED 2026-08-26 — the make targets are the gate, and nothing calls a tool from
`$PATH`.** A pinned tool and the same tool on `$PATH` are two different tools, and this
repo used to run both: `make lint`/`make fmt` took the bingo pin while the
`golangci-lint-mod` pre-commit hook and the ladder's bare `gofumpt -l .` took whatever
the machine had. They disagreed — the pinned linter reported 0 where the hook's copy
reported 11, and the pinned formatter rewrote a file the `$PATH` copy called clean — so
a commit could pass one gate and fail another with nothing in the tree having changed.

Now: `bingo get` moves a pin, `make` is the only thing that runs a tool, and the hook
and CI run make. `.pre-commit-config.yaml` is two `local` hooks (`make lint`,
`make fmt-check`); `.github/workflows/ci.yml` runs the same two plus `make govulncheck`;
the ladder rung is **`make fmt-check`**, never `gofumpt -l .`. `make check` stays the
zero-dependency gate (`go vet` + stdlib `gofmt`) for a bare checkout that has fetched
nothing. One pin moves with a second file: `goreleaser`'s version is written into
`.github/workflows/release.yml` as well, and the comment on that line says so.

Pinned versions live in `.bingo/*.mod`: golangci-lint **v2.13.1**, gofumpt **v0.11.0**,
govulncheck **v1.7.0**, goreleaser **v2.18.0**. The first three were already the latest
release when the toolchain moved to Go 1.27 on 2026-08-27 and did not need to move;
goreleaser did, because v2.18.0 requires Go 1.27 and the pin had been held at v2.17.1
waiting for exactly that. All four run against a Go 1.27 module unchanged — golangci-lint
v2.13.1 in particular reports 0 issues on it, so no pin had to be moved to keep a gate
green.

### Commits

- [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`).
- The subject is the claim; the body is the reasoning and the mistakes found on the way.
- Pre-commit hooks must pass before a commit lands. They are `make lint` and `make fmt-check`, so the hook and the ladder are the same two commands running the same two pinned binaries — `make pre-commit` and a local ladder cannot disagree any more.

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

That refusal is anchored to the BOARD, not to the port, so `--port` is not a way around
it: before binding anything, `serve` reads this project's instance record for this name
and asks the process it names over `/health`, and a board that answers as this project's
own is named in the refusal whatever port was requested (`PORT=` too). A record that does
not answer is a killed board, so it is overwritten rather than obeyed; the port probe
stays in the derived walk for the mirror case, a live board whose record was deleted.
It WAS per port until 2026-08-26 — `serve --port <a free port>` started a second server
on the same state file, rewrote `run/instance.json` to point at itself, and on exit
removed it, leaving `aboard status` reporting no board while the original served on.
Measured, then fixed. Use `--name` for a second board.
**And never `pkill -f "aboard serve"`**: it matches every board on the machine, including
the human's. Kill by the pid in the instance record, as `restart.sh` does.

## Decisions already made — do not relitigate

- **Tabs are data, not code.** A new renderer is three declarations, and all three are load-bearing — [why](docs/explanation/why-tabs-are-data.md).
- **The board is a local, persistent, non-authoritative channel**, and `.aboard/` is gitignored — [why](docs/explanation/why-a-local-non-authoritative-channel.md).
- **Five guarantees are server-enforced, not conventions**: no agent deletes a tab, clears a `touched` marker, un-acks a chat message, clears another actor's `seen`, or writes the human's own `requests` — [why](docs/explanation/why-the-guarantees-are-server-enforced.md).
- **A tab's `note` and the human's `requests` are two different fields, deliberately.** The `note` is the purpose strip — the AGENT's brief statement of what the tab is for, which the human may edit. `requests` is the other direction: their notes to an agent about that tab, added and deleted only by them, and an agent may only ADD a `done` stamp to one. They were one field and the merge was lossy both ways — a purpose rewritten into a to-do stops being a purpose, and a to-do in the purpose strip has nowhere to record that it was dealt with. Two consequences worth keeping in mind, because both were found the hard way: a request carries an ID, so it is the only thing outside a tab's `state` the id allocator has to walk (`ids.go`); and `aboard requests done` is a write whose ENTIRE content is a change to that list, so the 409 merge compares it — without that, a same-tab collision took the board's copy and reported "applied (merged)" having stamped nothing.
- **Nothing in the UI may START an agent session.** The board may ask; a session may choose to wait — [why](docs/explanation/why-nothing-in-the-ui-starts-a-session.md).
- **A diff renderer is rejected. Closed, not deferred** — [why](docs/explanation/why-no-diff-renderer.md).
- **`aboard boards` is a `/proc` scan, Linux only, honest everywhere else — and there is still no registry.** The human dropped this feature on 2026-08-26 and REVERSED that the same day, with the design: scan the process table, no file. Both halves are load-bearing and neither is open. **The scan** (`pkg/aboard/boards_linux.go`) walks `/proc/[0-9]*`, matches on `cmdline` rather than `comm` — `comm` is 15 characters and, under `ape aboard serve`, is the HOST's name, so a name filter misses one of the two ways this project is meant to run — honours a `--cwd` found in the argv, `FindRoot`s from there, and then does exactly what `status` does for one project: read the root's `instance*.json`, keep the record whose `pid` matches, verify it over `/health`. One row per (root, name), sorted, with the FULL project path, because a reader of a machine-wide listing is by definition not standing in the project it names. **The registry stays rejected**: `~/.aboard/known-roots.json` would be new user-level state outside `.aboard/`, written on every serve and still only a hint, where a process either exists or does not and nothing has to be cleaned up when it dies. **`/proc` is Linux-only, and that is answered rather than argued with**: the scanner is behind `//go:build linux`, everywhere else the command still exists, is still declared, and exits 2 with one line naming the platform and pointing at `aboard status` inside each project. A command missing on two of three platforms is worse than one that is present and honest. Two things it deliberately does NOT do: drop a record whose process has gone (it is listed as "recorded but not answering" — a stale record is information), and hide how much of the machine it could see ("N processes inspected", and "N could not be inspected (permission)" for another user's board).
- **A named board owns everything it writes for itself; the project owns `uploads/` and `recipes/`.** `--name` used to qualify only the state file and the instance record, so two boards in one project shared `journal.jsonl`, `rendered.json` and `logs/<tab>.log` — every record keyed by TAB ID, and tab ids are allocated per board, so both documents have a `ab1`. The shared journal then held two entries naming `ab1` and meaning different tabs, and `aboard history ab1 --name review` offered the DEFAULT board's version as a document to restore. Each board now writes `journal.<name>.jsonl`, `rendered.<name>.json` and `logs/<name>/<tab>.log`. **No migration, and none is possible**: an entry already in `journal.jsonl` does not record which document the write went to, so old entries stay readable and count as the default board's. The two shared paths are shared on purpose and neither is keyed by tab id — an upload is content a human pasted and either board may show it, a recipe is a document about the project. That is why **`aboard uploads` reads every board's document** and prints a named board's tab as `review:ab1`: it used to scan one board's tabs, so `--prune --yes` deleted an image the other board was displaying. `--name` does not narrow it. See [named boards](docs/reference/layout.md#named-boards).
- **Two kinds of recipe, decided by the human on 2026-08-26.** The **embedded** recipes (`pkg/aboard/recipes/builtin/`, nine) are small and are basic functionality — they ship in every binary. The **external** recipe files in the repository's top-level `recipes/` folder are the **curated library**: not embedded, not a discovery tier. Any user may write their own recipes, or copy one from the curated folder into a project tier (`_apex/aboard/recipes` > `_aboard/recipes` > `.aboard/recipes`). A recipe is promoted from the library into the built-ins only when it turns out to be needed everywhere.
- **`html` tabs are sandboxed with `connect-src 'none'`**, and `frame-ancestors` deliberately admits VS Code's webview origins — [why](docs/explanation/why-html-tabs-are-sandboxed.md).
- **Two identities, one `.aboard/`**: `aboard` and `ape-aboard` are hosts; the manifest's app name is neither — [why](docs/explanation/why-two-identities.md).
- **The capability surface is declared and checked, never scraped**; controls are a list because their order is the toolbar's — [why](docs/explanation/why-the-manifest-is-declared.md).
- **The write path costs the EDIT, not the board.** The server keeps the state document parsed in memory — bytes, tabs with opaque state, and per tab a normalised form and the largest id it uses — and a POST decodes the incoming body exactly ONCE and the document on disk not at all. Unchanged tabs are compared as bytes and carry their derived facts forward, so one small edit on a board of 5 000 tabs no longer canonicalises 5 000 states twice and walks the whole document for ids twice. The write path still RE-READS the file every time and compares it before trusting the cache: a stat can miss a foreign edit of the same length inside one mtime tick, and on the write path that would mean reconciling against a document that no longer exists. `GET /aboard.json` is served from that cache with an `ETag`, and a re-read stats the file on both sides of itself so a read that straddles a rename can never pin the old document under the new file's stat; the watcher is stat-gated, so a 10 MB board costs one syscall per tick at idle instead of a whole-file SHA-256 five times a second. Measured in `pkg/aboard/bench_test.go`, and every claim above has a counting test in `document_test.go`.
- **A `409` is a merge, once, and a same-tab collision still stops.** `apply` used to
  hand back one sentence and discard the agent's whole document — built from a board it
  can no longer read — because compare-and-set is whole-document and ANY concurrent write
  conflicts with any other. It now re-reads, asks the journal which tabs moved since the
  base it started from (and what each held AT that base, which is the only record of the
  third document), re-applies its own tabs where the server did not touch them, and
  retries ONCE. Both refusals that remain are deliberate: a genuine same-tab collision is
  NAMED — with the FIELD as well as the tab — and never merged silently, exactly as the
  browser refuses to; and a conflict the merge cannot reason about — a timestamp base, a
  journal rotated past the base — falls back to the plain refusal with the reason on
  stderr. `JournalEntry` gained `rev` for this: `at` is a millisecond clock, which is the
  token this project stopped trusting. It then gained the whole TAB (2026-08-26): with
  only a `state` on the record, a tab somebody else renamed while you wrote to a
  different tab could not be classified, so the merge compared against the fresh copy,
  found a difference neither side of your write had made, and refused. That case merges
  now. A pre-`schema` entry still cannot attribute a rename and still refuses, and the
  message names the generation rather than blaming the caller.
- **Writes are serialised, and a `409` is the guarantee that nothing of yours landed.** One lock across read → compare-and-set → reconcile → write, and the token compared is a **revision counter** (`rev`), never a timestamp; the losers of a simultaneous write are refused, not queued on top — [why](docs/explanation/why-writes-are-serialised.md).
- **The board answers loopback only, and refuses a cross-site write.** A `Host` outside `localhost`/`127.0.0.1`/`[::1]` is `403` (the DNS-rebinding guard), and so is any mutating request whose `Origin` is not the board's own — [the rules](docs/reference/http-api.md#who-is-allowed-to-ask). Neither is authentication; the server has none. They stop a browser from being the thing that reaches it for somebody else.
- **A write warning goes to the journal entry and to the screen, never into a tab.** `POST /aboard.json` runs the write-time checks over the tabs the write TOUCHED, and the strings ride the journal entry, the POST reply, the SSE frame, the tab's notice banner and the trace tab. Scoped on purpose: a whole-document scan would re-report every pre-existing mistake as though this write had made it — the example board's deliberately invalid `sparkline` would then ride along on every write ever made, and a warning that always fires is one people learn to skip. It still warns on a write that touches ITS tab, and there is no suppression mechanism for it. The reply and the frame also name the tabs the checks RAN over, which is the only thing that can take a banner back DOWN: a clean tab is absent from the warnings, which is the same shape as a tab the write never looked at, so a page that only ever received warnings could raise a banner and never lower one. `apply --check` runs the checks and posts nothing; `apply --strict` refuses on any warning. Warning-not-refusing stays the default, because a spec can legitimately lag its renderer. `apply --label "…"` records WHY a write happened — stripped off the payload beside `__by` and `__base`, stored on the journal entry and never in the board document, and printed by `journal`, `watch` and the trace tab. A journal answered who and what and never why, so "the write that broke the gallery" could not be found without reading every payload; it is navigation inside a local, rotating file, never a record to cite anywhere permanent.
- **Nothing in the browser executes anything.** Action buttons, `gate` verdicts and `ui` buttons RECORD an intent; the agent that asked acts on it. That is what makes a stray click harmless on a server with no auth.
- **Ids are board-wide monotonic, tagged `ab`, with no type prefix** — [reference](docs/reference/state-file.md#ids). Form *field* ids stay semantic. The tag was `bb` until 2026-08-28 and `n` on the spike before that; every parser reads `^[a-z]*(\d+)$`, so a change of tag renames nothing on disk and older ids keep resolving.
- **An id is enough coming FROM the human and not enough going TO them.** They say an id and you can look it up; you say one and they may have nothing. Name the thing and put the id beside it — "the Migration review tab (`<id>`)", never "the button in `<id>`".
- **Prefer `ui` over `html`** whenever a component tree can express it: it cannot get the theme, contrast or type sizes wrong, and the next session can change one node instead of reading a page of someone else's JavaScript. `html` is for when the INTERACTION is the point.
- **Colours only from `app.css` tokens, in two variants, and a project may patch them.** 21 tokens, each declared twice — `:root` is DARK and is the default, `:root[data-theme="light"]` is light — with text pinned to WCAG AAA and no hex in any view. The palette is described by what it IS (a neutral near-black ramp, one olive accent, a periwinkle for what an agent says, an orange for what the human asks) rather than by the name of the editor theme it came from; the product name is gone from the tree. `TestBothThemesDeclareTheSameTokens` refuses a token declared in one variant and forgotten in the other, which is the drift that renders as no error at all: an un-redeclared custom property simply inherits, so a light board comes up with one black-on-black label and nothing anywhere says so. The periwinkle token is `--agent`; `claude` resolves to nothing, and a write naming an unknown colour warns.
  - **The switch is per viewer** — a topbar button, `localStorage` under `aboard.theme`, stamped on `<html>` by a classic script in the head so the first paint is already right. It never goes near the state document, like scroll, zoom and `?chrome=`. There is deliberately no keyboard shortcut: a shell-level key is taken away from every renderer that might want it.
  - **`.aboard/theme.json` is a project's house style**: `{version, default, dark:{…}, light:{…}}`, a PATCH over the built-ins so a file written today does not lose a token added tomorrow. Content rather than runtime (it describes the project, so it is meant to be committed — the one path worth un-ignoring), per project rather than per board. Validated against the declared token names with the same voice `apply` uses for a colour name; unknown token, unusable value, unknown default and unparseable file are each DROPPED with a warning, never fatal — a trailing comma in a config file must not blank a board. Warnings reach three audiences on purpose: the serve log, `aboard status`, and the browser console. It is SPLICED into the shell before first paint (a fetch would flash the built-in palette on every load) and watched, so an edit reaches an open page over SSE under a `theme` key.
  - **Three things a custom property cannot reach, and each is told instead**: an `html` tab's frame is a separate document (both variants are spliced into it and the parent posts `{__aboard:'theme', kind, tokens}` — `kind` picks the variant, `tokens` re-sends theme.json's overrides because the frame's copy of those was spliced in at LOAD and an edit does not reach an open document; told, not reloaded, because a reload throws away whatever the widget held); a `diagram` bakes token VALUES into its rendered SVG (it re-renders on an `aboard:theme` event); a mermaid fence in markdown does the same with no mount to call (one document-level listener re-renders every fence on screen). An `html` tab's frame still **parses** `app.css` and inherits the whole set rather than carrying a copy — a copy had already lost five tokens once — and still fails closed to a built-in literal, because a widget with no ground at all is worse than one on a stale palette.
  - **An embedder may hand the board a palette**: `{__aboard:'theme', tokens, kind}` from `window.parent`, authenticated by `e.source` exactly like the `active` message going the other way, validated against the same token names, applied as inline custom properties (which outrank both variants) and written NOWHERE — it is the host's opinion, not the human's choice. Pressing the board's own switch clears it, because a human pressing a button and nothing happening is the worse failure. This is the hook the VS Code panel needs.
  - **The token names are parsed out of `app.css`**, not listed in Go: `aboard capabilities` reports them under `theme`, and `tones`/`colors` in the specs are CHECKED against that set rather than duplicating it. Adding a token therefore moves `capsHash`, which is correct — the palette is part of the declared surface. Full detail in [docs/reference/theme.md](docs/reference/theme.md).
- **The journal is the board's undo, and `aboard history` is how you reach it.** Every
  accepted write already records each changed tab AS IT WAS — the whole tab, since
  2026-08-26, stamped `schema: 2` on the entry; `GET /history?tab=`
  and `aboard history <tab>` read it out, and `--at N` prints a WHOLE document `apply`
  accepts. Merged onto a freshly read document rather than printed alone, and that is the
  whole risk of the feature: a single-tab document is a document that DELETES every other
  tab, which the server would answer with a removal request on each one in front of the
  human — the same shape of mistake an absent `__by` used to make. The listing says where
  the record ends, because rotation keeps one generation and an empty list is otherwise
  indistinguishable from "everything about this tab rotated away". The change banner in
  the shell links to it, read-only: restoring from a button would be a write the human
  made without seeing the document it produces. **The record has two generations and
  every reader dispatches per ENTRY, not per file**: rotation keeps one older generation,
  so `journal.jsonl.1` can hold pre-`schema` lines — whose `before` is a bare `state` —
  for as long as the board lives while the live file holds whole tabs. A restore from the
  wide record puts back the tab's CONTENT — name, type, note, stateFrom and key as well
  as state — and NEVER
  `touched`, `pendingRemoval`, `seen` or `requests`: re-raising a dismissed dot, re-opening
  an answered removal request or putting back a note they deleted would be the one command
  whose job is to undo walking around four of the five guarantees.
- **The browser reports what it drew, into a sidecar.** After every mount — and, debounced,
  after a control is pressed — `aboard.html` posts the declared control ids on screen, any
  the renderer built that no spec declares, and any unknown-component marker, to
  `POST /rendered` → `.aboard/run/rendered.json`. `aboard rendered <tab>` prints it, and
  `aboard wait --for "rendered <id>"` blocks until a browser mounts one. **Not the DOM
  sweep** that was measured and abandoned on the spike: every id here is already declared
  in `views/*.spec.json`, and nothing is matched against `gestures`. It prints its own two
  limits, because they are what stop it being read as proof: no receipt means nobody had
  the tab OPEN, and a recorded press means the control was REACHED, never that it behaved.
  Swept on ACTIVATION and not only on mount, because `ui`'s `tabs` component builds only
  the open panel — the gallery's deliberate `sparkline` marker sits in the fifth one and no
  mount-only sweep would ever see it.
- **`aboard uploads` is the accounting for `.aboard/uploads/`**, with `--prune --yes` the
  only way to delete anything. The reference scan reads each tab's RAW state text plus its
  name and note, never its declared fields: an `html` widget's markup can name a file no
  spec knows about, and a declared-field scan would call that image an orphan and offer to
  delete something the human is looking at. `--prune` alone prints and refuses.
- **Per-viewer UI state never goes in the state file** — selection, zoom, collapsed blocks, marks-hidden, chat drafts, mount receipts, each tab's scroll offset (`sessionStorage`, `aboard.scroll.<tab>`), and the chrome a host asks for when it frames the board (`?chrome=full|notabs|none`). Two viewers can look at one board in the same second and must disagree about all of it while agreeing about content, so it lives in the URL — [the shell's URL surface](docs/reference/http-api.md).
- **The board draws its own questions.** `views/dialog.js` is the only confirm/prompt on the board — see the gotcha below for why a native one is not an option, and `docs/how-to/run-in-vscode.md` for the user-facing version. Its buttons go through plain `button()` rather than `controlsFor()`: a dialog's OK and Cancel are chrome belonging to no renderer, which is the case `views/controls.js` documents for the plain helper, and what an agent needs declared is the control that OPENED the dialog.
- **The board can be FRAMED, and says so out loud.** Three things exist for a host that owns the tab strip — a VS Code extension is the first: `?chrome=notabs` suppresses the board's own strip for that viewer; the page posts `{__aboard: 'active', tab: '<id>'}` to its parent whenever the active tab changes, so a sidebar highlight follows `[`, `]` and `1`–`9` pressed inside the board and not only clicks that started outside it; and every `localStorage`/`sessionStorage` access is wrapped, because a third-party frame can be refused storage outright and an unguarded read would take the whole page down rather than lose a remembered scroll position. None of the three is server state. **A fourth arrived on 2026-08-27 and it IS a hook, deliberately and narrowly**: `?chrome=notabs` now hides the whole tab strip including the `+` — it used to leave the button alone on a row of its own, which is a line of a small panel — so a host posts `{__aboard: 'newtab'}` and the board opens its OWN new-tab sheet. Authenticated by `e.source === window.parent` like the theme message, because an `html` tab can reach `window.top` and this one draws a modal. It opens the sheet and stops: the human still names the tab and can still cancel, an embedder cannot create one, and the flow stays on the board because the sheet knows every type and every empty state — a host rebuilding that would hold a copy of the board's schema with nothing to notice when it went stale. **The rule that nothing in the UI starts a session still holds across the frame boundary**; what a host may now do is ask for a dialog the human answers. **A fifth and sixth arrived on 2026-08-28, and together they are one idea: a host says what it can DO, before being asked.** `{__aboard:'host', name, clipboard}` on every frame load becomes `window.ABOARD_HOST` and an `aboard:host` event; `{__aboard:'clipboard-image', id, dataUrl}` out and `{__aboard:'clipboard-result', id, ok, error}` back is the board asking that host to put a PNG on the system clipboard, which a webview genuinely cannot do — Chromium refuses under a permissions policy the host cannot lift, so the extension host runs `xclip`. The announcement is there because the board used to discover all of this by TIMING OUT, and **a timeout cannot tell "nothing framed me" from "an old host" from "a host that broke" — nor any of them from a working host a moment before it succeeds**. One clipboard failure survived three rounds of reinstall-and-restart on that evidence. Each is now its own sentence naming its own hop. Documented in [http-api.md](docs/reference/http-api.md).
- **One resolved root.** Paths are joined in `layout.go` and nowhere else — enforced by `TestNothingOutsideLayoutJoinsAPath`, an AST walk, because the rule had four violations for as long as nothing checked it. The port is derived from the discovered root, so the URL is the same from any subdirectory.
- **Dependencies are cobra + pflag + yaml.v3 and their closure. The JSON one is gone** — as of **2026-08-27** and Go 1.27, `encoding/json/v2` and `encoding/json/jsontext` are STDLIB, so `github.com/go-json-experiment/json` (the Go team's own published mirror, depended on from the port until then) left `go.mod` and nine files swapped an import path. Nothing else moved, because the mirror had always been the same implementation — which is the fact that made the migration cheap and is worth knowing before anyone proposes reverting it. Two things it turned up, both load-bearing. **v1's `json.RawMessage` is now `= jsontext.Value`** (`$GOROOT/src/encoding/json/v2_stream.go`), an ALIAS rather than a second `[]byte` type, so converting between them is a no-op `unconvert` reports — one existed in `codec_test.go` and the comment there records why it went. And **`encoding/json` keeps v1 semantics** even though v1 is now implemented on top of v2, which is what keeps `TestTheWrittenDocumentIsByteIdenticalToTheOldEncoder` a real comparison rather than a tautology: it passed unchanged across the swap, so no board on disk changes shape on its next write. The v2 codec is here for the raw-value paths the board is made of — a tab's `state` is opaque bytes — where it is 3–7× the v1 encoder, and for `jsontext.Value.Canonicalize()`, which replaced an unmarshal-and-re-marshal round trip. `writeOptions` in `pkg/aboard/document.go` pins `Deterministic` and `EscapeForHTML` so the bytes it writes are byte-identical to what `encoding/json` wrote (asserted); do not drop the second without escaping at `htmltab.go`'s `<script>` splice first. No vendor directory; the mermaid bundle is committed at `pkg/aboard/web/lib/` because Go treats `vendor/` specially. **`playwright-go` is TEST-ONLY**: it is reached only from `test/e2e/`, every file of which is behind `//go:build e2e`, so it never enters the binary — `go list -deps ./cmd/aboard` does not mention it and neither does `go version -m ./aboard`. Its module path is `github.com/mxschmitt/playwright-go`, which is what the community fork publishes in its own `go.mod` at these tags.
- **Handoffs are transient, and a finished one is DELETED.** Decided by the human on
  2026-08-27: "the handoffs were transient implementation artifacts; if already
  implemented, delete them." `development/handoffs/` held six of them, all saying DONE or
  SUPERSEDED at the top, and they were still being cited from `CLAUDE.md`, the plans, the
  CHANGELOG and `development/README.md` as though live — which is the actual cost: a
  finished handoff does not go quiet, it goes **stale while still being read**. The rule
  has two halves and only the first is obvious. **Before deleting one, promote what a
  future reader would be WRONG without**: a measurement, a rejected alternative with the
  reason it lost, a judgement call that still stands. Then delete it, and fix every
  reference — `git log` holds the text. The scratch skill (`.claude/skills/handoff/`) is
  KEPT and already writes to gitignored `_output/handoffs/`, which is the right place: what
  was retired is keeping handoffs **in the repository**, not writing them at all.
  Retargeting it at `development/planning/` was rejected for institutionalising exactly the
  mistake. Where the six went is in the commit that removed them; the durable pieces are the
  write-cost measurements in `docs/explanation/how-aboard-runs.md`, the rejected browser
  drivers in `docs/how-to/run-the-browser-suite.md`, the embedding non-goals in
  `docs/reference/http-api.md`, and plan-2 §10's five porting judgement calls.
- **A board is not an artifact, and the comparison is written down** —
  [why a board and not an artifact](docs/explanation/why-a-board-and-not-an-artifact.md).
  Twenty dimensions, re-verified against this repository rather than the spike it was
  written on, plus the seven proposals that died in review. Read it before proposing that
  the board grow sharing, hosting, or a way to act on a click: those are the three things
  the other medium is for, and each has a reason here it cannot have.

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

- **Never a native dialog. Not `alert`, not `confirm`, not `prompt`.** A VS Code webview — and any `<iframe>` whose `sandbox` omits `allow-modals` — SUPPRESSES all three: `confirm()` returns `false`, `prompt()` returns `null`, nothing is drawn, nothing is logged, nothing throws. So a gesture guarded by one works perfectly in a browser tab and is dead in the panel, and the only symptom is the one the human reported on 2026-08-26: "I clicked it but nothing happens". Three of them were dead at once (the removal banner's Remove, the tab-strip rename, the form's reset) and the browser suite was green throughout, because every test in it ran at the top level where `confirm()` works. Ask through **`views/dialog.js`** — `askConfirm(message, opts)` and `askPrompt(label, initial, opts)`, drawn in the page as a `<dialog>`, which the sandbox does not touch. It has no `<form>` in it on purpose: a submit button would need `allow-forms` and would have swapped one silently-swallowed thing for another. `TestNoNativeDialogInTheWebTree` refuses the three calls in `aboard.html` and `views/*.js`, and the e2e session fails any test whose page raises one. **And a `<dialog>` does NOT take the keyboard away from the page, where `window.confirm` did**: `showModal()` makes the rest of the document inert for POINTERS and focus and says nothing about a listener bound to `document`, which is where every one of the shell's hotkeys lives — so without `stopPropagation` on the dialog's own keydown, `]` and `1`–`9` switch the tab behind an unanswered question and `?` stacks the help panel on top of the modal. **This applies to an `html` widget too** — that frame is `sandbox="allow-scripts"`, so the three are dead there as well; the skill says so. The three OLDER in-page modals — the dag's delete-confirm, markup's clear-marks and the shell's New tab sheet — do not stop propagation and so still leak the shell's hotkeys behind themselves. Measured, not missed: left alone deliberately, because a behaviour change to three renderers is not something a fix for native dialogs should smuggle in, and their inputs make `typingNow()` true for most of the keys that matter.
- **A backtick inside a view's injected CSS ends the template literal.** Every renderer injects its stylesheet as `` const CSS = `…` ``, so a CSS comment written as ``/* the `form` rule */`` closes the string and the module stops parsing — which takes the whole shell down, because `aboard.html` imports it. The symptom is not a styling bug: the board never draws its tab strip at all. Same family as the `</script>` trap below, and it cost a build here. `make e2e` catches it in forty seconds (every test fails at "the tab strip never appeared"); `cp pkg/aboard/web/views/<f>.js /tmp/x.mjs && node --check /tmp/x.mjs` parses one view in one, printing nothing and exiting 0 when it is fine — the `.mjs` copy is not fussiness, `node --check` on the `.js` in place accepts the broken file. There is deliberately **no Go test counting backticks**: `views/markdown.js` legitimately holds an odd number of them, so the check would open by calling correct source a mistake, which is how people learn to ignore a checker.
- **The web tree is compiled into the binary.** After editing anything under `pkg/aboard/web/`, rebuild (`make build`) or run `aboard serve --dev`, or your change appears to do nothing.
- **`make caps` builds twice, and neither build is redundant.** `pkg/aboard/web` is embedded, so the first binary emits `views/controls.generated.js` from the current specs and the second embeds the module it just wrote. Drop one and the server serves the previous controls while your spec edit appears to do nothing.
- **`test/` is embedded too**, and it still holds two probe pages (`pkg/aboard/web/test/mermaid-probe.html`, `theme-probe.html` — the latter takes `?theme=light` and reads its token list out of the stylesheet rather than carrying one, so it can show the variant the switch introduced; it had already lost `--accent-dim` and `--danger` from a hand-written list). Editing anything under `pkg/aboard/web/` and re-running a browser check tests the OLD copy, silently. Rebuild first, not after.
- **The browser suite drives the FULL Chromium, and it took a clean cache to notice it had not been.** `runOptions()` sets `NoInstallShell: true` — the suite wants the real browser, not the ~90 MB `chrome-headless-shell` — but the launch named no channel, and a headless launch resolves to the shell unless it does. That contradiction was invisible for as long as `~/.cache/ms-playwright` happened to hold a shell from some other install; on a wiped cache (2026-08-27) the run died before its first test with `Executable doesn't exist at …/chrome-headless-shell`, which reads like a broken machine rather than a broken assumption. `Channel: new("chromium")` in `launchOptions()` is what makes the intent true. **The shell and the full browser are not the same witness**: the very first full-Chromium run found a `403` on `/favicon.ico` that had been in every real browser's console since the first commit, because the shell never asks for one. If a test suddenly reports console errors nobody has seen, ask which browser saw it.
- **Do not run `make e2e` twice in one shell call.** It is ~1 min, so two runs blow a two-minute tool timeout. It needs no server, so there is nothing to start in the foreground and nothing to kill — that class of accident is gone with `test/smoke.sh`.
- **The browser suite cannot touch your board any more.** `make e2e` seeds a temp root, serves it in-process on a free port, and deletes it. The old `make smoke` had to be aimed at a real board with `PROJECT=`, WROTE to it, and poked the notify channel — releasing any session genuinely blocked on `aboard wait`. `test/shot.sh` still takes `PROJECT` and still needs a running server, because it reads the board and writes only pictures.
- **A failing `$(...)` aborts a `set -e` script with no message at all.** `sh` is `dash` here, so `BASE=$(sed … missing-file)` ended the old shell suite instantly and `make` printed nothing but its own error line — it read as "the suite is broken", not "the file is missing". `test/shot.sh` is the one shell script left where this can still bite: every command substitution whose failure is survivable needs `|| true`. Same family as `$(cmd; echo $?)` reading empty.
- **Playwright scrolls a STICKY element into view before clicking it, and that moves the page.** A sticky element's LAYOUT box stays where it was in the document while only its painted position follows the viewport, so `scrollIntoViewIfNeeded` drags the document back to the top of that box even though the element was on screen the whole time. The tab strip is sticky, so every `s.tab(id)` in the browser suite scrolls the page as a side effect. Invisible for every test that came before, and fatal for one about scroll: the offset being remembered on leaving a tab was the driver's 131 where the human had left 600. A person clicking a visible tab scrolls nothing, so the honest fix is to dispatch the click in the page (`switchTab` in `test/e2e/scroll_test.go`) rather than to change the board. Two lessons in one: the harness is part of the measurement, and a green scroll assertion may be green because the BROWSER restored the position rather than because the code did — which is why `history.scrollRestoration` is now `manual`.
- **A headless screenshot needs `?nosse=1`.** The SSE stream never closes, so chromium never reaches network-idle and writes no file at all — exit 2, no message. `test/shot.sh` appends it; a hand-rolled chromium command does not.
- **Headless chromium does not reliably paint iframe content**, so verify an `html` tab by shooting `/tab/<id>/html` directly. `--virtual-time-budget` also starves cross-process `postMessage`, which makes frame auto-sizing look broken when it is fine in a real browser.
- **A browser check that cannot run must FAIL, not skip.** The retired shell suite had ten sections that printed `skip …` and let the run exit 0, so a third of the checks could be absent with nothing to say so. `test/e2e` has no skip path for a missing dependency: `TestMain` installs the driver and fails the run if it cannot, and a fixture that has gone missing is a `t.Fatal` naming what it needed. The one `t.Skip` in the suite names a genuine ambiguity (nowhere empty on the dag canvas to drop on), which is what a skip is for.
- **The whole browser suite shares ONE temporary board**, so run it shuffled now and then: `go test -tags e2e -count=1 -timeout 10m -shuffle=on ./test/e2e`. Declaration order hides order dependencies, and it hid two: a test helper allocating scratch tab ids from a private counter the board's own allocator walked into (two tabs, one id), and a real shell defect where a tab removed in the browser came back on screen if a reload landed inside the save it was awaiting. Allocate ids from the document's `nextId`, and do any agent-side setup BEFORE `open()` — a write issued after the page is up is a foreign change racing the thing under test.
- **`make e2e` writes its evidence twice on a failure** — into the temp board it drove, and into `<repo>/.aboard/run/e2e/<TestName>/`, which is gitignored and survives the run. A trace (`npx playwright show-trace`), a full-page screenshot, the board document, and that page's console. **Look at the screenshot.**
- **A CLI warning can only reach the actor who runs the CLI.** Obvious once said, and it
  is the reason the write path looks the way it does. `apply` printing a warning on stderr
  is invisible to a human working in the browser, and invisible to an agent whose stderr
  nobody read — so for a long time every `ui` mistake was found by the human looking at the
  board, which is backwards: the agent is the one still holding the context to fix it. That
  is why **warnings now travel with the WRITE** rather than with the command (the decision
  above: the journal entry, the POST reply, the SSE frame, the tab's banner and the trace
  tab), and it is also why a `gate` verdict recorded with no reason grew an *"add why"*
  affordance in the UI instead of an `apply` warning — the human records verdicts in the
  browser, where no terminal is listening. **The general form: if the human does it in the
  UI, the affordance belongs in the UI.** A check nearly got built on the wrong side of
  this once; it would have fired only on an agent's write, for a mistake only a human can
  make.
- **A CLI command in a doc is a claim. Run it.** The spike's resume section said `-journal -l 20` when the flag was `-limit`, so the third command a resuming session ran exited 2. Nothing tests the commands in prose; if you write one, execute it once.
- **`apply` succeeding is not evidence that anything renders.** It prints `applied` and exits 0 for a document that draws an empty box — `ui` is the worst offender, because an unknown component shows a marker but an unknown PROP shows nothing at all. Read the stderr warnings, then shoot the tab and **look at the picture**.
- **Screenshots land under the target project's `.aboard/run/shots/`** because a snap-confined chromium cannot write outside `$HOME`. That constraint and `PROJECT=/tmp/...` pull against each other: if `make shot` writes nothing at all against a scratch project under `/tmp`, the confinement is why, and a scratch project under `$HOME` is the way out. `test/shot.sh` exits **1** when no picture was written at all, and clears each shot's previous PNG before taking it — a stale file from an earlier run is indistinguishable from a fresh one, so without that the exit code would still have been lying. A PARTIAL run exits 0 on purpose: one mistyped tab id among five is a typo, not a broken environment.

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

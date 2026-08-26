# Plan 2 — the finish line: everything owed, in order

**Status: COMPLETE. Written 2026-08-25 at `8fedd67`; execution started 2026-08-25 (briefs at
`28252bb`) and finished 2026-08-26. All nine items are done, plus 10a (the make targets are the
gate) and 10b (the human's answers to four §10 questions) — the hash beside each is the commit
that landed it; item 8 landed in the `aboard_vscode` repo (`6711c15` + `ca72ca6` there), not this
one. §10 is the only list still open, it is four entries shorter since the human answered
those four on 2026-08-26 (their dispositions are §10c), and every entry left in it is a question for
the human rather than work to pick up. A session resuming after a context clear reads this line
and knows there is no queue: ask the human what is next.**

This is the master list. Every item points at the document that holds its detail; this file
holds the ORDER, the scope boundary of each item, and what "done" means. Decided with the human
on 2026-08-25, including the order. Plan 1 (`plan-1_port-from-spike.md`) is complete and is the
record of how the repo came to be; its 16 decisions still bind.

## Goal

`aboard` becomes a tool a stranger can trust: its two known races are gone and the write path is
provably serialised; a real browser suite exercises every gesture, the sandboxed widget bridge and
the multi-writer paths, locally and without a human; the JSON path scales with the edit rather
than the document; the eleven reviewed features exist; the VS Code panel's server-side
prerequisites are in; and the VS Code extension is implemented as code in its own repo — built,
not yet installed or run. At the end, the queue in `development/handoffs/` is empty except for
what is explicitly gated on the human (§10).

## Resume protocol

```sh
cd /home/diegos/_dev/exoport/aboard
git log --oneline | head -5          # where the tree is
cat development/planning/plan-2_finish-line.md | head -20   # this Status line
make build && ./aboard status         # is a board running here? (.aboard/ is gitignored, local only)
```

Then read `CLAUDE.md` (the two hard rules, the closed decisions), the handoff for the next
unticked item, and only then start. The spike at `/home/diegos/_dev/ai/board` is read-only
history now; a board may still be running there on 46624 — never write to it.

## Working method (the same one that built the repo)

- **One item = one workflow**: an implementer with a written brief → an independent reviewer that
  re-runs the ladder, refutes, fixes and reports → the orchestrator commits. Briefs go in
  `development/planning/plan-2-briefs/` so they survive a context clear (the session scratchpad
  does not). Agents never commit; the orchestrator does, in the repo's message style (the claim
  as subject, the reasoning and the mistakes as body, `Co-Authored-By: Claude Fable 5
  <noreply@anthropic.com>`).
- **The ladder, every time**: `go build/vet ./... && go test -race ./...`; `make fmt-check`
  (the pinned gofumpt — never a bare `gofumpt -l .`, see item 10);
  `make lint` at zero; `make caps` when a spec or command changes (builds twice; `aboard
  capabilities --check` = 0; `capsHash` moves are stated in the commit); `make docs-cli` when the
  tree changes; `make docs-check`; `make smoke` (until item 4 retires it) then `make e2e`, once per
  tool call, server started DETACHED; screenshots LOOKED at; `make ci-local` before a commit that
  touches tooling.
- **A behaviour change updates the docs in the same commit** — CLAUDE.md, the skill, `docs/`,
  and the http-api/cli references. A CLI command written in a doc is executed once.
- **Judgement calls are recorded, not silently taken**: in the commit body and, if they need the
  human, in §10 of this file.
- **Never**: push (a remote exists on both repos, but nothing has been pushed to either and
  pushing waits for the human — §10), touch the spike, run two smoke/e2e runs in one tool call,
  or restart a healthy server.

## The order

### 1. Review fixes A — the two races  ☑ `63d9efd` (plus `PROJECT=` for the browser suite, the commit before it)

Source: `development/research/review-d6c2f84-20260825.md` (High). Scope: `postState` gets a
mutex from `ReadFile` through `writeAtomic`; `fanout`/`notifyWatchers` stop sending on channels
the reader closes (delete from the map without closing, or send under the lock); `watch`/`watchUI`
recover. Tests: N concurrent POSTs off one base → exactly one 200 and the journal agrees; an SSE
client disconnecting mid-broadcast under `-race -count=20`.
**Done when** both reproductions from the review fail to reproduce and the tests exist.

### 2. Review fixes B — behaviour mediums  ☑ `d432a90`

Source: the review's Medium list, behaviour half (13 items): `apply` with no `updatedAt` refuses
without `--force`; the CAS token becomes a monotonic revision (or the content hash) instead of a
millisecond timestamp; an agent write carries `pendingRemoval` forward like `touched`;
`POST /aboard.json` refuses a foreign `Origin`/`Sec-Fetch-Site`; a `Host` allow-list
(`localhost`, `127.0.0.1`, `::1`) at the top of `route`; `--port`/`PORT` no longer bypasses
duplicate detection; `FindRoot` resolves symlinks; `--name`/`ABOARD_NAME` validated
(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`); `journal` falls back to disk on transport failure too;
`init` validates `--output-format` before writing; cobra arg-count errors exit 2; yaml output
carries paired tags (and `recipes list` in yaml keeps scope/path/shadowedBy/error); in the browser,
an SSE reload routes through `mergeOntoFresh()` and re-arms the debounce, and `baseline` advances
after a 409 merge. Each with a test that fails before and passes after; the two browser items get
their real tests in item 4 and a DOM-level probe now.
**Done when** every item has its test, `docs/reference/http-api.md` and the skill describe the new
behaviour (revision token, origin refusal, `--force`), and the review file's Medium entries are
struck through with the commit hash.

### 3. Review fixes C — coverage, docs, lows  ☑ `2b701a8`

Source: the review's Medium (coverage/docs) and Low lists. Scope: the parity test walks
subcommands (a `Subcommands` field); `reconcileNextID` table tests (the JSON item refactors it);
one `writeWarnings` test per detector class plus the two negatives; `make smoke: build` and a
non-zero exit on any skipped section; `smoke.html` render counts compared, not logged; `bb71` in
the interaction fixture has cards (item 4 owns the fixture — coordinate); the three docs that claim
`go install` reports `dev` corrected; journal rotation makes `journal.jsonl.1` readable by `tail`;
`writeAtomic` keeps 0644; recipe discovery survives one unreadable file; the two `log.Printf` go
through `Options.Logger`; Go tests for base-path injection and the wait-predicate vocabulary; a
drift gate for the recipe index in `make caps`; the four false doc claims fixed; the 18 unverified
lows triaged (fixed, refuted with a line in the review file, or queued).
**Done when** the review file has a disposition beside every finding and `make ci-local` is green.

### 4. The end-to-end browser suite  ☑ `c3db7a7`

Source: `development/handoffs/handoff-e2e-browser-suite.md`. Scope, in this order: the harness
(`test/e2e/`, `//go:build e2e`, playwright-go pinned, engine in-process on a temp root, the
interaction fixture, `control()`/`tab()`/`drag()`/dialog helpers, trace + screenshot + state
snapshot on failure into `.aboard/run/e2e/`); the first tests — the bridge write half in a tab and
in a stack block, the debounce-vs-SSE reload, the double 409, `touched` Dismiss, Keep/Remove,
`prompt()` rename, `confirm()` remove; then the gesture surface renderer by renderer (kanban drag
with cards present, read-only kanban, dag reparent/pan/zoom/`<dialog>`/dblclick, markup tools and
modals, table sort and type-and-save, gate allow/reason/undo, ui intent, notify releasing a real
`aboard wait`, SSE without reload, `--dev` CSS re-link, UI-signature reload); `make e2e` with
`E2E_HEADED`/`E2E_TRACE`; the three browser-free static checks ported to Go; `test/smoke.sh` and
the `node` dependency retired once every one of its checks has an `e2e` equivalent;
`docs/how-to/run-the-browser-suite.md`; the DevTools MCP named in the skill as the exploratory
complement. Visual regression is NOT in this item.
**Done when** `make e2e` runs green from a clean checkout with no human present, every renderer's
declared gestures have at least one test, the "provable only by a human click" sentence is deleted
from every document, and `smoke.sh` is gone.

### 5. JSON hot paths  ☑ `da96af0`

Source: `development/handoffs/handoff-json-hot-paths.md`. Scope, in its own decided order: the
benchmark harness with the "before" numbers recorded in that file; (2) parse once and keep the
document in memory, compare by hash, `reconcileNextID` walks only changed tabs, watcher gated on
mtime+size, `GET` from cache with ETag/304, the browser's baseline clone replaced, `maxBodyBytes`
raised deliberately; (1) `encoding/json/v2` via `go-json-experiment/json` (or Go 1.27 if ape moves)
with `jsontext.Value` and the stricter-defaults test pass. (3) per-tab resources is NOT built.
**Done when** the "Measured" table in the handoff has before/after rows and the acceptance line of
every structural item holds under the benchmark, with `make e2e` still green.

### 6. The eleven reviewed features  ☑ `d69197a` (items 1–10 built in three parallel worktrees and squash-merged; `bb372` DROPPED by the human on 2026-08-26 and REVERSED the same day — built as a `/proc` scan, §10c)

Source: `development/handoffs/handoff-13-features.md`. Scope: items 1–10 as specified
(`bb361` warnings travel with the write; `bb362` `apply --check`/`--strict`; `bb363` per-tab
history and restore; `bb364` html tabs read the real palette; `bb365` mermaid fences in markdown;
`bb366` `apply` merges instead of failing; `bb367` `export` renders a `ui` tree; `bb368` mount
receipts from the browser; `bb369` uploads accounting and prune; `bb371` write labels in the
journal), each with Go tests and an `e2e` test where it has a browser half. Item 11 (`bb372`
`boards`) was **gated on the human** (§10). Their first answer, on 2026-08-26, was to DROP it;
they REVERSED that the same day with a design, and it is built — see §10c. The
`~/.aboard/known-roots.json` registry was a proposal and is still a proposal nobody will take
up: what shipped is the process-table scan, with no file anywhere.
**Done when** items 1–10 are shipped with their handoff sections marked done and `capsHash`
regenerated, and item 11 is either built to the human's answer or explicitly closed.

### 7. Panel prerequisites on the aboard side  ☑ `913eef6` (built in a worktree in parallel with item 5, rebased onto it)

Source: `development/handoffs/handoff-board-for-vscode-panel.md` §4–§6, in its stated order:
`?chrome=` suppresses the board's own tab strip per viewer (a URL parameter, never server state);
the page announces the active tab to its embedder (`{__aboard:'active', …}` postMessage, with the
nonce the handoff describes); `localStorage`/`sessionStorage` reads wrapped so a third-party frame
cannot take the page down. Each verified per the handoff's Verification subsections, plus `e2e`
tests (the active-tab message is observable from the harness).
**Done when** the extension's M2 and M4 no longer have a "if it has not landed" clause.

### 8. The VS Code extension — implemented, not installed  ☑ aboard_vscode `6711c15` + `ca72ca6` + `08c127c` (built out of order, in parallel with item 3, because it is a separate repo; a reconcile against items 7's landed contract happens in item 7/9)

Source: `/home/diegos/_dev/exoport/aboard_vscode/docs/handoff.md` (§5 layout, §6 the two moving
parts, §7 the tree, §8 milestones M1–M5). Scope: the repo scaffold exactly as §5 (`package.json`
with contributes/activation/commands/menus, `tsconfig.json`, `esbuild.mjs`, `.vscodeignore`,
`src/{extension,board,tree,panel}.ts`, `media/{panel.html,dot-change.svg,dot-removal.svg}`, no
runtime dependencies); discovery by walking up from each workspace folder to `.aboard/run/
instance.json` and verifying `/health` (both identities, `project` check); the panel with one
iframe and the ~20-line bridge; the `TreeDataProvider`; the `goto` nonce bridge; SSE with
reconnect/backoff and tree refresh on `origin` frames; dots/tooltips/badge from `/capabilities`
and the `active` message; dismiss/removal/rename/note/notify actions through `POST /aboard.json`
with a `409` retry; the "start the board" fallback offering `aboard serve` or `ape aboard serve`
depending on what is on PATH; errors as notifications. Unit tests for the pure parts (`board.ts`
discovery and `/health` acceptance, tree mapping, message parsing) under a Node test runner.
**Explicitly NOT in this item: M6.** No `.vsix`, no `code --install-extension`, no launching VS
Code, no hand-verification checklist (§11), no marketplace ladder (§9). Build (`npm run build` →
`dist/extension.js`) and `npm test` must pass; that is the whole proof for now.
**Done when** the aboard_vscode repo builds and tests green from a clean clone, M1–M5 are
implemented as code with §10's hardening cases handled where they are pure logic, and the README
says plainly that it is unverified in a real VS Code.

### 9. Close the books  ☑ `8a0b923`

Scope: CLAUDE.md's status and the skill reflect everything above; `development/README.md`'s order
list is replaced by "see plan-2 — complete"; each handoff's Status line says done; the review file
is fully dispositioned; `CHANGELOG.md` Unreleased carries one line per item; `make ci-local`
green; the memory note for this project updated.
**Done when** a fresh session reading `development/` finds nothing open except §10.

Three real fixes came with the bookkeeping rather than being recorded as more debt, because each
was one line and each made a green ladder lie: `make help` never listed `e2e` (its grep was
anchored on `^[a-zA-Z_-]+:`, and `2` is not in that class) while CLAUDE.md called it the list of
available targets; `test/shot.sh` exited 0 when every screenshot FAILED; and the browser suite's
baseline test asserted two wall-clock millisecond comparisons measured inside a shared machine's
Chromium, which flaked once and would again.

### 10a. The make targets are the gate  ☑ `b1dc79c` (added 2026-08-26 on the human's instruction: pinned tools updated, the pre-commit hook and CI run `make lint`/`make fmt-check`)

### 10b. The human's answers to four §10 questions  ☑ `5999eaa`

### 10c-bis. `aboard boards` over /proc, and gomodguard_v2  ☑ `2bd572c` (the human reversed the drop the same day with a design: Linux only, honest elsewhere, no registry)

### 10d. The user-facing docs audit and gap fill  ☑ `d0846a2`

### 10e. A named board owns its runtime files; one live board per project however bound  ☑ `2f37497`

### 10f. The extension after its first real run (aboard_vscode)  ☑ `fc886c1` (the human ran M6 once on 2026-08-26: the dot SVGs were malformed XML, the framed board was a pre-`?chrome=` binary; both fixed, plus a backoff that never backed off)

### 10g. The board never calls a native dialog  ☑ `2875113` (a VS Code webview swallows confirm/prompt/alert; found by the human clicking Remove in the panel)

### 10h. The extension after the human's checklist  ☑ aboard_vscode `edadda6` (10 of 12 §11 rows observed passing on 2026-08-26; the bell now lights from an `aboard.waiting` context key; Copy Reference and Copy Link are two commands)

### 10i. The human's notes to the agent (guarantee 5) and per-tab scroll memory  ☑ `1566bbb` (`tab.requests`, `aboard requests`, `wait --for request`, capsHash → `34af0bc9`)

### 10j. Two recipes: the decision wizard, reviewed, and a human checklist  ☑ `bedc408` (built in a worktree, rebased onto 10i)

### 10. Gated on the human — do not start without an answer

- **Remote, first tag, first release**: `origin` exists on BOTH repos already
  (`git@github.diegos_exo:exoport/aboard.git` and `…/aboard_vscode.git`) and **nothing has been
  pushed to either**. What is still the human's: pushing at all — which waits on their own manual
  test of both repos and their review of this section — the org's Actions OIDC/`id-token: write`
  permission for keyless cosign, and `v0.1.0`. The release pipeline is untested against a real tag
  until that happens; `make snapshot` is the local proof.
- **Go 1.27 for the `json/v2` item** — only if `apex_process_ape` moves too (one toolchain for the
  `ape aboard` mount). It now gates a second thing: **goreleaser v2.18.0 requires Go 1.27**, so
  the pin stops at v2.17.1 until this is answered (item 10, 2026-08-26).
- **The `ape aboard` mount itself** — out of scope for this plan by the human's word ("in the
  future"); when it comes it is a plan in the ape repo: `require github.com/exoport/aboard` at a
  real tag, `root.AddCommand(cli.NewRootCmd(cli.Options{Host: aboard.HostApe}))`, and the two latent
  hosted-mode findings from the review (invocation strings that say `aboard`, `version`/`/health`
  reporting the host's commit) become real then.
- **Installing and testing the extension (M6)** — the human's call on when; item 8 stops at a
  green build.
- The judgement calls listed in `handoff-phase-e-finish.md` (`make dist` dropped, `restart.sh`
  kept, NOTICE in archives, the `vuln` job, hidden commands outside the declared table) stand
  until the human says otherwise.
- **`aboard <cmd>` is hardcoded in user-facing prose** — 13 places by item 6's reviewer's count,
  four of them added by item 6 itself, and more again if help text and the generated headers are
  counted. This is the latent hosted-mode finding above, now measured: under `ape aboard` every one
  of those sentences names a command the user does not have. Wiring `Options.Argv0` (or equivalent)
  through the messages is a whole-repo change and belongs WITH the ape mount, not before it —
  recorded here so nobody rediscovers it one string at a time, and so the ape plan starts with a
  number rather than a survey.

### 10c. Answered on 2026-08-26 — CLOSED, not deferred

Four §10 entries were questions and now have answers. They are struck from the list above and
recorded here with the reason, so nobody re-derives them from the shape of the code.

- **`bb372` `boards` — DROPPED, then REVERSED and BUILT, both on 2026-08-26.** The first answer
  was to drop it: every project already answers "is a board running here" with `aboard status`,
  and its `.aboard/run/instance.json` and `GET /health` already say WHICH binary serves it, so a
  machine-wide list buys only cross-project discovery. The human reversed that the same day, with
  the design attached — **implement it as a `/proc` scan, run it only where `/proc` exists, give a
  proper message elsewhere, and print a summary of every running board including the FULL project
  path.** That is what shipped, in `pkg/aboard/boards.go` and `boards_linux.go`.

  What the reversal KEPT from the refusal, and it is the half easiest to lose: **no registry
  file.** `~/.aboard/known-roots.json` — written on serve, verified on read — stays rejected. A
  process either exists or it does not, so the process table needs nothing written at startup and
  nothing cleaned up after a crash, which is the one thing a registry cannot promise.

  What the reversal OVERTURNED is the platform objection, by answering it rather than arguing
  with it. `/proc` is Linux-only and `aboard` does ship for macOS and Windows — so the scanner
  sits behind `//go:build linux`, and everywhere else the command still exists, is still declared
  in the manifest, and exits 2 with one line naming the platform and pointing at `aboard status`
  inside each project. A command missing on two of three platforms is worse than one that is
  present and honest.

  The `comm` note from the earlier entry survives as a design CONSTRAINT rather than an
  objection: the scan reads `/proc/<pid>/cmdline` and never `comm`, because `comm` is 15
  characters and under `ape aboard serve` it is the HOST's name.

  Disposition: `handoff-13-features.md` §11 is DONE; `CLAUDE.md`'s decision bullet carries the
  design as it now stands; `docs/reference/layout.md` and the skill's multi-session reference
  document the command and, beside it, what two boards in one project SHARE.

- **The example board's prose says "the agent".** All seven strings in
  `pkg/aboard/example/aboard.json` renamed; every id and every other byte kept, and the fixture's
  formatting is unchanged (`TestTheWrittenDocumentIsByteIdenticalToTheOldEncoder` reads it).
  `views/chat.js` still matches `claude` as a historical ACTOR name, and code comments naming
  Claude are untouched — this was a decision about the demo content the human sees on every
  `aboard init --example`, not about the repo's vocabulary.
- **The notify button's acknowledgement is a toast.** Option (b): the button keeps telling the
  truth about live state and repaints from the SSE `waiters` frame exactly as before, and the
  acknowledgement of the press moves to a transient notice the repaint cannot reach — the same
  `flashSaved` mechanism the inline editors use. "notified N sessions" / "no session was waiting",
  from the `/poke` reply. The rejected alternative was suppressing `refreshWaiters` for ~1.5 s,
  which would have made the button lie about live state for its duration.
- **`JournalEntry.Before` carries the whole tab.** Option (a): entries gain a `schema` integer,
  stamped `2`, and `Before` becomes the tab — `id`, `name`, `type`, `note`, `stateFrom`, `state`,
  `key`, `touched`, `pendingRemoval`, `seen` — instead of a bare `state`. Every reader handles both
  generations, because a rotated `journal.jsonl.1` can hold `schema`-less entries while the live
  file holds v2 ones. `apply`'s 409 merge can now classify a foreign rename/note/type change and
  merges it; a same-tab rename on both sides is still named as a collision and still stops.

# CHANGELOG

## Unreleased

Everything below closes `development/planning/plan-2_finish-line.md`, which is complete.
The entries are one per user-visible change, so the larger plan items appear as several
lines and the ones with no user-visible surface — the suite, the extension — appear as one.

- **feat: the human can leave notes for an agent on a tab, and an agent ticks them off.**
  A tab's `note` and "what the human wants done about it" were one field, and the merge
  was lossy both ways: a purpose rewritten into a to-do stops being a purpose, and a
  to-do living in the purpose strip has nowhere to record that it was dealt with. So the
  strip above a tab is now the AGENT's brief statement of what the tab is for (the human
  may still edit it), and a second strip under it — **notes for the agent** — carries
  `tab.requests`: `{ id, at, by, text, done? }`, ids from the board's own allocator. They
  add one with Enter, delete any with a ✕ and no confirmation (it is their own sentence),
  and see it struck through with `✓ agent-1 · 14:02 · redrew the arrow` once a session
  answers. A tab with outstanding notes carries a count on its button — the same
  discipline as the unread dot, a numeral rather than a dot because unlike `touched`
  there can be several.
  **Guarantee 5, server-enforced** (`tabs.go`, and the explanation page is now
  *why-the-guarantees-are-server-enforced*): an agent write may only ADD a `done` stamp
  to a request that already exists, under its own name — creating, editing, reordering,
  deleting or un-stamping one is undone and the previous list restored, exactly as
  `touched` is. The case that matters is not malice: it is the read-modify-write that
  hands the whole document back without a field nobody looked at, which is how `touched`
  used to be lost. The `by` on a stamp is rewritten to whoever actually wrote it and a
  missing `at` is stamped, so the attribution the human reads is never one nobody
  checked. **`aboard requests`** lists what is pending, oldest first, naming the tab;
  **`aboard requests done <id> --by agent-1 --note "…"`** is the stamp (a thin `apply`,
  idempotent, refusing `--by human`); **`aboard status`** prints the count, because it is
  the first command a resuming session runs and a request nobody discovers is a request
  that was not made; and **`aboard wait --for "request [<tab>]"`** blocks until one
  arrives — answering AT ONCE if one is already waiting, since a note left an hour ago is
  a fact about the document rather than an event still to come. Either way that wait
  answers `{"event": "request"}`, where every other predicate answers `change`: `change` is
  true for the others because a write is the only thing that can satisfy one, and a request
  can be satisfied by a write *or* by a note that was already there — so the field a caller
  branches on must not depend on the human's timing. Two smaller things had to
  move with it: the id allocator now counts a request's id, which is the only id outside a
  tab's `state` and would otherwise have let `nextId` hand out an id that already named
  one; and the 409 merge compares the request list, because `requests done` is a write
  whose entire content is a change to it and the merge would otherwise have taken the
  board's copy and reported "applied (merged)" having stamped nothing. `capsHash` moves
  to `34af0bc9`.
- **feat: the board remembers where you were on each tab.** Every tab shares one
  scrolling document — views are shown and hidden, they do not scroll independently — so
  leaving a long list half way down, glancing at another tab and coming back landed you
  at the top with no record of where you had got to. Each tab's offset is now saved when
  you leave it and while you scroll (debounced), and put back when you come back to it,
  under `aboard.scroll.<tab>` in `sessionStorage`: **per viewer**, so it never goes near
  the state file, and this sitting only, so it survives the board reloading its own code
  and does not still point half way down a tab next week. A tab you have never opened
  starts at the top rather than wherever the last one left the page. The restore waits
  for the page to GROW rather than firing once: a scroll past the current bottom is
  clamped silently, and a renderer that lays out asynchronously — a `diagram` waiting on
  mermaid — is shorter at the moment it mounts than it is a moment later, which put a
  reloaded 60-node graph back at 0 instead of 500. A `ResizeObserver` watches for the
  actual event instead of guessing at a delay, and stops the moment the position is
  reached, the human scrolls, or they switch tabs again. The browser's own
  `history.scrollRestoration` is switched off, because it restores the DOCUMENT on
  whichever tab comes up first — right only by coincidence, and wrong the moment a
  `#tab=` link opens a different one.
- **fix: the board never calls a native dialog, so its questions survive a webview.**
  A VS Code webview — and any `<iframe>` whose `sandbox` omits `allow-modals` —
  SUPPRESSES `window.alert`/`confirm`/`prompt`: `confirm()` returns `false`, `prompt()`
  returns `null`, nothing is drawn, nothing is logged and nothing throws. Three gestures
  were therefore dead inside the extension's panel while working perfectly in a browser
  tab: answering a removal request with **Remove tab**, renaming a tab by double-clicking
  it, and a form's **Reset answers**. The human's report was the whole of the symptom —
  *"I clicked it but nothing happens"*. All three now ask through `views/dialog.js`,
  which draws the question in the page as a `<dialog>` (unaffected by `allow-modals`,
  being an element rather than a call into the host), with Enter to confirm, Escape to
  dismiss, focus trapped while it is up and returned to whatever opened it. It also takes
  the KEYBOARD while it is up, which a `<dialog>` does not do by itself and
  `window.confirm` did: without that, `]` and `1`–`9` still reached the shell and switched
  the tab behind an unanswered question. The removal
  question keeps its paragraphs, which a native `confirm` ran together. Deliberately
  **no `<form>` in it**: the obvious shape gets Enter for free but needs `allow-forms`,
  and swapping one silently-swallowed thing for another was the mistake being fixed.
  Two checks keep it: a Go source test refuses `confirm(`/`prompt(`/`alert(` anywhere in
  the web tree, and the browser suite fails any test whose page raises a native dialog at
  all — plus one test that removes a tab from inside a frame sandboxed
  `allow-scripts allow-same-origin allow-forms`, which fails against the old code for
  the same reason the panel did.
- **fix: a second board for one project is refused however its port was chosen.** The
  duplicate check was anchored to the PORT, so `aboard serve --port <anything free>` (or
  `PORT=`) had no occupant to recognise: it started a SECOND server on the same state
  file, rewrote `run/instance.json` to point at itself — every client command then
  followed the newcomer — and on exit removed the record, leaving `aboard status`
  reporting no board while the original served on. Two write locks that could not see
  each other. Before it binds anything, `serve` now reads this project's instance record
  for this name and asks the process it names over `/health`; a board that answers as
  this project's own is named in the refusal with its URL and pid, whatever port was
  requested. A record nothing answers is a killed board: it is overwritten, not obeyed.
  The per-port probe stays in the derived walk for the mirror case — a live board whose
  record was deleted underneath it.
- **fix: a named board owns its journal, its mount receipts and its sidecar logs.**
  `--name` qualified the state file and the instance record and nothing else, so two
  boards in one project wrote into `run/journal.jsonl`, `run/rendered.json` and
  `run/logs/<tab>.log` together — and every one of those is keyed by TAB ID while tab ids
  are allocated per BOARD, so both documents have a `bb1`. The journal held two entries
  naming `bb1` and meaning different tabs, and `aboard history bb1 --name review` would
  offer the DEFAULT board's version as a document to restore. Each board now writes
  `journal.<name>.jsonl` (`.1` rotation included), `rendered.<name>.json` and
  `logs/<name>/<tab>.log`; `journal`, `watch`, `history`, `rendered` and `log` each read
  their own board's. The default board's paths are unchanged, byte for byte.
  **No migration is possible** for entries already mixed into `journal.jsonl`: the record
  does not say which document a write went to, and guessing from a tab id is the
  ambiguity being removed. Those entries stay readable and count as the default board's.
- **fix: `aboard history`'s restore line carries `--name`.** The listing ends with
  `aboard history <tab> --at N | aboard apply --by agent-1`, and copied off a named
  board's listing that read the default board's journal and wrote the default board's
  document. Both halves now carry `--name <board>` when the board is named; the default
  board's line is unchanged. `GET /history` gained a `board` field for the same reason,
  and the **change banner in the browser** — which prints the same pipeline, and is where
  a human actually meets it — splices the same flag from that field. Two places print
  that command; fixing one of them would have left the other quietly wrong.
- **fix: `aboard uploads` accounts for every board in the project.** `.aboard/uploads/` is
  shared by all of them — an image is content the human pasted, and either board may show
  it — but the reference scan read ONE board's tabs, so an image used only by the review
  board came back "no tab mentions it" from the default board and `--prune --yes` deleted
  a picture somebody was looking at. The scan now reads every `aboard.json` and
  `aboard.<name>.json` in the project, prints a named board's tab id as `review:bb1`, and
  says which documents it read. `--name` deliberately does not narrow it. A board
  document that will not parse is now a hard error rather than a skipped file: a board
  whose references cannot be read might be referencing anything, and the next thing the
  caller does with the report may be a deletion.
- **feat: a journal entry records the whole tab, so `apply`'s merge survives a foreign
  rename.** `JournalEntry.Before` held a tab's `state` and nothing else, so a tab
  RENAMED on the board while an agent wrote to a different tab could not be classified
  at all — the merge compared our copy's name against the live one, found a difference
  neither side of the write had made, and refused. Entries now carry `schema: 2` and
  record the tab: id, name, type, note, stateFrom, state and its markers. That case
  merges; both sides renaming one tab is still a collision, and is now named with the
  FIELD as well as the tab. `aboard history --at N` restores the name, note and type
  along with the state (never `touched`, `pendingRemoval` or `seen` — putting back a
  dismissed dot is not an undo), and the listing shows a rename as `old → new`. **Every
  reader handles both generations, per entry rather than per file**, because rotation
  keeps one older generation and a `journal.jsonl.1` full of the narrow shape outlives
  the change. One thing fixed on the way: a tab that existed with an EMPTY state was not
  recorded at all, which made it indistinguishable from a tab being created — the one
  distinction the merge reads `before`'s presence to make.
- **fix: the notify button's acknowledgement is no longer repainted away.** Pressing it
  wrote "notified 1 session" into the button's own label, and the poke destroyed it:
  releasing the waiter changes the waiter count, which broadcasts on the SSE stream,
  which repaints the button. The confirmation moved to a transient flash beside it — the
  same one an inline editor shows after a save — where nothing that repaints the button
  can reach it, and the button goes on reporting live state and only live state. "no
  session was waiting" for a poke that released nobody, which is reachable when the last
  waiter times out between the repaint and the click.
- **docs: the example board's prose says "the agent", not "Claude".** Seven strings in
  `aboard init --example`'s demo content — two dag node titles, two node notes, a form
  intro, an image caption and a region note. Every id and every other byte unchanged.
  `views/chat.js` still recognises `claude` as a historical ACTOR name in a transcript.
- **feat: `aboard boards` lists every board running on this machine.** The cross-project
  half of `aboard status`, and the one command that needs no project of its own: it walks
  the PROCESS TABLE — `/proc`, for an `aboard serve` or an `ape aboard serve` — resolves
  each one's root (honouring a `--cwd` in the argv), and then does per project exactly
  what `status` does: read the root's `instance*.json`, keep the record whose pid matches,
  verify it over `/health`. One row per (project, name), sorted, with the FULL project
  path — the reader of a machine-wide listing is by definition not standing in the project
  it names — plus app, url, port, pid, started, version, tab count and who wrote last.
  `--output-format json|yaml`.
  **There is no registry file, and that is the design**: a process either exists or it does
  not, so nothing is written on startup and nothing needs cleaning up after a crash.
  It reads `cmdline` and never `comm`, which is 15 characters and, under `ape aboard
  serve`, is the host's name — the reason the original design was thought unable to see a
  hosted board.
  **`/proc` is Linux only, and the command says so rather than disappearing**: elsewhere it
  exits 2 with one line naming the platform and pointing at `aboard status` inside each
  project. Two things it refuses to hide: a record whose process has gone is listed as
  "recorded but not answering" rather than dropped, and the listing prints how many
  processes it inspected and how many it could not (another user's board), because "no
  board found" after 3 processes and after 400 are different answers. A third row state
  the design did not originally name: a serve process that no instance record identifies —
  caught in the moments before it writes one, or started with `--state` — is listed with
  its pid and its project, keyed on that pid so two of them in one project stay two rows,
  and labelled `[board not identified]` rather than `[default]`, which is a name it has
  not got.
  Documented beside it, because it is the reason `boards` has one row per (project, name):
  **`--name` qualifies the state file and the instance record and nothing else.** The
  journal, the sidecar logs, the mount receipts, `uploads/` and `recipes/` are per PROJECT,
  so `aboard journal` and `aboard history` in a named board show the other board's entries
  too, and tab ids are per board — a `bb12` in the journal may belong to either.
- **chore: `make lint` is silent again.** `.golangci.yaml` disables `gomodguard` so that
  `gomodguard_v2`, which `default: all` already enables, is the only one running; v2.13.1
  printed a deprecation warning on every run while both were on. The opposite of the
  `exhaustruct`/`exhaustruct_v5` and `wsl`/`wsl_v5` pairs beside it, which name both
  versions because this repo decided against those linters. Neither gomodguard has any
  settings here, so nothing about what is allowed changed.
- **chore: the make targets are the gate, and the pinned tools moved.** A `$PATH` copy
  of a tool and the `.bingo` pin are two different programs, and this repo ran both: the
  pinned linter reported 0 where the pre-commit hook's `$PATH` copy reported 11, and the
  pinned formatter rewrote a file the `$PATH` copy called clean — two gates that could
  each be green while the other was red over a tree that never changed. `bingo get`
  moved golangci-lint to v2.13.1, gofumpt to v0.11.0, govulncheck to v1.7.0 and
  goreleaser to v2.17.1; the pre-commit hooks and CI now run `make lint` and the new
  `make fmt-check` instead of calling a tool themselves, and the ladder's rung is
  `make fmt-check`. The newer linter's ~110 findings are fixed in code — the repeated
  wire keys are named in `pkg/aboard/wire.go` — with one config line, `exhaustruct_v5`
  added beside the `exhaustruct` this repo already disabled. `Root.LogFile` now validates
  the tab id itself rather than taking it on trust from the caller.
- **fix: the write path is serialised, and a `409` means nothing of yours landed.**
  Two overlapping POSTs both passed compare-and-set and both wrote, so one edit was
  destroyed with a `200` and the journal recorded the lost write as though it had
  landed (40/40 barrier-synchronised trials). One lock now spans read → compare-and-set
  → reconcile → write; the losers are refused, not queued on top of each other. And the
  SSE fanout no longer sends on a channel its reader has closed — a client disconnecting
  inside that window killed the whole process, leaving a stale instance record behind.
- **perf: a write costs the edit, not the board.** The server keeps the state document
  parsed in memory and a POST decodes the incoming body exactly once and the document on
  disk not at all; unchanged tabs are compared as bytes and carry their derived facts
  forward. One small edit on a board of 5 000 tabs no longer canonicalises 5 000 states
  twice and walks the whole document for ids twice. `GET /aboard.json` is served from that
  cache with an `ETag`, the file watcher is stat-gated, and the JSON codec is
  `encoding/json/v2` (via the Go team's published mirror) for the raw-value paths a board
  is made of. Every claim has a counting test; the numbers are in
  `development/handoffs/handoff-json-hot-paths.md`.
- **feat: the board can be framed by an editor.** `?chrome=full|notabs|none` hides the
  board's own tab strip for one viewer — a URL parameter, never server state, because two
  viewers must be able to disagree about chrome while agreeing about content — the page
  announces its active tab to its embedder (`{__aboard:'active', tab}`), and every
  `localStorage`/`sessionStorage` read is wrapped, so a host that refuses storage to a
  third-party frame can no longer take the page down.
- **test: a real browser suite — `make e2e`.** playwright-go behind `//go:build e2e`,
  seeding its own temporary board and serving the engine in-process, so it needs no server,
  no `PROJECT` and no human, and it cannot touch anybody's board. It clicks, drags, wheels,
  right-clicks and types through every renderer's declared gestures, reaches inside the
  sandboxed widget frame, and exercises SSE rather than switching it off. `test/smoke.sh`,
  the `node` dependency and the "provable only by a human click" sentence are gone with it.
- **A VS Code extension exists, in its own repo** (`aboard_vscode`): the board's tabs as a
  native `TreeView`, the board itself in a webview panel, and every write a human is allowed
  to make. Implemented and unit-tested, and **never loaded into a real VS Code** — that
  install is gated on the human.
- **feat: a write warning reaches the person who can see it.** `POST /aboard.json`
  runs the write-time checks over the tabs the write TOUCHED — never the whole
  board — and the strings land on that journal entry, in the POST reply, on the
  SSE change frame, in the tab's own notice banner, and on the trace tab beside
  the write that caused them. Until now a warning could only ever reach the actor
  who ran the CLI, so an `apply` whose stderr nobody read produced an empty box
  that only the human ever found — which is backwards, since the agent is the one
  still holding the context to fix it. Scoped deliberately: a whole-document scan
  would re-report every pre-existing mistake as though this write had made it, and
  a warning that always fires is one people learn to skip. No warning is ever
  written into a tab's `state`. The reply and the frame also name the tabs the
  checks RAN over (`checked`), which is what lets a banner come back DOWN: a clean
  tab is absent from `warnings`, indistinguishable from a tab the write never
  looked at, so without it the human would keep a warning about a tree the agent
  had already repaired.
- **feat: `aboard apply --check` and `--strict`.** `--check` runs those same checks
  and stops — nothing posted, no board need be running, and no `rev` required,
  because it is a question about content and not about concurrency. `--strict`
  turns any warning into a refusal (exit 1, nothing written), for a loop that must
  stop rather than ship a wrong tab. Warning-not-refusing stays the default; a
  spec can lag its renderer.
- **feat: `aboard apply --label "…"` records WHY a write happened.** Stripped off
  the payload beside `__by` and `__base`, stored on the journal entry and never in
  the board document, and printed by `aboard journal`, `aboard watch` and the trace
  tab. A journal answered who and what and never why, so "the write that broke the
  gallery" could not be found without reading every payload. It is navigation
  inside a local, rotating file — never a record to cite anywhere permanent.
- **feat: `aboard history <tab>`, and the change banner links to what a tab said
  before.** The journal has always recorded the state each changed tab held before a
  write; it was reachable only by parsing `journal.jsonl` by hand. `GET /history?tab=`
  and the new subcommand read it out, newest first, naming who replaced each version and
  saying plainly where the record ends — rotation keeps one generation. `--at N` prints a
  WHOLE document `apply` accepts, with one tab's state replaced: a single-tab document
  would be a document that deletes every other tab.
- **feat: `apply` merges a `409` instead of discarding the write.** Compare-and-set is
  whole-document, so any concurrent write conflicts with any other — the human dismissing
  a notice used to throw away an agent's whole document. It now re-reads, asks the journal
  which tabs moved since its base, re-applies its own tabs where the server did not touch
  them, and retries once. A genuine same-tab collision is named and stops, exactly as the
  browser refuses to merge one silently. Journal entries carry `rev` so "since when" has an
  answer that is not a millisecond clock. It also stops, rather than guessing, when the
  journal cannot say who moved a tab: it records a tab's state before a write but not its
  name, note or type, so a tab renamed on the board while you wrote to a different one is
  named and refused — and said in those words, because "both sides changed the same tab"
  would be a sentence about an edit the caller never made.
- **feat: mount receipts — `aboard rendered <tab>`.** After every mount, and debounced
  after a press, the browser posts the declared control ids it drew, any undeclared one,
  and any unknown-component marker to `POST /rendered` → `.aboard/run/rendered.json`
  (a sidecar, never the board document). `aboard wait --for "rendered <id>"` blocks until a
  browser mounts a tab. The command prints its own two limits: no receipt means nobody had
  the tab open, and a recorded press means the control was reached, never that it behaved.
- **feat: `aboard uploads` accounts for `.aboard/uploads/`** — every file with its size and
  the tabs whose raw state, name or note mention it. `--prune` prints what it would remove
  and refuses without `--yes`. `GET /uploads` is now declared in the manifest.
- **feat: an `html` tab's frame is painted from `app.css`'s own `:root`.** It carried a
  hand-copied palette, and the copy had already lost `--accent-dim`, `--drop` and all three
  `--status-*` tokens — a widget naming one got no colour and no warning. The frame now
  parses the stylesheet it is served beside (embedded, or on disk under `--dev`) and injects
  the whole set, failing CLOSED to the old literal so a widget is never left with no ground
  and no ink.
- **feat: a ```` ```mermaid ```` fence in markdown renders as a diagram** — in a `notes` tab
  and in a stack's notes block, through `diagram.js`'s own loader and theme config (exported
  and shared, never copied). A write-up with one figure in it needed two tabs before, and the
  figure could not travel with the prose being promoted. A fence that will not parse shows its
  source verbatim rather than an empty box.
- **fix: a context menu no longer closes itself the moment it opens.** It armed its
  close-on-scroll listener synchronously, and a scroll event is dispatched at the next
  rendering opportunity rather than when the scrolling happened — so a scroll already in
  flight when the right-click landed arrived just after the menu had opened and shut it,
  with no JavaScript of ours in the stack to blame. On screen it read as a menu that
  flickers; in the suite it read as a right-click test that failed only in some orders, on
  a board with enough tabs for the page to scroll at all. The listener is armed a frame
  late, and the menu's first item is focused with `preventScroll` — it is `fixed` and was
  just placed inside the viewport, so nothing should ever scroll to reach it.
- **feat: `aboard export` renders a `ui` tree** as an indented outline, resolving every
  `{bind}` against `state.data`. `ui` is the type `CLAUDE.md` tells agents to prefer and it
  was the one type export could not read. Which prop carries a node's text is declared in
  `views/ui.spec.json` (`text`, `layout`), so `aboard capabilities ui` answers it too. A `ui`
  table exports as bullets: a markdown table two bullet levels deep is not a table to most
  renderers.
- **fix: the compare-and-set token is a revision, not a clock** — the document
  carries a server-stamped `rev`, and `__base` compares against it. `updatedAt`
  was the base and two writes inside one millisecond shared it, so a stale write
  passed the check and destroyed another's edit. `updatedAt` stays as the human's
  "when". An old-style timestamp base is honoured only while the live document has
  no `rev` of its own, and refused with an explanation after that. `__base` is read
  as a number as readily as a string — `rev` is a number in the document, so the
  obvious hand-written base is one — and a `__base` that is present and is neither
  is `400` rather than silently ignored.
- **fix: `apply` refuses a document with no compare-and-set base** (exit 2), where
  it used to write unconditionally with exit 0 and nothing on stderr. `--force`
  writes without the check and says so.
- **fix: an agent write carries `pendingRemoval` forward**, exactly as it carries
  `touched`: only the human answers a removal request.
- **fix: mutating requests must be same-origin, and every request must carry a
  loopback `Host`** — `403` otherwise, naming the reason. The board has no
  authentication, so these are what stop a page on another origin from rewriting
  it and a rebound DNS name from reading it.
- **fix: `--port`/`PORT` no longer skips duplicate detection**, so a second server
  can no longer take over one project's state file and instance record.
- **fix: `FindRoot` resolves symlinks**, so a project reached through a link is one
  root, one port and one board.
- **fix: `--name`/`ABOARD_NAME` is validated before any path is joined** —
  `--name ../../evil` wrote files outside the project and reported success.
- **fix: `aboard journal` falls back to `journal.jsonl` when the recorded board is
  dead**, not only when the instance file is missing — the resume protocol's third
  command used to exit 1 after a crash.
- **fix: `aboard init` validates `--output-format` before creating anything.**
- **fix: argument-count errors exit 2**, the status the declared table promises.
- **fix: `--output-format yaml` is the same document as `json`** — paired tags on
  every output struct, and `recipes list` no longer drops `scope`, `path`,
  `shadowedBy` and the parse error.
- **fix: an SSE reload merges instead of replacing**, so an edit inside the save
  debounce survives another writer; and `baseline` advances after a merge while a
  stashed copy is never overwritten, so "Restore mine" hands back the human's
  words rather than the agent's. The merge compares tabs by VALUE, not by their
  JSON bytes: `init` serves its authored document verbatim and the server
  re-marshals through its own structs on the first accepted write, so key order
  moves with nothing else, and comparing the text made a freshly initialised
  board treat the human's first concurrent edit as a collision.
- **fix: a `--base-path` with a quote in it injected script into the shell.** The
  prefix is spliced into `window.ABOARD_BASE = "…"`, so it is validated now —
  `/segments` of letters, digits, dot, underscore, tilde or hyphen, refused as a
  usage error from `serve` and again inside `Serve`.
- **fix: the state file keeps mode 0644 through a write** (respecting the umask,
  and preserving a mode its owner chose). `os.CreateTemp` creates at 0600 and the
  rename carried it, so the board dropped out of reach of the other tools the
  developer runs, on the server's first accepted write.
- **fix: an agent cannot plant a `seen` stamp for somebody else** on a tab that
  had none, or on a tab it is creating. Guarantee 4 had a condition on it.
- **fix: `aboard journal` sees history across a rotation** — `tail` read only the
  live file, so the kept generation was unreachable the instant it existed.
- **fix: one unreadable or dangling recipe file no longer hides every recipe**,
  the built-ins included. It is listed with its reason, like every other file
  discovery cannot use. A recipe in a SUBDIRECTORY is reported too, rather than
  silently dropped: recipe directories are flat.
- **fix: `aboard init` reports what it created when `--gitignore` fails.** It
  reported total failure over a board it had just written, so the corrected retry
  then failed with "a board already exists". It also no longer announces a board
  it failed to create: a partial run says what does exist and stops there.
- **fix: a `ui` link with a root-absolute href honours `--base-path`.**
- **fix: a new `markup` tab starts with the state its renderer reads.** The shell
  seeded `{image, caption, regions, strokes}`; `markup.js` reads `{layout, images}`.
- **fix: `POST /` no longer returns the board shell.** The shell is GET-only, which
  is what the HTTP reference always claimed. The reference's refusal table is right
  now too: an unmatched `GET` outside the static allow-list is `403`, not `404`, and
  the four outcomes are listed in the order the server decides them.
- **`aboard capabilities --check`'s stale messages name a remedy that runs anywhere.**
  They said "run `make caps`" — a target in aboard's own checkout, which the projects
  that copy the skill do not have. And the check no longer reports "nothing to check"
  for a generated file that is present but unreadable.
- **fix: engine logging goes through `Options.Logger` without exception** — a
  dropped tab and an unserialisable reply went to the standard logger, where a
  host embedding the tree could not redirect them.
- **hardening: an `html` tab's CSP carries `sandbox allow-scripts`**, so the opaque
  origin holds when the document is fetched standalone rather than framed.
  `connect-src 'none'` is still the containment.
- **`capsHash` ends this release at `4a42dfe3`.** It moved three times on the way:
  `9facfc76` → `6ff337ed` when the command table gained subcommands, so
  `recipes list`, `recipes show` and their flags became part of the described
  surface; `6ff337ed` → `9defc6c6` when `history`, `rendered`, `uploads` and
  `apply`'s `--check`/`--strict`/`--label`/`--force` did; and `9defc6c6` →
  `4a42dfe3` when the journal record widened, because the manifest's own line for
  `GET /journal` said `before` held a previous STATE and that stopped being the
  whole truth. All three moves are correct and all three are recorded, because the
  intermediate value is the one a half-updated skill will be carrying — and `aboard status` comparing a skill's stamp against
  the binary's is how a session finds out its reference describes a board that no
  longer exists.

- **feat: aboard, ported from the `board` spike** — a single Go binary serving a
  shared visual board for a human and one or more agent sessions, with the whole
  UI embedded. Tabs are data, not code: an agent opens one for whatever it needs
  to show, and both sides read and write the same document on disk.

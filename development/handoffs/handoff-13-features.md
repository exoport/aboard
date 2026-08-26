# Handoff — 13 features for `aboard` (the first build queue)

**Status: DONE for items 1–10, 2026-08-26 (plan-2 item 6, commit `d69197a`); item 11
(`bb372` `boards`) is DONE too — dropped by the human on 2026-08-26 and REVERSED by
them the same day, with the design it was then built to (plan-2 §10c).** Each
item's own section carries a **DONE** heading and a note saying what actually shipped,
which is not always what the original sketch below it proposed; where the two disagree,
the DONE note is the record and the sketch is preserved as written. **Nothing in this
file is a live instruction any more.**

## Provenance

This rewrites `handoff-13-features.md`, generated 2026-08-25 on the `board` spike
(`/home/diegos/_dev/ai/board`) by `_output/make-handoff.py` from the human's ticks on
that board's own *Board vs artifacts* tab (`bb244`). The spike has since been ported
into this project — see `development/planning/plan-1_port-from-spike.md` for every
decision this rewrite depends on. Do not relitigate anything marked DECIDED there.

**Every `bb`-id below (`bb359`…`bb372`) is a historical label carried over from the
spike's `board.json`, not a link to a live tab.** The spike's board — and the tab
these ids named — no longer exists; nothing in `aboard`'s own fresh state resolves
them. Keep the ids only because they are how this feature set is referred to in prior
prose and (if promoted) commit messages. `aboard export bb359` will not work; there is
no such tab here.

Two of the original thirteen items were folded into the port itself rather than
queued (plan-1 §8, decision 8) and are already closed — §0 below. The remaining
eleven were the actual build queue. `bb372` was re-specified against the new layout
rather than merely renamed, because its original design (a `/proc` scan) does not
survive the port's own decisions — and then the human dropped the whole feature; see
its entry.

**`bb370` does not appear anywhere in this list.** It was on the human's ballot and
was left unticked (`pick: false`) — declined, not lost in translation, and not one of
the thirteen that were ever in scope. Do not go looking for it or re-propose it from
the id gap.

Read `CLAUDE.md` and (once it exists) `.claude/skills/aboard/` first. Every item below
was checked, on the spike, against that project's closed decisions; here, check it
against plan-1's decisions instead — the `Risk` line for each item points at the
nearest one.

## §0 — Already fixed during the port

Per plan-1 decision 8, these two were folded into the port's own "split" commit
rather than queued, because both are corrections to the manifest/guarantee machinery
that the rest of this queue depends on being trustworthy:

- **`bb359` — spec drift the checks couldn't see.** Fixed alongside the port: any
  renderer drawing raw `<button>` markup outside `controlsFor(type)` is now
  declared, and `pkg/aboard/web/views/html.spec.json`'s stale claim that an `html`
  block inside a `stack` does not render was corrected (it does — the spike proved
  it by hand on 2026-08-25).
- **`bb360` — the four guarantees, made real.** `mergeSeen` (`pkg/aboard/tabs.go`) is
  now called from `reconcileTabs`, so an agent write can no longer erase another
  actor's `seen` stamp. An absent `__by` on `POST /aboard.json` (`pkg/aboard/server.go`
  `postState`) now defaults to `"unknown"` — agent-level powers only — never to
  `"human"`. `aboard apply --by human` is refused by the CLI before it ever posts.

**Consequence worth stating for every item below:** because `bb360` landed, a caller
of `apply` (or a raw `POST /aboard.json`) that used to get human powers by *omitting*
`--by` no longer does. Anything downstream that relied on that omission (a script, a
habit carried from the spike) now needs an explicit, and refused, `--by human` — which
means it needs a human's own hand on the keyboard, in the browser, not a flag.

## The list

| # | id | feature | size | status |
|---|---|---|---|---|
| 1 | `bb361` | Warnings travel with the write | M | **DONE** (plan-2 item 6a) |
| 2 | `bb362` | `apply --check` and `--strict` | S | **DONE** (plan-2 item 6a) |
| 3 | `bb363` | Per-tab history and restore | M | **DONE** (plan-2 item 6c) |
| 4 | `bb364` | html tabs read the real palette | S | **DONE** (plan-2 item 6b) |
| 5 | `bb365` | Mermaid fences in markdown | S | **DONE** (plan-2 item 6b) |
| 6 | `bb366` | `apply` merges instead of failing | M | **DONE** (plan-2 item 6c) |
| 7 | `bb367` | `export` renders a `ui` tree | M | **DONE** (plan-2 item 6b) |
| 8 | `bb368` | Mount receipts from the browser | M | **DONE** (plan-2 item 6c) |
| 9 | `bb369` | Uploads accounting and prune | S | **DONE** (plan-2 item 6c) |
| 10 | `bb371` | Write labels in the journal | S | **DONE** (plan-2 item 6a) |
| 11 | `bb372` | `boards` — every board on this machine | S | **DONE** — dropped then reversed, 2026-08-26; built as a `/proc` scan, plan-2 §10c |

---

## 1. Warnings travel with the write — **DONE**

`bb361` · size **M — a day or two** · shipped in plan-2 item 6a

**What shipped.** `commitState` (`pkg/aboard/server.go`) runs the checks over the
tabs the write CHANGED — `changedTabWarnings` in `caps.go`, reading each changed
tab's already-parsed state, so a write still decodes the incoming body once and
the board not at all. The strings land on the journal entry (`JournalEntry.Warnings`,
keyed by tab id), in the POST reply, on the SSE change frame, in the shell's notice
banner (`.banner--warning`, dismissible, per-viewer) and on the trace tab (a ringed
dot, and the lines in the detail panel). `aboard journal` prints them under the
entry.

Two things came out different from the sketch. The banner reads the SSE frame
rather than re-fetching `/journal`: the page already re-reads the document on that
ping, and `/journal` reads both generations of the file — up to 32 MB — to surface
a sentence. And the scoping got a counter (`warningScans`) so "never the whole
document" is a test rather than a promise; it fails at N-per-write on the
unscoped version.

**What it does.** `postState` (`pkg/aboard/server.go`) runs `writeWarnings`
(`pkg/aboard/caps.go`) over the tabs actually touched by the write, before it
commits. The resulting strings are stored on that journal entry, not on the tab, and
surfaced in the browser's notice banner.

**Gap it closes.** The recorded structural limit (spike `CLAUDE.md`, "gotchas") is
that a CLI warning only ever reaches the actor who ran the CLI. `postState` never
calls `writeWarnings` at all today, so a browser write, a raw `POST /aboard.json`, or
an `aboard apply` whose stderr nobody read all produce an empty box that only the
human ever finds.

**Touches.** `pkg/aboard/caps.go` (`writeWarnings`, scope its `checkTabState` walk to
the entry's own tabs), `pkg/aboard/server.go` (`postState`, before the atomic write),
`pkg/aboard/journal.go` (the journal-entry struct gains a `warnings` field),
`pkg/aboard/web/aboard.html` (`buildNotices`/`renderNotices`), `pkg/aboard/web/views/trace.js`.

**Risk / nearest decision.** The strings belong on the journal entry and the banner
reads them from the journal, never written into a tab's own `state` — mirrors how
plan-1 keeps warnings out of the state document itself. The real hazard is noise: the
gallery's deliberately-invalid `sparkline` example warns on every write that touches
that tab, by design, and no suppression mechanism may be added for it (spike
`CLAUDE.md` is explicit on this). So the walk must stay scoped to the tabs actually in
the write, not the whole document, or one warning becomes background noise.

---

## 2. `apply --check` and `--strict` — **DONE**

`bb362` · size **S — an afternoon** · shipped in plan-2 item 6a

**What shipped.** Both flags on `apply`, declared in `commands.go` and implemented
in `Apply` (`client.go`, now taking an `ApplyOptions` rather than a fifth
positional bool). `--check` runs the checks and returns before the board is
contacted; `--strict` refuses on any warning with a typed error (`ErrWarnings`)
and exit 1 — not 2, because nothing about the INVOCATION was wrong.

One judgement not in the sketch: `--check` skips the no-base refusal entirely. A
base is a question about concurrency, and a check that posts nothing cannot lose
anybody's work — refusing to check a document because it has no `rev` would make
the cheap habit unavailable in the case it is cheapest, a document being built up
before it is ever applied. `test/smoke.sh` no longer exists (plan-2 item 4); the
coverage is Go tests plus the browser suite.

**What it does.** New flags on the existing `apply` command (`pkg/aboard/commands.go`
declares them; `pkg/aboard/server.go`'s `applyStdin` implements them). `--check` runs
`writeWarnings` and exits without ever calling `POST /aboard.json`. `--strict` turns
any warning into a refusal (non-zero exit, nothing written).

**Gap it closes.** `aboard apply` prints `applied` and exits 0 for a `ui` tree that
draws an empty box — the write succeeded; the content is wrong. `apply --check` is
the cheap habit before a write (no server contact needed beyond what `apply` already
makes); `--strict` is the guard for a loop that must stop rather than ship a wrong
tab.

**Touches.** `pkg/aboard/commands.go` (declare both flags on `apply`, distinct from
the same-named `--check` that already exists on `capabilities` — different
subcommand, no collision), `pkg/aboard/server.go` (`applyStdin`), `pkg/aboard/caps.go`
(`writeWarnings`, the version-mismatch check it shares with the stamped-`version`
guard), `test/smoke.sh` (run via `make smoke`).

**Risk / nearest decision.** `writeWarnings` warns and never refuses by default
because a spec can legitimately lag its renderer — that stays the default. `--strict`
does not cross that; it is opt-in per call. The failure mode to design against is a
session that habitually drops `--strict` and gets no benefit from it existing.

---

## 3. Per-tab history and restore — **DONE**

`bb363` · size **M — a day or two**

**DONE** (plan-2 item 6c). `pkg/aboard/history.go` is the whole read path:
`historyFrom` filters the journal tail, `GET /history?tab=&limit=` serves it,
`aboard history <tab>` lists it and `--at N` prints a restore. The restore is a
WHOLE document with one tab's state replaced, exactly as the Risk line demanded —
a single-tab document would have been answered with a removal request on every
other tab. Numbered newest-first from 1, so `--at 1` is the undo. `JournalEntry`
gained `rev`, which `bb366` needed too. The listing states where the record ends
in both the human and JSON forms (`ends`), and the change banner in `aboard.html`
links to what the tab said before — read-only, with the restore command printed
rather than a button, because a restore made without seeing the document is the
kind of write history exists to recover from. A tab that is GONE is refused
rather than rebuilt: the journal records a name and a state and never a `type`.

**What it does.** `journal.go` already writes every changed tab's prior state to
`.aboard/run/journal.jsonl` on every accepted write. This adds a read path over it: a
new `GET /history?tab=` route, and a new `aboard history <tab> [--limit N] [--at N]`
subcommand — **added to plan-1's CLI grammar table when it shipped, along with
`rendered` and `uploads`.**
`history <tab>` lists prior versions with who wrote them; `history <tab> --at N`
prints the document form `aboard apply` accepts, for an explicit restore.

**Gap it closes.** The journal is the board's only recovery path from a bad write and
today it is reachable only by parsing `journal.jsonl` by hand.

**Touches.** `pkg/aboard/journal.go` (a tab-filtered read over the tail, the
`/history` handler), `pkg/aboard/server.go` (route registration), `pkg/aboard/caps.go`
(`declaredRoutes`, the new command's entry), `pkg/aboard/cli/history.go` (new —
follows the one-file-per-verb shape the other subcommands use), `pkg/aboard/export.go`
(the output shape for `--output-format`), `pkg/aboard/web/aboard.html` (the change
banner links to what a tab said before).

**Risk / nearest decision.** A journal entry's `Before` holds only a tab's inner
`state` blob, so `history --at N` must either merge it onto a freshly read full
document before handing it to `apply`, or the restore — submitted as a single-tab
document under a real actor — must not be mistaken for a document that deletes every
other tab (the same shape of mistake `bb360` fixed for an absent `__by`). The listing
must name who wrote each entry, and say plainly where history ends — rotation keeps
one generation, per the spike's own journal.

---

## 4. html tabs read the real palette — **DONE**

`bb364` · size **S — an afternoon**

**DONE** — `htmltab.go` parses `app.css`'s `:root` out of the served `fs.FS` (embedded, or on disk under `--dev`) and injects the whole set, failing closed to the old literal. Asserted as set EQUALITY in both directions, plus the six ways a stylesheet can fail to parse.

**What it does.** `htmltab.go` parses `pkg/aboard/web/app.css`'s `:root` block out of
the embedded (or, under `--dev`, on-disk) `fs.FS` and injects the full current token
set into the `html`-tab frame, instead of the hand-copied subset it ships today.

**Gap it closes.** `htmltab.go` hardcodes a duplicate `:root` that has already
drifted from `app.css` once in the spike's history — several tokens existed in the
real stylesheet and were simply missing from the frame, so a widget naming one got no
colour and no warning. Porting the file verbatim carries the same hardcoded subset
forward.

**Touches.** `pkg/aboard/htmltab.go` (parse the embedded `app.css`, fail closed to
the current literal on a parse error), `pkg/aboard/web/app.css`, `test/smoke.sh` (via
`make smoke`; assert the frame's token set equals `app.css`'s).

**Risk / nearest decision.** Sits directly on the single-dark-theme, tokens-only
colour rule; enforces it rather than reopening it. Parsing must fail closed to the
existing literal so a malformed stylesheet never leaves a widget with no ground at
all — silence there is worse than a stale-but-present palette.

---

## 5. Mermaid fences in markdown — **DONE**

`bb365` · size **S — an afternoon**

**DONE** — `markdown.js` keeps the fence info string; a ```` ```mermaid ```` fence renders through `diagram.js`'s exported loader and hoisted theme — one function, not a copy — and keeps its source verbatim on a failure. Demonstrated in the example board's stack notes block; two `e2e` tests.

**What it does.** `pkg/aboard/web/views/markdown.js` keeps the fence info string
instead of discarding it; a ` ```mermaid ` fence renders through `diagram.js`'s
loader, now exported for reuse rather than private to the `dag`/`diagram` mount path.

**Gap it closes.** The board vendors mermaid (`pkg/aboard/web/lib/mermaid.min.js`)
yet today a diagram can only be a whole tab — never a figure inside a `notes` block,
a `stack`, or a write-up being promoted into a project document.

**Touches.** `pkg/aboard/web/views/markdown.js` (retain the fence's info string),
`pkg/aboard/web/views/diagram.js` (export `loadMermaid`, hoist its theme config out
of the mount function so it is not private to one renderer), `pkg/aboard/web/views/notes.js`,
`pkg/aboard/web/views/notes.spec.json`.

**Risk / nearest decision.** The loader must be a shared function, not a copy, or the
two theme maps drift the same way the html-tab palette did (item 4, above) — the same
mistake, two places it can recur. A parse failure must show the mermaid source
verbatim, the way `diagram.js` already does, never an empty box.

---

## 6. `apply` merges instead of failing — **DONE**

`bb366` · size **M — a day or two**

**DONE** (plan-2 item 6c). `pkg/aboard/merge.go`, called from `client.go`'s 409
branch. It mirrors `mergeOntoFresh` rather than calling it, and it has a better
source than the browser does: the journal records what each moved tab held AT our
base, which is the only record of the third document — so "did I change this or
did they" is answered rather than guessed. One retry. A same-tab collision is
NAMED and stops. A conflict the merge cannot reason about (a timestamp base, a
journal rotated past the base) falls back to the plain refusal with the reason on
stderr. One defect found on the way, in code this change refactored rather than in
the feature: `--force` sends no base while its document still carries a `rev`, so
a post path that re-derived the base from the document would have turned every
forced write back into a compare-and-set one.

**What it does.** On a `409` from `POST /aboard.json`, `applyStdin` re-reads the live
document, consults `/journal` for entries since the base it started from to see which
tabs moved, re-applies its own tabs onto the fresh document, and retries once.

**Gap it closes.** `aboard.html`'s browser-side `mergeOntoFresh` already proves this
exact strategy against the same endpoint, refusing to lose the human's in-progress
edit on a conflict. `apply` today hands back one sentence on a `409` and discards the
agent's whole document — built from a board it can no longer read — which is the same
class of loss the browser side was fixed to stop.

**Touches.** `pkg/aboard/server.go` (`applyStdin`'s conflict branch),
`pkg/aboard/journal.go` (read entries after a given timestamp),
`pkg/aboard/web/aboard.html` (`mergeOntoFresh` is the pattern to mirror, not to call
into — the CLI has no browser DOM).

**Risk / nearest decision.** Whole-document compare-and-set is untouched; only what
happens after a refusal changes. Must keep the browser's rule that a genuine
same-tab collision is never merged silently — name the tab and stop. One retry only,
so a busy board cannot spin `apply` forever.

---

## 7. `export` renders a `ui` tree — **DONE**

`bb367` · size **M — a day or two**

**DONE** — `export.go` walks a `ui` tree into an indented outline, resolving every `{bind}` through the same `lookupBind` the write checker uses. Which prop is a node's text is DECLARED in `views/ui.spec.json` (`text`, `layout`), not tabled in Go. Golden test. `log`/`html`/`trace` are explicit non-cases now.

**What it does.** `pkg/aboard/export.go`'s type switch gains a `ui` case: walk
`state.root`, resolve every `{bind}` against `state.data` (reusing the resolution
`caps.go`'s `checkBind` already does for write-time validation), print an indented
outline.

**Gap it closes.** `export.go`'s switch does not cover `ui` today, so the type
`CLAUDE.md` tells agents to prefer over `html` is the one type that cannot be
promoted or read back without a browser. Promotion into the project's own documents
is the board's whole posture (plan-1 §14 carries this reasoning into `aboard`'s own
docs); its most-recommended type currently falls outside it.

**Touches.** `pkg/aboard/export.go` (`ui` case in the markdown path; `log`/`html`/
`trace` stay explicit non-cases, unchanged), `pkg/aboard/caps.go` (reuse
`checkUITree`'s catalog walk and `checkBind`'s resolution rather than re-deriving
them), `pkg/aboard/web/views/ui.spec.json`.

**Risk / nearest decision.** Must not be sold as replacing a screenshot — `apply`
exiting 0 (or `export` printing a clean outline) is not evidence anything renders
correctly; a text outline cannot see a legal-but-unreadable layout any more than a
clean write can. `checkUITree` validates prop *names*, not which prop is the display
text, so per-node printing logic is genuinely new work, not a reuse of an existing
table.

---

## 8. Mount receipts from the browser — **DONE**

`bb368` · size **M — a day or two**

**DONE** (plan-2 item 6c). `pkg/aboard/receipts.go` + `POST /rendered` →
`.aboard/run/rendered.json`, `aboard rendered [tab]`, and the `rendered <id>` wait
predicate (released from the receipt endpoint, not from the write path — a mount
is not a write). `views/ui.js`'s markers carry `data-unknown`; `controls.js`
already carried `data-gesture` and `data-undeclared`. Both honest limits are
printed by the command itself.

One thing the handoff did not anticipate: a mount-only sweep is not enough.
`ui`'s `tabs` component builds only the OPEN panel, so the gallery's deliberate
`sparkline` marker — sitting in the fifth panel — is genuinely not drawn until the
human reveals it, and a mount-only sweep would have reported the board clean
forever. The receipt is therefore swept on ACTIVATION, which reports what is on
screen now.

**What it does.** `aboard.html` posts unknown-component markers and fired control ids
to a new sidecar endpoint after every mount; `aboard rendered <tab>` — **a new
subcommand, not in plan-1's CLI grammar table yet** — prints what was recorded.

**Gap it closes.** Nothing today tells a session what the browser actually drew. Two
symptoms this closes: an agent declaring a tab ready and the human finding it wrong,
and a hand-kept "genuinely unproven" list that nothing updates on its own.

**Touches.** `pkg/aboard/web/aboard.html` (a post-mount sweep in `ensureMounted`, a
delegated `[data-gesture]` listener), a new `pkg/aboard/receipts.go`,
`pkg/aboard/server.go` (the route), `pkg/aboard/web/views/controls.js`
(`GESTURE_ATTR`, `data-undeclared`), `pkg/aboard/wait.go` (an optional render
predicate for `aboard wait --for`), `pkg/aboard/cli/rendered.go` (new).

**Risk / nearest decision.** Per-viewer, so it belongs in a sidecar under
`.aboard/run/` like the sidecar logs, never in `.aboard/aboard.json` — the same rule
that keeps selection, zoom and drafts out of the state document. It reports; it does
not act. Do not confuse this with the abandoned DOM-sweep idea (spike `CLAUDE.md`,
"Phase 4… do not resurrect the sweep") — that scraped `button[title]` against
free-text `gestures` at a hopeless signal ratio; this records invocations of ids
already machine-declared in `views/*.spec.json` and says nothing about `gestures`.
State two honest limits in the output itself: no receipt means nobody had the tab
open, and a recorded click proves a control was *reached*, never that it behaved
correctly.

---

## 9. Uploads accounting and prune — **DONE**

`bb369` · size **S — an afternoon**

**DONE** (plan-2 item 6c). `Uploads`/`PruneUploads` in `pkg/aboard/upload.go`,
`aboard uploads [--prune] [--yes]`, and `GET /uploads` added to `declaredRoutes`.
The scan reads each tab's raw state text plus its name and note, as the Risk line
required. `--prune` prints and refuses without `--yes`, and the listing says on
what evidence it called a file unreferenced — the reader's next action is deleting
things.

**What it does.** `aboard uploads` (new subcommand — flag it as an addition to the
grammar) lists every file under `.aboard/uploads/` with its size and the tabs that
mention it; `aboard uploads --prune --yes` deletes the ones no tab mentions.

**Gap it closes.** `GET /uploads` already lists url, bytes and mtime but is absent
from `caps.go`'s `declaredRoutes`, and there is no reference scan and no delete path
at all today.

**Touches.** `pkg/aboard/upload.go` (the accounting scan, the delete path),
`pkg/aboard/caps.go` (`declaredRoutes`, the new command's flags),
`pkg/aboard/cli/uploads.go` (new).

**Risk / nearest decision.** Deletion is irreversible and `.aboard/uploads/` is not
meant to be under git (plan-1 §15: the repo gitignores `.aboard/` wholesale in every
project except this one, and `aboard`'s own content lives there too). The reference
scan must read each tab's *raw* state text, not just declared fields — an `html`
widget's markup can name a URL no declared field holds. `--prune` must print what it
would remove before removing anything, and refuse without an explicit `--yes`.

---

## 10. Write labels in the journal — **DONE**

`bb371` · size **S — an afternoon** · shipped in plan-2 item 6a

**What shipped.** `apply --label "…"` → `__label` on the envelope, stripped in
`postState` beside `__by`/`__base`, stored as `JournalEntry.Label`. Printed by
`aboard journal` (indented under the entry, so the tab column a reader scans is
not pushed off the terminal), carried on `aboard watch`, and shown by the trace
tab in the dot's tooltip and in the detail panel.

The risk the entry named — an unused optional field going stale — is answered by
both consumers existing, and the entry's own warning about promotion is now in the
skill's multi-session etiquette. Added beyond the sketch: the label is
whitespace-collapsed and clamped to 200 runes, because it arrives from a caller
and the journal is a rotating file this server appends to on every write.

**What it does.** `apply --label "…"` stores the caller's line on that journal entry;
`aboard journal`, `aboard watch` and `views/trace.js` print it.

**Gap it closes.** A journal entry answers who and what changed, never why — "the
write that broke the gallery" cannot be found in a large `journal.jsonl` without
reading every payload. An artifact version carries a label shown in its picker; this
is the same navigation aid, threaded exactly like the proven `__by`/`__base` pattern
`postState` already strips off the payload before storage.

**Touches.** `pkg/aboard/commands.go` (declare `--label` on `apply`),
`pkg/aboard/server.go` (`postState` strips `__label` beside `__by`/`__base`),
`pkg/aboard/journal.go` (the entry struct, the CLI printer), `pkg/aboard/web/views/trace.js`.

**Risk / nearest decision.** Cheap, and near nothing decided, which is also the
argument against it: an unused optional field goes stale the way `gestures`-as-prose
did once nothing read it — it earns its place only if `journal` and the trace tab
both show it and both stay readable without one. Local and rotating, so a label is
navigation, never a promotion record on its own — it must not become an excuse for
citing a tab id in a commit message (the spike's own naming rule: name the thing, put
the id beside it, and an id from a rotated journal is nothing to a future reader).

---

## 11. `boards` — every board on this machine — **DONE**

`bb372` · size **S — an afternoon**

**Dropped by the human on 2026-08-26 and REVERSED by them the same day, with the
design attached. Built to that design; see plan-2 §10c for the reversal as it was
recorded.** The record of both answers is kept here because the drop is the more
persuasive of the two and would otherwise be the last word a reader found.

**What shipped.** `aboard boards` (`pkg/aboard/boards.go`, `boards_linux.go`,
`pkg/aboard/cli/boards.go`) walks `/proc/[0-9]*`, recognises an `aboard serve` or an
`ape aboard serve` from `cmdline`, honours a `--cwd` in the argv, `FindRoot`s from
there, and then does per project exactly what `status` does: read the root's
`instance*.json`, keep the record whose `pid` matches, verify it with `ProbeBoard`.
One row per (root, name) — the full absolute project path, the name (`default` when
empty), app, url, port, pid, started, version, tab count and the
`lastEditedBy`/`updatedAt` pair — sorted by root then name, with
`--output-format json|yaml`. A record whose process has gone is listed as "recorded
but not answering"; the number of processes inspected and the number that could not
be is printed with the result.

**What came out different from both sketches below.** No registry file, so the
"append on serve, verify on read" design in the ORIGINAL PROPOSAL is not what
shipped and is not held in reserve. And the platform objection was ANSWERED rather
than argued with: the scanner is behind `//go:build linux`, and everywhere else the
command exists, is declared, and exits 2 with the reason and `aboard status` as the
alternative.

**The `comm` note survives as a constraint, not an objection.** The bullets below say
the scan "breaks under `ape aboard`" because `comm` is `ape`. That is true of `comm`
and it is why the scan reads `cmdline` instead: `/proc/<pid>/cmdline` carries the
whole argv, so it finds `ape aboard serve`.

Everything from here to the next rule is the ORIGINAL PROPOSAL, preserved unchanged
and superseded by the paragraphs above.

**What it does — re-specified, not just renamed.** The spike's design scanned
`/proc` for `comm == "board"`, read each match's `cwd`, and read that project's
instance file. **That design does not survive the port and is dropped, not carried
forward:**

- **It breaks under `ape aboard`.** Decision 6 gives a hosted board the process name
  `ape`, not `aboard` — a `comm` filter for `"aboard"` would silently miss every board
  that ape is running on someone's behalf, which is one of the two ways this project
  is meant to run.
- **It only ever worked on Linux.** `/proc` has no equivalent on macOS or Windows
  without shelling out to something like `lsof` — the spike's own writeup already
  flagged this and shipped it anyway as a known gap. Losing macOS/Windows entirely
  is a worse trade now that `aboard` is a real distributable binary (`.goreleaser.yaml`
  builds for both), not a single-machine spike.

**The replacement: a small registry, verified rather than trusted.** `aboard serve`
appends `{root, name}` to a flat, user-level file — `~/.aboard/known-roots.json` —
on startup (dedup by root+name; this is a judgement call, recorded as one, since
plan-1 does not name a location for cross-project state). `aboard boards` reads that
file and, for **each** entry, does exactly what `probeBoard` already does for a
single project: reads `<root>/.aboard/run/instance.json`, calls `GET /health` on the
port it names, and keeps the row only if `health.project` matches `root` and
`health.app` is `"aboard"` or `"ape-aboard"`. A dead or mismatched entry is dropped
from the printed list and the registry is rewritten with only the live entries as a
side effect of listing — no separate cleanup step, no clean-shutdown hook to forget.

**Why this beats both the rejected registry-only design and the original scan:** a
registry that is *trusted* goes stale the moment a server dies without a clean
shutdown — the spike's own reasoning against a registry ("the same failure as a
heartbeat dot a cron keeps green"). A registry that is *verified on every read*, the
way this one is, cannot go stale in the way that mattered: the printed list is never
more optimistic than a live `/health` call a moment ago. And because the write side
is pure file I/O in the engine (`pkg/aboard/layout.go`-adjacent, not CLI-specific),
it fires identically whether the host is `aboard serve` or `ape aboard serve` — the
registry entry gets written by the engine regardless of which binary called it,
unlike a `/proc` scan keyed to one binary's name.

**Touches.** `pkg/aboard/layout.go` or a new `pkg/aboard/registry.go` (append-on-serve,
read-and-verify-on-list), `pkg/aboard/server.go` (`probeBoard` reused, not
reimplemented), `pkg/aboard/cli/boards.go` (new subcommand — **add `boards` to
plan-1's CLI grammar table when this ships**), `pkg/aboard/caps.go` (`declaredRoutes`
unaffected; this is CLI-only, no new HTTP route), `test/smoke.sh` (via `make smoke`).

**Risk / nearest decision.** Two honest limits to print in the command's own output,
not just note here: (1) a board that has never once run `aboard serve` since the
registry file was created will not appear, even if `.aboard/run/instance.json`
exists on disk from a previous run — say so, do not print an empty list that reads as
"no boards anywhere on this machine". (2) The registry file itself is new
machine-local state outside `.aboard/run/`, so it needs its own place in the
project's docs (`docs/reference/` once that exists) — it is not project content and
must never be considered for git.

---

## Before you call any of this done

- `go build ./... && go vet ./... && gofumpt -l . && go test -race ./...`
- `make caps` if any `views/*.spec.json` changed — it builds twice, and both builds
  count (`pkg/aboard/web` is embedded, so the first build only writes
  `controls.generated.js`; the second embeds what it wrote).
- `make e2e` once per tool call — **not `make smoke`**, which no longer exists:
  `test/smoke.sh` went with plan-2 item 4 and the browser suite replaced it. Every
  `Touches` line below that names it is the ORIGINAL sketch, preserved as written;
  read the `DONE` note above each one for what actually shipped. `pkg/aboard/web`
  is embedded, so a stale binary tests a stale copy of itself silently — `make e2e`
  depends on `build`, a hand-started server does not.
- `aboard status` must not warn about a stale skill reference once
  `.claude/skills/aboard/references/reference.generated.md` exists.
- `make shot` and **look at the pictures** — `apply` printing `applied` and exiting 0
  is not evidence anything renders; it is evidence a write was accepted.
- Get the human's sign-off before committing. This project's own board (once it has
  one) decides how — a gate tab if it sets one up, direct review otherwise. There is
  no `bb128` here; that was the spike's own content, not a mechanism this project
  inherits.

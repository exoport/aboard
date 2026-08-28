# Why a board and not an artifact

An **HTML artifact** is a hosted, versioned, shareable web page: an agent writes a
self-contained HTML document, the platform publishes it to a default-private URL, and
every publish mints an immutable version the owner can label, roll back to, and share
with whoever they choose. It is a good medium, and for a great many things it is the
better one.

**aboard** is not that. It is one JSON document on a developer's own disk, served to
`127.0.0.1` on a port derived from the checkout's path, with no authentication, no
hosting and no reader outside that machine — and `.aboard/` in `.gitignore`. Its
conclusions do not count until somebody has rewritten them into the project's own
documents. It is a working channel, not a record.

This page exists because "why not just publish an artifact?" is a fair question that
deserves an answer longer than a preference. The answer is that they are mostly **not
competitors**: they are two media whose failure modes are almost exactly complementary,
and knowing which one a piece of work belongs in is more useful than knowing which is
better.

## Where it came from, and how to read it

The matrix below was built on the predecessor of this project — the `board` spike — by
ten agents working in parallel: four read the two surfaces and this project's closed
decisions, one built the matrix, two proposed features from different framings, two
verified every proposal against the real code, and one ranked what survived. The human
then argued with the result. Across 20 dimensions the edge falls **board 9 · different
jobs 5 · artifacts 3 · tie 3**.

Two honest caveats about the columns:

- **The "this board" column has been re-verified against this repository** and states
  today's truth, not August's. Many of its original cells described gaps that are now
  closed — `rev` replacing a timestamp base, five guarantees actually wired up, `apply`
  merging on a `409`, per-tab history and restore, warnings travelling with the write,
  `export` covering `ui`, two themes and a project house style, the human's own
  `requests`, and an `html` frame that parses the real stylesheet rather than carrying a
  copy of part of it. Where a row's verdict survived that anyway, it survived on a
  narrower margin, and the row says so.
- **The "HTML artifacts" column is the human's own record of the other medium**, kept as
  they wrote it. It describes what was true of that surface for that user in that
  session; some of it is capability-roster-specific and none of it was re-verified here,
  because it is not this project's to verify.

## The matrix — 20 dimensions

| Dimension | This board | HTML artifacts | Edge | Because |
| --- | --- | --- | --- | --- |
| **Who can see it, and how they get there** | One browser on `127.0.0.1`, at a port derived from the checkout path (base 41000 plus a hash of the path, so 41000–48999) and recorded in `.aboard/run/instance.json`. No auth, no hosting, no remote reader; a `Host` outside loopback is `403`. A second developer on the same repository gets their own board. Showing it to anyone else means `aboard export` and committing the text. | A hosted claude.ai URL, default-private, that the owner chooses to share. The user's doors are `/artifacts` in the terminal (o opens, c copies), ctrl+] for the session's most recent, and the web gallery; `action:"list"` with `scope: shared` enumerates what others shared with them. | artifacts | The board has no reader outside the machine that runs it; an artifact is a link. |
| **Whether the medium is authoritative** | Explicitly not, as policy — see [why a local, non-authoritative channel](why-a-local-non-authoritative-channel.md). If the board and a committed document disagree, the document wins, and `.aboard/` is gitignored in every project **including this one**: the demo content ships compiled into the binary instead. | Usually is the deliverable — a hosted, versioned, URL-bearing page with a version picker. Nothing in the tool surface describes an artifact as provisional or as needing extraction into something else. | different jobs | The board is a working channel whose conclusions must be rewritten elsewhere to count; an artifact is a published record that already counts. |
| **Lifetime, versioning, and rollback** | One mutable JSON document. The server stamps `rev`, `nextId`, `updatedAt` and `lastEditedBy` on every accepted write — the caller cannot choose its own. There is no version history *in the document*, but there is a bounded one beside it: `journal.jsonl` records **each changed tab exactly as it was**, and `aboard history <tab>` reads it back, with `--at N` printing a whole document `apply` accepts. Rotation keeps **one** older generation, so the past is bounded and the listing says where it ends. | Every publish mints an immutable version; `label` (≤60 chars) names it in the picker; `contract` pins, upgrades or rolls back the runtime; `force` overwrites a newer version but is refused over one saved from inside the page. | artifacts | Narrower than it was — the board had no content rollback at all when this row was written, and now has a real one. But it is local, bounded to one rotation, and gone with the directory, where an artifact's version list is the medium itself. |
| **Who may write, and how a collision resolves** | Whole-document compare-and-set on `POST /aboard.json`, keyed on **`rev`** — a counter, never `updatedAt`, because two writes inside one millisecond share a timestamp and a base built from the first still matched after the second had landed. Any concurrent write anywhere on the board `409`s. Both sides then merge: the browser re-applies only the tabs the server has not touched and stashes a real collision behind "Restore mine"; `apply` does the same **once**, using the journal to learn what each moved tab held at its base, and names a genuine same-tab collision — with the field — rather than picking a winner. | Publish carries a tracked `baseVersion`; a concurrent publish conflicts and the rejection hands back the newer content to merge and republish. In-page `artifact.publish()` rejects `conflict`, every open view is already reloading to the winner, and the correct handling is no retry — this edit is dropped. | tie | Both are whole-document optimistic concurrency resolved by re-read-and-merge; the board's unit is the entire board, the artifact's the entire page. |
| **Trust and attribution — who the writer actually is** | `--by` is free text stamped into `lastEditedBy`, each tab's `touched.by` and the journal, and there is **no authentication of any kind**. What has changed is the direction of the defaults: an absent `__by` is now `"unknown"` with agent powers only, never `"human"`, and `aboard apply --by human` is refused by the CLI before it posts. So the human-only powers need a human's hand on a keyboard in a browser rather than an omitted field. It raises a floor, not a wall — a raw `POST` is still unauthenticated. | Publishing runs with the viewer's own platform authority and identity — each viewer's click publishes as them. `not_writer` / `not_granted` are real, unspoofable rejections, and a read-only view resolves the namespace but cannot write, so member presence never signals writability. | artifacts | The board's actor label is still a courtesy convention on unauthenticated loopback; the artifact's is a platform session the page cannot forge. |
| **How the human's input reaches the agent** | Everything the human does lands in the document in a shape **the agent designed**: `form` field values, `gate` verdicts with reasons, a `vote` ballot, `table` rows, `markup` marks, and intents appended by an action-strip or `ui` button. Since then one field runs the other way and is the human's alone: `tab.requests`, their notes to an agent about that tab, which an agent may only stamp `done` — `aboard requests` lists what is waiting. | The page must publish its own state back into its own HTML — declare `{artifact:{}}`, embed shared state as data, regenerate the document, `artifact.publish(html)`. The agent recovers it with `action:"read"` and re-parses the page it wrote. Nothing a viewer types is kept unless the page publishes it, and there is no capability for the page to ask Claude anything. | board | The board hands back a JSON document whose schema the agent chose; an artifact hands back the agent's own HTML to re-parse. |
| **How the agent reads the human's edits after the fact** | Read the document, plus `GET /journal` and `aboard journal`, which record when, who, from where, which tabs changed, what the write-time checks said, why (`apply --label`), and **each changed tab as it was before the write** — the whole tab since the record's second generation, so a rename is attributable and not only a state diff. `aboard watch` streams accepted writes as JSON lines; `aboard history` reads one tab's past out of the same file. | `action:"read"` + `url` returns the current raw HTML and nothing else. No per-write attribution feed, no before-state, no version enumeration; republish watching is unavailable in this session (`action:"watch"` only reports that) and artifact comments cannot be read or answered here. | board | The board can answer "who changed what, and what did it say before"; an artifact can only answer "what does it say now". |
| **What the page can DO when a human presses a button** | Nothing, by closed decision. Action buttons, `gate` verdicts and `ui` buttons all **record** an intent or a decision into the document; the agent that asked reads it and acts. That is precisely what makes a stray click harmless on a server with no auth. | A real side effect is available. `mcp.callTool(server, tool, input)` executes the viewer's connected connector with the viewer's credentials (tokens never exposed, and a declaring page cannot be shared publicly); `downloads.save({filename, data})` hands the viewer a file after a confirmation they may decline; `artifact.publish()` mutates the stored document. | different jobs | The board can only record because it is unauthenticated loopback; the artifact can act because the platform holds the viewer's identity — and neither can start a new agent session from the page. |
| **Network reach and containment** | The shell is trusted first-party code talking to its own server, and two guards stop a *browser* being the thing that reaches it for somebody else: a `Host` allow-list, and a same-origin rule on every mutating request. Neither is authentication. The untrusted layer is one tab type: an `html` tab is served with `connect-src 'none'`, `sandbox="allow-scripts"` and no `allow-same-origin`, `img-src data: blob:` only, and a `frame-ancestors` list that deliberately admits VS Code's webview origins — see [why html tabs are sandboxed](why-html-tabs-are-sandboxed.md). | The whole page is the sandbox: a strict CSP blocks every external host, with exactly one exception (stylesheets from fonts.googleapis.com, fonts from fonts.gstatic.com). Page-initiated downloads are inert for viewers — `<a download>` with data:/blob: included — and the rendered page must stay under 16MB with data: URIs counting. | tie | Both contain by cutting egress; the board contains only its `html` tabs while `ui`/`dag`/`table` stay trusted code, whereas an artifact contains everything the agent wrote. |
| **Theming — whose job the palette is** | The medium's, and more so than when this row was written. 21 tokens, each declared **twice** — a dark variant and a light one — with text pinned to WCAG AAA and no hex in any view; a test refuses a token declared in one variant and forgotten in the other. `ui` tones and `markup` colours are **names** from declared palettes, and a write naming a colour this board does not have warns. A project may patch the tokens in `.aboard/theme.json`; a viewer switches variant per browser; an embedder may hand the board a palette that is applied and written nowhere. The one leak is closed: the `html` frame **parses** the real stylesheet instead of carrying a copy, which had already lost five tokens once. | The author's, in three states: `data-theme="dark"`, `data-theme="light"`, and NOTHING stamped for the system default. Full light palette on bare `:root`; dark overrides under `@media (prefers-color-scheme: dark)` guarded `:root:not([data-theme="light"])` and again under `:root[data-theme="dark"]`; `body` needs an explicit token background or it borrows the host's ground. | board | The board owns the palette and checks the names an agent may use; an artifact must be legible in three theme states the authoring agent never sees. |
| **How much the agent authors per view** | For 14 of the 15 types, JSON only: a component tree against a declared 25-component catalog, or a node list, or rows. Zero HTML, zero CSS, zero JS, and the 54 declared controls are drawn by the renderer from its own declaration rather than by the caller. `html` is the single exception, reserved for when the INTERACTION is the point. | A complete HTML page every time — layout, tokens, both dark blocks, responsive rules, `overflow-x: auto` wrappers, and any interactivity — all inlined, because the CSP blocks external scripts and stylesheets. Plus a mandatory design pass before the file is written, and a capabilities pass before any runtime code. | different jobs | The board buys brevity by fixing the vocabulary at 15 renderers, 25 components and 54 controls; an artifact buys unbounded expression at the price of authoring the whole page. |
| **Navigating more than one view of the work** | A tab strip over the document's `tabs` array with an unread dot per changed tab, `?tab=` and `#tab=…&node=…` deep links, a `stack` type mounting several renderers in one tab (one nesting level), and `stateFrom`, so a kanban and a dag render **one** node set two ways with no duplicated data. | One page per URL. More than one view means the agent hand-codes tabs or sections into the page, or publishes a second artifact at a different file path (a different path claims a new URL). Which panel is open is per-viewer browser state at best. | board | `stateFrom` is the sharper difference: two renderers over one dataset with no duplicated data, which an artifact can only imitate by rendering both from one copy by hand. |
| **How a second agent session coexists** | Designed for it, and the gap this row originally recorded is closed. Several sessions write one document under distinct `--by` labels, and **five** guarantees are enforced by the server rather than by convention: an agent cannot delete a tab (a dropped one comes back as a removal request the human answers), clear a change marker, un-ack a chat message, clear another actor's read state, or write over the human's own `requests`. A `chat` tab with `@mention` is the channel, `aboard poke` releases every waiter, and the journal and the `trace` renderer say who did what. | Two sessions coexist only through publish conflicts and the tracked `baseVersion`. Nothing addresses a second session: no channel, no wake-up, no per-writer field guarantee — `action:"list"` finds artifacts but cannot talk to whoever is editing one. | board | Multi-session is the case the board was built for; for an artifact it is an unaddressed one. The fourth guarantee was written, correct and never called when this row was first drafted — which is worth keeping as the reason the guarantees now have tests rather than a comment. |
| **Discoverability of the medium's own capabilities** | The binary describes itself, with no server, no state file and no repository: `aboard capabilities` emits 15 types, every state field, every control **in toolbar order**, 25 `ui` components with their props, the declared palettes and theme tokens, 19 routes and 18 commands. `capsHash` fingerprints the described surface — not the source bytes — and `aboard capabilities --check` catches a committed skill reference that has drifted from it. The two false claims this row originally named are both fixed, and the mechanism that let them survive is the subject of [why the manifest is declared](why-the-manifest-is-declared.md). | The surface reaches the agent as documentation, never as a query: the tool description, the capabilities skill, and the typed contracts for the runtime. The page cannot ask what it may do — a null return from the runtime probe is the only signal, and its causes (not served / not granted / module failed) are indistinguishable by design. | board | One medium answers "what can you do" as a command; the other answers it as prose the agent has to have been given. |
| **Images, screenshots and binary assets** | The human pastes or drops; `POST /upload` takes up to 12 MiB, sniffs `png`/`jpeg`/`gif`/`webp` from **magic bytes** rather than the claimed name, excludes SVG deliberately because it can carry script, and slugs the name so it can never be used as a path. Files land in `uploads/` — project content, not the embedded assets — and `markup` annotates them with normalised 0–1 coordinates. `aboard uploads` is now the accounting for that directory, scanning each tab's **raw** state text for references, with `--prune --yes` the only way to delete anything. | Either a data: URI inlined against the 16MB page budget, or the tool-side asset store (`upload_asset` → `_blob/{id}`, `list_assets`, `read_asset`, `delete_asset`) — which requires the page to declare the `assets` capability, and `assets` was not in this user's roster. | board | The board takes a 12 MiB paste from the human and hands it straight to a renderer they can draw on, and now accounts for what that leaves behind. |
| **How a conclusion leaves the medium** | Deliberately, and by a separate code path that needs no server: `aboard export <tab>` renders a tab as markdown tuned for **promotion** — a gate decision leads with its reason and flags one added late, a vote option carries its comments. It now covers **twelve** types including `ui` and `stack`, resolving every binding the way the write checker does; `log`, `html` and `trace` are explicit non-cases with a sentence saying why, rather than falling through a default. See [how to promote a tab into a document](../how-to/promote-a-tab-into-a-document.md). | It usually doesn't leave. The artifact IS the deliverable at a URL; `action:"read"` pulls the HTML back for further editing, but there is no export-to-document step and no notion of demoting a published page once something else becomes the record. | different jobs | The board's whole posture assumes extraction — and the hole this row used to name, that its most-recommended type had no text form at all, is filled. |
| **Blocking on a human decision** | A first-class primitive. `aboard wait --for "answer ab128"` long-polls and the agent's own process blocks. Seven predicates, no more: `poke`, `change`, `tab <id>`, `answer <id>` (which requires a human writer, so another agent's edit can never satisfy it), `node <id>=<status>`, `rendered <id>` and `request [<tab>]`; an unknown one is refused up front with the list, exit 2, rather than blocking on something that will never fire. Default 10 minutes, hard cap an hour; exit 0 released, exit 3 timed out. A disabled notify button honestly means nobody is listening. | No such primitive. The agent publishes and the turn ends. Republish watching is unavailable in this session, comments are unreadable here, and the only thing that can react to a viewer's click is code running inside the page — never the agent's process. | board | This is where the board's reason for existing shows: a human's yes/no can pause an agent, which no artifact affordance can do. |
| **How the medium fails when the agent gets it wrong** | Loudly at the write, and — unlike when this row was written — **to both parties**. The server itself runs the checks over the tabs a write touched, so a browser write and a raw `POST` are checked too, and the strings ride the reply, the journal entry, the event stream, the tab's own banner and the `trace` renderer. `apply --check` runs them and posts nothing (exit 0); `apply --strict` refuses on any warning (exit 1). Warning-not-refusing stays the default, because a declaration can legitimately lag its renderer. | At publish, or at the call. The platform rejects a bad publish; in-page calls reject with stable codes (`invalid_content`, `too_large`, `not_writer`, `conflict`, `read_only_path`, `not_declared`) and the doctrine is to branch on `.code`, never message text, retrying only `upstream_error` once. Nothing pre-checks the page for legibility. | tie | Half of this gap closed and the other half cannot: both media can still ship something legal and unreadable, and both close *that* only with a human looking at a picture. |
| **Setup, portability, and what it costs to exist at all** | A Go binary with an embedded web tree: copy it into an empty directory and every renderer, gesture and capability answer travels with it. It must be **running** for anything write- or wait-shaped — `apply`, `requests done`, `wait`, `poke`, `watch`, `log` (though `apply --check` and `apply --strict` validate a document with nothing up). What needs no server has grown to twelve of the eighteen commands: `capabilities`, `export`, `init`, `recipes`, `version`, `boards` and a degraded `status`, plus `journal`, `history`, `requests`, `rendered` and `uploads`, which read the document and the sidecar files straight off disk — `journal` and `history` say so on their first line, since a reader has to know the answer is the file rather than the server. | Nothing to install and no process to keep alive; a publish is a stateless tool call. But the medium exists only where the platform serves it, and what it serves varies per user and per session: this session cannot watch republishes, cannot read comments, and cannot declare `assets` or `permissions`. | different jobs | The board's cost is a process on the user's machine; the artifact's cost is a dependency on whatever the platform happens to serve that viewer. |
| **Append-heavy or streaming output** | Kept deliberately out of the shared document. `<cmd> 2>&1 \| aboard log ab126` posts stdin line by line and appends to a sidecar file (8 MiB with one rotation), precisely because the document is rewritten whole on every write. The `log` renderer polls for the tail every couple of seconds, and only while its tab is visible. | No append channel exists. Every change to a published page is a whole-document publish that mints a full immutable version, which is why the contract says debounce into one call and batch all changed files — a streaming log would be one version per line. | board | Both media rewrite the whole document per write; only the board built a side channel for the case where that shape is wrong. |

## What the matrix is actually saying

Three findings survive re-verification, and they are the reason the page is worth
keeping rather than a curiosity:

1. **They are not competitors.** An artifact is a shareable published record; the board
   is a deliberately unshareable working channel whose output has to be rewritten into
   something else before it counts. Asking which is better is asking which of "a
   whiteboard" and "a memo" is better.
2. **Only the board can block an agent on a human.** `aboard wait` parks the agent's own
   process until a human writes the tab, and the predicate that matters checks that the
   writer *was* a human, so another agent cannot satisfy it. Publishing an artifact ends
   the turn; nothing can wake that session up again.
3. **Only an artifact can act on a click.** Board buttons record intents, by closed
   decision, because the server has no authentication and a stray click has to be
   harmless. An artifact page runs the viewer's own connectors with the viewer's own
   credentials. Both media agree on exactly one prohibition, and it is the same one:
   neither may start an agent session from the page.

## Rejected in review — kept so nobody re-proposes them

Seven proposals died while this comparison was being turned into work. They are recorded
the way this project records every closed decision: with the reason, so that a future
session with a good idea finds the argument before re-deriving the proposal.

- **A one-tab write endpoint.** Superseded by making `apply` merge on a `409`, which
  solves the same pain with an algorithm already proven in the browser. And the location
  is the worst on the board: the write path is the single choke point off which every tab
  guarantee, every journal record and every wait predicate hangs, so a subtle bug there is
  the most expensive kind there is. Do not re-propose it unless the merge has shipped
  *and* two sessions still collide in practice. It has shipped; nobody has collided —
  see [what a write costs](how-aboard-runs.md#per-tab-writes-are-not-going-to-be-built)
  for the numeric version of the same refusal.
- **`tab.owner`, a fifth guarantee.** Rejected on ORDER, not on merit: at the time,
  guarantee four existed as a correct function with zero call sites, and the actor name is
  unauthenticated free text. Adding another lock to a door with no frame invites it to be
  read as access control, which it would not be. The four were fixed and a fifth arrived
  later for a different reason — the human's own `requests` — so if this is ever
  reconsidered it starts from a different place: the guarantees now have tests, and the
  question is whether ownership is a thing an unauthenticated server can honestly claim
  to enforce. The answer is still no.
- **A byte-budget warning at the write.** A real board was 0.9 % of the body cap when
  this was measured, against a cap that has since been raised from 8 MiB to 32 MiB, so
  the ratio only got smaller. A threshold calibrated to fire on that is the muted check
  that killed the DOM sweep; one calibrated to fire on real shape mistakes cannot be
  derived from a single data point. The four size caps — body, upload, log file, journal
  rotation — are written down in [the HTTP API reference](../reference/http-api.md), not
  in the manifest, and reporting the document's size in `status` is a line in whatever
  ships next, not a feature of its own.
- **Version labels on writes** — merged into write labels in the journal rather than
  rejected. Same mechanism, same field, same flag: `apply --label` rides the envelope
  beside `__by` and `__base` and lands on the journal entry.
- **A self-contained tab snapshot with an import map.** Merged into "save a tab as a
  standalone page", keeping the cheaper browser-clone mechanism. The dropped half —
  rewriting relative view imports to bare specifiers for a data:-URI import map, and
  stubbing the live endpoints two renderers poll — is the expensive part, and it buys
  interactivity in a saved file nobody asked for.
- **Control coverage from `data-gesture`.** Merged into mount receipts: both are a
  delegated browser listener or a post-mount sweep writing to one sidecar, read back by
  one command. Two entries describing one piece of plumbing read as duplication to the
  person being asked to choose between them.
- **A browser "Restore" button on tab history.** Deliberately out of scope, and it stayed
  out. Restoring a state from before a gate request silently *unasks* a question the human
  already answered. History emits a document and prints the command; the write is the
  caller's, made with the document in front of them.

## See also

- [Why a local, non-authoritative channel](why-a-local-non-authoritative-channel.md) — the three tiers, and how a conclusion is promoted out of the middle one.
- [How to promote a board tab into a document](../how-to/promote-a-tab-into-a-document.md) — the mechanics of the extraction this whole posture assumes.
- [Why nothing in the UI starts a session](why-nothing-in-the-ui-starts-a-session.md) — the one prohibition both media share.
- [Why the manifest is declared](why-the-manifest-is-declared.md) — why "the binary describes itself" is a claim that can be checked.

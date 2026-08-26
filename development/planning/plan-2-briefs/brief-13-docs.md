# Brief 13 — the user-facing docs: fill the Diátaxis quadrants where a user actually looks

Read `COMMON.md` first. The human's ask, 2026-08-26: *"we need a docs folder with diataxis
user-facing docs … the shared-boards question is a good example of something that could go
there, also how it is run, or that it could be used with a vscode extension; the README.md must
point to an index of the diataxis docs."* `docs/` already exists with the four quadrants, an
index (`docs/README.md`), per-quadrant READMEs and `make docs-check`. So this item is an AUDIT
and a GAP FILL, written for a USER who has never read `CLAUDE.md` or `development/` — not for
the agent that built it. Every page: run every command you write; no links into the source tree
(name files in backticks); keep `make docs-check` green; link new pages from the quadrant README
AND the index.

## Gaps to fill (each a page, in the right quadrant)

1. **How to run a second board in one project** (how-to; currently a "planned guide"): `init
   --name`, `serve --name`, what gets its own file and port (state, instance record, port) and
   **what is shared** — journal, sidecar logs, mount receipts, uploads, recipes — with the
   consequences stated plainly (journal/history/watch show both boards; tab ids are per board so
   an id in the journal can belong to either; `uploads --prune` only sees one board's tabs). Read
   `pkg/aboard/layout.go` for the truth, not the old README bullet. When a side investigation
   deserves its own board, and when a tab is enough.
2. **How to put aboard behind a reverse proxy / a path prefix** (how-to; planned): `serve
   --base-path`, what the injected base covers, `/health` reporting `base`, the ordering trap
   (a prefixed board answers `/health` only at `<base>/health`), the Host allow-list and the
   same-origin rule for writes (a proxy must forward a loopback `Host`), and what to check in the
   browser's network panel.
3. **How to promote a board tab into a document** (how-to; planned): `aboard export` (markdown,
   csv, the `ui` outline), what to rewrite rather than paste, demoting the tab afterwards, the
   three tiers from the explanation page.
4. **How to use the VS Code extension** (how-to, new page beside `run-in-vscode.md`, which is
   about docking the Simple Browser): what the extension is (a viewer: tree of tabs in the
   sidebar, the board in a webview panel, human-only actions), where it lives
   (`/home/diegos/_dev/exoport/aboard_vscode` — say "the aboard-vscode repository"), how it finds
   a board (`.aboard/run/instance.json` up the tree, `/health`), what it needs from the board
   (`?chrome=notabs`, the `active` message — both landed), how to build it (`npm ci && npm run
   build && npm test`) and that it is **not yet verified in a real VS Code** (M6 is pending) —
   say that plainly, users must not read the page as a promise. Do not duplicate its README;
   link the contract by name.
5. **How aboard runs** (explanation or reference — pick by the Diátaxis compass and say why):
   one page a user reads to understand the moving parts — one binary, `init` seeds `.aboard/`,
   `serve` binds a loopback port derived from the project root (41000 + hash % 8000, walking up
   to 24 ports past a stranger, refusing a duplicate of its own board), writes an instance
   record, serves the embedded UI, watches the state file; agents write through `apply`
   (compare-and-set on `rev`), humans through the browser; the journal is the undo; the two
   identities (`aboard`, `ape aboard`); what is machine-local under `run/`. Cross-link `layout.md`,
   `state-file.md`, `http-api.md` rather than repeating them. Include "what if the port is taken
   by another service" as a stated behaviour.
6. **`aboard boards`** (item 12 lands it): make sure `cli.md` (generated) and a how-to sentence
   cover it, including the Linux-only message.
7. **Audit** every existing page against its quadrant (tutorial teaches, how-to solves, reference
   lists, explanation explains) and against the code as it is NOW after plan-2 (revision token,
   origin/host refusals, `--force`, `--check/--strict/--label`, `history`, `rendered`, `uploads`,
   warnings on the write, the toast, `?chrome=`, the journal `schema: 2`). Fix every sentence that
   is no longer true; list each in the report. `docs/reference/README.md` and the index name every
   page; the how-to README's "Planned guides" list is emptied (or holds only what is still
   planned, with a reason).
8. `README.md`: the Documentation section points at `docs/README.md` as THE index (it does — keep
   it, and make the section's short list match the index: tutorial, the four quadrants, the two
   or three pages a newcomer needs first). The README must mention the VS Code extension and
   `boards` in one line each.

## Done when

`make docs-check` 0; every new page reachable from the index and its quadrant README; every
command in every new page executed once (a scratch project under the scratchpad, server
detached, stopped afterwards); the report lists each stale sentence found in the audit and its
fix; `make ci-local` once (docs-check is in it) and `make e2e` once (nothing here should touch
it, but say so).

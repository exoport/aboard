# Brief 18 — the human's notes to the agent on a tab, and per-tab scroll memory

Read `COMMON.md` first. Two things the human asked for on 2026-08-26 after using the board
through the VS Code panel. Items 1–16 are landed; the suite is `make e2e`.

## 1. "THIS TAB IS FOR" and the human's notes are two different things

Today a tab has one `note`: the purpose strip ("THIS TAB IS FOR …"), documented as "what the
tab is FOR, in the human's words". The human's decision: the purpose strip is a brief statement
of the tab's purpose, written by the AGENT that opened the tab (the human may still edit it —
keep the Edit button). Separately, the human needs to leave NOTES FOR THE AGENT on a tab: a
request to fix/correct/change something on the board, which a waiting agent picks up (or the
next agent asked to check the board processes). The human can add several, delete any of them —
even before an agent acted — and must get FEEDBACK that the agent read a note and acted:
strike-through, a check mark, who and when, optionally a one-line reply.

Design it as data, with the server enforcing the asymmetry (the same posture as the four
guarantees — read `docs/explanation/why-four-guarantees-are-server-enforced.md`):
- Schema: tab gains `requests: [{ id, at, by: "human", text, done?: { by, at, note? } }]`
  (the name is yours to settle — `requests`, `asks`, `notes` are candidates; pick the one that
  reads right in `aboard status` output and in the skill, say why). Ids from the board allocator.
- Guarantee 5, server-enforced in `tabs.go`: an agent write may only ADD a `done` stamp to an
  existing request (with its own `by`), never create, edit, or delete one; the human may do all
  of those. An agent write that drops or alters a request has it restored, like `touched`. A
  `done` stamp is never cleared by an agent; the human deleting the whole request is how it goes
  away. Tests for each direction; the explanation page gains the fifth guarantee.
- The browser: a strip under the purpose strip — "NOTES FOR THE AGENT" — listing each request
  with its time; done ones struck through with "✓ agent-1 · 14:02 · <reply>"; a ✕ per note
  (human only, no confirm needed — it is the human's own note, and never a native dialog); an
  add box (Enter adds; tokens only; goes through `button()` for chrome). Per-viewer nothing.
  The change banner is unaffected. Show a small count on the tab button when a tab has pending
  requests (the same discipline as the `touched` dot, a different glyph).
- The CLI and the wait channel: `aboard requests [--tab <id>]` lists pending (and, with
  `--all`, done) requests across the board, oldest first, naming the tab; `aboard requests done
  <request-id> --by agent-1 [--note "…"]` stamps one (a thin `apply`). `aboard wait --for
  "request"` (and `request <tab>`) fires when a pending request exists or appears. `aboard
  status` prints the pending count. Declare the command and predicate in `commands.go`/`caps.go`;
  `make caps` (capsHash moves — say so); `make docs-cli`.
- Docs: `state-file.md` (the field, the guarantee), `http-api.md` (if the shape reaches a route
  — `/aboard.json` carries it; `/wait` predicate), the skill (SKILL.md: read `aboard requests`
  before acting on a board; the recipe `react-to-their-edits.md` gains the requests flow), the
  purpose-note wording everywhere it says "in the human's words" → "the agent's brief statement
  of what the tab is for; the human may edit it", `CLAUDE.md`, `CHANGELOG.md`, the example board
  gets one done request and one pending one on a tab so the gallery shows both states.
- e2e: add a request in the browser, an agent write that tries to delete it (restored), an agent
  `done` stamp renders struck through, the human deletes it, the tab-button count; Go tests for
  the guarantee and the CLI.

## 2. The board remembers the scroll position per tab

Like a `ui` component's open panel (`views/ui.js`, `sessionStorage` `aboard.panel.<key>`), the
board must remember each tab's scroll position per VIEWER: save the view container's scroll
offsets when leaving a tab (and on scroll, debounced) under `aboard.scroll.<tab>` in
`sessionStorage`, restore after the renderer mounts on activation, and survive the self-reload.
Never in the state file. Guard storage with the existing try/catch helpers. e2e test: scroll
the checklist-shaped stack tab, switch away and back, offset preserved; reload → preserved.

## Done when

Both shipped with tests; guarantee 5 documented and enforced; ladder green (`make lint`,
`make fmt-check`, `make pre-commit`, `make e2e` once per tool call, `make ci-local` once);
`capsHash` reported. Never touch the human's boards (47781, 44917); scratch projects only.

# Brief 11 — the human's answers to plan-2 §10, applied

Read `COMMON.md` first. On 2026-08-26 the human answered four of plan-2 §10's open questions.
Each answer is a decision; do not re-argue them. Apply all four, each with its test, in one pass.

## 1. `bb372 boards` — DROPPED

Decision: not built, not deferred — dropped. `aboard status` per project answers "is one running
here", and each project's `.aboard/run/instance.json` / `GET /health` already say WHICH binary
serves it (`app: "aboard"` or `"ape-aboard"`), so a process scan would buy only cross-project
discovery, and only on Linux (`/proc/<pid>/cmdline` does carry the full argv — `comm` does not).
Record it as CLOSED in: plan-2 §10 (strike the entry, keep the reason, one line on `/proc`
`cmdline` vs `comm` so nobody re-proposes the scan); `handoff-13-features.md` item 11 (PARKED →
DROPPED with the reason); `CLAUDE.md`'s decisions list gets one bullet ("no `boards` command —
closed, not deferred", same shape as the diff-renderer entry); `development/README.md` if it
mentions it. No code.

## 2. The example board's prose names "the agent", not "Claude"

Decision: rename the seven strings in `pkg/aboard/example/aboard.json` — dag node titles `bb5`
and `bb149` ("Claude reads and reacts"), dag node notes `bb10` and `bb11`, the form intro `bb46`
("Claude asks, you answer here"), the markup image caption `bb152`, the markup region note
`bb153` — to say "the agent". Keep every id, every other byte, and the file's formatting (it is a
fixture the Go tests and `init --example` read; `TestTheWrittenDocumentIsByteIdenticalToTheOldEncoder`
and the e2e fixture overlay depend on its shape — run them). Grep the whole tree for any test,
doc or e2e assertion that spells one of those seven strings and update it. `views/chat.js` keeps
`claude` as a historical ACTOR name — untouched. Code comments naming Claude — untouched.
Record the disposition in plan-2 §10 (entry closed, with the commit) and one CHANGELOG line.

## 3. The notify button's acknowledgement is a toast

Decision: option (b) — the button keeps telling the truth about live state (it repaints from the
SSE `waiters` frame as today), and the acknowledgement of the press moves to a transient notice
that the repaint cannot touch: the same mechanism as the "Saved" flash (find it in
`pkg/aboard/web/aboard.html` — `views/inline.js`'s saved flash or the shell's notice stack; reuse,
do not invent a third). Text: "notified N sessions" / "no session was waiting" from the `/poke`
reply. Tokens only. `test/e2e/notify_test.go` currently asserts the poke and NOT the message
(with a comment saying why); make it assert the toast text now, and remove the comment that
records the limitation. Plan-2 §10 entry closed; CHANGELOG line; the skill/`http-api.md` only if
they describe the button's feedback.

## 4. `JournalEntry.Before` carries the whole tab

Decision: option (a). Today `Before` holds only a tab's `state`, so `apply`'s 409 merge cannot
classify a tab whose `name`/`note`/`type` moved on the board and refuses by name. Widen the record:
- `JournalEntry` gains a `schema` (or `v`) integer — pick the name, say why — stamped `2` on every
  new entry; `Before` becomes the whole tab (`id`, `name`, `type`, `note`, `state`, and the
  other tab fields the document carries — read `docTab`/the tab shape in `document.go`).
- Every READER of the journal (`history`, `journal`/`watch` printers, `tabsMovedSince` and the
  merge in `merge.go`, `trace.js`, the `/history` route, `history --at N` restore) handles BOTH
  shapes: an entry with no `schema` (or `1`) is the old one whose `Before` is a bare state; a
  `2` entry's `Before` is a tab. A rotated `journal.jsonl.1` may hold old entries while the live
  file holds new ones — the read path must not care which file an entry came from.
- The merge now classifies a foreign rename/note/type change: if only the OTHER tab moved, the
  write merges; if the same tab's name/note/type moved on both sides, it is a collision named as
  such. The `ErrCollision` wording from item 6 ("cannot attribute") changes accordingly.
- Tests: an old-shape entry read by every reader (write a fixture line by hand); a foreign rename
  that now merges (fails before: `apply` refused); a same-tab rename on both sides that still
  stops; `history --at N | apply` round trip on a v2 entry restores name AND state; rotation with
  mixed generations. `docs/reference/http-api.md` (journal entry shape, `/history` shape),
  `docs/reference/state-file.md` if it documents the journal, the skill, CHANGELOG. Plan-2 §10
  entry closed.

## Also

- The remote: both repos have `origin`; nothing is pushed; do not push.
- `make lint`/`make fmt-check`/`make pre-commit` are the gates now (item 10); the ladder in
  COMMON.md is current.

## Done when

All four dispositions recorded and their code landed with tests; ladder green including
`make e2e` (once), `make pre-commit`, `make ci-local` (once, logged); plan-2 §10 shows only what
is still genuinely the human's (remote/tag, Go 1.27, the ape mount, M6, the phase-E judgement
calls, `aboard <cmd>` in prose).

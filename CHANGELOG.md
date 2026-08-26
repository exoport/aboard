# CHANGELOG

## Unreleased

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

- **feat: aboard, ported from the `board` spike** — a single Go binary serving a
  shared visual board for a human and one or more agent sessions, with the whole
  UI embedded. Tabs are data, not code: an agent opens one for whatever it needs
  to show, and both sides read and write the same document on disk.

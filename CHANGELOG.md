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
- **`capsHash` moved `9facfc76` → `6ff337ed`**: the command table declares
  subcommands, so `recipes list` and `recipes show` and their flags are part of the
  described surface and appear in the generated reference.

- **feat: aboard, ported from the `board` spike** — a single Go binary serving a
  shared visual board for a human and one or more agent sessions, with the whole
  UI embedded. Tabs are data, not code: an agent opens one for whatever it needs
  to show, and both sides read and write the same document on disk.

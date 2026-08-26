# Brief 14 — a named board owns its runtime files, and a project has one live board however it was bound

Read `COMMON.md` first. Two defects the docs audit (item 13) reproduced and documented; the human
said "fix them and update the documentation". Both are behaviour changes with tests.

## 1. One live board per (project, name), regardless of `--port`

Today `refuseDuplicate` is per PORT: with a healthy board on its derived port, `aboard serve
--port <any free port>` starts a SECOND server on the same state file, rewrites
`run/instance.json`, and on exit removes it — `status` then says "no board" while the first
still serves. Fix in `server.go`'s `listen`/`refuseDuplicate`: BEFORE binding anything, read this
project's instance record for this name; if it exists and answers `/health` with the same
`project` and `name`, refuse with its URL and pid — whatever port was asked for. A record that
does not answer is stale: proceed, and overwrite it. Keep the per-port probe on the derived walk
(a stranger on the port is still walked past). Tests: explicit `--port` with a live board →
refused naming the live URL; a stale record with a free explicit port → starts and rewrites the
record; the derived path unchanged (existing tests). Docs: `http-api.md`, `layout.md`'s `--port`
paragraph, `why-writes-are-serialised.md`, `CLAUDE.md` hard rule 2, the second-board how-to and
`how-aboard-runs.md` — every sentence item 13 wrote about this hole becomes past tense.

## 2. A named board owns its journal, logs and receipts

`Root` is name-aware only for `StateFile`/`InstanceFile`. Make `JournalFile(name)`,
`LogsDir(name)`, `RenderedFile(name)` name-aware the same way: default unchanged
(`journal.jsonl`, `logs/`, `rendered.json`), named → `journal.<name>.jsonl` (+ `.1` rotation),
`logs/<name>/`, `rendered.<name>.json`. Thread `name` through the server, `journal.go`, `logs.go`,
`receipts.go`, `history.go`, `merge.go` (the 409 merge reads the journal), `wait.go` (the
`rendered` predicate), and every CLI reader (`journal`, `watch`, `history`, `rendered`, `log` —
they already resolve `--name`/`ABOARD_NAME`; verify each reads the right file). `uploads/` and
`recipes/` stay shared — say why where `--name` is documented. Migration: none is possible for
entries already mixed into the default journal; say so in the how-to and CHANGELOG.
Tests: a named board's write lands in `journal.<name>.jsonl` and not the default; `history`,
`rendered`, `log` on a named board read their own files; the default board is byte-for-byte
unaffected (existing tests); `boards` still lists both.

## 3. The restore hint names the board

`pkg/aboard/history.go`'s listing prints `aboard history <id> --at N | aboard apply --by agent-1`
with no `--name`; on a named board that reads and writes the DEFAULT board. Carry `--name <name>`
into both halves of the hint when the board is named. Test on the printed string.

## 4. `uploads` sees every board in the project

`aboard uploads` scans one board's tabs; on a project with two boards `--prune --yes` deletes an
image the other board's tab references. The accounting scans EVERY state file in the project
(`aboard.json` and every `aboard.<name>.json` — a `Root.StateFiles()` helper in `layout.go`) and
the listing says which board(s) reference each file. Test: an image referenced only by the named
board is not an orphan from the default board's `uploads`.

## Docs

`docs/reference/layout.md` (the sharing table becomes the ownership table), `docs/how-to/
run-a-second-board.md` (rewrite the consequences section — they are gone, except uploads/recipes
shared by design), `docs/reference/http-api.md`, `docs/explanation/how-aboard-runs.md`,
`CLAUDE.md`, the skill's `multi-session.md`, `CHANGELOG.md`. The two boards the human runs
(`/home/diegos/_dev/exoport/aboard` and `/home/diegos/_dev/ai/borrar`, both default-named) are
NOT touched — never `pkill`; use scratch projects.

## Done when

All four with failing-first tests; ladder green (`make lint`, `make fmt-check`, `make pre-commit`,
`make e2e` once, `make ci-local` once); `capsHash` reported if it moved; every doc above true.

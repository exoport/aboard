# Brief 2 — behaviour mediums (plan-2 item 2)

Read `COMMON.md` first. Source: `development/research/review-d6c2f84-20260825.md`, the **Medium**
list — the BEHAVIOUR half, thirteen items, listed below with the file each anchors to. Item 1
(the two races) has already landed on HEAD; build on it, do not redo it.

## Scope — all thirteen, each with a test that fails before and passes after

| # | change | anchor |
|---|---|---|
| 1 | `apply` with no `updatedAt` in the submitted document is REFUSED (exit 2, a one-line reason naming `--force`) unless `--force` is given; with `--force` it writes without compare-and-set and says so on stderr. Declare `--force` in `commands.go`. | `client.go` `__base` |
| 2 | The CAS token becomes a **monotonic revision** (or the content hash `stateSignature` already computes — pick one, say why; a revision integer stamped by the server next to `updatedAt`/`version` is the orchestrator's preference because it is comparable and cheap to print). `__base` carries it; the browser (`aboard.html`) and `apply` (`client.go`) send it; `updatedAt` stays for humans. A stale `__base` with an equal millisecond must now be refused — that is the test. Keep reading an old-style `__base` (a timestamp string) for one version with a warning, OR refuse it with a clear message — say which and why. | `server.go` `postState` |
| 3 | An agent write carries `pendingRemoval` forward exactly as `touched` is carried: only a human write may clear it. | `tabs.go` `reconcileTabs` |
| 4 | `POST /aboard.json` (and every other mutating route — audit `/poke`, `/upload`, `/log`) refuses a request whose `Sec-Fetch-Site` is `cross-site` or whose `Origin` is present and not the server's own origin; 403 with a body naming the reason. Same-origin, `same-site`, `none`, and no-`Origin` (curl, `apply`) pass. | `server.go` `route` |
| 5 | A `Host` allow-list at the top of `route`: `localhost`, `127.0.0.1`, `[::1]`, each with or without a port; anything else 421 or 403 (say which and why). `/health` included. | `server.go` `route` |
| 6 | `--port`/`PORT` no longer bypasses duplicate detection: `probeOccupant` runs before the explicit-port branch. | `server.go` `listen` |
| 7 | `FindRoot` resolves symlinks (`filepath.EvalSymlinks`) on the found directory, so one project has one port. | `layout.go` |
| 8 | `--name`/`ABOARD_NAME` validated against `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`; refused with exit 2 before any path is joined. | `layout.go` `boardName` |
| 9 | `aboard journal` falls back to the on-disk `journal.jsonl` on transport failure (dial refused with a stale instance file), not only when the instance file is unreadable; say on stderr that it read the file. | `journal.go` / `cli/journal.go` |
| 10 | `aboard init` validates `--output-format` BEFORE writing anything; exit 2, nothing on disk. | `cli/init.go` |
| 11 | cobra argument-count errors exit 2 (wrap the `Args` validators in the typed usage error). | `cli/root.go`, `exit.go` |
| 12 | yaml output carries paired tags on every output struct (keys identical to JSON, `omitempty` honoured); `recipes list --output-format yaml` keeps `scope`, `path`, `shadowedBy` and the parse `error`. A test marshals each output struct both ways and compares key sets. | `cli/format.go`, `cli/recipes.go` |
| 13 | Browser: an SSE-triggered reload routes through `mergeOntoFresh()` and re-arms the save debounce, so an edit inside the 250 ms window survives a foreign write; and `baseline` advances after a 409 merge (`baseline = snapshot(merged)` where `doc = merged`; never overwrite an existing stash entry). **Real browser tests come in item 4**; for now add a DOM-level probe in `pkg/aboard/web/test/smoke.html` / `test/smoke.sh` that at least exercises the code path (e.g. drive `load()` with a synthetic event and assert `baseline` moved and the textarea kept its text). | `web/aboard.html` `load`, `mergeOntoFresh` |

## Documentation the change must carry

- `docs/reference/http-api.md`: the revision token, the origin/host refusals with their status
  codes, the `--force` semantics of `apply`. `docs/reference/cli.md` regenerated (`make docs-cli`).
- The skill (`.claude/skills/aboard/`): `apply` needs the document you READ (with its revision)
  as the base; `--force` exists and when it is wrong; a 409 means re-read.
- `CLAUDE.md`'s hard rule 1 paragraph, if the wording about `updatedAt` as the base is now false.
- `make caps` if `commands.go` changed (it will: `--force`), and say the `capsHash` moved.
- In the review file, append `— fixed: <one line>` to each of the thirteen Medium entries you
  closed. Leave the coverage/docs mediums (parity test, `reconcileNextID`, `writeWarnings`,
  `make smoke`, render counts, `bb71` fixture, the `go install` docs) and every Low untouched —
  they are item 3.

## Done when

Every item above has its failing-then-passing test; the docs and skill describe the new
behaviour; the ladder is green including `make smoke` on a scratch project; the thirteen review
entries carry their disposition.

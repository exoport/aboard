# Brief 6c — features, the new-routes cluster (plan-2 item 6, part c)

Read `COMMON.md` first. Source: `development/handoffs/handoff-13-features.md`, items
**3 (`bb363` per-tab history and restore), 6 (`bb366` `apply` merges instead of failing),
8 (`bb368` mount receipts from the browser), 9 (`bb369` uploads accounting and prune)**.
Item **11 (`bb372` `boards`) is GATED on the human — do not build it; do not create a registry
file.** Items 1–5 of plan-2 have landed: writes serialised, CAS token a revision, document
cached in memory with ETag (item 5) — read `server.go`, `journal.go` as they are NOW.

**You are working in a git worktree** whose path the orchestrator gave you in the prompt. Work
ONLY there. Two sibling worktrees implement other feature clusters; a merge agent squash-merges
all three into `main`. Keep changes to the files your features need; do not reformat unrelated
code; `make caps` (new commands move `capsHash` — expected); list every file touched. Scratch
projects under the scratchpad with your cluster's name.

## Scope

- `bb363`: `GET /history?tab=<id>` over the journal tail (both generations after item 3's
  rotation fix); `aboard history <tab> [--limit N] [--at N]`; `--at N` prints a document
  `apply` accepts — merged onto a freshly read full document so a restore can never read as
  "delete every other tab"; the listing names who wrote each entry and says where history ends.
  Declared route + command; the change banner in `aboard.html` links to what the tab said
  before (a `#history` view or a request to the agent — keep it to what the handoff asks).
- `bb366`: on 409, `applyStdin` re-reads, consults `/journal` since its base revision for
  which tabs moved, re-applies its own tabs where the server did not touch them, retries ONCE;
  a genuine same-tab collision names the tab and stops. Tests with a second actor.
- `bb368`: `aboard.html` posts unknown-component markers and fired control ids after every
  mount to a sidecar endpoint (`POST /rendered`? — declare it) stored under `.aboard/run/`,
  never in the state file; `aboard rendered <tab>` prints them with the two honest limits in
  its own output; an optional `rendered <id>` wait predicate. This is NOT the DOM sweep — it
  records ids already declared in the specs.
- `bb369`: `aboard uploads` lists every file under `.aboard/uploads/` with size and the tabs
  whose RAW state text mentions it; `--prune` prints what it would remove and refuses without
  `--yes`; `GET /uploads` added to `declaredRoutes`.
- Go tests for each; e2e tests for the receipts sweep and the banner link (the suite is
  `make e2e`, `test/e2e/`; one run per tool call, timeout 600000).
- Docs: `http-api.md`, `cli.md` regenerated, the skill, the handoff sections marked done; the
  plan-1 CLI grammar table gains `history`, `rendered`, `uploads` (and NOT `boards`).

## Done when

Four features shipped with tests and docs; `bb372` explicitly parked in the handoff with a
pointer to plan-2 §10; ladder green in your worktree (`make e2e` included); report lists files
touched.

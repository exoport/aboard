# Brief 4 — the end-to-end browser suite (plan-2 item 4)

Read `COMMON.md` first. Source: `development/handoffs/handoff-e2e-browser-suite.md` — the
decision (playwright-go, `//go:build e2e`, `make e2e`, local only, never CI) is MADE; do not
re-research drivers. Items 1–3 have landed on HEAD; in particular item 2 changed the CAS token
to a revision and item 3 gave `bb71` cards.

## Scope, in this order (the handoff's "Deliverable" §1–§5; §6 visual regression is NOT in scope)

1. **Harness** `test/e2e/` (package `e2e`, `//go:build e2e`): `TestMain` installs the driver
   if absent (`playwright.Install`, browser pinned by the module version — chromium only), starts
   the engine IN-PROCESS (`aboard.Serve` or whatever the engine exposes — read `pkg/aboard/aboard.go`
   and `server.go`) on an explicit free port with a `t.TempDir()` root seeded from the embedded
   example PLUS an interaction fixture (kanban with cards in three columns, a dag with a
   reparentable node, a table with rows, the sketch pad, a gate with a pending row, a notes tab).
   Put the fixture under `test/e2e/testdata/` (or extend the example — say which and why; the
   example is what `init --example` seeds, so a test-only fixture must not leak into it unless it
   improves the example). SSE ON. Helpers: `control(page, id)` → `[data-gesture="<id>"]`,
   `tab(page, id)`, a dialog handler, `drag(page, from, to)` (pointer-capture composition and
   `DragTo` for HTML5 DnD), `apply(doc, by)` writing through HTTP as a second actor. On failure:
   trace + screenshot + `aboard.json` snapshot into `.aboard/run/e2e/<test>/` of the temp root,
   AND copied to a stable path the human can find (`<repo>/.aboard/run/e2e/` — gitignored).
2. **First tests** exactly as the handoff lists them, in its order: bridge write half in a tab
   and in a stack block; the 250 ms debounce vs SSE reload; the double 409 ("Restore mine"
   returns the human's text); `touched` Dismiss sends `__by: human`; pending-removal Keep/Remove;
   tab-strip `prompt()` rename; `confirm()` remove. Then the gesture surface, renderer by
   renderer: kanban drag persists; read-only kanban has no drag handles with cards present; dag
   drag-reparent, `<dialog>` delete-confirm, dblclick rename, pan, wheel zoom; markup region +
   resize + clear-marks modal; table sort + type-and-save; gate allow with reason + undo; ui
   button → intent recorded; the notify button releases a real `aboard wait` (run the CLI as a
   subprocess or call the client package); SSE: a second actor's write appears without a reload;
   `--dev` CSS re-link keeps scroll/selection; a UI-signature change reloads the page.
   **Every renderer's declared gestures (`views/*.spec.json` `gestures`) must have at least one
   test** — write a Go test that reads the specs and asserts each renderer type appears in a
   `coveredGestures` map the tests register into, so a new renderer without a test fails.
3. **`make e2e`** depends on `build`; `E2E_HEADED=1`, `E2E_TRACE=always`; pin the
   `cmd/playwright` installer in `.bingo` like the other tools (or document why the Go module's
   `playwright.Install` makes that unnecessary — say which). Never the snap chromium.
   The dependency line in `CLAUDE.md` ("cobra + pflag + yaml.v3 and their closure") must be
   updated to state that `playwright-go` is a TEST-ONLY dependency behind the `e2e` tag and does
   not enter the binary — verify with `go version -m ./aboard` or `go list -deps`.
4. **Fallback flag** `--disable-features=IsolateSandboxedIframes` documented in the harness (and
   used only if the out-of-process frame path proves flaky — record what you observed).
5. **Retire `test/smoke.sh`**: port the three browser-free static checks (button helper,
   declared-control usage, plain-button advisory) into Go tests FIRST; then for every check
   `smoke.sh` makes, either point at its `e2e` equivalent in a table in the how-to doc, or write
   the missing test. Only when the table is complete: delete `test/smoke.sh`,
   `pkg/aboard/web/test/smoke.html` (if nothing else uses it — check `make shot` and the embed),
   the `make smoke` target, and the `node` dependency; update CLAUDE.md, the skill, `docs/`,
   `Makefile` help, `.github/` if it mentions smoke. `test/shot.sh` stays.
6. `docs/how-to/run-the-browser-suite.md` (reachable from `docs/README.md`), the DevTools MCP
   named in the skill's multi-session reference as the exploratory complement (one paragraph).
7. **Delete the sentence "provable only by a human click"** (and its variants) from every
   document in the repo — grep for `human click`, `provable only`, `only a human` — now that the
   bridge write half is tested.

## Notes

- First `playwright.Install` downloads ~330 MB into `~/.cache/ms-playwright`; do it once, early,
  in the background of other work, and report the version installed.
- `make e2e` is ~1–3 min: ONE run per tool call, `timeout` at 600000 for that call, output to
  a file, read the whole file. Nothing else runs a server during it (the harness owns its own).
- Flakiness is a defect: any test that fails once in three runs is fixed or quarantined with a
  `t.Skip` naming the reason, never retried into green.

## Done when

`make e2e` green from a clean checkout with no human present (run it twice in two calls and
both are green); every renderer's declared gestures have at least one test and the coverage
test proves it; `smoke.sh` is gone with the equivalence table in the how-to; the "human click"
sentence is gone; the ladder is green with `make e2e` in place of `make smoke`.

# Handoff — a real end-to-end browser suite, run locally, fully autonomous

**Status: LIVE. Order: after the review fixes
(`development/research/review-d6c2f84-20260825.md`, highs + behaviour mediums), before
`handoff-json-hot-paths.md`.** Decided with the human on 2026-08-25. Local only — `make e2e` —
never in GitHub CI (decision 13 of plan-1 stands: Go unit tests gate CI; the browser suite is a
local ritual). The research behind this (a coverage map of the current suite and a verified
survey of the drivers as of 2026-08-25) is summarised here so nobody re-derives it.

## Why: the current suite cannot interact

`test/smoke.sh` is one-shot `chromium --headless --dump-dom | --screenshot` plus `curl` and
`node -e`. It cannot click, drag, wheel, double-click, right-click or type; it cannot reach into
the sandboxed widget frame; and it switches SSE *off* (`?nosse=1`) rather than exercising it —
the one live SSE check hits `/events` with curl, bypassing `aboard.html` entirely. So the whole
interaction surface is untested by construction:

| renderer | gestures only a driver can exercise |
|---|---|
| dag | click select, dblclick rename, drag move, drop-on-node reparent, drag-background pan, wheel zoom, right-click menu, the `<dialog>` delete-confirm |
| kanban | drag card between columns (HTML5 DnD), click-title rename, right-click |
| markup | paste/drop image, region/ellipse/pen/move/resize tools (pointer capture), swatches, bulk-colour and clear-marks modals |
| table | header sort, type-and-save cells, right-click row |
| diagram, notes, chat, form, log | type-and-save, Enter/Shift+Enter, Tab indent, scroll-to-unfollow, filter typing |
| gate, vote, ui | record-only clicks: allow/deny/reason/undo, scores, button→intent, field→data.write |
| shell | new-tab dialog, tab dblclick→`prompt()` rename, note strip, tab-strip right-click menu, notify button, help panel, deep links, the 409 stash notice ("Restore mine"/"Keep theirs"), `touched` Dismiss, pending-removal Keep/Remove, `confirm()` on remove |
| html | **the bridge's write half**: a widget calling `aboard.set()` → postMessage → `views/html.js` → `state.data` — described as needing a human click in every handoff since the rename, until this suite drove it (`TestAWidgetWritesThroughTheBridge`) |

The review at d6c2f84 reproduced two browser-side defects by hand in Chromium that no existing
check could see: an SSE reload inside the 250 ms debounce discards the human's edit and flashes
"Saved" (`aboard.html` `load()`), and `baseline` never advances after a 409 merge so a second 409
hands back the agent's text under "Your version was kept". Those two are the first tests this
suite writes.

## What is already there to build on

- Every declared control carries **`data-gesture="<id>"`** (`views/controls.js`), view roots carry
  `data-view` / `data-tab` / `data-active`, tab-strip buttons carry `data-id`, the read-only badge
  `data-readonly`. No `data-testid` is needed.
- Two drag models: kanban is HTML5 DnD; dag, markup and the sketch canvas are pointer-capture
  (`setPointerCapture`) — plain mouse down → ≥2 moves → up over CDP.
- Three native dialogs (`confirm()` ×2, `prompt()` ×1) need a dialog handler; dag's delete uses a
  real `<dialog>` (clickable).
- The widget frame is `sandbox="allow-scripts"` with no `allow-same-origin` → opaque origin, and
  Chrome's `IsolateSandboxedIframes` (default since ~M132) puts it **out of process**. Reaching in
  needs a separate CDP session per frame. The sketch pad's **Undo/Clear** buttons call
  `aboard.set()` synchronously — the cheapest external trigger of the write half.
- The engine can be started in-process: `aboard.Serve(ctx, opts, ServeConfig{Root, Port, …})` with
  an explicit port and a `t.TempDir()` root seeded from the embedded example (`pkg/aboard/example`).
- Old headless was removed from Chrome in M132; `--headless` is new headless. Irrelevant once the
  driver provisions its own browser.

## Decision: playwright-go inside `go test`, behind an `e2e` build tag, as `make e2e`

Verified 2026-08-25: `github.com/playwright-community/playwright-go` v0.6201.1 (2026-08-17,
tracking Playwright 1.62.1, ~13 days behind upstream), active; `Locator`/`FrameLocator` reach the
sandboxed frame transparently; `Locator.DragTo` for HTML5 DnD, `page.Mouse` for pointer capture;
auto-retrying assertions; `Tracing` writes the same `trace.zip` the Playwright trace viewer opens.
No Node at runtime — a bundled driver (~50 MB) plus Chromium (~280 MB) under `~/.cache/ms-playwright`.
It lacks `toHaveScreenshot`; `orisano/pixelmatch` is already in its dependency graph.

Rejected, so nobody re-researches: **go-rod** — no functional commit since 2024-12, go 1.21,
Chromium pinned to a 2024 build. **chromedp** — active and pure Go, but out-of-process frames need
the manual `Target.setAutoAttach{flatten:true}` dance and there is no auto-wait; the frames are the
hardest part of this app, so that is the wrong place to hand-roll. **@playwright/test** (Node) —
the capability ceiling (trace viewer UI, `--ui`, codegen, built-in visual snapshots) at the cost of
a second toolchain (`package.json`, `node_modules`) in a Go repo; the only thing lost by staying in
Go is a UI that opens a Go-produced trace anyway. **Puppeteer** — caretaker mode, open OOPIF
attach bugs. **Cypress** — cannot automate a cross-origin frame at all; simulated events.
**WebdriverIO / Selenium-Go** — Node + BiDi frame bugs / unofficial binding with no releases.

## Deliverable

1. **Harness** — `test/e2e/` (package `e2e`, `//go:build e2e`): a `TestMain` that installs the
   driver if absent (`playwright.Install` with the browser pinned by the module version), starts
   the engine in-process on an explicit free port with a temp root seeded by `init --example` PLUS
   an interaction fixture (a kanban with cards in three columns, a dag with a reparentable node, a
   table with rows, the sketch pad, one gate with a pending row) — the example board's empty queue
   is exactly the vacuous fixture the review flagged. SSE ON (no `?nosse=1`). Helpers:
   `control(page, id)` → `[data-gesture="<id>"]`; `tab(page, id)`; a dialog handler; `drag(page,
   from, to)` that composes down/move×N/up for pointer-capture and `DragTo` for kanban; `apply(doc,
   by)` that writes through the HTTP API as a second actor. On failure: trace + screenshot +
   `aboard.json` snapshot into `.aboard/run/e2e/<test>/`.
2. **First tests, in this order** (each one a defect or a gap already recorded):
   - the bridge write half: `FrameLocator` into the html tab, click Undo/Clear, assert `state.data`
     changed via `GET /aboard.json` — then the same inside a stack block (`bb32`'s fifth block);
   - the 250 ms debounce vs SSE reload (type in a notes tab, `apply` from a second actor mid-debounce,
     assert the human's text survives and the server holds it);
   - the 409 merge twice in a row: "Restore mine" must return the human's text, never the agent's;
   - `touched` Dismiss sends `__by: human`; pending-removal Keep/Remove; the tab-strip rename via
     `prompt()`; remove via `confirm()`;
   - kanban drag between columns persists; read-only kanban has no drag handles **with cards
     present**; dag drag-reparent and the `<dialog>` delete-confirm; dag dblclick rename; markup
     region + resize + clear-marks modal; table sort + type-and-save; gate allow with reason + undo;
     ui button → intent recorded; the notify button releases a real `aboard wait`;
   - SSE: a second actor's write appears in the open page without a reload; the `--dev` CSS re-link
     keeps scroll and selection; a UI-signature change reloads the page.
3. **`make e2e`** — depends on `build`; `E2E_HEADED=1` for a visible browser, `E2E_TRACE=always`
   to keep traces on success; documented in CLAUDE.md's make table and in
   `docs/how-to/run-the-browser-suite.md`. Pin the `cmd/playwright` installer in `.bingo` like
   the other tools. Never drive the snap chromium (unsupported by Playwright).
4. **Fallback flag, documented in the harness**: `--disable-features=IsolateSandboxedIframes` if
   the out-of-process frame path proves flaky (Puppeteer disabled that isolation in its own tests
   for exactly this reason). Note the collision with Playwright's own `--disable-features` string
   when appending args.
5. **Retire `test/smoke.sh`** once `e2e` covers every check it makes — and with it the `node`
   dependency and the `--dump-dom` grep-your-own-source gotchas. Port the three static checks that
   need no browser (button helper, declared-control usage, plain-button advisory) into Go tests
   first; the review noted they silently skip without a built binary today.
6. **Visual regression — later, optional**: `pixelmatch` against baselines under
   `test/e2e/baseline/` with a threshold, per-tab, only after the interaction suite is stable.
   Font rendering makes it the flakiest layer; do not lead with it.

## Agent-driven exploration — a complement, not the gate

`chrome-devtools-mcp` (attaches to a running board with `--browser-url`; click/drag/fill/
screenshot/console/network/perf) or Playwright MCP (accessibility-tree snapshots, click-by-ref,
`--codegen`) let an agent drive the real board to hunt bugs. The evidence says not to gate on it:
Slack's 200-run study found ~20 % of agent runs repeat the same action sequence, at $15–30 and
5–11 minutes each. Pattern: **explore once, codify forever** — whatever the agent finds becomes a
test in `test/e2e/`. Mention the MCP in the skill's multi-session reference as the exploratory
tool; nothing more.

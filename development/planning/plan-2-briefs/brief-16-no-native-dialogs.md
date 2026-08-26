# Brief 16 — the board never calls a native dialog (a VS Code webview swallows them)

Read `COMMON.md` first. The human, testing the extension in a real VS Code on 2026-08-26:
"the remove tab button — I clicked it but nothing happens". Cause, confirmed in the source:
`pkg/aboard/web/aboard.html` `removeTab()` does `if (!confirm(msg)) return;`, the tab-strip
rename does `prompt('Rename tab', …)`, and `views/form.js` reset does `window.confirm(…)`. A VS
Code webview (and any iframe with a restrictive sandbox) SUPPRESSES `alert`/`confirm`/`prompt`:
`confirm()` returns `false`, `prompt()` returns `null`, no error, no log. So inside the panel the
pending-removal banner's Remove, the rename, and the form reset are dead, silently.

## Scope

1. **One in-page dialog helper**, `views/dialog.js` (or wherever the DAG's delete-confirm
   `<dialog>` already lives — REUSE that pattern, do not invent a second): `askConfirm(message,
   {ok, cancel})` → `Promise<boolean>`, `askPrompt(label, initial)` → `Promise<string|null>`.
   Buttons through `button()` from `views/controls.js` (chrome, not a declared control — say so
   in the comment, as the existing convention does); Enter/Esc; focus trapped in the dialog and
   restored afterwards; tokens only; works with `?chrome=notabs`/`none`.
2. Replace ALL THREE native calls with it — `removeTab` (banner Remove AND the strip's right-click
   remove), the rename `prompt`, form.js's reset — and assert with a Go/e2e source test that
   `confirm(`, `prompt(`, `alert(` no longer appear outside comments in `aboard.html` and
   `views/*.js` (the DAG's `<dialog>` is fine). Menu.js / any other caller: grep.
3. **e2e**: the existing tests that install a Playwright dialog handler for the `confirm()`
   remove, the `prompt()` rename and the form reset (`test/e2e/shell_test.go`, `renderers_test.go`
   — find them by `OnDialog`/`dialog`) are rewritten to click the in-page dialog's buttons; add
   one test that runs the removal from INSIDE a same-origin wrapper iframe with
   `sandbox="allow-scripts allow-same-origin allow-forms"` (no `allow-modals`), which is the
   closest headless stand-in for a webview — with the old code `confirm()` is suppressed there
   too, so the test fails before and passes after. Keep `covers()` registrations correct.
4. Docs: `docs/how-to/use-the-vscode-extension.md` and `run-in-vscode.md` (remove any "confirm"
   wording; say the board's dialogs are its own so they work in a webview), `CLAUDE.md` gotcha
   ("never a native dialog — a webview swallows it silently; use views/dialog.js"), the skill if
   it mentions confirm/prompt, `http-api.md` untouched, `CHANGELOG.md`. `make caps` only if a
   spec's `gestures`/`controls` text changes (rename/remove gestures are described in
   `views/*.spec.json` — update the wording if it says "confirm()"/"prompt()").
5. **Extension side** (`/home/diegos/_dev/exoport/aboard_vscode`, its own gates: `npm ci && npm
   run build && npm test && npx tsc --noEmit` from a clean copy): the sidebar's `approveRemoval`
   and `denyRemoval` have never been exercised against a real server. Extend
   `test/integration.test.ts` to request a removal as a second actor, then approve it through
   `board.write(approveRemoval(id))` and assert the tab is gone from `GET /aboard.json`; and deny
   one and assert the request cleared. Update its `docs/handoff.md` §11 line for removal answers
   with what is now proven headlessly vs what the human observed (Dismiss: observed working on
   2026-08-26). Commit nothing there either; the orchestrator commits both repos.

The human's boards (`/home/diegos/_dev/ai/borrar` on 44917, this checkout on 47781) are NOT to
be touched, poked or restarted; scratch projects only. `make e2e` once per tool call (timeout
600000); `make ci-local` once.

## Done when

No native dialog remains in the web tree (source test); the three flows work through the
in-page dialog in the suite AND in the no-`allow-modals` iframe test; the extension integration
test proves approve/deny against a real server; ladder green in both repos.

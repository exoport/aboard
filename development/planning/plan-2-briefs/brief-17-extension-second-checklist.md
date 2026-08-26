# Brief 17 — the extension after the human's checklist (aboard_vscode)

Repo: `/home/diegos/_dev/exoport/aboard_vscode`. Rules: `COMMON.md` in the aboard repo (never
commit/push; never touch the human's boards — this checkout's on 47781 and `/home/diegos/_dev/ai/borrar`
on 44917 — scratch projects only). Proof: `npm ci && npm run build && npm test && npx tsc --noEmit`
from a fresh copy (`git ls-files -z --others --exclude-standard --cached | xargs -0 cp --parents -t <dir>`).

The human worked through §11 in a real VS Code on 2026-08-26 (board `borrar`, aboard `93ba033`).
Results, verbatim from the board's checklist table (`bb213`):

PASS (observed): tab switching does not reload the page · the panel survives a drag to another
group and hide/reveal · html tabs paint in the panel, console clean · sidebar Approve/Deny on a
removal request · two viewers disagree about chrome and agree about content · the board restarts
under an open panel and the page reloads itself · a forced 409 warns rather than clobbers · `]`
moves the tree highlight · Rename and Set note from the sidebar · the "Start the board" fallback.
Earlier the same day: dots arrive live, Dismiss clears them, Remove in the board's banner works.

FAIL 1 — Notify: "the poke in the terminal exited ok, the notification icon was not lit". The
release works; the INDICATOR does not. Only the status-bar item changes (`$(bell-dot) aboard ·
notify N`); the view-title bell (`aboard.notify`, icon `$(bell)`) is static, and that is what a
human looks at. Fix: a context key (`aboard.waiting`) set from the waiter count, two
`view/title` entries for `aboard.notify` — `$(bell)` when nobody waits, `$(bell-dot)` (or a
coloured variant) when a session is parked, with tooltips saying so; the `waiters` frame and the
`/waiters` read on reload both drive it. Unit-test the context-key transitions through the vscode
stub (`test/vscode-stub.ts` records `setContext`), and the integration test parks a real
`aboard wait` (spawn the CLI from `ABOARD_BIN`) and asserts the key flips to true, then presses
notify through the controller and asserts it flips back and the CLI exited 0.

FAIL 2 — Copy reference: "copy id worked, there is no copy reference; there is copy link to
this tab and it works". `aboard.copyReference` is titled "Copy Link to This Tab" and copies a
URL. The board's own vocabulary (its right-click menu, `views/menu.js` `referenceFor`) is
**Copy reference** = the tab named with its id beside it (`Migration review (bb32)`) — the form
the docs say to use in prose; a link is a different thing. Offer both: `Copy Reference` (name +
id, matching the board's text exactly — read `menu.js` for the format) and `Copy Link to This
Tab` (the URL). Tests for both strings.

Also: `docs/handoff.md` §11 — tick every observed row with the date, keep the two optional rows
(old binary, Remote SSH) open, record the two defects and their fixes; README status block →
"verified in a real VS Code on 2026-08-26 (M6 step 1); .vsix not yet packaged"; the `[~]` on
removal → `[x]` (Approve/Deny from the sidebar observed). Keep the extension's contract table
correct. No runtime dependencies.

## Done when

Both fixes with tests (integration one runs against a spawned aboard, not skipped on this
machine); §11 and README reflect the run; build/test/tsc green from a clean copy.

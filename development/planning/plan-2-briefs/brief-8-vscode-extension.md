# Brief 8 — the VS Code extension, implemented but not installed (plan-2 item 8)

**This item lives in a DIFFERENT repo: `/home/diegos/_dev/exoport/aboard_vscode`** (currently
`docs/handoff.md`, `README.md`, `.gitignore`, one commit). The aboard repo at
`/home/diegos/_dev/exoport/aboard` is the CONTRACT you code against — read its
`docs/reference/http-api.md`, `pkg/aboard/server.go`'s `/health` handler and `route`, the
`instance.json` shape in `pkg/aboard/layout.go`, and `development/handoffs/handoff-board-for-vscode-panel.md`.
Read `COMMON.md` in the aboard repo for the standing rules (never commit, never push, never
touch the spike); the Go ladder there does not apply here — this repo's proof is
`npm run build` → `dist/extension.js` and `npm test`, both green from a clean clone.

Source: `/home/diegos/_dev/exoport/aboard_vscode/docs/handoff.md` §5 (layout), §6 (the two
moving parts), §7 (the tree), §8 (M1–M5), §10 (hardening — the pure-logic cases).

## Scope

- The scaffold exactly as §5: `package.json` (`name: aboard-vscode`, `displayName: Aboard Panel`,
  `publisher` — pick `exoport` and record it as a judgement call, `engines.vscode ^1.90.0`,
  `main: dist/extension.js`, contributes: the view container + tree view, commands, menus,
  activation on `workspaceContains:**/.aboard/**` and on the commands), `tsconfig.json`
  (strict), `esbuild.mjs` (one bundle, `vscode` external, CommonJS, node target), `.vscodeignore`,
  `LICENSE` (match aboard's), `README.md` carrying the §4 contract and stating plainly the
  extension is UNVERIFIED in a real VS Code, `src/{extension,board,tree,panel}.ts`,
  `media/{panel.html,dot-change.svg,dot-removal.svg}` with the two hex values in one place
  commented as copied from `pkg/aboard/web/app.css`. No runtime dependencies. Dev deps:
  `typescript`, `esbuild`, `@types/vscode`, `@types/node`.
- `board.ts`: discovery walking up from each workspace folder to `.aboard/run/instance.json`,
  `/health` verification (`project` equals the root; `app` is `aboard` or `ape-aboard`), the
  base path if `/health` exposes it (item 7 adds `basePath` — code for it, default `''`),
  `state()`, `write()` with `__base` = the document's revision/`updatedAt` as the server expects
  (READ the server: item 2 made the CAS token a revision — find the field name in
  `http-api.md`), `__by: 'human'`, `__origin: 'vscode'`, one 409 retry; `poke()`, `waiters()`,
  `events()` — SSE via `http.request` with reconnect/backoff, three frame kinds, `ui` ignored.
- `panel.ts` + `media/panel.html`: `WebviewPanel` with `enableScripts`, `retainContextWhenHidden`,
  `portMapping`, `asExternalUri`, the CSP from §6, the ~20-line bridge with the `goto` nonce and
  the `active` message authenticated by `e.source`.
- `tree.ts`: `TreeDataProvider` in document order, label/description/tooltip (from
  `/capabilities`)/icon/badge as §7; selection → goto; `active` → reveal, guarded.
- `extension.ts`: activate, register, the actions table (dismiss, approve/deny removal, rename,
  note, notify, copy id/reference), errors as notifications, the "start the board" fallback
  choosing `aboard serve` / `ape aboard serve` by PATH, both absent → an error naming both;
  polls `/health` after launching.
- §10 pure-logic cases handled: no workspace, multi-root (one board per folder, tree says
  which), folder with no `.aboard/`, dead instance file, health for another project, schema
  mismatch degrades visibly.
- **Unit tests under `node --test`** (no framework) for the pure parts: discovery walk (a temp
  dir tree), `/health` acceptance, tree mapping (doc → items, icon precedence, badge count),
  SSE frame parsing, message parsing, the fallback-command choice. Keep the pure parts free of
  the `vscode` import so they are testable — a thin adapter layer.
- `npm run build` and `npm test` green; `npm ci` from a clean clone works (commit-ready
  `package-lock.json`). NOT in scope: `.vsix`, `vsce`, `code --install-extension`, launching
  VS Code, §11.

## Report

Same format as COMMON.md, plus the exact commands and their output tails for build and test
from a fresh `git clone` into the scratchpad.

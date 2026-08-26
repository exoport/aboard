# Brief 15 — the extension after its first real run (aboard_vscode)

Repo: `/home/diegos/_dev/exoport/aboard_vscode`. Read `COMMON.md` in the aboard repo for the
standing rules (never commit, never push, never touch `/home/diegos/_dev/ai/board`; the two
boards the human runs — `/home/diegos/_dev/exoport/aboard` on 47781 and `/home/diegos/_dev/ai/borrar`
on 44917 — are NOT yours: never pkill, poke or restart them). Proof for this repo: `npm ci &&
npm run build && npm test` from a copy of exactly what a commit would contain, plus the
integration test below.

The human ran the extension in a real VS Code (Extension Development Host) on 2026-08-26 against
`/home/diegos/_dev/ai/borrar` and reported three things:

1. **The tree shows the tabs but no coloured dots**, although every tab on that board carries a
   `touched` mark (`{by, at, note}` objects — `isMark` in `src/model.ts` maps them to `dot:
   'change'`, so the model is right). The marks were written at 13:33, AFTER the extension
   activated at 13:22 — so the prime suspect is that the tree never refreshed on the SSE
   `origin` frame in a real VS Code (M3), or the icon never renders (`treeItem.iconPath = {light,
   dark}` with `Uri.joinPath(extensionUri, 'media', …)`). Find the actual cause, do not guess:
   write an INTEGRATION test (`node --test`, no vscode) that spawns the aboard binary
   (`/home/diegos/_dev/exoport/aboard/aboard` — build it with `make build` there if missing) on a
   temp project seeded with `aboard init --example --gitignore`, runs `board.ts`'s discovery →
   `state()` → `events()`, applies a write as a second actor that sets `touched` on a tab, and
   asserts the `origin` frame arrives, the refetched document carries the mark, and the tree
   model yields `dot: 'change'` with the icon URI pointing at an existing file. Then read
   `tree.ts`/`extension.ts` for the vscode half (`onDidChangeTreeData` fired on the frame; the
   debounce; `TreeItem.iconPath` shape — VS Code accepts a `Uri`, `{light, dark}` of Uris, or a
   `ThemeIcon`; an SVG must have an explicit fill, not `currentColor`, to show in a tree item)
   and fix what is wrong. Kill the spawned server by pid at the end of the test.
2. **The board's own tab strip was still visible inside the panel.** The board the human framed
   was served by a pre-`?chrome=` binary (`~/go/bin/aboard` was a build from 00:20, before
   item 7 landed at 04:30; it has been replaced with the current one). Verify `frameSrc()` builds
   `…/?chrome=notabs#tab=<id>&r=<n>` exactly (test), and ADD a runtime check: after `/health`,
   if the board is older than the `?chrome=` support, show ONE notification saying the board's
   binary predates the extension's contract and the strip will show — the `/capabilities`
   manifest is the honest source (`capsHash` moved when `?chrome=`/`active` landed; read
   `docs/reference/capabilities.md` and `http-api.md` in the aboard repo for a field a client can
   test — if there is none, say so and fall back to `/health.version` with a documented minimum).
3. **The warnings** — `DEP0040 punycode`, `DEP0169 url.parse`, `devbox.json ENOENT`: verify (grep
   `src/` and `dist/extension.js`) that none come from this extension; they are the Claude Code
   extension's and the devbox extension's. Write a short "What is not ours" troubleshooting
   section in README.md naming them, so the next person does not chase them here.

Also: README.md and docs/handoff.md — M6 has now happened once ("run in a real VS Code by the
human on 2026-08-26; the tree listed the tabs; two defects found and fixed here: …"); the
"unverified in a real VS Code" wording becomes "verified once, partially" with what was and was
not observed working. Keep `package-lock.json` current; no runtime dependencies.

## Done when

The integration test exists and passes against a spawned aboard; the cause of the missing dots
is named and fixed with a test; `frameSrc` asserted; the old-binary notification exists and is
unit-tested; README troubleshooting written; `npm ci && npm run build && npm test && npx tsc
--noEmit` green from a clean copy.

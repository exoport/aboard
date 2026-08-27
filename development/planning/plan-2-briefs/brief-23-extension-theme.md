# Brief 23 — the extension hands the board the VS Code theme (aboard_vscode)

Repo: `/home/diegos/_dev/exoport/aboard_vscode`. Rules: `COMMON.md` in the aboard repo (never
commit/push; never touch the human's boards — this checkout's on 47781 and `/home/diegos/_dev/ai/borrar`
on 44917 — scratch projects only). Proof: `npm ci && npm run build && npm test && npx tsc --noEmit`
from a fresh copy (`git ls-files -z --others --exclude-standard --cached | xargs -0 cp --parents -t <dir>`),
plus the integration test against a spawned `aboard` (`ABOARD_BIN`, default
`/home/diegos/_dev/exoport/aboard/aboard` — `make build` there if missing).

The human asked (2026-08-26): *"check if possible that when running in the VS Code extension,
the theme colours are derived from the actual VS Code theme being used."* Item 22 on the aboard
side landed the hook: the board accepts `{__aboard:'theme', tokens:{'--bg': '#…', …},
kind:'dark'|'light'}` posted by `window.parent`, applies it as a per-viewer override (never
written), validates token names against its 21 declared tokens, and ignores a message from a
non-parent source. Read the contract as it landed: `docs/reference/theme.md`,
`docs/reference/http-api.md` (the message beside `active`), and `pkg/aboard/web/app.css` for the
token roles and the built-in dark/light values.

## What is possible, and what is not

- Inside a **webview document**, VS Code exposes the live theme as CSS variables
  (`--vscode-editor-background`, `--vscode-editor-foreground`, `--vscode-focusBorder`,
  `--vscode-textLink-foreground`, `--vscode-errorForeground`, `--vscode-panel-border`,
  `--vscode-sideBar-background`, …) on the webview's own root, and `document.body` carries
  `vscode-dark`/`vscode-light`/`vscode-high-contrast` classes. The **iframe is cross-origin** and
  inherits none of it. So `media/panel.html` must read the variables with
  `getComputedStyle(document.documentElement).getPropertyValue(...)`, map them to the board's
  tokens, and `postMessage` the theme into the frame — on load, and again whenever the theme
  changes (a `MutationObserver` on `document.body`'s class, plus VS Code's
  `vscode.window.onDidChangeActiveColorTheme` on the extension side re-posting to the panel).
- The extension host API gives only `ColorTheme.kind`, never colour values; the values exist
  only in the webview. So the mapping lives in `media/panel.html` (a pure function you can also
  unit-test by extracting it into `src/theme.ts` and bundling/inlining — keep `media/panel.html`
  the ~20-line bridge it is; if the mapping must live in the page, put it in a `<script>` the
  test can load with `node:vm`).

## Scope

1. `src/theme.ts` (pure, no `vscode` import): `mapVscodeTheme(vars: Record<string,string>,
   kind: 'dark'|'light'|'high-contrast') → { kind, tokens }` covering every board token that
   has a sensible VS Code counterpart (background/sunken/surface/raised from editor, sideBar,
   panel and input backgrounds; text/muted/dim from foreground and descriptionForeground; line
   from panel.border/widget.border; accent from focusBorder or button.background; accent-ink;
   danger from errorForeground; drop/focus; status colours from the charts.* or gitDecoration
   colours if present) — and **leaves a token OUT when its source variable is absent** rather
   than guessing, so the board's own built-in value for that token stands. High contrast maps to
   `kind: 'dark'`/`'light'` by the class present, with the board's contrast rule preserved: the
   board pins text to WCAG AAA, and an arbitrary VS Code theme does not — compute the contrast of
   the mapped `--text` on the mapped `--bg` and, if it is below the board's threshold (read
   `docs/reference/theme.md` for the number), send `--bg`/surfaces but NOT the text tokens, so
   readability is never traded for fidelity. Say this in the README.
2. `media/panel.html`: read the variables, post the theme on the frame's `load` and on change;
   authenticate nothing (it is the parent posting to its own frame); keep the `goto`/`active`
   bridge intact.
3. `src/panel.ts`/`extension.ts`: re-post on `onDidChangeActiveColorTheme`; a setting
   `aboard.theme` = `follow` (default) | `board` (let the board's own theme.json/switch decide,
   post nothing) documented in `package.json` `contributes.configuration`.
4. Tests: the mapping (present/absent variables, high contrast, the contrast guard) under
   `node --test`; the message shape against the board's declared token list (read
   `aboard capabilities` output or the theme reference for the 21 names — assert every name the
   mapper emits is one of them); an integration test that loads the real board in a headless
   frame is NOT required here (the aboard side has the e2e for the message) — but do assert with
   the fake board that `panel.html`'s script posts a `{__aboard:'theme'}` envelope once the
   frame is loaded (load the script in `node:vm` with a stub `window`).
5. Docs: README's contract table gains the `theme` message and the setting; `docs/handoff.md`
   §6 the theme flow, §11 a row "the board follows the VS Code theme (light theme, high contrast)";
   the "what is not ours" section unchanged.

## Done when

The mapping is tested, the panel posts on load and on theme change, the setting exists, docs
updated; `npm ci && npm run build && npm test && npx tsc --noEmit` green from a clean copy.

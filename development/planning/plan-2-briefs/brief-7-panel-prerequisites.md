# Brief 7 — panel prerequisites on the aboard side (plan-2 item 7)

Read `COMMON.md` first. Source: `development/handoffs/handoff-board-for-vscode-panel.md`
§4–§6, in its order. Items 1–6 have landed; the suite is `make e2e` (`test/e2e/`, one run per
tool call, timeout 600000).

## Scope

1. §4 `?chrome=full|notabs|none` — a URL parameter stamped once as `document.body.dataset.chrome`;
   unknown → `full`; `notabs` hides `.tabs` but keeps `#add-tab`, `.topbar`, `.tab-note`; `none`
   hides `.board-head`; composes with `#tab=` deep links and survives the self-reload paths.
   CSS by data attribute, tokens only.
2. §5 `activate(id)` posts `{__aboard:'active', tab:id}` to `parent` when framed; `'*'` target;
   nothing else travels this way.
3. §6 both `localStorage` call sites wrapped in try/catch.
4. Also answer the open question in §3: does `GET /health` expose the configured base path?
   If not, add `basePath` to `/health` (declared in the manifest, documented in `http-api.md`),
   because the extension (item 8) needs it — say so in the handoff.
5. e2e tests: `class="tab"` count zero under `notabs`, non-zero without, `#add-tab` and
   `.tab-note` survive; a same-origin wrapper page iframes the board, presses `]`, receives an
   `active` message naming a different tab; storage refusal simulated (override
   `localStorage` getter to throw before load) and tab switching still works.
6. Docs: `http-api.md` (`?chrome=`, the `active` message, `/health.basePath`), the skill's
   multi-session/embedding reference, the handoff's Status line and §9 table updated to "landed".

## Done when

The extension handoff's M2 and M4 "if it has not landed" clauses are TRUE-today statements —
edit `/home/diegos/_dev/exoport/aboard_vscode/docs/handoff.md` §6 and §8 to say the three items
landed (keep the file's own voice; the extension repo is a sibling checkout, edit it in place —
another agent may be building the extension there concurrently, so touch ONLY `docs/handoff.md`
and only those clauses); ladder green.

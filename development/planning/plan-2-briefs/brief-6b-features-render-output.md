# Brief 6b — features, the render/output cluster (plan-2 item 6, part b)

Read `COMMON.md` first. Source: `development/handoffs/handoff-13-features.md`, items
**4 (`bb364` html tabs read the real palette), 5 (`bb365` mermaid fences in markdown),
7 (`bb367` `export` renders a `ui` tree)**. Items 1–5 of plan-2 have landed.

**You are working in a git worktree** whose path the orchestrator gave you in the prompt. Work
ONLY there. Two sibling worktrees implement other feature clusters; a merge agent squash-merges
all three into `main`. Keep changes to the files your features need; do not reformat unrelated
code; `make caps` if a spec changed; list every file touched. Scratch projects under the
scratchpad with your cluster's name.

## Scope

- `bb364`: `htmltab.go` parses the `:root` block of the embedded (or, under `--dev`, on-disk)
  `app.css` and injects the full token set into the html-tab frame; fails CLOSED to the current
  literal on a parse error (test both). A test asserts the frame's token set equals `app.css`'s.
- `bb365`: `markdown.js` keeps the fence info string; a ```` ```mermaid ```` fence renders through
  `diagram.js`'s loader, EXPORTED and shared (theme config hoisted out of the mount), never
  copied; a parse failure shows the source verbatim. `notes.spec.json` documents it. An e2e
  test asserts an svg appears inside a notes tab / stack notes block (the suite is `make e2e`,
  `test/e2e/`; one run per tool call, timeout 600000).
- `bb367`: `export.go` gains a `ui` case: walk `state.root`, resolve `{bind}` against
  `state.data` reusing `caps.go`'s resolution, print an indented outline; per-node display
  logic is new work (which prop is the text) — drive it from `ui.spec.json` if you add a
  declaration for it (`textProp` or similar) rather than a Go table, and say why. Go tests with
  a golden outline. `log`/`html`/`trace` stay explicit non-cases.
- Docs: the skill (export covers `ui`; mermaid in notes), `docs/`, the handoff sections marked
  done.

## Done when

Three features shipped with tests and docs; ladder green in your worktree (`make e2e`
included); report lists files touched.

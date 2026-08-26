# How-to guides

How-to guides are **recipes** — they answer "how do I X?" for a reader who already
knows the basics. Each guide solves a specific problem in a specific context. Unlike
[Tutorials](../tutorials/), how-to guides assume competence and skip the hand-holding;
unlike [Reference](../reference/), they're goal-oriented rather than exhaustive.

## Available guides

**Getting it running**

- [How to install aboard](install.md)
- [How to run aboard inside VS Code](run-in-vscode.md) — docking the board in the built-in Simple Browser
- [How to use the VS Code extension](use-the-vscode-extension.md) — the sidebar tree and panel, and what is still unproven about them

**Running it your way**

- [How to run a second board in one project](run-a-second-board.md) — `--name`, and the four things two boards still share
- [How to put aboard behind a reverse proxy](serve-under-a-path-prefix.md) — `serve --base-path`, and the four traps

**Getting work out of it**

- [How to promote a board tab into a document](promote-a-tab-into-a-document.md) — `aboard export`, what to rewrite, and demoting the tab
- [How to write a recipe](write-a-recipe.md)

**Building on it**

- [How to embed aboard in ape](embed-in-ape.md)
- [How to verify a release artifact](verify.md)
- [How to run the browser suite](run-the-browser-suite.md)

## Writing a how-to guide

- Start with the problem in the reader's words, not the solution.
- One outcome per guide. Don't bundle unrelated tasks.
- Show only the path that works for the stated problem. If a problem branches into materially different cases, write separate guides.
- Don't teach concepts here — link to [Explanation](../explanation/) instead.
- **Run every command you write.** These pages are the ones readers paste from.

See the [Diátaxis how-to guide rubric](https://diataxis.fr/how-to-guides/).

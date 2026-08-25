# Tutorials

Tutorials are **lessons** — they teach a beginner how to do something by walking them
through a complete, working example. A reader following a tutorial should arrive at a
known good outcome without making decisions about which path to take. Tutorials are not
the place to explain why something works; that's [Explanation](../explanation/). They're
not lookup tables either; that's [Reference](../reference/).

A good aboard tutorial takes the reader from "I have never run this" to "a board is
open in my editor, I changed it from the terminal, I watched the change land, and I
know how to get the text back out again."

## Available tutorials

- [Your first board](first-board.md) — install aboard, `aboard init --example`, `aboard serve`, dock the URL in VS Code's Simple Browser, apply a change from the terminal, watch it appear, and export a tab as markdown.

## Planned tutorials

- **Ask the human a question and block for the answer.** Open a `gate` tab, wait on it with `aboard wait --for "answer <tab>"`, and act on the verdict and its reason. End state: a session that hands control back and picks it up again.
- **Two sessions, one board.** Divide work through a `chat` tab, write with distinct `--by` actors, and read `aboard journal` to see who did what. End state: a working multi-session etiquette.

## Writing a tutorial

- One linear path. No "if you want X, do Y instead" branches.
- Concrete commands the reader copy-pastes — and **run every one of them before publishing**. A command in a doc is a claim.
- Verifiable checkpoints (`aboard status` should print this; the tab strip should show that).
- A clear end state — readers know they're done when they see it.

See the [Diátaxis tutorials guide](https://diataxis.fr/tutorials/) for the full rubric.

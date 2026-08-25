# Why the manifest is declared

The capability manifest is assembled from **declarations** — one JSON file beside each
renderer, one Go table for the CLI — and the code is checked against them. It is not
scraped out of the implementation, and it is not prose maintained alongside it.

This page is about why that seam is where it is, and what it costs where it cannot be
held.

## The problem was never documentation drift

It is tempting to say "the docs went stale, so we generated them". That is not what
happened, and the real diagnosis matters because it predicts which parts stay honest.

The board's per-type state fields never drifted far, and the reason is that **something
reads their declaration**: the write path validates against it, so a wrong field name
produces a wrong warning and somebody fixes it. Declarations with a consumer are
self-correcting.

The parts that rotted were the parts with **no consumer**. A `gestures` list fed nothing
but prose, so nothing broke when it went stale — and a `table` renderer shipped a
delete-row button documented nowhere while the skill advertised the feature. The lesson
generalises: *a declaration that is only read by a human is a comment, and comments rot.*

So the fix is not "write it down more carefully". It is to make the declaration
**load-bearing**.

## Rendered from, not described by

Every renderer's controls are declared in its spec, and the renderer **draws them from
that declaration** at runtime. The palettes a renderer accepts by name are declared, and
the swatch row and the tone map are built from them. A declaration that is wrong is
therefore a button that is wrong — visible immediately, on the screen, rather than in a
document nobody diffs.

Four checks hold the seam, and none of them is fuzzy:

- every renderer button goes through the control helper (a grep);
- every declared control is used by its renderer (catches a declaration left behind after its button was deleted);
- every rendered control resolves to a declaration (an id could be built at runtime);
- an undeclared control renders a visible marker rather than a blank button.

All four were verified by deliberately breaking them.

There is a deliberate second function for buttons that are **not** capabilities:
agent-authored content (a `ui` button's label is the agent's) and chrome belonging to no
renderer (the context menu, the inline editor, a dialog's Cancel). Whether a button is a
capability or merely an affordance is a judgement no rule makes, so it is two calls, and
the difference is visible in review.

## Why `controls` is a list

Unlike state fields, controls have an **order, and it is meaningful**: it is the order
they sit in the toolbar, and it is the order the help panel shows. Sorted alphabetically
by id, one renderer's twelve controls read "Colour ▾, Clear marks, ✕, Ellipse…" —
deterministic and useless.

So `controls` is a list, and **reordering a spec moves `capsHash`.** That is correct: the
order is part of the surface. (The generated JavaScript module stays an object keyed by
id, since that one is only ever a lookup.)

## Why the command table replaced walking the flags

The manifest used to build its flag list by walking the global flag registry at runtime.
That worked exactly because there was one flat flag set and one binary.

Under cobra neither holds. Flags are per-command, the global registry is empty, and a walk
would report **whatever happened to be registered on the path taken to reach it** — a
manifest whose contents depend on which subcommand printed it, and therefore a `capsHash`
that moves for no reason a reader could see.

So the CLI surface is declared as data and the cobra tree is asserted equal to it. Yes,
that is two things that can disagree. **Two things that can disagree, with a test that
fails when they do, beats one thing silently derived from the wrong source.**

## Generated, not fetched

The shell already pulls the manifest over HTTP at boot for its help panel, and async is
fine there. Button **labels** are not: they would render from a fallback and visibly
re-label a moment later.

So the control module is generated to a file that the browser imports synchronously — and
`make caps` **builds twice**, because the web tree is embedded: the first binary emits the
module from the current declarations, the second embeds the module it just wrote. Drop one
build and the server serves the previous controls while your declaration edit appears to
do nothing.

## `gestures` is the remainder, and nothing can verify it

What is left once controls carry themselves: drag, drop, wheel, double-click, right-click,
type-and-it-saves.

**Nothing can verify those, deliberately, and it is stated in the source rather than
hidden.** A control can be asserted three ways because it is a thing in the DOM. "Drag one
node onto another to reparent" is a behaviour spread across pointer handlers; no sweep can
confirm it exists, and none can confirm that a sentence still describes it. It stays prose,
reviewed by people.

The honest response is not to build a check that pretends otherwise — it is to make the
unverifiable half **as small as the truth allows**. Fifteen entries that merely restated a
button were moved into that button's own doc when controls gained one. The help panel
gained a Buttons section so the human lost nothing.

A related check was designed and then *not* built for the same reason: a DOM sweep
collecting every `button[title]` and asserting it appeared in the type's gestures. Measured
on a real tab, that was 23 candidate titles — 17 of them tab-strip chrome — to surface
about four real gaps. **A check with that ratio gets muted, and a muted check is worse
than none.** The fuzziness was removed at the source instead.

## The lesson that generalises

**Facts are generated; judgment stays authored.** The skill's reference half is emitted
from the manifest and the recipe index from recipe frontmatter; the arguments — this page,
its neighbours — are written by people and reviewed by people. A document that mixes the
two rots at the speed of its fastest-moving fact.

And one meta-lesson, learned expensively: **an "I can't verify this" claim needs a check
before it is made.** A help panel was shipped unverified on the grounds that it could not
be screenshotted; it could, the human found it broken on sight, and a one-line grep would
have said so. That kind of claim is the one that licenses skipping the check, which is
exactly why it has to be tested.

## See also

- [The capability manifest](../reference/capabilities.md) — what it contains and how to ask for it.
- [Why tabs are data](why-tabs-are-data.md) — the three declarations a renderer needs.
- [CLI reference](../reference/cli.md) — the generated page the command table backs.

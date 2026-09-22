# How to look at a tab the way the human sees it

`aboard apply` prints `applied` and exits 0 for a tab that draws an empty box. The
write warnings catch an unknown component, an unknown prop and a `{bind}` that
points nowhere, but not a layout that is legal and still unreadable. The only
check for that is a picture. `aboard shot` takes one with a real browser and
tells you where it put it.

## Take the picture

The board has to be running, and a chromium-family browser has to be installed
(chromium, google-chrome or Edge). `shot` finds one on `$PATH` or in its usual
install location. To use a different one, name it with `--browser` or
`ABOARD_BROWSER`.

```bash
aboard shot ab24
```

```text
ab24  /home/you/project/.aboard/run/shots/ab24.png
        shows the FIRST panel of its tabs component; pass --node <panel label> for another
```

A tab is named by id, key or type, and several can be shot at once. Pictures go
to `.aboard/run/shots/<tab>.png` in the project. The previous picture is deleted
first, so a file there is from this run or not there at all.

**Then read the picture.** Taking it proves nothing on its own.

## Read what did not fit

Under each picture, `shot` lists the boxes whose content did not fit in that
window, as the page itself measured them:

```text
ab24  /home/you/project/.aboard/run/shots/ab24-Wide.png
        does not fit at 700px wide:
          spill  text "https://example.com/aaaaaaaaaaaaaaaaaaa…" (panel Wide) — 1935px across
          scroll code "x = 1234567890 1234567890 1234567890 12…" (panel Wide) — 2864px across
          scroll table (panel Wide) — 210px across
```

This catches what a picture hides: text below a clipped edge, the columns a table
scrolls out of sight, a slide cut off at the bottom of its stage. `cut` means the
content is not on screen at all. `spill` means it draws past its box. `scroll` means
the human has to scroll a box inside the tab to reach it. The measurement is at
`--width`, so shoot at a narrower width too if the board may be seen in a side panel.

Only `ui` and `html` tabs are measured, including those inside a `stack`. For an
`html` widget, the list covers the page against its frame and any box with
`overflow: hidden` that cuts its content. A widget's own scrolling boxes are left
out. The same list reaches `aboard rendered` from a browser someone actually had
open, at that browser's width.

## Pick what is on screen

| to see                                    | pass                                      |
| ----------------------------------------- | ----------------------------------------- |
| a panel of a `ui` tab other than the first | `--node <panel label>`                    |
| one node, scrolled into view              | `--node <node id>`                        |
| the light variant                         | `--theme light`                           |
| the board's help panel over the tab       | `--help-panel`                            |
| a narrower window, as in a side panel     | `--width 600` (default 1400×900)          |
| the result as data                        | `--output-format json`                    |

```bash
aboard shot ab24 --node Summary
aboard shot kanban dag --theme light --width 1000
```

A `--node` the tab does not have is refused before the browser starts. The page
would still open, just on the first panel, and that picture would look exactly
like success. Each option goes into the file name (`ab24-Summary-light.png`), so
two panels of one tab are two files.

## What it handles for you

Each of these used to be a way to get no picture, or the wrong one:

- **The live stream.** The page is loaded with `?nosse=1`. Otherwise the event
  stream never closes, a headless browser never reaches network-idle, and it
  writes nothing at all.
- **Which tab.** The page is loaded with `?tab=<id>`. A fresh browser profile has
  no remembered tab and would open on the first one.
- **`html` tabs.** These are shot on their own route, `/tab/<id>/html`, because
  headless chromium does not reliably paint the frame inside the board. The
  output says the picture is of the widget alone.
- **Scrolled pages.** Chromium's `--screenshot` draws a scrolled document wrongly
  (a blank band, or an all-black frame). When `--node` scrolls to something far
  down, the board paints the same view at scroll 0.
- **A snap-confined chromium.** A snap can only write under `$HOME`, and not inside
  a hidden directory at its top such as `~/.cache`. When it cannot write, it says
  "No such file or directory", which is not true. `shot` has it render into the
  snap's own directory and moves the file into place.
- **Mount receipts.** The page posts none (`?shot=1`). `aboard rendered` and
  `aboard wait --for "rendered <id>"` keep meaning that a person had the tab
  open.

It never writes to the board.

## When it fails

| exit | meaning                                                                         |
| ---- | ------------------------------------------------------------------------------- |
| 0    | every picture was written, whatever did not fit in it                            |
| 1    | no board running, no such tab or node, no browser, or a picture was not written |
| 2    | a flag or argument it cannot act on (`--theme sepia`, `--node` with two tabs)    |

A picture that was not written is listed as `FAILED` with the browser's own last
words. The other pictures from the same run are still listed.

## Inside aboard's own checkout

`make shot` is the same command with the checkout's binary:

```bash
make shot SHOT_TABS="ab133 ab22" SHOT_FLAGS="--help-panel"
make shot PROJECT=~/work/other-project SHOT_TABS="ab4"
```

## See also

- [How to run the browser suite](run-the-browser-suite.md): `make e2e`, which
  photographs only a temporary board of its own.
- [The command reference](../reference/cli.md#aboard-shot): every flag.
- [HTTP API](../reference/http-api.md): the shell's URL parameters, `?shot=1`
  among them.

#!/usr/bin/env sh
# Screenshot one or more tabs, for eyeballing a restyle.
#
#   ./test/shot.sh                 # all five tabs
#   ./test/shot.sh dag diagram     # just these
#   ./test/shot.sh bb22#help       # the help panel over that tab
#   ./test/shot.sh /tab/bb72/html  # a raw path — how you shoot an html tab, since
#                                  # headless chromium does not paint iframes
#
# Two gotchas this encodes:
#  - a snap-confined chromium cannot write outside $HOME, so shots land in the
#    project (.aboard/run/shots/) rather than /tmp;
#  - ?nosse=1 is required: the live-reload stream never closes, so a headless
#    browser otherwise waits forever for network-idle and writes nothing.

set -e
cd "$(dirname "$0")/.."
REPO=$PWD

# Which board to shoot: a directory containing `.aboard/`, defaulting to the repo
# root. Shots land inside THAT project, which is what "shots land in the project
# rather than /tmp" was always supposed to mean — the paths below were
# repo-root-relative, so pointing this at a scratch board wrote its pictures into
# the repo.
#
# This one KEEPS a default, and that is a rule about writing rather than about
# consistency: the retired shell suite wrote to the board it was pointed at, so
# its default was a trap and became a hard error; this only reads the board and
# writes pictures into it. `make e2e`, which replaced that suite, drives a
# temporary board of its own and takes no PROJECT at all.
#
# One tension to know about: a snap-confined chromium cannot write outside $HOME,
# which is the whole reason shots go into the project rather than /tmp — so a
# scratch project under /tmp and a snap chromium do not mix, and the symptom is
# every shot reported FAILED with no other error. Put the scratch project under
# $HOME in that case.
PROJECT=${PROJECT:-$REPO}
case "$PROJECT" in
  /*) ;;
  *) PROJECT="$REPO/$PROJECT" ;;
esac
OUT="$PROJECT/.aboard/run/shots"

# Discover this project's port from the running instance rather than assuming
# one: the port is derived per project, so it is not a fixed number any more.
INSTANCE="$PROJECT/.aboard/run/instance.json"
if [ -z "$PORT" ] && [ -f "$INSTANCE" ]; then
  PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$INSTANCE")
fi
if [ -z "$PORT" ]; then
  echo "no running board found ($INSTANCE missing) — start it with 'aboard serve'" >&2
  exit 1
fi
# A board served under --base-path answers only under that prefix, so build every
# URL from the instance record rather than from the port alone.
#
# `|| true` under `set -e`: a failing command substitution aborts the script with
# no message at all, which is how a hand-supplied PORT with no instance file
# produced a silent exit 1. This is the last shell script in test/, so it is also
# the last place that trap can be sprung.
BASE=$(sed -n 's/.*"url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$INSTANCE" 2>/dev/null || true)
[ -z "$BASE" ] && BASE="http://localhost:$PORT"

BROWSER=""
for c in chromium chromium-browser google-chrome google-chrome-stable; do
  if command -v "$c" >/dev/null 2>&1; then BROWSER="$c"; break; fi
done
[ -z "$BROWSER" ] && { echo "no chromium-family browser found" >&2; exit 1; }

if ! curl -sf -o /dev/null "$BASE/aboard.json"; then
  echo "server not answering on $BASE — start it with 'aboard serve' in $PROJECT" >&2
  exit 1
fi

TABS="$*"
[ -z "$TABS" ] && TABS="kanban dag diagram form markup"
mkdir -p "$OUT"

# Counted, because this script used to exit 0 when every single shot FAILED —
# which is the confined-chromium case below, the one it warns about by name. A
# screenshot tool that reports success having written nothing is worse than one
# that cannot run: `make shot` in a ladder went green and the human found the
# missing pictures. A PARTIAL run still exits 0 on purpose: one mistyped tab name
# among five is a typo, not a broken environment, and the FAILED line already
# says which.
WROTE=0

# Said ONCE, up front, because the browser's own message for it is a lie. A
# confined chromium refusing to write outside $HOME reports "No such file or
# directory" for a directory that plainly exists, so the reader goes looking for
# a path bug that is not there. Checked rather than assumed: the confinement is
# real on some installs and absent on others, and the only cheap discriminator is
# where the file is going.
case "$OUT" in
  "$HOME"/*) ;;
  *) echo "note $OUT is outside \$HOME — if every shot below FAILS, that is a confined" >&2
     echo "     chromium refusing to write there (it will say 'No such file or directory'," >&2
     echo "     which is not true). Put the project under \$HOME and re-run." >&2 ;;
esac

for tab in $TABS; do
  # A target may carry a fragment — `./test/shot.sh bb22#help` shoots the help
  # panel over that tab. The panel opens on a `?` keypress, which headless
  # chromium cannot send, and #help exists precisely so it can be captured
  # anyway. Worth wiring in here: a panel nobody can screenshot is a panel whose
  # bugs reach the human first, which is exactly how its Buttons section shipped
  # with its labels printing through their own descriptions.
  name=$(printf '%s' "$tab" | tr '#/' '--' | sed 's/^-*//')
  case "$tab" in
    # A raw path shoots that URL directly. The one case that needs it is an html
    # tab: headless chromium does not reliably paint iframe content, so shooting
    # the shell shows an empty frame and proves nothing.
    /*)    url="$BASE$tab" ;;
    *"#"*) url="$BASE/?nosse=1&tab=${tab%%#*}#${tab#*#}" ;;
    *)     url="$BASE/?nosse=1&tab=$tab" ;;
  esac
  # Clear the previous run's picture first, or "did this shot work" is really
  # "has this tab EVER been shot into this directory" — a stale PNG makes a
  # failed run look like a successful one, which is the same lie the exit code
  # below exists to stop telling.
  #
  # `|| true` because a failing command aborts a `set -e` script with no message
  # at all — the trap this file's header already warns about, sprung here on the
  # first try: an unwritable shots directory made `rm` fail and the whole script
  # ended mid-loop printing nothing. And if the file survives the removal we
  # cannot tell this run's picture from the last one's, so that shot is failed
  # before the browser is even started.
  rm -f "$OUT/$name.png" 2>/dev/null || true
  if [ -e "$OUT/$name.png" ]; then
    echo "  $name FAILED (cannot clear the previous $OUT/$name.png)" >&2
    continue
  fi
  timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --hide-scrollbars \
    --window-size="${WIDTH:-1280},${HEIGHT:-940}" --virtual-time-budget=10000 \
    --screenshot="$OUT/$name.png" \
    "$url" 2>/dev/null || true
  if [ -s "$OUT/$name.png" ]; then
    echo "  $OUT/$name.png"
    WROTE=$((WROTE + 1))
  else
    echo "  $name FAILED" >&2
  fi
done

if [ "$WROTE" -eq 0 ]; then
  echo "no screenshot was written — nothing here was verified" >&2
  exit 1
fi

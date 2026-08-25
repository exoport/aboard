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
#    project (.board/run/shots/) rather than /tmp;
#  - ?nosse=1 is required: the live-reload stream never closes, so a headless
#    browser otherwise waits forever for network-idle and writes nothing.

set -e
cd "$(dirname "$0")/.."
OUT=".board/run/shots"

# Discover this project's port from the running instance rather than assuming
# one: the port is derived per project, so it is not a fixed number any more.
INSTANCE=".board/run/instance.json"
if [ -z "$PORT" ] && [ -f "$INSTANCE" ]; then
  PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$INSTANCE")
fi
if [ -z "$PORT" ]; then
  echo "no running board found ($INSTANCE missing) — start it with ./restart.sh" >&2
  exit 1
fi
# A board served under --base-path answers only under that prefix, so build every
# URL from the instance record rather than from the port alone.
BASE=$(sed -n 's/.*"url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$INSTANCE" 2>/dev/null)
[ -z "$BASE" ] && BASE="http://localhost:$PORT"

BROWSER=""
for c in chromium chromium-browser google-chrome google-chrome-stable; do
  if command -v "$c" >/dev/null 2>&1; then BROWSER="$c"; break; fi
done
[ -z "$BROWSER" ] && { echo "no chromium-family browser found" >&2; exit 1; }

if ! curl -sf -o /dev/null "$BASE/board.json"; then
  echo "server not answering on $BASE — start it with ./restart.sh" >&2
  exit 1
fi

TABS="$*"
[ -z "$TABS" ] && TABS="kanban dag diagram form markup"
mkdir -p "$OUT"

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
  timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --hide-scrollbars \
    --window-size="${WIDTH:-1280},${HEIGHT:-940}" --virtual-time-budget=10000 \
    --screenshot="$(pwd)/$OUT/$name.png" \
    "$url" 2>/dev/null || true
  if [ -s "$OUT/$name.png" ]; then
    echo "  $OUT/$name.png"
  else
    echo "  $name FAILED" >&2
  fi
done

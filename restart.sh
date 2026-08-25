#!/usr/bin/env sh
# Start this project's board, or report the one already running.
#
#   ./restart.sh              # start it, or point at the existing one
#   ./restart.sh -force       # stop the running one and start fresh
#   ./restart.sh -dev         # UI from disk, for iterating on pkg/aboard/web
#   ./restart.sh -name review # a second, isolated board in the same project
#   ./aboard status           # what is running here, and on which port
#
# A healthy board is left alone. Two Claude Code sessions in one project are
# meant to SHARE a board, so the second session running this script must not
# yank the server out from under the first — it just learns the URL. Use -force
# when you actually want to restart (after rebuilding, say).
#
# Only the pid recorded for this project+name is ever stopped; killing every
# process matching "./aboard" would take down other projects' boards too.

cd "$(dirname "$0")" || exit 1

# Pull -name out of the arguments so we act on the matching instance, and strip
# -force since it is ours, not the binary's. Anything else is passed through to
# `aboard serve` as a long flag.
NAME=""
FORCE=0
prev=""
ARGS=""
for arg in "$@"; do
  case "$prev" in -name|--name) NAME="$arg" ;; esac
  case "$arg" in
    -name=*|--name=*) NAME="${arg#*=}" ;;
  esac
  case "$arg" in
    -force|--force) FORCE=1 ;;
    # The old single-dash modes are gone; translate the two this script has
    # always advertised rather than handing cobra a flag it will refuse.
    -dev) ARGS="$ARGS --dev" ;;
    -name) ARGS="$ARGS --name" ;;
    *) ARGS="$ARGS $arg" ;;
  esac
  prev="$arg"
done
[ -z "$NAME" ] && NAME="$ABOARD_NAME"

if [ -n "$NAME" ]; then
  INSTANCE=".aboard/run/instance.$NAME.json"
else
  INSTANCE=".aboard/run/instance.json"
fi

read_field() { sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\{0,1\}\([^\",}]*\).*/\1/p" "$INSTANCE"; }

if [ -f "$INSTANCE" ]; then
  pid=$(read_field pid)
  url=$(read_field url)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    if [ "$FORCE" -eq 0 ]; then
      echo "aboard already running at $url (pid $pid)"
      echo "another session can just open that URL; pass -force to restart it"
      exit 0
    fi
    kill "$pid" 2>/dev/null && echo "stopped the running board (pid $pid)"
    i=0
    while [ $i -lt 30 ] && kill -0 "$pid" 2>/dev/null; do
      sleep 0.1
      i=$((i + 1))
    done
  fi
  rm -f "$INSTANCE"
fi

command -v go >/dev/null 2>&1 || { echo "no go toolchain on PATH" >&2; exit 1; }
go build -o aboard ./cmd/aboard || exit 1
# shellcheck disable=SC2086
exec ./aboard serve $ARGS

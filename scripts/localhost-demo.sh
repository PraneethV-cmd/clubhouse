#!/usr/bin/env bash
set -euo pipefail

ROOT="${CLUBHOUSE_DEMO_ROOT:-/tmp/clubhouse-demo}"
ADDR="${CLUBHOUSE_DEMO_ADDR:-127.0.0.1:8787}"
BIN="${CLUBHOUSE_BIN:-$(command -v clubhouse || true)}"

usage() {
  cat <<EOF
Usage: scripts/localhost-demo.sh [start|stop|reset|status]

Commands:
  start   Create host/alice/bob localhost rooms and print commands
  stop    Stop the demo host if it is running
  reset   Stop and delete $ROOT
  status  Show demo paths and current invite

Environment:
  CLUBHOUSE_DEMO_ROOT   default: /tmp/clubhouse-demo
  CLUBHOUSE_DEMO_ADDR   default: 127.0.0.1:8787
  CLUBHOUSE_BIN         default: first clubhouse on PATH
  CLUBHOUSE_OPEN_TERMINAL=1 opens host/alice/bob windows in macOS Terminal
EOF
}

ensure_bin() {
  if [[ -z "${BIN}" ]]; then
    if [[ -f ./go.mod ]]; then
      mkdir -p .context/bin
      go build -o .context/bin/clubhouse ./cmd/clubhouse
      BIN="$PWD/.context/bin/clubhouse"
    else
      echo "clubhouse not found on PATH, and this is not the repo root." >&2
      exit 1
    fi
  fi
}

write_config() {
  local dir="$1"
  local name="$2"
  local server="${3:-}"
  mkdir -p "$dir/.clubhouse"
  cat >"$dir/.clubhouse/config.txt" <<EOF
name = $name
room = localhost demo clubhouse
server = $server
addr = $ADDR
EOF
}

run_in() {
  local dir="$1"
  shift
  (cd "$dir" && "$BIN" "$@")
}

start() {
  ensure_bin
  mkdir -p "$ROOT/host" "$ROOT/alice" "$ROOT/bob"

  write_config "$ROOT/host" "host"
  write_config "$ROOT/alice" "alice"
  write_config "$ROOT/bob" "bob"

  if ! run_in "$ROOT/host" open --addr "$ADDR"; then
    echo "Could not open clubhouse on $ADDR." >&2
    echo "Try: $0 stop" >&2
    exit 1
  fi

  local invite
  invite="$(run_in "$ROOT/host" invite)"
  write_config "$ROOT/alice" "alice" "http://$ADDR"
  write_config "$ROOT/bob" "bob" "http://$ADDR"
  run_in "$ROOT/alice" enter "$invite"
  run_in "$ROOT/bob" enter "$invite"

  cat >"$ROOT/env.sh" <<EOF
export CLUBHOUSE_BIN="$BIN"
export CLUBHOUSE_DEMO_ROOT="$ROOT"
export CLUBHOUSE_DEMO_ADDR="$ADDR"
export CLUBHOUSE_INVITE="$invite"
EOF

  cat <<EOF

Demo ready.

Host:
  cd "$ROOT/host" && "$BIN" lounge

Alice:
  cd "$ROOT/alice" && "$BIN" lounge

Bob:
  cd "$ROOT/bob" && "$BIN" lounge

Invite:
  $invite

Stop demo:
  "$PWD/scripts/localhost-demo.sh" stop

Reset demo:
  "$PWD/scripts/localhost-demo.sh" reset
EOF

  if [[ "${CLUBHOUSE_OPEN_TERMINAL:-0}" == "1" && "$(uname -s)" == "Darwin" ]]; then
    open_terminal "host" "$ROOT/host"
    open_terminal "alice" "$ROOT/alice"
    open_terminal "bob" "$ROOT/bob"
  fi
}

open_terminal() {
  local title="$1"
  local dir="$2"
  local escaped_dir escaped_bin command
  printf -v escaped_dir '%q' "$dir"
  printf -v escaped_bin '%q' "$BIN"
  command="printf '\\033]0;${title}\\007'; cd ${escaped_dir}; exec ${escaped_bin} lounge"
  osascript - "$command" <<'EOF' >/dev/null
on run argv
  tell application "Terminal"
    do script (item 1 of argv)
    activate
  end tell
end run
EOF
}

stop() {
  ensure_bin
  if [[ -d "$ROOT/host" ]]; then
    run_in "$ROOT/host" close || true
  else
    echo "No demo host at $ROOT/host"
  fi
}

reset() {
  stop || true
  rm -rf "$ROOT"
  echo "Removed $ROOT"
}

status() {
  ensure_bin
  echo "Root: $ROOT"
  echo "Address: $ADDR"
  echo "Binary: $BIN"
  if [[ -d "$ROOT/host" ]]; then
    echo "Invite:"
    run_in "$ROOT/host" invite || true
  fi
}

cmd="${1:-start}"
case "$cmd" in
  start) start ;;
  stop) stop ;;
  reset) reset ;;
  status) status ;;
  -h|--help|help) usage ;;
  *) usage; exit 1 ;;
esac

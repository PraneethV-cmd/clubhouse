#!/usr/bin/env bash
set -euo pipefail

ROOT="${CLUBHOUSE_SMOKE_ROOT:-/tmp/clubhouse-hackathon-smoke}"
ADDR="${CLUBHOUSE_SMOKE_ADDR:-127.0.0.1:8787}"
BIN="${CLUBHOUSE_BIN:-$(command -v clubhouse || true)}"

ensure_bin() {
  if [[ -n "$BIN" ]]; then
    return
  fi
  if [[ ! -f go.mod ]]; then
    echo "clubhouse not found on PATH, and this is not the repo root." >&2
    exit 1
  fi
  mkdir -p .context/bin
  go build -o .context/bin/clubhouse ./cmd/clubhouse
  BIN="$PWD/.context/bin/clubhouse"
}

write_config() {
  local dir="$1"
  local name="$2"
  local room="$3"
  local server="${4:-}"
  mkdir -p "$dir/.clubhouse"
  cat >"$dir/.clubhouse/config.txt" <<EOF
name = $name
room = $room
server = $server
addr = $ADDR
EOF
}

run_in() {
  local dir="$1"
  shift
  (cd "$dir" && "$BIN" "$@")
}

hook_payload() {
  local cwd="$1"
  local path="$2"
  python3 - "$cwd" "$path" <<'PY'
import json
import sys

cwd, path = sys.argv[1:]
patch = f"*** Begin Patch\n*** Update File: {path}\n@@\n+// clubhouse smoke\n*** End Patch\n"
print(json.dumps({"tool_name": "apply_patch", "cwd": cwd, "tool_input": {"command": patch}}))
PY
}

stop_payload() {
  local cwd="$1"
  python3 - "$cwd" <<'PY'
import json
import sys

print(json.dumps({"tool_name": "Stop", "cwd": sys.argv[1], "tool_input": {}}))
PY
}

json_post() {
  local token="$1"
  local path="$2"
  local body="$3"
  python3 - "$ADDR" "$token" "$path" "$body" <<'PY'
import json
import sys
import urllib.error
import urllib.request

addr, token, path, body = sys.argv[1:]
req = urllib.request.Request(
    f"http://{addr}{path}",
    data=body.encode(),
    method="POST",
    headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
)
try:
    with urllib.request.urlopen(req, timeout=3) as resp:
        print(resp.status)
        print(resp.read().decode())
except urllib.error.HTTPError as e:
    print(e.code)
    print(e.read().decode())
PY
}

main() {
  ensure_bin

  echo "== clubhouse hackathon smoke =="
  echo "binary: $BIN"
  echo "root:   $ROOT"
  echo "addr:   $ADDR"

  "$BIN" close --port >/dev/null 2>&1 || true
  rm -rf "$ROOT"
  mkdir -p "$ROOT/host" "$ROOT/alice" "$ROOT/bob"

  write_config "$ROOT/host" "host" "hackathon clubhouse"
  write_config "$ROOT/alice" "alice" "hackathon clubhouse" "http://$ADDR"
  write_config "$ROOT/bob" "bob" "hackathon clubhouse" "http://$ADDR"

  echo
  echo "== open host =="
  run_in "$ROOT/host" open --addr "$ADDR"
  invite="$(run_in "$ROOT/host" invite)"
  echo "invite: $invite"

  echo
  echo "== enter alice/bob =="
  run_in "$ROOT/alice" enter "$invite"
  run_in "$ROOT/bob" enter "$invite"

  token="$(cat "$ROOT/host/.clubhouse/token")"

  echo
  echo "== prove collision prevention =="
  alice_join="$(json_post "$token" "/join" '{"name":"alice-agent"}')"
  bob_join="$(json_post "$token" "/join" '{"name":"bob-agent"}')"
  alice_id="$(printf '%s\n' "$alice_join" | tail -n 1 | python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("member_id") or data["MemberID"])')"
  bob_id="$(printf '%s\n' "$bob_join" | tail -n 1 | python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("member_id") or data["MemberID"])')"

  first_lock="$(json_post "$token" "/lock" "{\"MemberID\":\"$alice_id\",\"Path\":\"demo/payment.go\",\"Reason\":\"implementing checkout\"}")"
  second_lock="$(json_post "$token" "/lock" "{\"MemberID\":\"$bob_id\",\"Path\":\"demo/payment.go\",\"Reason\":\"also editing checkout\"}")"
  first_code="$(printf '%s\n' "$first_lock" | head -n 1)"
  second_code="$(printf '%s\n' "$second_lock" | head -n 1)"
  if [[ "$first_code" != "200" || "$second_code" != "409" ]]; then
    echo "collision smoke failed: first=$first_code second=$second_code" >&2
    echo "$second_lock" >&2
    exit 1
  fi
  echo "ok lock collision: alice got 200, bob got 409"

  echo
  echo "== prove Codex hook denial =="
  mkdir -p "$ROOT/alice/demo" "$ROOT/bob/demo"
  touch "$ROOT/alice/demo/hook-payment.go" "$ROOT/bob/demo/hook-payment.go"
  alice_hook_payload="$(hook_payload "$ROOT/alice" "demo/hook-payment.go")"
  bob_hook_payload="$(hook_payload "$ROOT/bob" "demo/hook-payment.go")"
  printf '%s\n' "$alice_hook_payload" | run_in "$ROOT/alice" hook pre >/tmp/clubhouse-smoke-alice-hook.out 2>&1
  set +e
  bob_hook_out="$(printf '%s\n' "$bob_hook_payload" | run_in "$ROOT/bob" hook pre 2>&1)"
  bob_hook_code="$?"
  set -e
  if [[ "$bob_hook_code" != "2" || "$bob_hook_out" != *"being edited"* ]]; then
    echo "hook denial smoke failed: exit=$bob_hook_code" >&2
    echo "$bob_hook_out" >&2
    exit 1
  fi
  printf '%s\n' "$(stop_payload "$ROOT/alice")" | run_in "$ROOT/alice" hook stop >/dev/null 2>&1 || true
  echo "ok hook denial: bob was blocked by alice's lock"

  echo
  echo "== prove MCP startup =="
  python3 - "$BIN" "$ROOT/host" <<'PY'
import json
import subprocess
import sys

bin_path, cwd = sys.argv[1:]
proc = subprocess.Popen(
    [bin_path, "mcp"],
    cwd=cwd,
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
)
try:
    init = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "clubhouse-smoke", "version": "0"},
        },
    }
    proc.stdin.write(json.dumps(init) + "\n")
    proc.stdin.flush()
    line = proc.stdout.readline()
    data = json.loads(line)
    if "result" not in data or data["result"].get("serverInfo", {}).get("name") != "clubhouse":
        raise SystemExit(f"bad initialize response: {line!r}")
finally:
    proc.terminate()
    try:
        proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        proc.kill()
print("ok MCP initialize returned serverInfo=clubhouse")
PY

  echo
  echo "== prove lounge render =="
  python3 - "$BIN" "$ROOT/host" "$ROOT/lounge-smoke.ansi" <<'PY'
import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

bin_path, cwd, out_path = sys.argv[1:]
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
proc = subprocess.Popen([bin_path, "lounge"], cwd=cwd, stdin=slave, stdout=slave, stderr=slave, close_fds=True)
os.close(slave)
raw = bytearray()
deadline = time.time() + 4
try:
    while time.time() < deadline:
        ready, _, _ = select.select([master], [], [], 0.2)
        if ready:
            try:
                chunk = os.read(master, 8192)
            except OSError:
                break
            if not chunk:
                break
            raw.extend(chunk)
        if b"PEOPLE" in raw and b"LOCKS" in raw:
            break
    os.write(master, b"q")
    try:
        proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        proc.send_signal(signal.SIGTERM)
        try:
            proc.wait(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=2)
finally:
    os.close(master)
with open(out_path, "wb") as f:
    f.write(raw)
text = re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b"", bytes(raw)).decode("utf-8", "ignore")
if "PEOPLE" not in text or "LOCKS" not in text:
    raise SystemExit(f"lounge smoke did not find expected labels; raw saved to {out_path}")
print(f"ok lounge rendered PEOPLE/LOCKS; raw capture: {out_path}")
PY

  echo
  echo "== prove menu =="
  "$BIN" menu --out "$ROOT/menu" >/dev/null
  test -f "$ROOT/menu/index.md"
  test -f "$ROOT/menu/graph.md"
  echo "ok menu generated: $ROOT/menu/index.md"

  echo
  echo "== demo ready =="
  cat <<EOF
Open lounge:
  cd "$ROOT/host" && "$BIN" lounge

Ask Codex after restart:
  who's in the clubhouse?

Share invite:
  $invite

Stop:
  cd "$ROOT/host" && "$BIN" close

Reset port if needed:
  "$BIN" close --port
EOF
}

main "$@"

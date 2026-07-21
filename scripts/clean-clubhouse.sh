#!/usr/bin/env bash
set -euo pipefail

APPLY=0
if [[ "${1:-}" == "--apply" ]]; then
  APPLY=1
elif [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<EOF
Usage: scripts/clean-clubhouse.sh [--apply]

Without --apply, prints the Clubhouse processes/files that would be cleaned.
With --apply, stops Clubhouse processes and removes local install/demo artifacts.
EOF
  exit 0
elif [[ $# -gt 0 ]]; then
  echo "unknown argument: $1" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOME_DIR="${HOME}"

installed_bins=(
  "$HOME_DIR/.local/bin/clubhouse"
  "$HOME_DIR/go/bin/clubhouse"
  "/usr/local/bin/clubhouse"
  "/opt/homebrew/bin/clubhouse"
)

repo_paths=(
  "$ROOT/.clubhouse"
  "$ROOT/menu"
  "$ROOT/.context/bin/clubhouse"
  "$ROOT/.context/site-release"
  "$ROOT/public"
  "$ROOT/site/public"
  "$ROOT/site/static/releases"
  "$ROOT/site/.hugo_build.lock"
  "$ROOT/internal/server/invite"
)

tmp_paths=(
  "${CLUBHOUSE_DEMO_ROOT:-/tmp/clubhouse-demo}"
  "${CLUBHOUSE_SMOKE_ROOT:-/tmp/clubhouse-hackathon-smoke}"
  "/tmp/clubhouse"
  "/tmp/clubhouse-menu-smoke"
)

appledouble_roots=(
  "$ROOT"
)

say() {
  printf '%s\n' "$*"
}

remove_path() {
  local path="$1"
  if [[ ! -e "$path" ]]; then
    return
  fi
  if [[ "$APPLY" -eq 1 ]]; then
    if rm -rf "$path" 2>/dev/null; then
      say "removed $path"
    else
      say "could not remove $path; remove it manually or rerun with permissions"
    fi
  else
    say "would remove $path"
  fi
}

kill_processes() {
  local pids
  pids="$(ps -axo pid=,command= | awk '$0 ~ /[c]lubhouse (__serve|mcp|lounge|watch)/ || $0 ~ /\/[c]lubhouse (__serve|mcp|lounge|watch)/ { print $1 }' || true)"
  if [[ -z "$pids" ]]; then
    say "no clubhouse runtime processes found"
    return
  fi
  if [[ "$APPLY" -eq 1 ]]; then
    # shellcheck disable=SC2086
    kill $pids 2>/dev/null || true
    say "stopped clubhouse processes: $pids"
  else
    say "would stop clubhouse processes: $pids"
  fi
}

remove_appledouble() {
	local root="$1"
	if [[ ! -d "$root" ]]; then
		return
	fi
	while IFS= read -r path; do
		local rel="${path#$ROOT/}"
		if git -C "$ROOT" ls-files --error-unmatch "$rel" >/dev/null 2>&1; then
			continue
		fi
		if [[ "$APPLY" -eq 1 ]]; then
			rm -f "$path" && say "removed $path"
		else
			say "would remove $path"
		fi
	done < <(find "$root" \( -path "$ROOT/.conductor" -o -path "$ROOT/.conductor/*" \) -prune -o -name '._*' -type f -print)
}

say "Clubhouse cleanup root: $ROOT"
if [[ "$APPLY" -eq 0 ]]; then
  say "Dry run. Re-run with --apply to remove these artifacts."
fi

kill_processes

for path in "${installed_bins[@]}"; do
  remove_path "$path"
done

for path in "${repo_paths[@]}"; do
  remove_path "$path"
done

for path in "${tmp_paths[@]}"; do
  remove_path "$path"
done

for root in "${appledouble_roots[@]}"; do
  remove_appledouble "$root"
done

#!/usr/bin/env bash
set -euo pipefail

ROOT="${CLUBHOUSE_SMOKE_ROOT:-/tmp/clubhouse-hackathon-smoke}"
WORKDIR="${CLUBHOUSE_CODEX_SMOKE_DIR:-$ROOT/host}"

if [[ ! -d "$WORKDIR" ]]; then
  echo "missing smoke workdir: $WORKDIR" >&2
  echo "run scripts/hackathon-smoke.sh first" >&2
  exit 1
fi

codex exec \
  -C "$WORKDIR" \
  --skip-git-repo-check \
  --dangerously-bypass-approvals-and-sandbox \
  --dangerously-bypass-hook-trust \
  'Use the clubhouse status MCP tool exactly once. Print who is in the clubhouse and the invite link. Do not edit files.'

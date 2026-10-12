#!/usr/bin/env bash
# Starts the fixture application for the scripts here, and stops it again.
#
# Self-contained on purpose: a measurement script that needs somebody to have started a server first is
# a measurement script whose numbers nobody reproduces. No pkill anywhere - the pattern would match this
# script's own command line and it would kill itself before printing anything.
set -euo pipefail

uito_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
uito_bin="$uito_root/bin/orchestrator"
uito_pids=()

uito_cleanup() {
  for pid in "${uito_pids[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
}
trap uito_cleanup EXIT

# fixture_start <fault> [slow_ms] -> echoes the base URL
fixture_start() {
  local fault=$1 slow=${2:-900}
  local log
  log=$(mktemp -t uito-fixture-XXXX.log)
  UITO_FAULT="$fault" UITO_SLOW_MS="$slow" "$uito_bin" serve --addr 127.0.0.1:0 >"$log" 2>&1 &
  local pid=$!
  uito_pids+=("$pid")
  for _ in $(seq 1 100); do
    if [ -s "$log" ]; then
      head -1 "$log"
      return 0
    fi
    kill -0 "$pid" 2>/dev/null || { echo "fixture exited:" >&2; cat "$log" >&2; return 1; }
    sleep 0.05
  done
  echo "fixture never printed its address" >&2
  return 1
}

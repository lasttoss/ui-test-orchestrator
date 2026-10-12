#!/usr/bin/env bash
# Measures the same four timing-sensitive checks, written two ways, against a fixture whose slow
# endpoint is deterministic: every third request to the events endpoint takes 900 ms.
#
# A flake nobody can reproduce is a flake nobody can measure, so the fault is a fixed pattern rather
# than a random one, and both suites run the same number of times.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib-fixture.sh

# Build first. A measurement script that uses whatever binary happens to be in bin/ reports the numbers of
# the last build, which is how a fixed bug keeps showing up in a table.
go build -o bin/orchestrator ./cmd/orchestrator

runs=${RUNS:-6}
echo "runs per configuration: $runs"

measure() {
  local dir=$1 attempts=$2 label=$3 url=$4
  local total=0 failures=0 flakes=0
  for _ in $(seq 1 "$runs"); do
    out=$(bin/orchestrator run --base-url "$url" --scenarios "$dir" --parallel 3 \
          --attempts "$attempts" --no-state --report /tmp/uito-measure --quiet 2>&1 || true)
    line=$(printf '%s\n' "$out" | grep -E '^[0-9]+ scenarios' | head -1)
    n=$(printf '%s' "$line" | awk '{print $1}'); k=$(printf '%s' "$line" | awk '{print $7}')
    f=$(printf '%s' "$line" | awk '{print $10}')
    total=$((total + ${n:-0})); failures=$((failures + ${f:-0})); flakes=$((flakes + ${k:-0}))
  done
  printf '| %-42s | %d | %4d | %6s%% | %6s%% |\n' "$label" "$attempts" "$total" \
    "$(awk -v f="$failures" -v n="$total" 'BEGIN{printf "%.1f", (n?100*f/n:0)}')" \
    "$(awk -v k="$flakes" -v n="$total" 'BEGIN{printf "%.1f", (n?100*k/n:0)}')"
}

healthy=$(fixture_start "")
echo "fixture (no fault): $healthy"
flaky=$(fixture_start "slow" 900)
echo "fixture (every third events request takes 900 ms): $flaky"
echo
echo "| suite | attempts | runs | failed | flaked |"
echo "|---|---|---|---|---|"
measure scenarios-naive 1 "asserting immediately, healthy server" "$healthy"
measure scenarios-naive 1 "asserting immediately, slow server" "$flaky"
measure scenarios       1 "waiting with assert_eventually, healthy server" "$healthy"
measure scenarios       1 "waiting with assert_eventually, slow server" "$flaky"
measure scenarios-naive 2 "asserting immediately, one retry, slow server" "$flaky"
measure scenarios       2 "waiting, one retry, slow server" "$flaky"

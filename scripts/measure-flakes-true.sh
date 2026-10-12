#!/usr/bin/env bash
# A real flake: the same two checks, one waiting 250 ms for an endpoint that sometimes takes 900 ms, and
# one waiting long enough. The first fails and then passes on the retry, which is what this tool calls a
# flake; the second does not.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib-fixture.sh

# Build first. A measurement script that uses whatever binary happens to be in bin/ reports the numbers of
# the last build, which is how a fixed bug keeps showing up in a table.
go build -o bin/orchestrator ./cmd/orchestrator

runs=${RUNS:-8}
flaky=$(fixture_start "slow" 900)
echo "fixture (every third events request takes 900 ms): $flaky"
echo "runs: $runs, attempts: 2 (a flake is a failure followed by a pass)"
echo
printf '| %-46s | %5s | %8s | %8s |\n' "suite" "runs" "failed" "flaked"
printf '|---|---|---|---|\n'
for pair in "scenarios-flaky:waiting 250 ms (shorter than the slow response)" "scenarios:waiting 8 s"; do
  dir=${pair%%:*}; label=${pair#*:}
  total=0; failed=0; flaked=0
  for _ in $(seq 1 "$runs"); do
    out=$(bin/orchestrator run --base-url "$flaky" --scenarios "$dir" \
          --parallel 3 --attempts 2 --no-state --report /tmp/uito-flakes --quiet 2>&1 || true)
    line=$(printf '%s\n' "$out" | grep -E '^[0-9]+ scenarios' | head -1)
    n=$(printf '%s' "$line" | awk '{print $1}'); k=$(printf '%s' "$line" | awk '{print $7}')
    f=$(printf '%s' "$line" | awk '{print $10}')
    total=$((total+${n:-0})); failed=$((failed+${f:-0})); flaked=$((flaked+${k:-0}))
  done
  printf '| %-46s | %5d | %8s%% | %8s%% |\n' "$label" "$total" \
    "$(awk -v f=$failed -v n=$total 'BEGIN{printf "%.1f", (n?100*f/n:0)}')" \
    "$(awk -v k=$flaked -v n=$total 'BEGIN{printf "%.1f", (n?100*k/n:0)}')"
done

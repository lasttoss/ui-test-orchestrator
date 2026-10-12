#!/usr/bin/env bash
# Shows the flake policy doing its job: quarantine, and release.
#
# The fault used here changes a heading, so the scenarios that read that heading fail on one server and
# pass on another - the same scenario ids both times, which is what the policy keys on. Two whole runs
# failing quarantine a scenario; three whole runs passing release it.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib-fixture.sh

# Build first. A measurement script that uses whatever binary happens to be in bin/ reports the numbers of
# the last build, which is how a fixed bug keeps showing up in a table.
go build -o bin/orchestrator ./cmd/orchestrator

state=${STATE:-state/quarantine.json}
mkdir -p "$(dirname "$state")"
printf '{"version":1,"policy":{"quarantine_after":2,"unquarantine_after":3},"scenarios":{},"runs":0}\n' > "$state"

broken=$(fixture_start "text")
healthy=$(fixture_start "")
echo "fixture (heading says Event instead of Events): $broken"
echo "fixture (healthy): $healthy"
echo "state file: $state, reset to empty"

run() {
  local label=$1 url=$2
  echo
  echo "--- $label"
  set +e
  out=$(bin/orchestrator run --base-url "$url" --scenarios scenarios --parallel 3 --attempts 1 \
        --state "$state" --report report --quiet 2>&1)
  code=$?
  set -e
  printf '%s\n' "$out" | grep -cE '^(QUAR|FAIL)' | sed 's/^/    failing scenarios: /'
  printf '%s\n' "$out" | grep -E 'flake history' | sed 's/^/    /'
  printf '%s\n' "$out" | grep -E 'quarantined' | head -1 | sed 's/^/    /'
  echo "    exit code: $code (0 means the build passes)"
}

run "run 1 against the broken server" "$broken"
run "run 2 against the broken server: the policy quarantines what keeps failing" "$broken"
run "run 3 against the broken server: still failing, build still green" "$broken"
run "run 4 against a healthy server" "$healthy"
run "run 5 against a healthy server" "$healthy"
run "run 6 against a healthy server: three passes in a row release it" "$healthy"
echo
bin/orchestrator policy --state "$state" | sed 's/^/  /'

#!/usr/bin/env bash
# What a visual assertion catches that a text assertion cannot.
#
# The fixture's visual fault changes one status badge from green to red and edits nothing else. Every
# word on the page is still there, so every text assertion still passes; the screenshot assertion is the
# only one that notices. This script prints both results, side by side, from the same suite.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib-fixture.sh

# Build first. A measurement script that uses whatever binary happens to be in bin/ reports the numbers of
# the last build, which is how a fixed bug keeps showing up in a table.
go build -o bin/orchestrator ./cmd/orchestrator

healthy=$(fixture_start "")
broken=$(fixture_start "visual")
echo "fixture (healthy): $healthy"
echo "fixture (badge green -> red, words unchanged): $broken"
echo

run() {
  local label=$1 url=$2 dir=${3:-scenarios} report=$4
  local out
  out=$(bin/orchestrator run --base-url "$url" --scenarios "$dir" --parallel 3 --attempts 1 \
        --no-state --report "$report" 2>&1 || true)
  printf '%s\n' "$out" | grep -E '^([0-9]+ scenarios)' | sed "s/^/  $label: /"
}

echo "--- text and DOM assertions"
run "healthy" "$healthy" scenarios /tmp/uito-visual-ok
run "broken " "$broken"  scenarios /tmp/uito-visual-broken

echo
echo "--- the visual assertion on the broken page, with its numbers"
bin/orchestrator run --base-url "$broken" --scenarios scenarios --parallel 3 --attempts 1 \
  --no-state --report /tmp/uito-visual-detail 2>&1 | grep -E "visual-badge" | sed 's/^/  /' || true

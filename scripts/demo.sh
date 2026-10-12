#!/usr/bin/env bash
# One command: build, serve the fixture app, run the suite, write the report, stop the app.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib-fixture.sh

go build -o bin/orchestrator ./cmd/orchestrator

app=$(fixture_start "${UITO_FAULT:-}")
echo "fixture app: $app"
echo

set +e
bin/orchestrator run --base-url "$app" --scenarios "${UITO_SCENARIOS:-scenarios}" \
  --parallel "${UITO_PARALLEL:-3}" --attempts "${UITO_ATTEMPTS:-2}" \
  --report "${UITO_REPORT:-report}" --state "${UITO_STATE:-state/quarantine.json}" "$@"
code=$?
set -e

echo
echo "report: ${UITO_REPORT:-report}/index.html (self-contained: the screenshots are inlined)"
exit "$code"

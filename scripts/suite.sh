#!/usr/bin/env bash
# Run the suite against any URL, with a browser of your own if you have one running.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${1:?usage: scripts/suite.sh <base-url> [extra orchestrator flags]}"
url=$1; shift
go build -o bin/orchestrator ./cmd/orchestrator
bin/orchestrator run --base-url "$url" --scenarios scenarios --parallel 3 --attempts 2 \
  --report report --state state/quarantine.json "$@"

#!/usr/bin/env bash
# Lint the chart, render every branch, and validate the result.
#
# The branches are the point: a chart whose only tested path is the default one has an untested path for
# every value a user sets.
set -euo pipefail
cd "$(dirname "$0")/.."
chart=charts/ui-test-orchestrator

helm lint --strict "$chart"

render() {
  local label=$1; shift
  local manifest
  manifest=$(helm template "$chart" -f lab/values-kind.yaml "$@" 2>&1) || { echo "render failed: $label"; echo "$manifest"; exit 1; }
  printf '%s\n' "$manifest" | docker run --rm -i ghcr.io/yannh/kubeconform:v0.7.0 \
    -kubernetes-version 1.31.0 -strict -summary - || { echo "invalid manifest: $label"; exit 1; }
  printf '  %s\n' "$label"
}

echo "rendering every branch:"
render "defaults (Job, chrome sidecar, NetworkPolicy)" 
render "cronJob.enabled=true" --set cronJob.enabled=true
render "chrome.enabled=false with an external browser" --set chrome.enabled=false --set cdpURL=http://browser:9222
render "NetworkPolicy off, own scenarios" --set networkPolicy.enabled=false --set scenarios.existingConfigMap=my-scenarios
echo "chart ok: every branch rendered and validated"

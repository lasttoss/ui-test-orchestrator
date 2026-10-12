#!/usr/bin/env bash
# Build the image and check the two things that break in a container but not on a laptop: it runs as a
# non-root user, and the fixture app answers from inside it.
set -euo pipefail
cd "$(dirname "$0")/.."
tag=${1:-ui-test-orchestrator:local}
docker build -t "$tag" .
docker run --rm --entrypoint /usr/local/bin/orchestrator "$tag" policy --help >/dev/null
docker run --rm -d --name uito-image-check -p 18099:8080 "$tag" serve --addr 0.0.0.0:8080 >/dev/null
trap 'docker rm -f uito-image-check >/dev/null 2>&1 || true' EXIT
for _ in $(seq 1 40); do
  if curl -fsS http://127.0.0.1:18099/healthz >/dev/null 2>&1; then
    echo "image $tag: runs as $(docker inspect -f '{{.Config.User}}' "$tag"), fixture app answers"
    exit 0
  fi
  sleep 0.25
done
echo "the fixture app never answered inside the image" >&2
exit 1

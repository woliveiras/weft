#!/usr/bin/env sh
# Send only proxy sources to the dedicated daemon; never build on the host daemon.
set -eu
container=${WEFT_TEST_SANDBOX_CONTAINER:-weft-sandbox-daemon-1}
service=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$container")
[ "$service" = sandbox-daemon ] || { echo 'Expected dedicated sandbox-daemon' >&2; exit 1; }
cd "$(dirname "$0")/.."
tar -cf - Dockerfile.egress server/go.mod server/go.sum server/internal/domain server/internal/egress server/cmd/weft-egress |
 docker exec -i "$container" docker build -f Dockerfile.egress -t weft/egress:latest -

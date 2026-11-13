#!/usr/bin/env sh
# Use the weftd image and Compose mTLS transport, without a host Docker socket.
# Offline by default. Requires cached Alpine, BuildKit, and the egress proxy.
# WEFT_TEST_ENV_TLS_ONLINE=1 runs the production Buildx commands against Docker
# Hub. It can download images and needs registry access through the test proxy.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
project=${COMPOSE_PROJECT_NAME:-weft}
runner=${WEFT_TEST_WEFTD_IMAGE:-weft/weftd:dev}
docker network inspect "${project}_sandbox" >/dev/null
docker volume inspect "${project}_sandbox-certs-client" >/dev/null
work=$(mktemp -d)
trap 'rm -rf "$work"; rm -f "$work.tar"' EXIT
online=${WEFT_TEST_ENV_TLS_ONLINE:-0}
if [ "$online" != 1 ]; then
  docker image inspect alpine:3.22 >/dev/null 2>&1 || {
    echo 'Offline TLS integration requires alpine:3.22 in the host Docker image cache.' >&2
    exit 1
  }
  mkdir "$work/base"
  docker image save alpine:3.22 > "$work/alpine.tar"
  tar -xf "$work/alpine.tar" -C "$work/base"
  rm "$work/alpine.tar"
fi
docker run --pull=never --rm --network none \
  -v "$root:/repo:ro" \
  -v weft-gomod:/go/pkg/mod -v weft-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 -w /repo/server golang:1.26-alpine \
  sh -ec 'go test -c -o /tmp/environment.test ./internal/environment >&2; cat /tmp/environment.test' \
  > "$work/environment.test"
chmod 755 "$work/environment.test"
tar -cf "$work.tar" -C "$work" .
docker run --pull=never --rm -i --network "${project}_sandbox" --user 10001:10001 \
  --tmpfs /var/lib/weft/.docker/contexts:uid=10001,gid=10001,mode=0700 \
  -v "${project}_sandbox-certs-client:/certs/client:ro" \
  -e DOCKER_TLS_VERIFY=1 -e DOCKER_CERT_PATH=/certs/client -e WEFT_TEST_ENV_TLS=1 \
  -e WEFT_TEST_ENV_TLS_ONLINE="$online" -e WEFT_TEST_TLS_REGISTRY_HOSTS --entrypoint sh "$runner" -ec '
    work=$(mktemp -d)
    tar -xf - -C "$work"
    if [ "$WEFT_TEST_ENV_TLS_ONLINE" != 1 ]; then
      export WEFT_TEST_TLS_OCI_LAYOUT="$work/base"
    fi
    exec "$work/environment.test" -test.run "^TestBoundedBuildRealTLS$" -test.v -test.timeout 6m
  ' < "$work.tar"

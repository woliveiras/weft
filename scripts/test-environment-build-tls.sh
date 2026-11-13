#!/usr/bin/env sh
# Use the weftd image and Compose mTLS transport, without a host Docker socket.
# Use a cached Alpine image as an OCI layout consumed directly by BuildKit.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
project=${COMPOSE_PROJECT_NAME:-weft}
runner=${WEFT_TEST_WEFTD_IMAGE:-weft/weftd:dev}
docker network inspect "${project}_sandbox" >/dev/null
docker volume inspect "${project}_sandbox-certs-client" >/dev/null
work=$(mktemp -d "$root/server/.weft-test-tls.XXXXXX")
trap 'rm -rf "$work"' EXIT
chmod 755 "$work"
mkdir "$work/base"
docker image inspect alpine:3.22 >/dev/null
docker image save alpine:3.22 > "$work/alpine.tar"
tar -xf "$work/alpine.tar" -C "$work/base"
docker run --pull=never --rm --network none \
  -v "$root:/repo:ro" -v "$work:/out" \
  -v weft-gomod:/go/pkg/mod -v weft-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 -w /repo/server golang:1.26-alpine \
  go test -c -o /out/environment.test ./internal/environment
docker run --pull=never --rm --network "${project}_sandbox" --user 10001:10001 \
  --tmpfs /var/lib/weft/.docker/contexts:uid=10001,gid=10001,mode=0700 \
  -v "${project}_sandbox-certs-client:/certs/client:ro" -v "$work:/test:ro" \
  -e DOCKER_TLS_VERIFY=1 -e DOCKER_CERT_PATH=/certs/client -e WEFT_TEST_ENV_TLS=1 \
  --entrypoint /test/environment.test "$runner" \
  -test.run '^TestBoundedBuildRealTLS$' -test.v -test.timeout 6m

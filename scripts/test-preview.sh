#!/usr/bin/env sh
# Real, test-owned preview on the existing dedicated daemon. No deployment.
set -eu
container=${WEFT_TEST_SANDBOX_CONTAINER:-weft-sandbox-daemon-1}
service=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$container")
[ "$service" = sandbox-daemon ] || { echo 'Expected dedicated sandbox-daemon' >&2; exit 1; }
task_tmp=$(mktemp -d)
trap 'rm -f "$task_tmp/docker-sandbox" "$task_tmp/weft-preview-linux"; rmdir "$task_tmp"' EXIT HUP INT TERM
cat > "$task_tmp/docker-sandbox" <<'WRAPPER'
#!/usr/bin/env sh
exec docker exec -i "$WEFT_TEST_SANDBOX_CONTAINER" docker "$@"
WRAPPER
chmod 700 "$task_tmp/docker-sandbox"
arch=$(docker exec "$container" uname -m)
case "$arch" in aarch64) arch=arm64;; x86_64) arch=amd64;; *) echo 'Unsupported daemon architecture' >&2; exit 1;; esac
export WEFT_TEST_SANDBOX_CONTAINER="$container"
export WEFT_TEST_ENV_DOCKER_BIN="$task_tmp/docker-sandbox"
export WEFT_TEST_PREVIEW_RELAY="$task_tmp/weft-preview-linux"
cd "$(dirname "$0")/../server"
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "$WEFT_TEST_PREVIEW_RELAY" ./cmd/weft-preview
go test ./internal/review -run '^TestPreviewRealDocker$' -count=1 -v -timeout 4m

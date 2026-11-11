#!/usr/bin/env sh
# Runs only temporary test builds/containers inside an already running dedicated
# sandbox daemon. Does not start/reconfigure the application or read its secrets.
set -eu
container=${WEFT_TEST_SANDBOX_CONTAINER:-weft-sandbox-daemon-1}
service=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$container")
[ "$service" = sandbox-daemon ] || { echo 'Expected the dedicated sandbox-daemon Compose service' >&2; exit 1; }
docker exec "$container" docker version --format '{{.Server.Version}}'
task_tmp=$(mktemp -d)
trap 'rm -f "$task_tmp/docker-sandbox"; rmdir "$task_tmp"' EXIT HUP INT TERM
cat > "$task_tmp/docker-sandbox" <<'WRAPPER'
#!/usr/bin/env sh
exec docker exec -i "$WEFT_TEST_SANDBOX_CONTAINER" docker "$@"
WRAPPER
chmod 700 "$task_tmp/docker-sandbox"
export WEFT_TEST_SANDBOX_CONTAINER="$container"
export WEFT_TEST_ENV_DOCKER_BIN="$task_tmp/docker-sandbox"
cd "$(dirname "$0")/../server"
go test ./internal/environment -run '^(TestBuildContextRealSandbox|TestProposalRealSandbox|TestAgentProposalRealSandbox|TestReuseRealSandbox|TestBuildCapacityRealSandbox|TestSharedCleanupRealSandbox)$' -count=1 -v -timeout 10m

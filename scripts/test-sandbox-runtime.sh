#!/usr/bin/env sh
# Integration test for the dedicated sandbox runtime (docs/sandbox-runtime.md).
# Runs against the Compose stack and checks, with real containers:
#
#   1. sandbox-daemon is healthy, accepts weftd's TLS client, and refuses
#      clients without a certificate;
#   2. the default sandbox image exists there and NOT on the host daemon;
#   3. a sandbox without network (Weft's default) reaches nothing;
#   4. a sandbox on a bridge network cannot reach the sandbox daemon or any
#      platform container (private addresses), but can reach the internet
#      (skipped when the machine is offline);
#   5. the host daemon runs no Weft sandbox containers.
#
# Usage: make test-sandbox   (starts sandbox-daemon and sandbox-image if needed)
set -eu

COMPOSE=${COMPOSE:-docker compose}
IMAGE=weft/sandbox:latest
failures=0

pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1"; failures=$((failures + 1)); }
skip() { echo "SKIP  $1"; }

$COMPOSE up -d --wait sandbox-daemon >/dev/null
$COMPOSE up sandbox-image >/dev/null 2>&1 || { echo "sandbox-image build failed; run: $COMPOSE up sandbox-image" >&2; exit 1; }

daemon_id=$($COMPOSE ps -q sandbox-daemon)
in_daemon() { docker exec "$daemon_id" "$@"; }

# 1. TLS client access, exactly as weftd uses it.
if $COMPOSE run --rm --no-deps -T sandbox-image docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
	pass "sandbox-daemon answers over mutual TLS"
else
	fail "sandbox-daemon does not answer over TLS"
fi
if ! sh "$(dirname "$0")/test-sandbox-mtls.sh"; then
	fail "sandbox-daemon missing-certificate check"
fi

# 2. Image location.
if in_daemon docker image inspect "$IMAGE" >/dev/null 2>&1; then
	pass "$IMAGE is built inside sandbox-daemon"
else
	fail "$IMAGE is missing from sandbox-daemon"
fi
if docker image inspect "$IMAGE" >/dev/null 2>&1; then
	echo "NOTE  the host daemon also has $IMAGE (from an older setup); Weft no longer uses it"
fi

# Probe helper: prints "open" or "blocked" for host:port from a sandbox.
probe() { # network host port
	in_daemon docker run --rm --network "$1" --cap-drop ALL --security-opt no-new-privileges "$IMAGE" \
		python3 -c "import socket,sys
s=socket.socket(); s.settimeout(4)
try:
    s.connect((sys.argv[1], int(sys.argv[2]))); print('open')
except Exception:
    print('blocked')" "$2" "$3" 2>/dev/null || echo blocked
}

# Addresses on the platform side: sandbox-daemon on the Compose network,
# its bridge gateway, and weftd when it is running.
daemon_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' "$daemon_id" | awk '{print $1}')
gateway=$(in_daemon docker network inspect bridge -f '{{(index .IPAM.Config 0).Gateway}}')
weftd_id=$($COMPOSE ps -q weftd 2>/dev/null || true)
weftd_ip=
if [ -n "$weftd_id" ]; then
	weftd_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' "$weftd_id" | awk '{print $1}')
fi

# 3. No network.
[ "$(probe none 1.1.1.1 443)" = blocked ] && pass "network none: internet unreachable" || fail "network none reached the internet"
[ "$(probe none "$daemon_ip" 2376)" = blocked ] && pass "network none: sandbox-daemon unreachable" || fail "network none reached sandbox-daemon"

# 4. Bridge network.
[ "$(probe bridge "$daemon_ip" 2376)" = blocked ] && pass "bridge: sandbox-daemon ($daemon_ip:2376) blocked" || fail "bridge reached sandbox-daemon at $daemon_ip:2376"
[ "$(probe bridge "$gateway" 2376)" = blocked ] && pass "bridge: daemon via gateway ($gateway:2376) blocked" || fail "bridge reached the daemon through $gateway"
if [ -n "$weftd_ip" ]; then
	[ "$(probe bridge "$weftd_ip" 8080)" = blocked ] && pass "bridge: weftd ($weftd_ip:8080) blocked" || fail "bridge reached weftd at $weftd_ip:8080"
else
	skip "bridge: weftd is not running"
fi
if in_daemon sh -c 'wget -q -T 4 -O /dev/null https://proxy.golang.org' 2>/dev/null; then
	[ "$(probe bridge proxy.golang.org 443)" = open ] && pass "bridge: internet reachable (proxy.golang.org:443)" || fail "bridge could not reach the internet"
else
	skip "bridge: sandbox-daemon itself is offline"
fi

# 5. Host daemon untouched.
if [ -z "$(docker ps -aq --filter label=weft.sandbox=true)" ]; then
	pass "host daemon runs no Weft sandboxes"
else
	fail "host daemon has Weft sandbox containers: $(docker ps -a --filter label=weft.sandbox=true --format '{{.Names}}')"
fi

if [ "$failures" -gt 0 ]; then
	echo "$failures check(s) failed"
	exit 1
fi
echo "sandbox runtime: all checks passed"

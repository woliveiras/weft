#!/usr/bin/env sh
# Read-only TLS checks against an already running Compose sandbox daemon.
set -eu

COMPOSE=${COMPOSE:-docker compose}
daemon_id=$($COMPOSE ps -q sandbox-daemon)
if [ -z "$daemon_id" ]; then
	echo "FAIL  sandbox-daemon must already be running" >&2
	exit 1
fi

# Establish that the same endpoint works with the configured client identity.
docker exec "$daemon_id" /weft/healthcheck.sh
if output=$(docker exec "$daemon_id" sh -c '
	printf "GET /_ping HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" |
		timeout 5 openssl s_client -quiet -tls1_3 -connect 127.0.0.1:2376 \
			-verify_ip 127.0.0.1 -verify_return_error -CAfile /certs/server/ca.pem
' 2>&1); then
	echo "FAIL  sandbox-daemon accepted a client without a certificate" >&2
	exit 1
fi

# A timeout, transport failure or untrusted server is insufficient evidence.
case "$output" in
	*"alert certificate required"*) echo "PASS  sandbox-daemon refused a client without a certificate" ;;
	*) printf "FAIL  expected a TLS certificate-required alert\n%s\n" "$output" >&2; exit 1 ;;
esac

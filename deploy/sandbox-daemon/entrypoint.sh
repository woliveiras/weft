#!/bin/sh
# Entrypoint of the sandbox-daemon service: the Docker daemon dedicated to
# Weft sandboxes (Docker-in-Docker). It starts dockerd through the image's
# own entrypoint (which generates the TLS certificates), then installs the
# firewall rules that keep sandbox containers away from the platform:
#
#   * sandbox -> private networks (the Compose network with weftd,
#     postgres, nats, ...; the host's LAN; cloud metadata): dropped;
#   * sandbox -> this daemon (its API port, anything on the gateway): dropped;
#   * sandbox -> sandbox on the same bridge and sandbox -> internet: allowed.
#     Containers created with --network none (Weft's default) have no
#     route at all; these rules matter for nodes that ask for a network.
#
# The rules fail closed: if they cannot be installed the daemon is stopped
# and the container exits, so no sandbox ever runs without them.
set -eu

rm -f /run/weft-iptables
PRIVATE_RANGES="10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 100.64.0.0/10 169.254.0.0/16"

# The journal survives controller restarts and is never mounted in agents.
mkdir -p /var/lib/weft/journal
chown 0:10001 /var/lib/weft/journal
chmod 2750 /var/lib/weft/journal
weft-workspace-journal -journal "${WEFT_WORKSPACE_JOURNAL:-/var/lib/weft/journal/events.jsonl}" &
journal=$!
echo "$journal" > /run/weft-workspace-journal.pid

dockerd-entrypoint.sh "$@" &
daemon=$!

stop() {
	kill -TERM "$journal" 2>/dev/null || true
	wait "$journal" 2>/dev/null || true
	kill -TERM "$daemon" 2>/dev/null || true
	wait "$daemon" 2>/dev/null || true
}
trap 'stop; exit 0' TERM INT

# dockerd creates the DOCKER-USER chain once its networking is up. The image
# entrypoint picks the legacy or nft iptables backend for dockerd only; use
# whichever backend holds the chain so the rules land where dockerd's are.
LEGACY=/usr/local/sbin/.iptables-legacy/iptables
IPT=
i=0
while :; do
	if [ -x "$LEGACY" ] && "$LEGACY" -nL DOCKER-USER >/dev/null 2>&1; then
		IPT=$LEGACY
		break
	fi
	if iptables -nL DOCKER-USER >/dev/null 2>&1; then
		IPT=iptables
		break
	fi
	i=$((i + 1))
	if [ "$i" -gt 120 ] || ! kill -0 "$daemon" 2>/dev/null; then
		echo "sandbox-daemon: dockerd did not create the DOCKER-USER chain; refusing to run without firewall rules" >&2
		stop
		exit 1
	fi
	sleep 1
done

install_rules() {
	# Inserted at the top, so the last insert ends up first.
	for iface in docker0 'br+'; do
		for range in $PRIVATE_RANGES; do
				"$IPT" -I DOCKER-USER -i "$iface" ! -o "$iface" -d "$range" -j DROP
		done
		"$IPT" -I INPUT -i "$iface" -m conntrack ! --ctstate ESTABLISHED,RELATED -j DROP
	done
}

if ! install_rules; then
	echo "sandbox-daemon: could not install firewall rules" >&2
	stop
	exit 1
fi
echo "$IPT" > /run/weft-iptables
echo "sandbox-daemon: firewall rules installed ($IPT); sandboxes cannot reach private networks or this daemon"

# A dead collector must not silently leave sandboxes running unobserved.
while kill -0 "$daemon" 2>/dev/null; do
    if ! kill -0 "$journal" 2>/dev/null; then
        echo "sandbox-daemon: workspace journal stopped; stopping sandbox daemon" >&2
        stop
        exit 1
    fi
    sleep 1
done
wait "$daemon"

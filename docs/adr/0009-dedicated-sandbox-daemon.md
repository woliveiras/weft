# 0009 — A Docker daemon dedicated to agent sandboxes

**Status:** Accepted

## Context
Agents that run commands (`shell.exec`, git tools) need the project's real
toolchain, and must not run on the operator's machine or inside the control
plane. The docker sandbox already isolated each replica in a container, but it
needed the platform to reach a Docker daemon, which in practice meant
mounting the host's `/var/run/docker.sock` into `weftd`: root-equivalent on
the host, sharing the operator's images and containers, and leaving artifacts
outside Weft's volumes. The Compose stack shipped without any of that, so
docker isolation did not work there at all.

## Decision
Run a Docker-in-Docker service, `sandbox-daemon`, in the Compose stack and
point the docker sandbox at it (`isolation.docker.host`,
`WEFT_SANDBOX_DOCKER_HOST`) over mutual TLS. It shares only the workspaces
volume (at the same path as `weftd`, so no host-path translation) and a
`sandbox` network with `weftd`. Its entrypoint installs firewall rules that
drop traffic from sandbox containers to private address ranges and to the
daemon itself, and fails closed if it cannot. A one-shot `sandbox-image`
service builds the default image inside the daemon. `/readyz` reports the
daemon (non-fatal) and the console shows it.

## Consequences
- Nothing is built or started on the host's Docker; deleting the
  `sandbox-daemon-data` volume (or `docker compose down -v`) resets every
  image, container and cache.
- Docker isolation works out of the box with Compose; sandboxes with a
  network reach the internet but not PostgreSQL, NATS, `weftd` or the LAN.
- The daemon container is `privileged` (required by Docker-in-Docker). Agents
  cannot reach it, but on shared hosts it must be treated as a privileged
  service; a rootless variant remains planned.
- External runtimes launched from `runtime.image` cannot run there (they must
  call `weftd` back); they use `runtime.endpoint` instead.
- Image pulls and builds need the daemon to be online once; layers are then
  cached in its volume.

## Alternatives
- **Mount the host socket:** simplest, but root-equivalent on the host and
  mixes Weft's artifacts with the operator's.
- **Socket proxy limited to create/exec/remove:** still uses the host daemon,
  its images and its network.
- **Rootless Docker-in-Docker by default:** less privilege, but depends on
  host kernel features (user namespaces, fuse-overlayfs) that vary across
  Docker Desktop, Colima and Linux; kept as a follow-up.
- **Kubernetes-style per-replica pods:** right for clusters, not for the
  single-machine Compose goal.

## Operational evidence

The [sandbox acceptance record](../sandbox-acceptance.md) documents real-daemon
isolation checks. The [2026-11-11 local rollout record](../../specs/workspaces-and-environments/dedicated-docker-sandbox-isolation/evidence.md)
records daemon recreation with existing volumes and successful sandbox readiness.
Rootless support remains deferred.

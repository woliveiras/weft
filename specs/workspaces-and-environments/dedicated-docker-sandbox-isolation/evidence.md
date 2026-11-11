# ENV-005 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- Release 1 sandbox acceptance record. The dedicated-daemon checks cover host inventory separation, private-network isolation and supported source/agent execution paths. See [the evidence map](../../../docs/sandbox-acceptance.md#evidence-map) for the exact suite boundaries.
- 2026-11-11. The local journal rollout verified zero restarts for both services over two minutes and sandbox readiness after recreation. [Detailed rollout](../workspace-change-journal-and-attribution/evidence.md).

## Direct regression checks

- 2026-11-11. `sh scripts/test-sandbox-mtls.sh` passed on the running Compose
  daemon. The configured TLS client passed the daemon health check. On the
  same listener, a TLS 1.3 client that verified the server certificate and
  presented no client certificate received a certificate-required alert.
- 2026-11-11. Subprocess tests injected an early forwarding-rule failure and
  an input-rule failure. Both initially published firewall readiness.
  After the correction, both reported the firewall error, stopped the
  daemon and collector stand-ins, exited nonzero, and left no readiness marker.
  The tests exercised the entrypoint with stand-ins for privileged commands.

## Default sandbox hardening inspection

On 2026-11-11, a disposable Go fixture compiled from `ae062b5` invoked the
Weft sandbox factory against the running dedicated Compose daemon (Docker
28.5.2, Linux arm64). The fixture used the cached `weft/sandbox:latest` image,
the default Docker configuration and a temporary replica workspace, without
environment or node overrides. It inspected the container while running.

| Docker inspection field | Observed value |
| --- | --- |
| `HostConfig.CapDrop` | `["ALL"]` |
| `HostConfig.SecurityOpt` | `["no-new-privileges"]` |
| `HostConfig.NetworkMode` | `none` |
| `Mounts` | Exactly one bind mount from the fixture workspace to `/workspace` |

Inside the sandbox, `/proc/self/status` reported zero effective and bounding
capabilities and `NoNewPrivs: 1`. The fixture closed the sandbox and verified
that its container was removed and the dedicated daemon remained healthy.

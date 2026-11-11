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

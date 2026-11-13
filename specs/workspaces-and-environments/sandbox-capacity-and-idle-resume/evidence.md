# ENV-006 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- The Release 1 sandbox acceptance record documents separate live daemon and fixture journeys for queued admission, resource release and idle resume. [Evidence map](../../../docs/sandbox-acceptance.md#evidence-map).
- 2026-11-13 · AC-001 · A one-off bounded environment build ran against an isolated Docker Engine 28.5.2 daemon with Buildx 0.24.0 over both a Unix socket and loopback TCP without TLS. On both transports, Docker inspection confirmed a running worker limited to 0.5 CPU and 256 MiB with no additional swap. The build log recorded capacity admission and release, and the worker was absent after completion.

# ENV-008 historical evidence

These results were recorded at the time of the run. The documentation migration did not rerun them.

- Release 1. The checklist records real reuse and reset checks for lockfile changes, worktree isolation, cache copies and preservation of other referenced resources. [Sandbox acceptance](../../../docs/sandbox-acceptance.md).
- 2026-11-13 · AC-001 · A one-off bounded Dockerfile build against an isolated Docker Engine 28.5.2 daemon with Buildx 0.24.0 reached `ready` in 5.2 s over `unix:///socket/docker.sock` and 5.1 s over `tcp://127.0.0.1:2375`, with TLS disabled. Both resulting images ran as UID `10001` and contained the file created by the Dockerfile. Both runs removed the builder and left only the default Docker context. The fixture used a cached BuildKit image and an Alpine root filesystem exported from a cached image, with no external network access. The running Compose stack was preserved.

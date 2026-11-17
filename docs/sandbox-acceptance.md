# Sandbox release acceptance

The supported scope is repository discovery or an initial developer-approved
proposal, real isolated execution, compatible image/cache-seed reuse, bounded
active resources, idle resume and reference-safe cleanup. This closes the sandbox
workstream, not the remaining Release 1 product workstreams.

## Evidence map

| Contract | Evidence |
|---|---|
| Priority, malformed input, containment | Discovery/selection/context/stages unit tests, API integration; real browser matrix for Weft YAML, devcontainer, Dockerfile and Compose |
| Initial proposal gate, stale/rejected/cancelled/failed work | Analysis/proposals/agent_tools unit and integration tests; browser approval/recovery; real gateway → persisted approval → build → continuation |
| Real shell, isolated workspaces and prepared-image reuse | Real browser uid 10001 commands, distinct workdirs and file markers, shared image inspection inside the dedicated daemon |
| Cache seeds, invalidation, concurrent builders and cleanup | Reuse/cleanup unit, storage/API and live tests; real browser lockfile rebuild and Reset preserving the other task |
| CPU/memory/count, build/setup admission and cancellation | Capacity/progress integration and cgroup tests; browser CPU-only/memory-only execution waits, cancellation/retry and preparation wait followed by setup |
| Idle resume | Engine/storage/queue tests, real container stop/start and preserved workspace/cache files; browser Files view before/after message-driven resume |
| Network restrictions and diagnostics | Policy/egress/history/retention unit, API/storage/UI integration and dedicated-daemon runtime/build/setup tests |
| Representative mise repository | [Runnable example](../examples/projects/mise-node/README.md); approval, pinned tool/dependency installation, offline build/tests, second-task reuse and private caches/workspaces |

The real-browser suite scripts only model inference; approval, persistence,
Docker builds/setup, native `shell.exec` and workspace writes use real components.
The deterministic-browser suite additionally scripts Docker and uses the echo
runtime; it verifies orchestration, not container isolation. Neither suite
measures external model proposal quality.

## Validation commands

Run these checks to verify the supported sandbox contracts. Live-daemon and
browser suites require the services described in the deployment and testing
guides; `make test` alone does not exercise all sandbox boundaries.

| Command | Coverage |
|---|---|
| `make test` | Go unit/integration suites, Python SDK and agent tests, console type-check and tests |
| `make lint` | Go formatting and vet |
| `make build` | weftd, weft and weft-egress binaries |
| `make test-e2e` | Deterministic Chromium journeys, fixture tests, E2E type-check and console build |
| `make test-e2e-real` | Chromium journeys using the dedicated Docker daemon |
| `make test-environment-context` | Agent proposals, build capacity, shared cleanup, COPY/ADD/stages/external images and reuse |
| `make test-sandbox-capacity` | Real capacity and idle lifecycle |
| `make test-compose` | Reviewed service group isolation, resource caps, suspension/resume and independent teardown |
| `make test-environment-policy` | Build/setup/runtime policy enforcement and cached-image revocation |
| `make test-egress` | Runtime/preparation proxies, destination restrictions, diagnostics, history and retention |
| `make test-sandbox` | Mutual TLS, dedicated-daemon placement, default hardening on a disposable sandbox and network isolation |

The mise example pins its public image and Node version and commits its pnpm
lockfile. `.dockerignore` excludes installed dependencies and generated outputs.
Generated outputs must be excluded so setup does not change the approved build
context and block continuation.
The second task shares the prepared image and compatible pnpm cache seed, not
another task's writable volume, node_modules or installed toolchain. Setup reruns.

## Limits retained deliberately

- Supported subsets of Compose and devcontainer; unsupported definitions fail
  validation. Initial proposals require no existing accepted definition;
  replacement proposals/diffs are a later workflow.
- Reuse fingerprints cover accepted contexts and bounded manifests/lockfiles,
  not every repository file or a full Git revision. Declare build inputs and
  exclude generated outputs deliberately. Mutable image tags are not immutable
  identities; pin digests where reproducibility matters.
- Idle suspension retains files and reuses stopped containers between turns;
  it does not checkpoint process memory or promise arbitrary restart continuity.
- CPU/memory/count reservations cover managed workers, not the entire VM or
  daemon pull/load/commit overhead. Wait reasons are visible; exact queue
  positions and ETAs are not provided.
- Runtime/build/setup HTTP/HTTPS egress controls do not govern daemon registry
  pulls. Diagnostics are sampled and bounded, not a guaranteed audit trail.
- Cleanup only removes eligible registered resources after reference checks;
  unregistered legacy resources remain untouched.
- Validation used the existing Docker-in-Docker service on Colima (Linux arm64).
  The optional rootless deployment variant remains unchecked in the checklist;
  other operating systems have not been validated.

See [environment operations](environments.md), [deployment](deployment.md) and
[getting started](getting-started.md) for supported behavior and troubleshooting.

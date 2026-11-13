# Weft web console

The operator console for Weft: a visual graph editor, read-only workspace
browser, node and channel inspectors, human-in-the-loop approvals, the event
timeline and a message explorer that walks a workflow's causal chain.

React 19 + TypeScript (strict) + Vite, with `@xyflow/react` for the canvas,
`@dagrejs/dagre` for auto-layout and `yaml` for the definition editor. There
is no UI kit and no state library: hand-written CSS (`src/styles.css`), React
context and hooks.

## Develop

Use pnpm 10.33.4, pinned in `package.json`. See [toolchain setup](../CONTRIBUTING.md#local-toolchain-versions).

```bash
pnpm install --frozen-lockfile
pnpm run dev          # http://localhost:5173, proxies /api, /healthz, /metrics to :8080
WEFT_API=http://host:8080 pnpm run dev   # proxy somewhere else
pnpm run dev:mock     # no server needed: fixture-backed fake API + simulated events
```

In mock mode (`VITE_MOCK=1`) `src/mock/server.ts` answers every API call and
pushes fake engine activity through `src/mock/events.ts`, which stands in
for `EventSource`. The mock is loaded lazily, so it adds nothing to the
production bundle's main chunk.

## Build

```bash
pnpm run build        # tsc --noEmit && vite build  ->  dist/
pnpm run typecheck
```

`dist/` is served by the Go server at `/`. Every API path is relative
(`/api/v1/...`), so the console works behind any prefix-free reverse proxy.

## Auth

The bearer token lives in `localStorage.weftToken`. Any 401 opens a
sign-in dialog; enter the `WEFT_ADMIN_TOKEN` configured for the server.
`EventSource` cannot send headers, so the SSE stream passes the token as
`?access_token=`.

## Layout of the code

API contracts: [HTTP reference](../docs/api.md) and [OpenAPI](../api/openapi.yaml).

```
src/
  api/types.ts        TS mirrors of the Go domain types (JSON tags verbatim)
  api/client.ts       typed client for the public HTTP API
  hooks/useEventStream.tsx  ONE shared SSE connection for the app (provider + hook)
  hooks/useData.ts    useAsync, useThrottled, useGraphStatus (2s poll + SSE nudges)
  components/
    canvas/           graph editor and model (graph -> nodes/edges, consumer
                      resolution, dagre layout), AgentNode, ChannelEdge
    FilesView         read-only workspace browser and file content viewer
    GraphWorkspace    toolbar (graph editing and Run/Stop/Pause/Resume/Kill) + tabs
    NodePanel         replicas bar, start/stop/scale, live output, operator
                      chat, prompt editor, grants/settings, recent turns
    ChannelPanel      semantics, consumer groups, inject form, history
    ApprovalsView     pending/decided approvals; answers for human.ask
    EventsView        live timeline with filters and pause
    MessagesExplorer  correlation-id causal tree (causation_id) + trace link
    YamlEditor        validate, save (If-Match), versions, export
    GraphList         create from YAML/JSON, lifecycle
  mock/               VITE_MOCK fixtures (dev only)
```

### Live updates

`EventStreamProvider` opens one `EventSource` to `/api/v1/events/stream`.
The server names SSE events by type, so the provider registers a listener
per known event type. It handles reconnecting itself, with exponential
backoff, and resumes with `?after=<last seq>`, dropping any replayed
sequence numbers. Components subscribe with `useEventStream(filter, fn)`.
Graph status is polled every 2 s and refetched (throttled) whenever a
`replica.*`, `execution.*`, `message.*`, `graph.*` or `approval.*` event
arrives, so changes show up without waiting for the next poll.

### Visual language of the floor

| Element | Meaning |
|---|---|
| solid amber edge | `queue`: one shared backlog, consumers compete; the count sits on the first branch only |
| blue double rail | `topic`: copy per subscribing node (including `subscribe:` pattern matches) |
| purple dashed | `fanout`: static copy per listed target |
| teal dotted | `broadcast`: copy per replica |
| lock icon | approval-gated channel |
| italic `if` | conditional route (`when`); hover the label for the expression |
| moving dashes | the channel has pending or in-flight messages |
| ghost cards behind a node | cluster; the chip reads `live of size` |
| purple `ring` badge | ring comms (replicas read neighbours) |
| dashed, faded card | disabled node |
| yellow `done` | every replica completed by its stop policy |
| pulsing `approval` | a replica is awaiting a human |

Back edges, where cycles occur, curve underneath the graph, and parallel
edges between the same two nodes fan out. Channels without a producer get
an "ingress" pill, and topics with no subscribers get a "no subscribers" pill.

Deep links: `#/g/<graph>/canvas?sel=node:<id>` or `?sel=channel:<id>`, and
`#/g/<graph>/messages?c=<correlation id>`.

<a id="browser-environment-journeys"></a>

## Browser journeys

With local Go, Git and Node installed, run from the repository root:

```bash
cd web
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
cd ..
make test-e2e
```

Playwright builds the production console and a test-only Go controller
(`server/cmd/weft-e2e`, build tag `e2e`), bound to `127.0.0.1:18089`.
Set `WEFT_E2E_PORT=18101` (or another free port) to isolate concurrent
worktree runs; both Playwright and the controller use the override.
The port must be free: the suite never reuses a running installation. Each run
creates temporary repositories, SQLite storage, credentials and workspaces,
removed when the controller shuts down. Tests use fixed public fixture tokens.
No local configuration or real provider credentials are loaded.

The browser uses the real API, persistence, approval controller, environment
manager and execution engine. Docker commands and the analysis provider are
controlled fixtures; execution uses the echo runtime. These tests verify browser
orchestration, not container isolation or external model behavior. Run
`make test-environment-context` separately for the dedicated-daemon coverage.

The graph connection journey (`e2e/graph-connections.spec.ts`) creates agents,
moves their cards and drags between real React Flow handles. It checks invalid
schedule validation without persisting the draft, saves and reloads connected
agents, cancels and confirms Delete for an agent and its affected channels, and
then cancels and confirms deletion of the remaining connection. API reads and
browser reopening verify persisted nodes, positions and channels. Only the
starting graph is seeded via the API; no browser responses are intercepted.
Run it alone with `WEFT_E2E_PORT=18101 pnpm run test:e2e graph-connections.spec.ts`
from `web/`. The test uses no shared Docker daemon.

The suite covers discovery/review/acceptance, approval before build and automatic
resume, compatible reuse in another task, rejection, analysis failure and build
failure. Recovery journeys also exercise CPU-only and memory-only admission
waits, cancellation followed by a fresh start, successful build retry after a
transient failure, build/setup waiting behind execution capacity, and idle suspension/message-driven resume. They read the same
workspace file through the Files screen before and after resume. The disposable
installation has a 1 CPU / 256 MiB budget, ten slots, and a one-second idle timeout;
continuous holders reserve 0.8 CPU or 224 MiB, so only the intended budget is
exhausted. The Docker fixture persists container running/stopped state and fails
one marked build exactly once; it does not enforce cgroups or execute commands.
`make test-sandbox-capacity` separately verifies real cgroups and container,
workspace and cache preservation on the dedicated daemon.

Failed tests retain screenshots and traces in `web/test-results/`;
`cd web && pnpm exec playwright show-report` opens the HTML report. There are no automatic
retries, intercepted browser API responses or fixed sleeps.

### Browser journey with the real sandbox

With the existing dedicated `sandbox-daemon` running and `alpine:3.22` already
available inside it, run `make test-e2e-real` from the repository root. This also
requires local Go, Git, Node, Docker CLI and Playwright Chromium. Override the
Compose container name with `WEFT_TEST_SANDBOX_CONTAINER` if necessary.
The mise journey downloads a pinned public mise image, Node and packages from the npm registry,
so preparation also requires internet access. Project commands stay inside the
dedicated daemon; no project toolchain is installed on the host.

The launcher verifies the `sandbox-daemon` service label, cross-compiles a
test-only controller and transfers it with the built console into a unique
temporary directory inside that container. It forwards HTTP on local loopback
port 18090 using `docker exec`/`nc`; it does not expose Docker, read TLS keys,
change firewall rules or start the regular Weft installation. Both the
controller and sandbox daemon see the same temporary workspace paths.

Chromium requests analysis of two repositories without environment definitions,
approves each proposal, waits for build/resume, and observes the native runtime
calling the real `shell.exec` gateway. Commands check uid 10001, require an absent
output file, and write a different marker in each workspace. The Files screen
checks both markers, including re-reading the first after the second execution.
Persisted tool/execution events, distinct workdirs and a shared prepared-image
identity prove execution, workspace separation and compatible reuse. Only model
inference is deterministic; no paid provider credentials are required.

The suite also discovers, accepts, builds and executes the supported Weft,
devcontainer, Dockerfile and Compose sources; a changed lockfile triggers rebuild.
Reset of one task preserves the other task's shared image and both workspaces.
The [mise example](../examples/projects/mise-node/README.md) runs approved tool
installation, locked dependency installation, build and tests with offline
execution in two isolated tasks, checking prepared-image reuse and private caches.

The controller stops its tasks, resets their environments and cleans only shared
generations registered in its temporary database on shutdown. It never prunes the
daemon or removes unrelated resources. Reports/traces are separate:
`web/playwright-report-real/` and `web/test-results-real/`. A forced process kill
can interrupt cleanup; inspect the reported temporary paths if shutdown fails.

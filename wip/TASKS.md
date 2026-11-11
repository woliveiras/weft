# Weft Implementation Tasks

This checklist breaks down the [product plan](./PLAN.md) into implementable work. Tasks are grouped by dependency and may be reordered when implementation reveals additional technical dependencies.

Checked boxes represent only work that has been implemented and verified. Release 1 is still in progress. Implemented features include browser interaction with the visual editor, managed providers through the real Python LangGraph adapter, read-only workspace/Git inspection, and opt-in skills/project steering with per-execution manifests. The supported sandbox lifecycle and durable Docker review/preview flows are implemented; see [sandbox acceptance](../docs/sandbox-acceptance.md) and section 4a. Rootless remains deferred. MCP live DCR/CIMD/vendor acceptance, full delegation trees and tool-bearing children, universal file-change provenance, native preview supervision, automatic PR/CI adapters and full Release 1 validation remain pending.

## Delivery Order and Final Acceptance (updated 2026-10-19)

Developer usability acceptance is deferred by user decision until the remaining
Release 1 implementation is ready. It is not a prerequisite for the next delivery
and must remain unchecked until performed. The UX reorganization and UX-01–UX-05 are implemented and activated. The global
onboarding panel was removed from the application on 2026-10-18 at the user's
request after reported layout breakage. The removal was activated and browser-validated on 2026-10-18;
earlier onboarding evidence below is historical, not current acceptance.

Continue unit, integration and relevant browser regression tests with each change.
Defer the consolidated developer walkthrough and clean-install acceptance exercise
to the final Release 1 validation stage. Live vendor qualification remains separately
pending where credentials, corporate approval or authorized test resources are
required; postponement does not qualify a provider or authorize external changes.

The native analysis delegation foundation and installation-managed profiles,
approval policy, finite limits and execution-tree controls are implemented (section 8).
Remaining backlog: broader runtime/profile support and enforceable monetary ceilings. Cumulative token admission is implemented below. Preserve explicit delegation grants
and the empty child permission set. Final developer acceptance remains deferred;
installation activation of the execution-tree controls was validated on 2026-10-18 (section 8).

Remaining Release 1 implementation areas also include vendor-specific MCP
capabilities and multi-instance OAuth transaction persistence, installation/setup
and operations gaps, and the unresolved limits of workspace change attribution.
Reconcile partially delivered checklist entries against code before implementation;
an unchecked umbrella item does not mean all of its subfeatures are absent.
Rootless sandbox support stays deferred by the earlier user decision. Items marked
Later and Release 2 reusable components do not block Release 1 UX completion.
The next delivery sequence is Phase A (task-oriented UX), then Phase B (standalone installation), then Phase C (issue to pull request under the gateway, section 10c, added 2026-10-30), as planned below. Other Release 1 gaps remain open; Release 2 is not authorized. Reuse existing graph/agent execution and message controls where possible. Define the task-to-execution contract before deciding whether additional persistent task metadata is necessary; a navigation rename alone does not justify a parallel execution engine.

## Release 1 — Build and Operate Graphs Through the Interface

### 0. Prepare Product Contracts and Acceptance Criteria

- [x] Inventory the current API, interface, runtime, workspace, storage, and Docker deployment flows that will be retained or changed. (Recorded in [product-contracts.md](../docs/product-contracts.md#1-current-state-inventory).)
- [x] Define API contracts and persisted models for provider configurations, MCP connections, per-agent permissions, instructions, interface-initiated tasks, and child executions. (The current provider pattern and proposed MCP, skill, task, and delegation contracts are documented in [product-contracts.md](../docs/product-contracts.md#3-installation-managed-connections-and-skills) through [section 4](../docs/product-contracts.md#4-user-tasks-and-child-executions).)
- [x] Define authorization and isolation between installation, graph, agent, workspace, and execution; no credentials may be sent to the agent runtime. (The installation boundary, role matrix, scoped callback, workspace isolation, and grant inheritance are documented in [product-contracts.md](../docs/product-contracts.md#5-authorization-audit-and-error-states).)
- [x] Define auditable events and their relationship to graph, agent, replica, task, and execution. (Existing event envelope and proposed event families are documented in [product-contracts.md](../docs/product-contracts.md#5-authorization-audit-and-error-states).)
- [x] Define error contracts and interface-visible states for validation, unavailability, pending approval, timeout, and version conflict. (Common response mapping and corresponding UI states are documented in [product-contracts.md](../docs/product-contracts.md#5-authorization-audit-and-error-states).)
- [x] Update architecture diagrams and documentation to reflect the documented contracts. (Added the explicitly planned component-boundary view in [product-contracts.md](../docs/product-contracts.md#6-planned-component-boundaries) and linked it from [architecture.md](../docs/architecture.md).)

**Done when:** Contracts shared by the backend, interface, and runtime are documented and reviewed before implementations diverge. The inventory and proposed contracts are recorded in [product-contracts.md](../docs/product-contracts.md); planned behavior is clearly distinguished from shipped functionality.

### 1. Remove Out-of-Scope Product Functionality

- [x] Remove endpoints, interfaces, and import/conversion code that are out of scope.
- [x] Remove examples, fixtures, tests, and dependencies used exclusively by the removed flows.
- [x] Update affected documentation, navigation, copy, messages, and development instructions.
- [x] Search for residual references in code, documentation, configuration, and tests; confirm no out-of-scope flows or mentions remain.
- [x] Run tests and builds for affected areas and fix regressions directly related to the removal.

**Done when:** Removed flows are neither accessible nor referenced in the product, and relevant tests/builds pass.

### 2. Turn the Canvas into a Graph Editor

#### Model and API

- [x] Define create, read, update, and delete operations for agents and channels, keeping validation centralized in the backend. (Decision: the whole-document `PUT /graphs/{id}` with `If-Match` is the only write path; the editor applies agent/channel operations to a draft — `web/src/components/canvas/edit.ts` — and the engine validates on save. Documented in [api.md](../docs/api.md).)
- [x] Persist visual positions and required settings without discarding advanced fields or YAML extensions that the interface does not edit. (`x-` keys on graph, metadata, nodes and channels round-trip through JSON storage and YAML export; other unknown keys are rejected with their path instead of being dropped.)
- [x] Use version control/optimistic concurrency when saving graphs and return recoverable conflicts.
- [x] Ensure the backend rejects invalid graphs and returns errors associated with the relevant agent, channel, or field.

#### Interface

- [x] Add agent creation on the canvas and define valid initial values.
- [x] Add editing for agent position, properties, runtime, model, instructions, and permissions. (Name, description, runtime details, model, prompt, loop/schedule/stop, replicas, workspace, approval gates, tool grants and per-tool approval.)
- [x] Add channel creation by visually connecting agents.
- [x] Add editing for supported channel type, routing, filters, conditions, approvals, and other properties.
- [x] Add multi-selection or appropriate confirmation before removing connected elements. (Toolbar and Delete-key removals, including multi-selection, confirm the agents/channels affected.)
- [x] Implement editing states: pending changes, save, discard, save in progress, and error.
- [x] Show validation errors next to the relevant element and property; allow users to fix and revalidate.
- [x] Detect version conflicts, preserve local changes, and provide an explicit recovery path.
- [x] Preserve drag, zoom, pan, selection, and element positions during real-time state updates.
- [x] Keep the YAML editor as an advanced alternative, synchronized with the same model and version control.
- [x] Store graph documents as written: `spec.defaults` are applied when graphs are validated and run, not copied into nodes, so changing a default reaches nodes without their own value (found while running the bookaneer test: changing the default model had no effect). The editor shows effective values (e.g. loop mode) where nodes leave them unset.
- [x] Edit graph-wide settings in the visual editor (name, description, workspace repository, parameters, max hops) and per-turn agent limits (turn timeout, max tool calls, token budget). Repository URLs must be remote; local paths and non-network transports are rejected on save and before cloning.
- [x] Add interaction tests for creating, editing, connecting, validating, saving, discarding, and recovering from conflicts. — Covered by component tests against the real API client (`GraphWorkspace.test.tsx`: create, edit, validate/revalidate, save, server 422, discard, conflict overwrite/reload, removal confirmation, YAML sync) and unit tests for the edit operations (`edit.test.ts`, including connect and keyboard removal). Chromium E2E now exercises real drag-to-connect and Delete-key cancellation/confirmation, agent/channel persistence and validation against the disposable real controller (`web/e2e/graph-connections.spec.ts`).

**Done when:** A user can create, configure, validate, save, and start a graph through the visual interface without writing YAML.

### 3. Provide Containerized Project Environments

Decisions recorded for this section: a Docker daemon dedicated to Weft (Docker-in-Docker, rootless where supported) replaces access to the host daemon; explicit interface configuration takes precedence, otherwise repository discovery follows `.weft/environment.yaml` → `.devcontainer/devcontainer.json` → Dockerfiles → Docker Compose → agent analysis with developer approval. No Weft-specific file is required. Compatible images and caches are reused across tasks for the same authorized repository, while workspaces and active containers remain isolated; execution runs without network by default, with an optional host allowlist through an egress proxy.

#### Isolated Container Runtime

- [x] Add a Docker-in-Docker service to the Compose stack (rootless variant where the host supports it, privileged fallback documented with its risk), with TLS between `weftd` and the daemon and its own data volume. (`sandbox-daemon`, privileged `docker:28.5.2-dind` with mutual TLS and the `sandbox-daemon-data` volume; risk documented in [sandbox-runtime.md](../docs/sandbox-runtime.md) and [ADR 0009](../docs/adr/0009-dedicated-sandbox-daemon.md).)
- [ ] Offer the rootless Docker-in-Docker variant where the host supports it, and document how to choose it.
- [x] Point the docker sandbox and environment builds at that daemon (`DOCKER_HOST`); remove the need to mount the host Docker socket and document the change. (`isolation.docker.host` / `WEFT_SANDBOX_DOCKER_HOST`; the default image is built inside the daemon by the `sandbox-image` service. Environment builds will use the same daemon.)
- [x] Make workspaces visible to the dedicated daemon at the paths sandboxes mount (shared volume), replacing `hostRoot` translation for this setup.
- [x] Put sandboxes on a network that cannot reach platform services (database, broker, observability, control plane); verify with a test that a sandbox cannot connect to them. (Firewall in `deploy/sandbox-daemon/entrypoint.sh`, fail-closed and checked by the healthcheck; `make test-sandbox` probes the daemon, its gateway and `weftd` from real sandboxes.)
- [x] Document reset: removing the daemon's volume deletes every image, container, and cache it created, and the next run rebuilds from definitions.
- [x] Report the sandbox daemon in `/readyz` (non-fatal) and in **Integrations → Sandbox runtime**; let operators choose docker isolation, network, image, and limits per agent in the visual editor.

#### Environment Model and Lifecycle

- [x] Define the environment contract: source (interface, repository, agent proposal), base image or Dockerfile, setup commands, working directory, resource limits, cache volumes, and network policy per phase (setup, execution); record it in [product-contracts.md](../docs/product-contracts.md). (`spec.environment` is the single effective definition; repository and agent sources produce a definition the operator accepts into it.)
- [x] Persist environments per graph with a content hash of the effective definition; rebuild only when the definition changes or on explicit request.
- [x] Implement the lifecycle `pending → building → ready | failed`, with build and setup logs stored and size-limited; record events for state changes with graph and environment identifiers. (States `absent/building/ready/failed`; 64 KiB log tail; `environment.*` events; interrupted builds detected through the store lease.)
- [x] Run image builds and setup steps in Weft with resource limits, timeouts, and cancellation; never through agent-controlled Docker access.
- [x] Start agent sandboxes from the ready environment image, with dependency caches mounted per environment; refuse to start nodes that require an environment that is not ready, with an actionable error.
- [x] Implement Rebuild and Reset (remove containers, image, and caches of one environment), and clean up environments of deleted graphs. (Reset is refused while the graph runs.)
- [x] Add metrics and traces for build duration, outcome, and failures, following existing telemetry names.

#### Definition Sources

- [x] Read-only discovery API with unit and API integration tests: candidate inspection for the agreed priority, hashes, service names, explicit overrides and bounded checkout reads. Supported-format validation, build contexts and operator-triggered analysis are delivered below; broader format/policy support remains pending.

- [x] Allow operators to configure an environment in the interface: a prebuilt image, or a Dockerfile plus setup commands. (**Graph settings → Environment**.)
- [x] Persist discovery/selection provenance, accepted definitions, durable proposal approvals and reuse identity. Use bounded manifest/context fingerprints, not a full Git revision. Per-graph setup/images/caches remain private while prepared generations are shared; see environment/proposals and storage/API integration tests.
- [x] Discover definitions in order: `.weft/environment.yaml`, `.devcontainer/devcontainer.json`, Dockerfiles, then Docker Compose. Preserve explicit interface overrides and show the effective source. A Weft-specific file is never required.
- [x] Parse the Weft definition and the supported image/post-create subset of devcontainer configuration. Report malformed and unsupported fields; an invalid higher-priority file must not silently trigger a fallback or agent proposal.
- [x] Support validated repository contexts for local COPY/ADD, Docker ignore rules, bounded tar snapshots and acceptance/cache invalidation by digest. Cover unit, API integration and real dedicated-sandbox builds. Additional Dockerfile features remain pending.
- [x] Discover Dockerfile candidates within bounded repository paths and present ambiguous candidates for operator selection; support validated build contexts confined to the authorized workspace, respecting ignore rules.
- [x] Add explicit build-target selection for multi-stage Dockerfiles in discovery review, Weft YAML and Compose; validate local named/indexed COPY --from dependencies and preserve stage paths. Unit, API/UI integration and real sandbox tests cover target acceptance, cache identity, invalid references, cycles and build failures.
- [x] Support COPY --from external images (names, tags, registries and digests), preserving local-stage precedence and validating references before Docker. Unit, API integration and real sandbox tests cover review/persistence, reference changes, tag/digest copies and unavailable registries.
- [x] Discover standard Compose filenames and standalone variants (no implicit merging). Select a development service and explicitly review its transitive dependency closure. Support up to three prebuilt runtime helpers per replica on an internal network with atomic CPU/memory/count admission, suspension/resume and owned-resource teardown. Reject mounts, ports, environment injection, privileges, interpolation, network overrides and health conditions. Unit, API/storage/runtime/UI integration and cached-image real-daemon tests cover the supported subset.
- [x] Present supported normalized definitions and source for explicit acceptance into the graph draft, then admin Save graph before Build. Persist origin and referenced-file hashes; revalidate on save and before Docker operations, including cached builds. Unit, API integration and component tests cover review, stale files, unsupported settings, permissions and conflicts. Installation-wide policy configuration now validates preview/save/build/runtime and agent approval; see Network Policy.
- [x] When no definition file is found, let the operator invoke a bounded analysis agent with read-only repository access that works without the project environment. Analyze manifests, lockfiles and toolchain declarations; propose tools, image, setup, caches, network needs and rationale without installing dependencies or running repository commands.
- [x] Treat repository and agent definitions as untrusted: they cannot raise resource limits, mount host paths, request privileges, or widen network policy beyond installation settings.
- [x] Persist full environment proposals in developer Approvals; admin approval revalidates inputs, saves the definition and starts build. Block graph/node starts until ready, optionally start the graph after readiness, restore approved work after restart, and expose rejection/cancellation/expiration/errors and approved-build retry. Unit, API/UI integration and real sandbox tests cover the workflow. Repository fingerprinting covers analyzed inputs rather than Git commit identity.
- [x] Add the `environment.status` tool and the `environment.propose` tool (own graph, Docker node without image override; complete agent definition; durable approval/build and blocking turn continuation): the complete proposal opens a developer approval gate. Keep the task waiting until acceptance; rejection/cancellation must not build or run anything. Revalidate the approved revision, build after acceptance, and resume execution only when ready; expose build failures and retries.
- [x] Provide the [mise proposal example](../examples/projects/mise-node/README.md): pinned public image and Node, locked pnpm install, developer approval, offline build/tests and second-task reuse with private workspaces/caches. Verified by `make test-e2e-real`; inference is scripted.

#### Reuse Across Tasks and Resource Budgets

- [x] Define a repository/installation-scoped reuse key from the full image-definition digest, accepted context, bounded manifests/lockfiles, daemon platform/address and versioned policy. Setup output stays private; arbitrary setup reruns in each task. The fingerprint is bounded and is not a full Git revision.
- [x] Reuse compatible prepared images across repository graphs with renewable leases, force rebuilds and input invalidation. Setup runs per isolated workspace; shared images exclude setup container state. API/UI status and logs explain reuse. Unit, API integration and real sandbox tests cover the flow.
- [x] Reuse immutable dependency-cache seeds within the exact repository/installation scope. Copy seeds read-only into task-private writable volumes before setup; never publish runtime mutations. Cache declarations must exclude credentials and task outputs. Concurrent writers never share a volume across graphs.
- [x] Isolate graph/replica workspaces and active containers. Idle continuation reuses the same stopped container after capacity readmission; real daemon tests verify files/cache preservation and two-task isolation. Process-memory checkpointing and arbitrary controller-restart continuity are not promised.
- [x] Enforce finite memory/swap caps and a global count/reserved-memory budget for Docker execution sandboxes; persist reservations, inspect daemon inventory, wait/cancel admission and release only after confirmed removal. Preserve existing node CPU limits. Unit, configuration/app integration, concurrency and real cgroup tests verify the flow.
- [x] Extend global memory/concurrency admission to image builds, setup and cache-copy helpers. Use bounded temporary BuildKit workers, sequential setup/copies, cancellable waiting and durable reservations retained on cleanup failures.
- [x] Add global CPU accounting for execution/build/setup/cache-copy pipelines: finite total/default CPU, decimal parsing, atomic durable reservations, conservative legacy handling, kernel caps, cancellation and idle readmission. Unit, configuration/storage/component integration, concurrent-manager and real-daemon cgroup/CPU-only admission tests cover the flow.
- [x] Stop idle execution containers between turns with configurable `isolation.docker.idleTimeout` (5m default, 0s disables). Preserve container files, workspace and caches; reacquire capacity before new work, keep paused tasks paused, retry failures without losing inputs, and document restart versus explicit teardown behavior.
- [x] Preserve shared images/cache seeds on Reset and graph deletion; graph status retains resource references, and shared generations are conservatively retained.
- [x] Implement explicit admin preview/cleanup of registered shared generations and newly tracked crash leftovers. Track exact generations across rebuilds; revalidate status/definition/build/container references under reuse leases; preserve stopped containers, other tasks and private workspaces; invalidate reuse before partial deletion and support retry. Legacy unregistered resources remain untouched. Unit, storage/API integration and real daemon tests cover the flow.
- [x] Add Integrations UI for shared generation previews, retention reasons, selection (up to 100) and explicit confirmation; display partial/missing/retained outcomes and refresh after attempts. Cover policy units, component/API-client integration, and real sandbox cleanup validation.
- [x] Record reuse/build reasons in environment events and status, capacity transitions in events and bounded metrics, plus build/admission/lifecycle/cleanup outcome and duration metrics. Covered by progress/API, capacity metric and live tests; exact queue position/ETA is outside scope.

#### Network Policy

- [x] Enforce installation `environmentPolicy` on preparation/build RUN and runtime networks, manual/repository/agent definitions, approvals, cached images and node overrides. Add feature restrictions for proposals/Dockerfiles/setup/companions. Unit, storage/API integration and real sandbox command-egress tests cover the delivered subset; daemon registry egress remains external.
- [x] Add an opt-in runtime HTTP/HTTPS egress proxy with exact per-environment host lists bounded by the installation list, public-IP validation/pinned dials, isolated internal networks, bounded denial logs and aggregate capacity/lifecycle.
- [x] Manage installation network modes and host ceiling in Integrations with admin-only preview/confirmation, persisted revisions, usage/impact listing, dynamic enforcement and unit/integration/real-sandbox revocation/resume tests. Proxy diagnostics are delivered separately below.
- [x] Configure installation history retention in Integrations (admin review/confirmation, revision checks, bounded days/count, dynamic enforcement, unit/API/storage/UI integration and real sandbox tests).
- [x] Persist collected proxy denials per graph with seven-day/1,000-event retention, background and lifecycle collection, concurrent-safe deduplication, independent history UI/API, and unit/integration/real sandbox coverage. Guaranteed audit delivery remains outside this subset.
- [x] Show per-environment runtime/build proxy process state and bounded blocked/failed request samples, preserve the last preparation snapshot, handle unavailable diagnostics explicitly, and verify with unit, storage/API/UI integration and real sandbox tests.
- [x] Add preparation egress with separate setupAllowedHosts, installation validation, isolated BuildKit/setup networking, proxy capacity and cleanup, image reuse invalidation, editor fields and unit/integration/real sandbox tests. Daemon-wide registry restrictions remain pending.
- [x] Test that runtime egress cannot bypass the proxy, allowed HTTP/HTTPS hosts work, forbidden/private destinations fail, and suspension/resume/proxy failures remain isolated. Unit, API/storage/UI integration and real dedicated-daemon tests cover the subset.

#### Interface

- [x] Show the environment of a graph: source, effective definition, state, last build, logs, and cache usage. (Cache paths and volumes are listed; volume sizes are not shown yet.)
- [x] Add Rebuild and Reset actions with confirmation, restricted to operators/admins according to the role matrix.
- [x] Show discovery candidates, selected source and definition acceptance; show pending agent proposals in Approvals with rationale and full definition. Expose waiting, rejected, cancelled and build-failed states without implying execution succeeded. Proposals currently require no existing environment; diff/replacement workflows remain pending.
- [x] Show reuse decisions, build reason categories and structured build phases/capacity waits in settings and the agent panel; show recent execution admission history and shared-resource effects before Reset. Exact file diffs and durable queue positions/ETAs remain pending.
- [x] Show the graph environment name, image, readiness, failure and start blocker on the agent panel, respecting inherited isolation and explicit image overrides; report unknown readiness on fetch errors. Unit, component/client and storage/API integration tests plus real daemon progress/reuse validation cover this flow.

#### Tests and Documentation

- [x] Unit tests for definition parsing, precedence, hashing, policy enforcement on untrusted definitions, and lifecycle transitions, including build failures and cancellations. — Done for validation, hashing, the user layer, lifecycle, failures, cancellation, concurrency, interrupted builds and reset. Precedence and repository/agent-source validation are now covered; installation-wide policy, cached-image revocation and persisted approval revalidation are now covered.
- [x] `make test-e2e-real` discovers, accepts, builds and executes native `shell.exec` for all four supported repository sources plus approved proposals. It checks uid 10001, shared-image location in the dedicated daemon, workspace isolation, lockfile invalidation/rebuild and Reset preservation. `make test-sandbox` separately checks host sandbox inventory and network isolation.
- [x] Interface tests for configuration, state display, proposal approval/rejection, Rebuild and Reset: component/client integration, deterministic browser tests and real browser lifecycle checks.
- [x] Add unit tests for discovery priority, ambiguous candidates, malformed definitions, supported Compose/devcontainer fields, context containment and absent-definition fallback; include approval rejection, stale approval, timeout and analysis failure. See discovery, selection, context, proposal_analysis and proposals tests.
- [x] Add unit, storage/API integration and real sandbox tests for reuse keys, input invalidation, build deduplication across manager instances, lease-wait cancellation, private cache copies, failed setup/copy, workspace isolation and Reset retention.
- [x] Test execution-sandbox budgets, multi-controller admission, memory/count boundaries, queue cancellation, cleanup failures and restart accounting, including a real dedicated daemon.
- [x] Test build resource budgets with unit, storage/API integration and real dedicated-daemon checks, including selected contexts, failure, cleanup/recovery and competition with execution admission.
- [x] Test idle/resume lifecycle: unit eligibility/failure checks, engine/storage/queue integration for active turns, schedules, paused tasks and input retry, plus real daemon stop/start, preserved files/container identity and capacity release/readmission.
- [x] Add integration tests with repository fixtures and the real dedicated daemon for each source and the approved-proposal flow; prove no build/task starts before approval and failures remain visible. Covered by API/proposal integration, `make test-environment-context` and the real browser source matrix.
- [x] Add integration tests proving compatible tasks reuse an image/cache, changed lockfiles invalidate reuse, concurrent builds are deduplicated, task workspaces and unauthorized repositories remain isolated, and reset cannot remove referenced resources. Verify queue admission, resource release and idle-container resume. Covered by reuse/capacity/cleanup integration and dedicated-daemon suites.
- [x] Browser E2E covers discovery/review, initial proposal approval and rejection, pre-approval blocking, analysis/build failure, compatible reuse, CPU/memory execution waits, cancellation/restart, retry, build/setup waiting and idle resume with preserved files. `make test-e2e` uses real controllers with Docker/model fixtures; `make test-e2e-real` uses real containers, all discovery sources, rebuild/Reset and the mise project. Only model inference is scripted in the real suite.
- [x] Document discovery priority, supported file formats, ambiguity/error handling, approval, reuse/invalidation, resource budgets, shared-resource reset and troubleshooting in the environment, deployment and getting-started guides. See the updated guides and [acceptance record](../docs/sandbox-acceptance.md).

**Done when:** A repository needs no Weft-specific file: discovery follows the agreed precedence, and absence of definitions leads to agent analysis with developer approval before building or executing. Compatible subsequent tasks reuse images/caches with isolated workspaces and bounded active resources. Rebuild, reset and cleanup respect shared references; all project commands stay inside Weft containers, nothing is installed on the host, and agents never access Docker.

### 4. Create the File and Interaction Panel

#### Workspace API and Security

- [x] Define file listing, reading, diff, and history endpoints limited to the graph's authorized workspace.
- [x] Apply path containment, symlink validation, size limits, and traversal protection to every operation.
- [x] Expose read operations to users; do not implement user-invocable write, delete, or upload endpoints.
- [x] Enforce through backend authorization that only authorized agents can modify the workspace. — Tool grants remain default-deny; native `workspace.readOnly` overrides mutation/shell grants, Docker mounts read-only and rejects writable-container reuse. External runtimes cannot claim this policy; arbitrary process execution remains trusted code. Unit, integration and browser tests cover denied writes and unchanged files.
- [x] Provide Git, branch, worktree, agent/replica, and execution metadata associated with changes when available.
- [ ] Record file changes and operations in a queryable form associated with the execution that produced them. — Partial: durable `workspace.observed` events capture bounded before/after changes for filesystem, Git-write and shell calls, including failures, linked to execution/node/replica and shown in Files (latest 100 operation/observation events). Unit/integration tests cover changes, limits, read failures and persistence across restart; browser tests cover shell-created files. Continuous monitoring now also records net changes from concurrent/external/background writers without assigning an execution or author. The dedicated Linux daemon journal now adds kernel PID evidence, transient create/delete events and replay after controller downtime while the collector remains active. Exact agent/execution attribution for arbitrary processes, invisible PID namespaces, collector outages and lost kernel events remain outside the proven contract.

- [x] Monitor authorized shared workspaces and known replica worktrees independently of tool calls and task state; persist net changes from external/background and concurrent writers, show unknown authorship and coverage gaps in Files. — Unit/integration coverage includes baseline, discovery/read failures, recovery, cancellation, independent OS processes and restart persistence; browser coverage observes delayed concurrent writers after task completion.

- [x] Capture kernel process evidence, transient filesystem events and concurrent writers in the dedicated Linux daemon, persist a private journal and replay it idempotently after controller downtime. — Live fanotify tests cover PID/start identity, transient create/delete and concurrent processes; storage tests cover restart/replay/cursor failures; Chromium consumes the real pre-controller journal. Other runtimes and collector-offline history remain unsupported.

#### Operational Interface

- [x] Add a file browser with a tree, loading and error states, and safe navigation.
- [x] Add a read-only file viewer with a server-enforced content limit.
- [x] Add read-only Git status and diff views.
- [x] Show the directory, branch, replica, and execution associated with the workspace/change.
- [x] Allow users to start a task for a graph or agent from the interface. — Existing run controls exercised by provider, environment and Git browser journeys.
- [x] Allow users to send instructions/commands to an agent using existing operator mechanisms, without enabling direct file editing. — Existing operator-message controls and message-driven environment resumption have browser coverage.
- [x] Present task state, progress, events, messages, approvals, and results in an accessible flow. — Graph Activity combines current replica state/wait reasons, recent executions and summaries/errors, messages, approvals, graph-wide reviews and events. Explicit execution/message identities filter related records; file inspection and agent controls are linked. Independent failures, refresh/recovery, viewer-safe controls and bounded recent-history limits are visible. Unit and real-client component integration tests cover attribution, failures/retry, decisions and graph switching; real-controller Chromium covers approval, rejection, message inspection, persisted results and navigation to files. No first-class task resource or percentage-complete estimate is implied.
- [x] Link changed files, diffs, messages, and executions; clearly indicate when attribution is inconclusive.
- [x] Add authorization tests ensuring the interface cannot modify, delete, or upload files to the workspace.
- [x] Add end-to-end tests for starting a task on the web, monitoring execution, and inspecting resulting files/diffs.

**Done when:** Users can operate tasks in the browser and inspect results, but no user action in the API or interface directly changes workspace files.

**Verified scope:** `/git/status`, `/git/diff` (tracked changes against HEAD) and `/git/history` are read-only. Git runs against a private bounded metadata copy; executable configuration, external metadata redirection, path escapes and concurrent configuration replacement are covered by real repository tests. The UI shows recent execution/operation context rather than claiming diff authorship. See [API contracts](../docs/api.md).

### 4a. Durable Human Review and Application Previews

Delivered for Docker sandboxes. See `PLAN.md` section 5a and the operational
contract in `docs/sandbox-runtime.md#human-review-and-application-previews`.
The explicitly selected native `process` mode remains outside managed preview
supervision.

#### Review Contract and Continuation

- [x] Persist candidate SHA-256, frozen content including dirty changes, generation, reviewer/method policy, decision actor/time and a durable continuation outbox. Preserve existing approval clients.
- [x] Support managed HTTP previews and manually supplied external URLs, local validation, and acceptance without execution; label reported checks/external links as unverified.
- [x] Yield the runtime turn and release its input lease; block further model/tool callbacks and output. Recheck candidate identity at decision, continuation delivery and completion.
- [x] Implement approve, request changes, reject, lifecycle cancellation and review expiry. Keep preview expiry separate; decisions do not merge or deploy.
- [x] Insert continuation messages once across controller recovery without resetting consumed messages. External effects retain normal idempotency requirements.

#### Service Supervision and Protected Access

- [x] Supervise an explicitly authorized argv/cwd/HTTP port/readiness path in Docker with bounded lifetime, process group termination and bounded logs.
- [x] Reuse shared CPU/memory/slot admission and approved helper configuration; suspend the agent sandbox during human waiting, keep preview accounting separate, and copy caches into private runtime storage.
- [x] Reconnect after controller restart; stop on readiness loss, expiry or decision, expose cleanup failures, retry cleanup and retain accounting until confirmed release. Preserve persistent workspaces/snapshots/containers.
- [x] Bundle a local-by-default gateway with loopback host publication and per-review origins, without external services or public DNS. Supply a scoped stdio relay without published application ports.
- [x] Authenticate browser exchange/cookies and dedicated client headers per session, preserve application Authorization, enforce origins and body/concurrency/stream bounds, and revoke active streams.
- [x] Support HTTP, React routes/assets, application cookies and WebSockets. Document remote TLS/DNS/private-tunnel configuration and local resolver troubleshooting.

#### Interface, Contracts and Validation

- [x] Show candidate evidence, instructions, readiness/expiry/errors, preview URL, browser opening, private credential issuance, cURL template, logs, decisions, revoke/restart/renew actions.
- [x] Document API, tool yield/continuation, configuration, lifecycle events and bounded metrics/traces without credential or HTTP payload logging.
- [x] Add unit/integration coverage for invalid inputs, permissions, stale candidates, concurrent decisions, cleanup failure, expiry, generation changes, cross-session credentials, storage recovery and engine gating.
- [x] Exercise real Docker HTTP requests and cURL, network-none/no-published-port isolation, private cache mutation, controller reconstruction, expiry/restart and final shutdown (`make test-preview`).
- [x] Exercise real Chromium React navigation, mutable HTTP with application auth, cookies, WebSockets, approve/revoke and request-changes/new-review (`make test-e2e-real`), through a private gateway forwarder while sandbox IPs remain unreachable.
- [x] Provide a manual Postman recipe and remote TLS configuration. Automated tests use HTTP/cURL and a private remote topology; an interactive Postman session and a public DNS/TLS deployment are not claimed as tested.
- [ ] Later: native process supervision under explicit host-execution policy, with its weaker isolation clearly reported.
- [ ] Later: provider-specific PR/CI adapters with authenticated event validation and exact revision matching. Manual external/local review already works. Pushing and opening pull requests now belong to Phase C (SCM-01–SCM-06); CI adapters and revision matching remain here.

**Done when:** The remote Docker web and HTTP review journeys in `PLAN.md` 5a
pass, including protected access, durable human waiting, exact candidate binding,
recovery and resource release. A stored link or a blocking `human.ask` call alone
does not satisfy this milestone.

### 5. Configure Model Providers Through the Interface

#### Backend and Secrets

- [x] Define secure provider credential storage with an installation-configurable encryption key. (`internal/secrets`: record-bound AES-256-GCM, `WEFT_SECRET_KEY`/0600-generated file, previous-key rotation; revisioned `llm/providers` setting in all three storage adapters.)
- [x] Persist configurations without returning or logging plaintext secrets; allow credentials to be updated, replaced, and removed.
- [x] Protect read, write, and connection-test endpoints with authentication and authorization. (Viewers can read, operators can test, admins can modify.)
- [x] Implement connection/available-model tests without exposing credentials in errors, logs, or events. (The test performs real inference through the gateway; errors that echo the key are redacted.)
- [x] Allow selecting the default provider/model and per-agent overrides.
- [x] Ensure runtimes, prompts, and agents receive only mediated model access, never the provider key. (Tested for native and external runtimes through the `llm/chat` callback.)

#### Interface and Runtime

- [x] Add a screen for creating and editing providers and their required parameters. (Extension of the existing **Integrations** screen.)
- [x] Indicate when a credential is configured without revealing its value.
- [x] Add a connection-test action and show actionable success/error states.
- [x] Offer default provider/model selection and per-agent configuration in the visual editor.
- [x] Validate configuration before starting an execution and explain missing or invalid settings.
- [x] Check tool calling in the provider connection test, and send/configure the context window (Ollama `num_ctx`, default 32768 instead of Ollama's silent 4096 truncation); prompts that do not fit fail with a clear error. (Found running the bookaneer test with local models.)
- [x] Make native agent turns fail with the cause instead of succeeding without work: tool calls written as text (recovered when they name an offered tool), empty or cut-off answers, and running out of model rounds (rounds now follow `limits.maxToolCalls`). (Warning on save; `422` with the field path when starting a graph/node.)
- [x] Report a once-mode agent whose only turn failed as `failed` (not `completed`), explain model timeouts (not retried; `OLLAMA_TIMEOUT_SEC`), run environment setup with `CI=true`, and offer "Build and start when ready" when a start is refused because the environment is not ready.
- [x] Verify and document the full path from the LangGraph runtime to the selected provider. — Real Python SDK/LangGraph → callback gateway → managed provider, including node override, failure propagation and credential isolation. The local model endpoint is deterministic; this is not live vendor/model-quality validation. See [provider-langgraph-validation.md](../docs/provider-langgraph-validation.md).
- [x] Add tests for encrypted persistence, secret update/removal, connection testing, and per-agent selection.
- [x] Add an end-to-end test from interface configuration to an execution that uses the selected provider. — `make test-e2e-providers` configures/tests a managed provider in Chromium, selects the model, executes the real LangGraph adapter and checks explicit errors and artificial-credential redaction. Requires Python with the SDK LangGraph extra.

**Done when:** An operator creates and tests a provider on the web, selects it for an agent, and runs the agent without exposing the secret to the runtime.

### 6. Integrate User-Configurable MCPs

**Quota/history validation:** `make test` (179 frontend tests plus Go/Python),
`make lint`, `make build`, and `make test-e2e` (31 standard Chromium cases plus
1 hosted CIMD case) passed. Race-enabled Go tests passed for MCP, gateway, API
and storage. Coverage includes concurrent shared admission, restart persistence,
minute/clock boundaries, approval refusal accounting, storage failures, SQL/TLS
API integration, sparse cursor pages, safe projection and browser quota/history
filters with failure/retry. No deployment or live-connection quota changes were
performed for this increment.

**Lifecycle/recovery validation:** `make test` (176 frontend tests plus Go/Python),
`make lint`, `make build` and `make test-e2e` (30 standard Chromium cases plus
1 hosted CIMD case) passed. Targeted Go race tests passed. Deletion/recovery
were exercised against disposable API/storage/TLS/browser fixtures, not deployed
to the personal installation or tested by deleting a live vendor connection.

**Live evidence (2026-10-17):** GitHub browser OAuth consent/callback and discovery
succeeded on the local installation. Explicit `get_me`/`get_file_contents` agent
grants were exercised through the gateway with a deterministic model and real
Ollama `llama3.2:latest` (4,096-token context). The latter read the profile and
`octocat/Hello-World` README and completed successfully. No writes, private
repository access, live renewal/revocation or organization-policy tests were
performed. Atlassian DCR remains uncertain; do not register again without external
evidence. These checks do not complete the vendor acceptance items below.

#### Connectivity and Integrations

- [x] Document the official endpoint, transport, authentication and scope contracts for GitHub, Bitbucket Cloud, Jira Cloud and Confluence Cloud. — [Source survey, 2026-10-17](../docs/mcp-integrations.md); documentary verification only. Runtime OAuth qualifications, per-tool permission mappings and live acceptance remain below.
- [x] Choose an initial transport compatible with Docker and implement connections without running arbitrary commands in the main process. — Remote HTTPS Streamable HTTP discovery, protocol 2025-06-18, JSON/SSE, public-IP-only dialing, no redirects or subprocesses; see [MCP scope](../docs/mcp.md).
- [x] Design a generic MCP connection registry, with no artificial connection limit and support for multiple connections per service. — Revisioned per-connection settings with stable IDs and safe read models.
- [x] Implement the connection lifecycle: create, edit, test, disable, re-enable, revoke, and delete. — Deletion requires admin, current revision, disable and prior OAuth revocation, with typed UI confirmation. Local credentials/catalogue are removed; a minimal tombstone permanently reserves the ID. Historical grants and stale callbacks/writes cannot revive it. Remote applications/registrations are not deleted.
- [x] Implement discovery and refresh of tools offered by each connection. — Initialization, paginated tools/list, persisted catalogue revisions and visible failure state; fresh sessions per operation.
- [x] Implement timeouts, cancellation, payload limits, and explicit handling for disconnection, errors, and invalid responses. — Discovery and calls: 15s deadline, 2 MiB response bound, 64 KiB call arguments, 20 discovery pages/500 tools, JSON/SSE validation and redacted errors.
- [x] Implement bounded session recovery for the supported request-scoped transport. — A session-bearing HTTP 404 reinitializes once within the original deadline; discovery restarts and pre-dispatch initialization can recover. Dispatched tools/call requests are never replayed. Network/other HTTP failures remain explicit; persistent SSE cursor resumption is not supported.
- [x] Add contract tests for connection, discovery, invocation, failures, and timeouts. — Unit and real TLS/API tests cover calls, JSON/SSE, cancellation, failures, session cleanup, payload limits and stale revisions.

#### Credentials, Authorization, and Auditing

- [x] Encrypt tokens/secrets at rest and ensure they never appear in read responses, logs, prompts, or events. — Bearer credentials use the installation secret box; safe views and lifecycle events omit values/ciphertext; no runtime exposure.
- [x] Implement assignment of connections and allowed tools to specific agents. — Exact discovered grants saved in node tools; validation rejects disabled/unknown/wildcard MCP names. The shared gateway enforces these grants on each call.
- [x] Enforce authorization at the gateway for every invocation, regardless of what the agent/model requests. — Exact MCP grants only; wildcard/cross-connection requests denied.
- [x] Add per-connection/agent rate limits, timeouts, and response-size limits. — Persistent fixed-minute connection quotas aggregate agents/replicas using shared storage; admin configuration, atomic admission, restart persistence, fail-closed storage failures and rate-limit audit outcomes are covered. Per-agent grants, deadlines and payload bounds remain independent. In-flight concurrency and provider billing quotas are outside this increment.
- [x] Allow human approval to be configured for selected tools. — Existing requiresApproval grants use the shared approval flow, with revision revalidation before dispatch.
- [x] Audit invocations with agent, connection, tool, result, duration, and error, removing or masking sensitive data. — Qualified tool name encodes connection; events contain outcome, not raw arguments/results; metrics aggregate MCP names.
- [x] Ensure an agent cannot select another connection, escape its grants, or read credentials. — Trusted execution principal and registered endpoint/credential only; exact names, no wildcard authority.
- [x] Test effective revocation: after access is revoked, the next invocation must be denied. — Connection disable denies subsequent calls and calls waiting for approval; already dispatched operations are not rolled back. Graph grants follow the execution snapshot contract.

#### Interface

- [x] Add a screen for registering multiple Bitbucket, GitHub, Confluence, and Jira connections. — Generic remote MCP form with service labels; vendor-specific authentication/scopes and live-service acceptance remain pending.
- [ ] Complete vendor-specific authentication/scope guidance. — Generic pre-registered OAuth configuration, requested/granted scope display and explicit authorization are delivered; provider qualification remains pending.
- [x] Show connection status, last validation, discovered tools, and errors without revealing secrets.
- [x] Allow connections to be enabled/disabled and discovered tools to be refreshed.
- [x] Allow available connections/tools to be selected for each agent. — Dedicated selector, persisted graph edits and removal of unavailable grants.
- [x] Show a filterable history of calls, approvals, results, and errors. — Viewer-readable persisted history filters connection/graph/agent/tool/event/outcome/time, with newest-first exclusive cursors and bounded sparse scans. Results are safe outcome summaries; raw arguments, results, approval comments and upstream errors are omitted. Deleted connection history remains queryable.
- [x] Add interface tests for connection setup, validation, agent authorization, and revocation. — Components and Chromium cover setup, saved grants, successful agent execution, wildcard denial and connection disable against a real TLS fixture.
- [ ] Add end-to-end tests for each priority integration using appropriate test credentials.

#### Vendor Delivery Checklist

All behavior changes below require unit and real-boundary integration tests;
complete each product with browser E2E and an opt-in live test using authorized
credentials/resources. Fixture coverage alone does not complete live acceptance.

- [x] Implement the initial versioned template catalogue with public endpoint/service defaults, authentication guidance, source/review date and validation status; persist and validate template references without granting tools. — Five v1 presets, unit tests, API/persistence integration and Chromium save/edit coverage.
- [x] Extend templates with parameterized endpoints and credential modes, and preview version upgrades without silently expanding access. — GitHub v2 offers bounded read-only toolsets and PAT/OAuth modes; GitLab v1 validates the instance origin and OAuth mode. Settings preview an upgrade, allow cancellation/application to the draft, and retain revision checks and credential/agent-grant boundaries. Unit/API integration/browser coverage proves persistence and explicit saving; vendor qualification is separate.
- [ ] GitHub: add the github.com PAT template, read-only/toolset profiles and a tested per-tool fine-grained permission matrix; distinguish repository defaults from enforced credential restrictions. — PAT/OAuth template v2 and five read-only toolset profiles are delivered; an official-source read-permission worksheet is documented, but the live per-tool fine-grained permission matrix remains pending.
- [x] GitHub.com OAuth wire compatibility: handle omitted authentication methods with the pinned confidential-client profile, POST client secret, comma-separated scopes and HTTP 200 OAuth errors; unit, TLS API integration and Chromium fixture coverage. See `docs/mcp.md` for manual registration. Live account consent, discovery and explicit profile/public-file reads through the gateway are verified (2026-10-17); broader vendor acceptance remains pending.
- [x] MCP access-failure guidance — Distinguish remote 401 credential rejection, 403 resource/scope/corporate denial and 429 provider quota without exposing response bodies/headers or replaying calls. Preserve OAuth reauthorization on 401 and avoid revocation on 403. Unit, real TLS/store/gateway integration and browser save/reload coverage verify diagnostics, redaction and connection independence. Live vendor scope/SSO acceptance remains pending.
- [ ] GitHub: implement host-owned OAuth app registration/configuration and guided consent; test expiry, organization/SSO restrictions and insufficient permissions.
- [ ] GitHub: validate repository/issue/PR reads, an approved test mutation, inaccessible repository denial and revocation in browser and live-service acceptance.
- [ ] Atlassian foundation: qualify v2 protocol, tool schemas and catalogue bounds; support the exact reviewed `tools=all` query option while rejecting unknown/secret query parameters; test pagination and catalogue drift. — Exact flat endpoint and canonical/advertised OAuth resource binding are implemented. TLS fixtures and browser tests cover pagination, drift, stale authority and dependency failures within existing bounds. Live catalogue/schema qualification remains pending.
- [x] Atlassian foundation: implement OAuth and encrypted Basic email/token credentials, retain service-account Bearer support, and explain admin enablement, site consent and IP/domain restrictions. — Explicit Basic mode seals both email/token, preserves write-only views, rejects unsafe mode/endpoint reuse and filters credential echoes; unit, TLS persistence/transport integration and browser coverage. OAuth/Bearer remain separate; live corporate qualification is tracked below.
- [ ] Atlassian foundation: use flat per-operation discovery and product-specific selection; prevent `executeRead`/`executeWrite`/`executeDestructive` dispatcher grants from bypassing exact authorization; resolve site context and test cross-product denials. — Flat operations are delivered; dispatcher exclusion is enforced for current and legacy catalogues, grants and every invocation, including custom labels on the official host. An optional reviewed read policy now selects Jira/Confluence and exact cloud UUIDs, filters unknown/cross-product tools and enforces the cloudId argument before dispatch, with stale-revision denial. Project/space constraints, Bitbucket boundaries and live corporate acceptance remain pending.
- [x] Add optional exact MCP argument restrictions per connection. — Admin-edited bounded rules filter discovery to listed tools with explicit string schemas; every invocation enforces all matching fields before credentials/dispatch. Revision changes invalidate stale authority. Unit, manager/store/gateway integration and browser execution tests cover allowed/denied arguments, schema failures, persistence and invalid configuration. Independent rules do not preserve resource pairs; the optional combination rules below do. Neither establishes vendor ownership or query-cost limits.
- [x] MCP resource combinations — Reviewed exact string tuples preserve paired resource identifiers alongside independent rules, revision invalidation and explicit agent grants. Unit, manager/store/gateway integration and two Chromium journeys cover invalid/incomplete rows, cross-pair denial before dispatch, persistence, stale authority, dependency failures and allowed execution. Full host Go/Python/web tests (333 web), race checks, vet and build passed. Docker Makefile checks were attempted but the Docker socket was unavailable. Vendor ownership and live provider acceptance remain unverified.
- [ ] Bitbucket Cloud: guide organization-linked workspace/repository selection and read/write MCP scopes; test unlinked workspaces, cross-workspace denial, PR reads and an approved test mutation; pipelines/merge are separate grants.
- [ ] Jira Cloud: guide site/project selection and read/search/write MCP scopes; test JQL/issue reads, missing scope/project access, field/transition failures and approved test writes; exclude delete/manage from initial defaults.
- [ ] Confluence Cloud: guide site/space selection and read/search/write MCP scopes; test restricted content, page/search reads, approved updates, version conflicts and permission failures; do not fetch returned attachment links automatically.
- [x] Offer GitHub, Atlassian, GitLab instance and custom OAuth setup defaults and exact callback/registration guidance; preserve legacy labels and explicitly report pending external qualification.
- [x] Exercise independent installation-owned connections, including repeated services, overlapping OAuth transactions, renewal/revocation isolation and per-agent exact grants with unit, TLS integration and browser fixtures.
- [x] Support the documented GitLab public pre-registration contract with PKCE, canonical instance endpoints and `mcp` scope; reject REST scope substitution and unrelated metadata.
- [ ] Complete live Atlassian MCP client provisioning/consent and corporate domain/site/IP approval with the administrator; do not substitute an unqualified REST 3LO app.
- [ ] Qualify the chosen GitLab instance/version using a user-approved registration, real tool read, renewal, revocation and scope/policy denials.

#### Automatic OAuth Client Setup — DCR and CIMD

- [x] Verify advertised provider capabilities using official documentation and public metadata (2026-10-17). — GitLab.com advertises DCR, not CIMD; instance administrators may disable DCR. Atlassian MCP v2 binds to an issuer advertising both DCR and CIMD. GitHub documents no DCR. See [evidence and exact metadata URLs](PLAN.md#6-add-mcp-configuration) in the MCP plan; this is capability verification only, without registration or consent.
- [x] Add capability-driven selection: reuse configured pre-registration first, otherwise prefer usable advertised CIMD, then advertised DCR, then manual setup. Show the mechanism/reason and retain an explicit manual option; never register on link opening or metadata inspection, silently replace an existing client, or switch mechanisms after authorization failure.
- [x] Implement backend DCR with validated resource/issuer/registration endpoint, reviewed callback/scopes and supported client authentication; validate responses, bound requests and handle denied registration, malformed responses, rate limits and ambiguous POST outcomes without blind retries.
- [x] Persist registration identity and encrypted credentials per connection/issuer; reuse after restart, serialize concurrent registration, and invalidate incompatible pending flows on reviewed issuer/callback/mode changes. Keep local revoke, upstream grant revoke and remote registration deletion distinct; do not implicitly delete registrations used elsewhere.
- [x] Implement CIMD with a stable, provider-reachable public HTTPS metadata URL, matching URL-form client ID and exact approved callbacks derived from publicUrl; publish no secrets or connection-private data. Explain localhost/unreachable-document limitations and offer DCR/manual setup without automatically exposing the installation.
- [ ] Qualify live GitLab DCR for the selected instance/version and `mcp` scope, including administrator disablement and manual fallback; enable CIMD only if that instance explicitly advertises it. Do not extend the existing public-client compatibility exception to unqualified registration responses.
- [x] Persist safe DCR failure categories and provider HTTP status, refresh failure state in the UI and clear stale confirmation; cover 403/503, restart/concurrency, secret redaction and browser reload without a duplicate POST. Historical uncertain attempts remain blocked pending external evidence; diagnostics cannot reconstruct a past response.
- [ ] Qualify live Atlassian v2 CIMD and DCR against the advertised issuer, callback and corporate policies; keep compatible pre-registration available and explain provisioning constraints. Neither metadata advertisement nor REST 3LO support is proof of live MCP acceptance.
- [x] Add unit tests for selection, callback/document validation, registration responses, credential redaction, expiry and failures; preserve PKCE/state protections and public-only endpoint validation.
- [x] Add real TLS/API/encrypted-storage/gateway integration tests for DCR and CIMD through consent, discovery, renewal and revoke; include concurrent registration, restart reuse, dependency failures, issuer mismatch, connection isolation and denial without explicit agent grants.
- [x] Add browser E2E for DCR, hosted CIMD and manual alternatives, unavailable CIMD on localhost, consent refusal, registration failure and reauthorization; show capability availability separately from successful authorization/discovery. — 28 standard cases plus 1 hosted CIMD case passed via `make test-e2e`; provider-specific administrator disablement remains in live qualification.
- [ ] Record separately authorized live GitLab and Atlassian registration, consent, discovery/tool read, renewal/revocation and corporate-policy denials; update affected guides and bounded audit outcomes. Keep external acceptance pending until proven; no credentials, registrations or installation authentication changes are authorized by this checklist.

**Validation:** `make test` (171 frontend tests plus Go/Python suites), `make lint`, `make build` and `make test-e2e` passed. Targeted Go race tests cover registration and the API. Public-client CIMD and DCR none/basic/post are supported; initial registration tokens, expiring client secrets, private-key JWT and remote registration deletion are outside this increment. No live provider registration or installation deployment was performed.

#### Browser-Link Connection and OAuth Lifecycle

- [x] Add shareable links to a template/version on the local or hosted Weft installation; validate public parameters, reject extra parameters/unknown templates and preserve selection through login. — Hash route, review screen, explicit admin save, unit and Chromium tests; opening a link never creates a connection or grants tools.
- [x] Add endpoint/scope review and an explicit authorize action for administrators; retain protected API-key/PAT entry for providers without OAuth. Never include secrets in connection links.
- [x] Implement resource/issuer metadata validation and a configured backend callback independent of the frontend hash route for pre-registered public or client_secret_basic clients; preserve the public-only HTTPS policy. — Generic TLS fixture coverage; see [runtime limits](../docs/mcp.md#browser-oauth-pre-registered-clients).
- [ ] Qualify each vendor's metadata/resource URI, client registration, scopes and refresh/revocation behavior with live credentials; deliver DCR/CIMD through the dedicated checklist above and qualify provider-specific compatibility where required.
- [x] Add distributed pending OAuth transaction storage for multi-instance routing. — Encrypted shared CAS document, installation-wide 128-entry bound, ten-minute expiry, session/browser binding, single-winner capture/consumption and fail-closed storage/key errors. Separate SQLite handles and reopen tests exercise restart/routing and replay denial; browser OAuth journeys retain explicit completion. An uncertain exchange still requires fresh authorization; no provider-side retry.
- [x] Implement authorization-code flow with PKCE and expiring, single-use state bound to the initiating user/session, installation and connection; reject weftd, mismatched, replayed or expired callbacks and untrusted return destinations.
- [x] Return to the originating Weft connection screen after consent; exchange the code on the backend, encrypt tokens and run test/discovery before showing success. Redact callback codes and clean the visible URL; keep tokens out of frontend storage, logs and model context. Handle denial, cancellation, lost login session, provider failure and discovery failure explicitly.
- [x] Require explicit per-agent tool assignment after connection; OAuth consent or a template link never grants agent authority or silently expands existing grants on re-consent.
- [x] Implement server-side renewal where supported, with serialized concurrent refresh, atomic rotated-token persistence and bounded failure handling; show reauthorization required when refresh is unavailable, expired or revoked. Never retry ambiguous remote writes automatically.
- [x] Implement revocation that immediately blocks subsequent local calls, invalidates pending callbacks/refreshes and removes stored tokens; request upstream revocation where supported and report remote failure/unsupported revocation separately with provider-side instructions. Prevent stale callbacks/refreshes from restoring a revoked connection; distinguish revocation from temporary disable and from connection deletion.
- [x] Add unit tests for link validation, callback/state/PKCE handling, credential redaction, expiry, renewal and revocation races; retain the documented boundary that already dispatched calls cannot be rolled back.
- [x] Add integration tests across API, encrypted persistence, gateway and a real TLS OAuth/MCP fixture: code exchange, discovery, concurrent refresh, rotation, revocation, dependency failures and denial after revocation.
- [x] Add browser E2E for template link/review → provider consent → callback → discovery → explicit agent grants → execution → revocation; cover denied consent, invalid return, renewal failure and failed upstream revocation. — Existing template E2E covers login preservation; replay/session mismatch and races have API/unit coverage.
- [ ] Validate each supported OAuth provider with opt-in live credentials and authorized test resources before marking its vendor journey complete.


#### Additional Template Backlog

- [x] Survey additional official MCP servers for software, data, ML and AI engineering. — [13 candidates and delivery order](../docs/mcp-templates.md), reviewed 2026-10-17; this does not certify compatibility or add all candidates to Release 1.
- [ ] Qualify first-wave templates with protocol/live smoke tests: DeepWiki, Context7, Hugging Face and W&B. — Presets and template/link unit/integration/browser coverage are delivered. On 2026-10-18 opt-in production-transport discovery passed against live anonymous DeepWiki (three tools) and Hugging Face (four tools); a real DeepWiki public documentation read passed through the gateway after exact-grant denial/acceptance checks. Context7 anonymous discovery also passed with two tools, and a real anonymous resolve-library-id lookup passed through the exact-grant gateway. Authenticated HF/Space operations, Context7 query-docs/authenticated modes and W&B qualification remain pending.
- [ ] Add typed, encrypted custom-header support and bounded non-secret metadata headers for dbt/LangSmith; qualify per-tool auth requirements; add Sentry OAuth and Databricks workspace/service authentication profiles. — Typed dbt ID fields and standalone LangSmith encrypted key/workspace/region fields are implemented with unit, TLS transport/storage and browser coverage. Header injection and unsafe metadata changes are rejected. Sentry v1 OAuth defaults/setup guidance are delivered with unit, API/storage and browser configuration coverage; live Sentry consent/catalogue and per-tool scope qualification remain pending. Databricks workspace/service v1 provides bounded parameters and PAT/user OAuth guidance with unit, API/storage and browser configuration tests. Live workspace qualification, service-principal renewal, hosted LangSmith OAuth and per-tool requirements remain pending.
- [ ] Qualify BigQuery stateless protocol and Snowflake lifecycle differences before implementing compatibility profiles, IAM/role guidance and data-query cost controls. — Contract review and a credential-free BigQuery stateless tools/list probe are recorded in docs/mcp-templates.md (nine tools, no query/job). The live read-query schema has no maximum-billed-bytes input. An explicit bounded stateless 2026-07-28 transport is implemented for custom connections with TLS/gateway/API and browser coverage; pinned templates keep their original protocol. MRTR and custom parameter-header catalogues are explicitly unsupported. Enforced warehouse data/cost boundaries, Snowflake-specific compatibility and authenticated acceptance remain pending.
- [x] Design isolated managed MCP hosting for Playwright, MLflow and Qdrant, including inbound TLS/auth, upstream credentials, process lifecycle, resource limits and explicit private-network policy; never run arbitrary server commands in the main process. — `docs/mcp-hosting.md` defines versioned reviewed profiles, dedicated-daemon ownership, credential/session separation, durable admission/reconciliation, generation draining and required qualification tests. Design only: no launcher or qualified vendor image is delivered.

**Done when:** Connections for each priority integration can be opened from a browser template link, reviewed, created, tested, assigned to agents and audited; supported OAuth flows authorize and return to Weft, renew or request reauthorization, and revoke access with clear local/upstream outcomes. Unauthorized calls are denied, and browser/live-provider acceptance is recorded separately from fixture coverage.

### 7. Add Skills and Steering

#### Content Model and Loading

- [x] Define formats and storage for global instructions and application-managed skills.
- [x] Define recognized formats and paths for project-specific instructions in the repository.
- [x] Implement explicit association of a workspace/repository with authorized graphs.
- [x] Implement deterministic precedence among global, graph, agent, project, and task instructions.
- [x] Define explicit behavior for conflicts, invalid/duplicate files, size limits, and missing instructions.
- [x] Load project instructions only from the authorized workspace and record their source, version/hash, and scope.
- [x] Treat instructions and skills as untrusted content; they cannot change grants, credentials, policies, or limits.
- [x] Enforce size/token limits and inform operators when content is omitted or truncated.

#### Interface, Auditing, and Tests

- [x] Add an interface for creating, editing, versioning, enabling/disabling, and deleting global instructions/skills.
- [x] Add an interface for associating instruction sets with graphs and agents.
- [x] Show recognized repository instructions, their source, and whether they were accepted for use.
- [x] Record and show the effective set of instructions loaded for each execution.
- [x] Add tests for precedence, workspace isolation, malformed instructions, size limits, and privilege non-escalation.
- [x] Add interface tests for administration and inspection of loaded content.

**Verified scope:** Administration and graph associations require admin; guidance is opt-in and cannot alter grants. Project loading only considers immediate `.weft/steering/*.md` files in the authorized workspace. Composition reserves agent/task text before optional content; required-budget/storage failures stop before runtime invocation and preserve operator requests. Immutable manifests record loaded content, versions/hashes and omissions. Unit, storage/API/engine/SDK integration, component and Chromium tests cover this scope. See [instructions.md](../docs/instructions.md) for precedence, 16 KiB content/128-entry bounds, 8192-byte conservative budget and retained-history limits.

**Done when:** Global instructions are managed in the application, project instructions travel with the repository, and each execution is traceable without allowing instructions to grant privileges.

### 8. Allow Dynamic Delegation to Subagents

**Tree-budget recovery and retention (2026-10-18):** Startup and periodic
reconciliation now close budgets whose root execution is durably terminal.
The engine's existing restart/replica-takeover recovery supplies interruption
state; age alone never marks a worker dead. CAS closure refunds undispatched
holds, charges unknown dispatched usage conservatively and preserves known
settlements. Active foreign workers, missing execution evidence, corrupt state
and failed writes keep capacity unavailable. Errors are surfaced in engine logs;
healthy trees continue reconciling, with failed work retried on a later sweep.

After 30 days from terminal completion, per-call details are compacted into an
indefinitely retained closed summary. Historical totals remain readable and late
admission/dispatch cannot reopen credit; late callbacks cannot alter compacted
usage. Undated and active roots are not compacted. Execution, approval and
cancellation history remain intact; only old per-call budget detail is compacted,
retaining its totals and closed-tree marker. This bounds per-tree reservation
details, not the total number of retained summaries. No new API,
permission, migration or admin policy is introduced.

- [x] Recover terminal tree reservations idempotently after restart or lease takeover, preserving active workers and conservative unknown consumption.
- [x] Compact old terminal reservation details while retaining historical totals and closed-tree fencing.

**Recovery verification:** `make test` passed (Go unit/integration, Python and
274 frontend tests), as did `make lint`, `make build` and `make test-e2e`
(58 main Chromium scenarios plus one hosted-CIMD scenario). Focused race-enabled
recovery tests passed. Unit tests cover idempotent closure and unchanged known
usage. SQLite integration covers concurrent reconcilers/dispatch, active and
missing roots, corrupt state, storage failures/retries, the 30-day boundary,
undated roots, compaction, late calls and preserved totals. Engine integration
reopens storage containing crash leftovers and runs actual startup recovery,
while preserving another worker's active reservations. Browser coverage seeds
crash/active/historical records in the disposable SQLite installation before
startup, then verifies recovered/compacted totals, reload stability and viewer
restrictions through the real API. It does not kill an installed process.

The initial full test run hit an existing environment start/stop timeout; five
focused repetitions and the full rerun passed without changing that test. The
initial new E2E restored admin credentials on reload in its test setup; correcting
that setup made the unchanged viewer-restriction assertion pass. No security
checks were removed or relaxed.

Local installation activation is recorded below. Real multi-process crash
qualification remains pending; the earlier installed token-budget validation
remains valid for its original scope. Cancellation-marker retention, broader
runtimes and monetary ceilings remain separate work.

**Recovery installation activation (2026-10-18):** Activated commit `acafb69`
with `docker compose build weftd` and
`docker compose up -d --no-deps --no-build --force-recreate weftd`. Both commands
completed successfully. Only weftd was recreated; its four named volume mounts,
five graph versions and PostgreSQL migration versions 1–3 remained unchanged.
Execution/settings counts increased with normal activity (23,346/22,081 before,
23,378/22,113 after); no history or volumes were removed. The application is
healthy, and the previous image is retained as `weft/weftd:before-budget-recovery`.

Startup reconciliation closed both existing terminal ledgers. The reported-usage
tree `exe_mvdd8sd60e4739a0459e18136e6c` retained 904 charged tokens and zero held
reservations; its revision advanced once from 5 to 6. The uncertain-consumption
tree `exe_mvddb2to3970ef76e1626003a006` retained 4,966 charged/estimated tokens and
zero reservations; its revision advanced once from 3 to 4. Authenticated Chrome
inspection and reload confirmed both totals. More than one minute after startup,
both revisions and update timestamps remained unchanged, confirming repeated
reconciliation did not rewrite or double-charge them. Validation graphs remained
stopped. No credentials, connections, grants or global policies were changed.

These existing ledgers were already settled before restart: this installed check
qualifies closure, historical totals and idempotency, not recovery from an induced
crash with outstanding holds. The unit/integration and disposable-browser tests
above supply that recovery evidence. No installed record was old enough for
30-day compaction; retention-boundary coverage remains automated. Real
multi-process crash qualification remains pending.

**Cumulative tree tokens (2026-10-18):** Native parents with an exact
`delegation.request` grant share a persistent CAS ledger with their children.
Admin policy adds `maxTreeTokens` (default 65,536), intersected with parent token
limits. Estimated input plus bounded output is reserved per call, including
children awaiting approval. Each call settles once; inclusive parent/child usage
is never summed. Pre-dispatch failure/rejection/cancellation refunds the reservation;
unknown post-dispatch usage converts it to an estimated charge. Reported overages
block subsequent admission. Settlement failures fail closed without freeing token capacity.
Existing policies receive the new default without rewriting stored settings.
Runs exposes the limit, charges, held reservations and estimated portion. Fixed
cardinality metrics and spans cover CAS operations. No grants, approval policies,
recursive capability or external runtimes are added.

This is conservative token admission, not a provider tokenizer or monetary cap.
Terminal-root reconciliation and safe detail retention are implemented above;
reservations without terminal execution evidence remain held. Installation validation with local Ollama is recorded below;
broader provider qualification remains pending. See [delegation](../docs/delegation.md#cumulative-tree-token-budget).

**Tree-token installation activation (2026-10-18):** Activated code commit
`b516e34` with `docker compose build weftd` and
`docker compose up -d --no-deps --no-build --force-recreate weftd`. Only the
application was recreated; its four existing named volume mounts were preserved,
PostgreSQL migrations remained 1–3, and all four existing graph versions stayed
unchanged. The application is healthy. A rollback image was retained locally.
No credentials, MCP connections, global approval policy or existing grants changed.

Installed Chrome validation used local Ollama `llama3.2:latest` and PostgreSQL:
- Root `exe_mvdd8sd60e4739a0459e18136e6c` reserved 4,966 tokens, then settled
  904 reported tokens. Its child reserved 2,020 while awaiting approval. Stopping
  that child released the entire child reservation without charging it; the root
  succeeded, with 904 charged and zero reserved. The saved ledger agrees with UI.
- A separate `tree-token-budget-limit` graph with parent `maxTokens: 1` rejected
  execution `exe_mvddasa69b022c641bea74834408` with tree-budget exhaustion before
  model dispatch: zero usage, no child, no approval and no persisted reservation.
- Stopping the entire tree during root inference
  (`exe_mvddb2to3970ef76e1626003a006`) produced cancelled status and zero reserved.
  Unknown post-dispatch consumption correctly retained 4,966 estimated charges,
  rather than refunding possibly consumed tokens. The first tree stayed unchanged.

Both validation graphs were left stopped with their history preserved; delegation
capacity leases returned to zero. These checks qualify reservation, admission and
cancellation on the installed local provider, not provider billing accuracy or
crash reconciliation. The existing unit/integration, race and 58 browser scenarios
below passed before activation; the deployment additionally rebuilt the web and
server images successfully. No behavior changes were needed during activation.

**Tree-token verification:** `make test` passed (Go/Python suites and 274
frontend tests), as did `make lint`, `make build` and `make test-e2e` (57 main
Chromium scenarios plus one hosted-CIMD scenario). Race-enabled delegation/engine
budget tests passed. Tests cover atomic concurrent admission, idempotent settlement,
provider-reported overages, independent trees, retained reservations after storage
reopen, corrupt/unavailable storage, settlement write failures, legacy policy
compatibility, pre-inference exhaustion, rejection and child/tree cancellation,
unknown usage, parent/child accounting, admin review and existing viewer restrictions.
Browser tests use a real disposable backend and deterministic model fixture.
Initial new browser assertions incorrectly assumed the fixture reported parent
usage; they now verify conservative estimated charges. An unrelated graph-drag
visibility timeout passed on the full rerun without changing that test.

**Execution tree and interruption (2026-10-18):** Runs → Execution tree now
shows persisted parent/child links, agent/profile, state, duration and inclusive
reported usage, independently of the recent execution/event windows. Viewer
reads remain separate from operator/admin controls. Reviewed child interruption
returns an error to the parent, which may continue; tree interruption resolves
the root and cancels its current descendants. Durable idempotent requests retain
the first actor/time and are observed across shared storage. The console reports
requested versus terminal states separately. Pending approvals expire and child
capacity is released; graph definitions and grants remain unchanged. Cancellation
is cooperative, does not undo effects, and does not stop future independent runs.
Native depth remains one; broader runtime/profile support and cumulative billing
ceilings remain pending. See [delegation](../docs/delegation.md).

**Execution-control verification:** `make test` passed (Go/Python and 272 frontend
tests), together with `make lint`, `make build`, and `make test-e2e` (56 main
browser scenarios plus one hosted-CIMD scenario). Focused race tests passed for
execution controls, approval cleanup and engine cancellation. One existing cache
selection test exceeded its 5-second timeout during concurrent suite execution;
the complete `make test` rerun without browser-suite contention passed without
changing or skipping tests. Coverage includes
child/tree cancellation during approval and active inference, parent continuation,
late callback rejection, a late successful runtime result, unchanged grants,
viewer denial, explicit confirmation, storage failures/corruption, invalid tree
relationships/bounds, historical lookup outside the recent list, idempotency (including terminal retries),
capacity cleanup, and SQLite migration/backfill preserving existing records.
Browser journeys use the real disposable backend with a deterministic model.
Broader multi-process/provider qualification and final developer usability
acceptance remain pending. Local installation activation is recorded below. Cancellation-marker retention/cleanup is not yet implemented.

**Execution-tree installation activation (2026-10-18):** Activated commit
`27e13bf` using `docker compose build weftd` and
`docker compose up -d --no-deps --no-build --force-recreate weftd`. Only the
application service was recreated. Existing volume mounts were verified unchanged,
`/healthz` returned OK, and additive PostgreSQL migration 3 completed with no
parent-index/document mismatches. The three pre-existing graphs retained versions
1, 7 and 1; existing execution history was preserved.

Authenticated Chrome validation used a separate `execution-tree-validation`
graph with the existing local Ollama `llama3.2:latest` provider and only the
`delegation.request` grant. With a child awaiting approval, Stop child plus
confirmation produced a cancelled child and a successful parent. A second run
used Stop entire tree plus confirmation and both parent and child became
cancelled. Both pending approvals expired, no delegation capacity leases remained,
and the graph stayed at version 1 with its original grant. The validation graph
was stopped afterward; its four execution records were retained for inspection.
No repositories, MCP tools, secrets or authentication settings were changed.
The previous application image is retained as
`weft/weftd:before-execution-tree`. This validates a local PostgreSQL-backed
worker and pending-approval interruption; live interruption during child inference
and distributed-worker qualification remain outside this installation check.

**Configurable profiles and policies (2026-10-18):** Administration → Delegation
now supports reviewed admin-only edits to revisioned native analysis profiles and
required/automatic approval policies. Viewer/operator reads remain available;
mutations require admin and compare-and-set revisions. Profiles customize guidance
and output allowances while retaining the parent's model. The default profile has no tools/workspace; optional inherited capabilities are now supported below.
Depth stays one. Child count, shared concurrency, duration, output tokens and
objective/result sizes have finite configurable bounds. Snapshots retain policy,
profile and objective; requests revalidate before inference. Lowered capacity
counts existing high-numbered slots. Automatic policy does not grant tools or
bypass a tool grant's own approval requirement. Disable profiles rather than
removing historical configuration. See [delegation](../docs/delegation.md).


**Configuration verification:** `make test` passed (Go/Python suites and 263
frontend tests); `make lint`, `make build`, and `make test-e2e` passed (54 main
browser scenarios plus one hosted-CIMD scenario). Race-enabled delegation/settings
integration tests passed. The first full run encountered an existing workspace
review test failure; five diagnostic repetitions and full reruns passed without
changing or disabling that test. Configuration tests cover persistence/CAS, invalid
inputs, storage failures, required/automatic policy, generic tool approval,
disabled profiles, immutable execution snapshots, revision changes before start,
finite limits and capacity reduction under concurrency. Browser tests cover
reviewed admin saves, automatic execution and parent result, disabled profiles,
explicit grant denial, viewer restrictions and stale revisions. Tests use SQLite
and a deterministic model fixture; live-provider, multi-process PostgreSQL and
installation validation are not claimed. Developer acceptance remains deferred.

**Earlier foundation (2026-10-18; configuration extended above):** Explicit native `delegation.request` grant,
immutable analysis template, same parent model, no child tools/workspace/history,
mandatory operator approval, persisted parent/child/approval linkage, structured
result/error return and fixed depth/count/shared-capacity/deadline/output limits.
Runs distinguishes children and links to the parent. Reported child consumption
is included in the parent without double-counting native gateway inference.
Pending approval cleanup and restart interruption are covered. See
[delegation](../docs/delegation.md) for boundaries, including the absence of a
hard monetary ceiling and external runtime delegation. Execution-tree controls
are extended below.

**Verification:** `make test` (Go suites and 257 frontend tests), `make lint`,
`make build`, `make test-e2e` (52 standard browser tests plus one hosted-CIMD
scenario) and `go test -race ./internal/engine -run Delegation -count=1` passed.
Browser scenarios use a disposable real backend and a deterministic model fixture;
live-provider qualification, installation activation and developer acceptance
are not claimed.

#### Verified Foundation

- [x] Persist native analysis children, parent links, approval IDs, outcomes and reported usage without modifying the saved graph.
- [x] Require an exact tool grant and operator approval under the default required policy; automatic mode remains subject to explicit grants. Default analysis children receive no tools/workspace. Profiles can select inherited capabilities; recursion requires an exact inherited grant and increased depth.
- [x] Return results or bounded failures to the parent, with finite request/count/depth/capacity/deadline/output limits; supported limits are now configurable.
- [x] Cover success, rejection, timeout, provider/empty/partial/tool-call failures, admission budgets, wildcard denial, concurrent capacity, persistence failure and recovery with unit/integration tests.
- [x] Cover approval/rejection, viewer denial, parent continuation and child-to-parent navigation in browser E2E tests.

The broader checklist below remains open where additional runtimes, arbitrary
permission intersections or cumulative billing budgets are required. Tree navigation
and cooperative interruption for the current native scope are verified above.

#### Model, Policies, and Execution

- [x] Define entities and states for delegation, child execution, approval, cancellation, result, and consumption. — `docs/state-machines.md` records the actual native execution/approval/cancellation/result/ledger identities and transitions, including separate tool approvals, child failure with parent success, cooperative cancellation races and idempotent settlement/recovery. External runtimes remain separate; native recursion is bounded to four generations below the root.
- [x] Add a runtime capability for requesting a delegated subtask and waiting for/returning its result to the parent agent. — Native and external parents can request server-managed native children through the exact delegation.request grant. External callbacks share approvals, token admission, cancellation and execution-token expiry; Python exposes ctx.delegate without replay or duplicated gateway usage. Unit, real HTTP runtime/callback/provider/storage integration and browser approve/reject/cancel tests cover this contract. External child processes and runtime/model overrides remain unsupported.
- [x] Implement temporary child execution instantiation without editing the permanent graph. — Native analysis foundation; see scope above.
- [x] Define how child configurations are selected from installation-approved templates; do not accept arbitrary configurations that could elevate privileges. — Explicit installation profile ID; guidance/output allowance only, no model/runtime/tool overrides.
- [x] Implement grant inheritance and guarantee that a child receives no more permissions than its parent. — Native profiles select up to 32 exact parent declarations without rewriting constraints. Child calls use the existing gateway, inherited workspace restrictions, per-call approvals and shared rate/connection limits; no wildcard expansion, model override, memory/emit or environment/review continuation capability; recursive delegation additionally requires its exact inherited grant and reviewed depth. Calls consume the parent allowance, and each model round reserves root-tree tokens. Unit, real-gateway integration, race and browser coverage validate the boundary.
- [x] Implement a configurable global policy: approval required for every delegation or bounded automatic execution. — Implemented for native profiles, including inherited capabilities; richer runtimes remain separate.
- [x] Implement pending-approval state and block the child execution until a human decision when required. — Native analysis foundation; see scope above.
- [ ] Enforce global server-side limits for depth, children per parent, concurrency, duration, and cost. — Bounded depth/count/concurrency/duration/output limits are implemented; cumulative token admission is implemented for native trees; a hard monetary ceiling remains pending and is planned in Phase C (COST-01–COST-05).
- [x] Account for consumption across the entire tree and stop/reject new delegations when limits are reached. — Native tree CAS reservations and exactly-once settlement; unknown usage is charged conservatively. No monetary guarantee or external-runtime support.
- [x] Implement child execution cancellation and defined propagation of failure/cancellation to the parent agent. — Native child/tree cooperative interruption and parent continuation are implemented; external-runtime propagation remains separate.
- [x] Define behavior for timeout, failure, partial result, and delegation with no result. — Native analysis foundation; see scope above.
- [x] Add auditing with parent/child, objective, model/configuration, effective grants, approval decision, and consumption. — Native execution snapshots include enforced access/output bounds and the final approval decision/actor/time; automatic admission is distinct from human approval. Runs exposes the snapshot and labels legacy missing evidence. Reported usage and tree reservations remain separate. Unit, real-engine integration and approve/reject browser journeys verify the audit without changing grants.

#### Interface and Tests

- [x] Add configuration for global approval policy and limits. — Admin-only reviewed UI and revision-checked API; finite supported bounds, no hard billing cap.
- [x] Add a delegation-approval queue with enough context for an informed decision. — Native analysis foundation; see scope above.
- [x] Show the execution tree, state, agent, duration, consumption, and parent/child relationship. — Persisted bounded trees for native delegation; usage is inclusive, not a monetary ceiling.
- [x] Allow an operator to stop a child execution or tree according to their permissions. — Explicit confirmation, operator/admin API enforcement and durable cooperative cancellation; no hard process kill.
- [x] Add tests for approval/automatic policies, limits, grant inheritance, recursion, concurrency, and cancellation. — Native required/automatic, inherited constraints, depth 1–4, capacity/revision failures, recursive shared-ledger accounting and cancellation during grandchild approval are covered. Browser tests review depth and display a three-generation tree; race tests cover nested settlement. External parents use the bounded callback path with native children; external child runtimes remain unsupported.
- [x] Add end-to-end tests for delegation, returning results to the parent, and viewing the tree in the interface. — Native approve/reject and automatic profile journeys, disabled profiles, explicit grants, viewer restrictions, stale updates and child/tree interruption are covered with a real disposable backend and deterministic model.

**Done when:** Agents decide when to delegate; administrators choose individual approval or bounded automation; the server enforces limits and permissions; the tree is observable and controllable through the web.

### 9. Make the Docker Installation Operable Through the Interface

- [x] Review `docker-compose` for data persistence, secrets, internal networks, and restart behavior of required services. — Documented the actual volume/network/health/restart contract and remaining privileged-daemon tradeoff. Published services now default to loopback with an explicit remote interface override; preview stays local. Source and real Compose-render integration tests use only synthetic credentials and verify missing-credential rejection. No deployment or live reachability claim.
- [x] Preserve newer operator commands during observed graph-state reconciliation. — Atomic snapshot comparison in filesystem/SQL storage prevents stale running/stopped/failed observations from overwriting commands or run/version identity; unit, storage integration, repeated API lifecycle and browser coverage.
- [ ] Ensure MCP services, providers, storage, and runtimes are reachable as configured without exposing unnecessary ports.
- [x] Define secure initial configuration for credentials, authentication, and first-operator creation. — Documented required independent bootstrap secrets, initial admin bearer identity, named operator provisioning through tokenFile/OIDC, TLS/public URL and the distinction from MCP OAuth. New-install instructions refuse to overwrite `.env`. Browser account management is not implemented; no installation identity or secret was changed.
- [x] Add health checks and show dependency status in the console. — Image/Compose probes and Administration health already cover liveness, store, queue and optional sandbox readiness. Public dependency failures now return bounded states instead of raw error diagnostics. Unit and real HTTP/SQLite integration cover failures, redaction and recovery; existing browser health navigation and component retry coverage remain passing. Provider/MCP qualification is still a separate explicit test.
- [x] Document installation, upgrades, backup, restore, key rotation, and failure recovery. — `docs/operations.md` adds the complete recovery-set inventory, isolated restore sequence, version-fenced upgrades, rollback and failure triage. Corrected the older claim that all credentials are automatically re-sealed: MCP still needs previous keys until separately migrated. Actual restore/clean-install acceptance remains pending.
- [ ] Ensure users can perform initial setup, connect tools, configure models, build a graph, and operate tasks without shell access, manual server-file editing, or CLI usage.
- [ ] Run a clean-install test following only the documentation and interacting through the browser after starting the containers.

**Done when:** A new installation can be configured and operated in the browser; infrastructure commands are limited to initial deployment and documented administrative maintenance.

### 10. First-use guidance — global onboarding withdrawn

**Decision (2026-10-18):** Remove the global resumable onboarding panel following
user-reported layout breakage. It is no longer a Release 1 implementation or
acceptance requirement. Keep direct navigation and contextual help. Historical
implementation/activation evidence below describes the previous version.
Browser-local progress is ignored, not deleted; graphs, connections, grants,
drafts and execution history are preserved. Installation activation was validated on 2026-10-18.

Removal regression coverage checks absent onboarding for fresh and existing
browser state, direct navigation, draft preservation, explicit echo execution,
viewer restrictions, dependency failures, and narrow-screen layout.

**Removal verification:** `make test` passed (Go/Python and 272 frontend tests),
`make lint` and `make build` passed, and seven targeted Playwright journeys passed
(onboarding removal, navigation and UX feedback). The browser suite builds the
web application and uses the real disposable backend. Existing isolated guide
unit tests are retained, but the guide has no application mount or production
import.

**Onboarding removal installation activation (2026-10-18):** Commit `a2b33b3`
was activated as a frontend-only update over the installed `9b8d1e8` baseline.
The web assets were rebuilt and only `weftd` was recreated with existing
volumes. The backend binary hash and volume mounts were verified unchanged;
the service is healthy, `/healthz` succeeds, and schema versions remain 1 and 2.
Three graphs and existing providers and MCP connections remain present, while
background execution history continues to grow. Authenticated browser checks
covered Models, Graphs, the reader configuration and Connections without the
global onboarding panel. GitHub remains authorized and the reader retains its
two tool grants. No configuration edits or live tool calls were needed. The
previous image is retained as `weft/weftd:before-onboarding-removal` for
rollback. This activation does not include the separate delegation changes.

### 10a. Phase A — Task-oriented Navigation and Execution Flow (implemented 2026-10-19)

Implementation and automated evidence are recorded below; external acceptance
remains in the final round. Earlier per-delivery notes describe the state at that
time and are superseded by completed subsequent items. Each behavior task
includes unit, integration and relevant E2E success/error/permission tests, plus
updates to affected product contracts. Preserve existing routes, drafts, grants,
approvals, diagnostics and graph controls. Do not recreate global onboarding.

- [x] FLOW-01 — Define task/project/workflow terminology and the task-to-graph/execution contract. Inventory existing capabilities and missing persistence; specify entry-point/input mapping, concurrent reuse of a saved graph, version capture, ownership, refresh recovery and result association. Reuse the current execution engine; document any necessary metadata additions.
  - Design evidence (2026-10-19): [task workflow contract](../docs/task-workflows.md) inventories graph runs, turns, injection and replica worktrees; specifies durable admission, explicit completion bindings and shared-scope concurrency gates. Documentation reviewed; runtime behavior and tests remain pending in FLOW-03 onward.
- [x] FLOW-02 — Group navigation into Work (Tasks, Projects, Approvals) and Configuration (Workflows, Connections, Models, Skills & instructions, Administration). Preserve graph editing, deep links and unsaved drafts; add no roles and preserve all backend authorization checks. Test keyboard navigation and narrow layouts.
  - Evidence (2026-10-19): Work contains Approvals plus explicitly disabled Tasks/Projects until their flows exist. Configuration exposes Workflows (existing graph routes), Connections, Models, Skills & instructions and Administration through a keyboard-operable disclosure. Existing graph sessions/drafts, OAuth aliases and role checks are unchanged. Three navigation unit tests, an API-client/App integration error-path test and 11 browser scenarios cover navigation, narrow layouts, viewer denial, draft preservation and legacy connection routes. `CI=true make test` passed (296 frontend tests plus Go/Python suites); the subsequently added App integration test passed separately. `make lint`, `make build`, web build and E2E type-check passed. Installation activation and human acceptance remain pending.
- [x] FLOW-03 — Build New task around editable outcome presets and freeform input. Let users choose project and existing graph (including Software Factory), inspect requirements and review/start with minimal steps. Validate incompatible workflows, unavailable models and missing permissions without granting tools or mutating the shared graph implicitly. Provide clearly labeled synthetic examples without real account access.
  - [x] FLOW-03a — Persist and review task drafts (2026-10-19). Tasks list/new/detail now supports explicit outcome, saved workflow version and entry agent, revision-protected updates, pagination and reload/resume. API roles and audit intent are preserved; saves never execute work or change grants. Unit/API tests and SQLite/filesystem reopen integration tests passed; 4 browser cases cover draft save/reload/review, no execution, viewer denial and navigation. `CI=true make test` passed (300 frontend tests plus Go/Python), as did lint/build; additional storage-restart tests passed separately. Start/admission, channel targets, complete grant review and task execution remain unchecked under FLOW-03. Not activated in the installation.
  - [x] FLOW-03b — Reviewed execution copies and durable start admission (2026-10-19). Explicit entry/result agents, complete agent grant/model review, admin-only confirmed start, immutable source version, unique managed graph/workspace scope, CAS admission and stable operator input; retries do not redispatch. Task progress links retained execution/file evidence and stops only the owned copy. Legacy graph mutation routes reject execution copies. Unit/API integration and browser tests prove separate requests, source preservation, stale/denied requests, uncertain preparation without replay, reload and cancellation. `CI=true make test` passed (301 frontend tests plus Go/Python), as did `make lint` and `make build`; 83 main browser cases passed after correcting an obsolete navigation label. The hosted CIMD and isolated provider-management suites also passed (one case each), rerun after an in-progress project API compilation error was corrected. Projects, channel input, environment preparation and broader workflow completion contracts remain open. No installation activation.
  - [x] FLOW-03c — Registered projects and reviewed task worktrees (2026-10-19). Admin registration verifies a server-side repository root and records its initial commit; task review displays the project, full base commit and destination. Admission creates a unique branch/worktree, preserves dirty source checkouts, rejects existing destinations and retains partial failures. No automatic commit/push/cleanup. UI explicitly distinguishes server/container paths from the user's host. Unit, SQLite reopen/Git integration and API tests passed; 6 relevant browser cases passed, including registration → task review → worktree execution. `CI=true make test` passed (303 frontend tests plus Go/Python); lint/build passed. Native packaging and editor capability detection remain open; this provides part of LOCAL-04's reusable foundation without claiming native installation readiness.
  - [x] FLOW-03d — Structured queue ingress and completion edge cases (2026-10-19). Explicit channel/type/JSON review supports workflows such as the Software Factory contract; unsupported/ambiguous inputs are rejected. Publication follows durable admission without task-API replay and retains message approvals. Informational warnings no longer block valid launches; retained consumerless output does not imply active work; rejected/expired message gates produce failure after work settles. Unit, engine/API integration and 5 focused browser tests passed; `CI=true make test` (303 frontend tests plus Go/Python), lint and build passed. External runtime availability, issue-source selection and synthetic first-use UX still require their own evidence.
  - [x] FLOW-03e — Synthetic first-use path. Tasks previews an echo-only example and requires explicit consent to save its workflow/draft; ordinary review/start remains separate. No tools, repository, model or account access. Partial errors retain records without retry. Unit tests and production native browser integration cover preparation, execution and reload; no real model validation is implied. `CI=true make test` passed (310 frontend plus Go/Python), as did lint/build and two native browser cases. Full browser regression passed (86 main, one hosted CIMD and one isolated provider case). Broader issue-source and environment work remains in FLOW-04/05.
  - [x] FLOW-03f — Complete editable goal instructions and unambiguous input/result suggestions from the selected graph, with generated JSON synchronization that preserves customized payloads. Validated the preinstalled native Software Factory draft through the real API/browser without starting agents or changing grants. Unit cases cover ambiguous/missing/disabled inputs and preservation of edited text/JSON. Full tests passed (316 frontend plus Go/Python), as did lint/build, six task/project browser cases and three production native journeys. External real-model acceptance remains deferred.
- [x] FLOW-04 — GitHub/Jira issue references through exact entry-agent MCP grants. Save/review performs no fetch; the task-owned agent receives generated read arguments and uses the existing gateway. Invalid URLs/IDs are rejected, Jira requires an explicit cloud UUID, unavailable grants remain in retained drafts, and source graphs/permissions are unchanged. Unit, durable SQLite/fs storage and API integration passed; 12 task browser journeys passed, including both TLS fixture reads and missing-grant repair. `CI=true make test` passed (332 frontend plus Go/Python), lint/build passed. GitHub Enterprise URL mapping and real vendor/site acceptance are not claimed. Local stdio uses the same tool contract; no automatic import.
- [x] FLOW-05 — Add project execution preferences and capability-aware Local worktree/Sandbox selection. Explain actual execution location and access; remember valid authorized choices. Unavailable native execution remains unavailable until Phase B. Add short English worktree help linking to the user's article, without duplicating it or implying security isolation.
  - [x] FLOW-05a / LOCAL-06a — Persist explicit workflow/local/sandbox selection and revalidate at admission without changing grants or isolation. Native local work requires a registered project; sandbox checks every enabled agent and probes only the configured daemon with a bounded read-only command. Local work does not require Docker. Unit/API tests cover invalid mode, incompatible workflow, missing native capability and unavailable daemon; six native browser journeys passed, including saved/reloaded local selection. `CI=true make test` passed (327 frontend plus Go/Python), lint/build passed. Remembered project preferences and task-owned environment build preparation remain open.
  - [x] FLOW-05b — Administrator-managed per-project execution preference, with optimistic concurrency and durable restart persistence. Selecting a project suggests its preference for the task; existing drafts and agent permissions remain unchanged, and admission still revalidates capabilities. Unit, SQLite reopen/API and component tests cover invalid modes, stale writes and role boundaries. Six native browser journeys passed, including preference save/reload and task selection. `CI=true make test` passed (328 frontend plus Go/Python), lint/build passed. Task-owned sandbox preparation remains open.
  - [x] FLOW-05c / LOCAL-06b — Explicit task-owned environment build followed by separate reviewed execution start. Durable CAS intent prevents duplicate dispatch and build/cancel races; retry and cancellation retain diagnostics, images and caches. Existing environment policies, capacity and provenance checks remain in force; graph-bound agent proposal approval is not implicitly copied. Unit and real API/manager integration cover readiness, stale requests, failed-build retry, cancellation and foreign leases. Nine task browser journeys and six native journeys passed; `CI=true make test` passed (330 frontend plus Go/Python), lint/build passed, with additional retry/cancellation tests run separately. External real-daemon/vendor acceptance stays in the final round.
- [x] FLOW-06 — Consolidate request, progress, action-needed state, result and changed files in one task view. Keep approvals contextual and in the inbox; retain expandable tool outcomes, events, traces, token usage and child/tree interruption. Distinguish failed tools from completed runs and absent telemetry from inactivity. Repair actions return to the same task.
  - [x] FLOW-06a — Show scoped pending approvals directly in task progress, preserve operator/admin/viewer decision boundaries and reuse the existing decision API/inbox. Unknown approval state is distinct from an empty result. Activity, messages, tool events and diagnostics remain available on demand inside the task page. Unit/client tests and real API/browser integration cover approve/reject, reload, terminal outcomes and viewer restrictions. `CI=true make test` passed (321 frontend plus Go/Python), lint/build passed and six task browser journeys passed. Contextual dependency repair and broader child/test aggregation remain open.
  - [x] FLOW-06b — Review current MCP catalogue availability before task admission, with model/connection repair links opening separately from the retained draft. Rechecking requirements resets start consent; no tools are granted automatically. Unit tests and real MCP/API integration cover unavailable/repaired connections and unchanged draft bindings; seven task browser journeys passed. `CI=true make test` passed (324 frontend plus Go/Python), as did lint/build.
  - [x] FLOW-06c — Surface failed replica preparation as a blocked task when no execution has started, without inferring completion or retrying. Unit and real API/filesystem integration cover scoped failures and retained blocking data; five native browser journeys passed, including failure/reload. `CI=true make test` passed (325 frontend plus Go/Python), lint/build passed.
- [x] FLOW-07 — Add review handoff with diff/test evidence, actual worktree location and capability-aware Open in VS Code. Show explicit commit/push state and preserve manual editor review. Do not commit, push or delete worktrees on task completion. Complete automated Phase A regression coverage; keep human acceptance pending.
  - [x] FLOW-07a — Native workspace editor handoff. Native launch identity comes from the launcher, not an editable configuration flag. Task progress requires explicit same-computer/editor confirmation and safely encodes the workspace URI; server/container sessions show the server path. Opening VS Code does not grant tools, commit, push or clean up. Unit tests, standard/native API integration, `CI=true make test` (306 frontend plus Go/Python), lint/build and the native browser journey passed. Actual diff/test aggregation and editor-launch human acceptance remain pending.
  - [x] FLOW-07b — Task-local review evidence reuses the existing read-only Git inspector on explicit request, preserving status/diff/history, errors and observation limits. Per-execution links open tool outcomes without treating agent success as a passing test or confirmed commit/push. Native browser integration edits only a disposable task worktree, verifies its diff in the task page and proves the source file unchanged. Task/project binding validation fixes inspection of registered-source worktrees without trusting arbitrary `.git` redirection; API integration and binding unit tests cover valid and tampered cases. `CI=true make test` (312 frontend plus Go/Python), lint/build, nine relevant browser cases and two production native journeys passed. Automated check aggregation and full workflow/agent workspace coverage remain open.

  - [x] FLOW-07c — Inline selected-execution tool evidence, token usage and expandable execution tree/interruption reuse existing run components and permission checks. Bounded history and unavailable telemetry are explicit; completion is never treated as passing tests. Component tests cover exact scope/denied outcomes and real browser integration exercises evidence and terminal tree controls. `CI=true make test` passed (322 frontend plus Go/Python), lint/build passed and six task browser journeys passed. Broader workspace coverage and final manual editor acceptance remain distinct.
  - [x] FLOW-07d — Select prepared agent/replica workspaces for read-only Git inspection and native editor handoff, preserving exact scope and unavailable-snapshot states. Fix task-root/replica Git ref collisions with a separate replica branch namespace. Unit and real Git/API integration cover known scopes, unknown-node rejection, independent changes and preserved source files; four production native browser journeys passed. `CI=true make test` passed (325 frontend plus Go/Python), lint/build passed. Manual editor/vendor acceptance remains pending.

  - [x] FLOW-07e — Explicit test/commit/push verification labels distinguish recorded commands and local Git history from unverified remote state. No automatic commit, push or remote fetch. Four component tests and six production native browser journeys passed, including real worktree inspection; final full regression is tracked separately.

  - [x] FLOW-08 — Final automated Phase A regression: `CI=true make test` passed (332 frontend plus Go/Python), `make lint` and `make build` passed. `make test-e2e` passed (93 main journeys, one hosted CIMD and one isolated provider journey). Eight production native browser journeys and native format-1 upgrade/restart qualification passed separately. Updated the old project-browser wording assertion to the delivered workspace label without removing preservation checks. Human/external acceptance remains unchecked; no existing installation activation.

### 10a.1 Prompt-first tasks and authorization modes — implemented with local qualification (2026-10-19)

Next Release 1 product priority: **write a request → follow the agents → review
changes**. The earlier form-based FLOW deliveries remain implemented; the items
below track new work; only checked items are delivered. No global onboarding or new roles.
One commit per task, with unit/integration/relevant E2E tests and evidence updates.
External credentials and final user acceptance remain deferred.

- [x] PROMPT-01 — Prompt-first New task editor with Request first, visible workflow/environment selectors and collapsed Review settings. New drafts derive a bounded editable title; loaded/custom titles and structured payloads are preserved. Hidden invalid required fields reveal their settings; save failures retain the request. Existing authorization is explicitly shown; mode selection remains AUTH-02. Saving remains draft-only with unchanged review/start and permissions. Automated verification is recorded in PLAN.md.
- [x] PROMPT-02 — Explicit Resolve request persists the original prompt before bounded, tool-free default-model interpretation. Server validation derives objective, repository reference, selected/named/default Software Factory workflow and unambiguous entry/result/channel bindings. Missing models, invalid output, ambiguity and repository prerequisites persist for repair/resume; CAS protects concurrent edits and unresolved drafts cannot start. Normal requirement review retains connection/access repair and unchanged grants. URL import and automatic start remain PROMPT-03/05; live model quality remains external acceptance. Evidence and limits are recorded in PLAN.md and docs/task-workflows.md.
- [x] PROMPT-03 — Explicit public HTTPS smart-Git import from Projects/task drafts, with durable progress, pinned ref/commit, cancellation, revision-fenced retry in fresh directories and concurrent URL/ref reuse. Managed task worktrees preserve source checkouts and shared workflow grants/settings. Public DNS validation, no redirects/host credentials, two active imports per process, three-minute/128 MiB transfer limits. Unit, real Git/API integration and Chromium tests cover invalid inputs, failures, restart/retry, uncertain publication, permissions and separate worktrees. Retained partial directories, snapshot-only reuse, live-host/platform acceptance and private Git authentication limits are recorded in PLAN.md and docs/task-workflows.md.
- [x] PROMPT-04 — Explicit per-attempt HTTPS username/token authentication plus native-only selection of existing Git credential helpers or GitHub CLI login. Server-side native gating, bounded noninteractive helper resolution, fixed-endpoint credential routing, sanitized refusal/repair and fresh-attempt retry preserve imports/grants. Tokens are never persisted or returned, never passed to fetch/checkout, and cleared from the form after submission. Synthetic unit, real Git/API integration and Chromium coverage recorded in PLAN.md; live private repositories, keychains and corporate approval remain external acceptance. MCP OAuth, credential setup/rotation and push authorization are separate.
- [x] AUTH-01 — Define the server-side task authorization contract and precedence for Ask for approval, Workspace access and Full access. Bind mode to reviewed capabilities, actor, task, workflow/policy revision and audit evidence. Preserve exact per-agent/MCP grants, mandatory installation restrictions and child permission ceilings. Specify enforceable native command boundaries: a worktree/cwd is not OS isolation. Do not implement a risk-based Approve for me evaluator in this scope.
- [x] AUTH-02 — Implement the three task modes in gateway/approval admission and the UI, independently from local/sandbox choice. Full access requires a concise scope confirmation covering execution location, files, network, external actions and commit/push. Preauthorize only eligible operations within that scope, avoiding repeated per-command prompts. Inherited child behavior cannot expand grants. Test forbidden actions and unsupported native confinement explicitly.
- [x] AUTH-03 — Persist optional explicitly confirmed project preferences and allow task mode reduction/stop. Revalidate scope changes, stale versions and pending/subsequent actions; define in-flight cancellation without claiming rollback of external effects. Record automatic decisions and manual changes; no silent broadening through saved defaults.
- [x] PROMPT-05 — Administrator-confirmed Run task resolves the saved prompt and starts through existing reviewed/version-fenced admission only when requirements are satisfied. Missing model/project/configuration, clarification and environment preparation retain the same draft for explicit Continue task; runtime approval gates and grants are unchanged. Durable identity-scoped submission keys/fingerprints prevent duplicate resolution/launch on replay; uncertain reservations fail closed. Unit, storage/API integration and Chromium success/repair/replay coverage recorded in PLAN.md. No implicit clone, credential lookup, environment approval, automatic replay or broader authority.
- [x] PROMPT-06 — Present understandable task stages, result and code changes in one view. Reuse the same durable approval in the task and global Approvals inbox; approval resumes, rejection/cancellation is explicit, and mandatory denials link to configuration without an approval bypass. Keep events/tool calls/traces/usage/tree details accessible.
- [x] PROMPT-07 — Qualified the integrated prompt → URL import → selected/default Software Factory → isolated worktree → agent work → review journey with local fixtures. Covered all modes, errors, permission denials, mode changes, reload/store recovery and source/data preservation using local fixtures and native execution. Evidence, required Make target limitations and host checks are recorded in PLAN.md. Live provider/private repository, real Docker sandbox and final user acceptance remain separate and uncompleted.

### 10b. Phase B — Standalone Weft on the User's Machine (after Phase A)

Baseline already present: SQLite and filesystem queue defaults, existing storage
and broker interfaces, and native process execution. This is not evidence that
standalone packaging or local worktree UX is complete. No PostgreSQL/NATS install
should be required for the basic native flow. Keep Docker independently usable;
no native companion plus container is required for ordinary local work.

- [x] LOCAL-01 — Audit standalone dependencies and supported OS/architectures: UI assets, native/external runtimes, model providers, authentication, paths, observability and container assumptions. Define the supported initial platform matrix and native/Docker capability contract before packaging.
  - Audit evidence (2026-10-19): [standalone dependency contract](../docs/standalone.md) records the existing SQLite/filesystem queue, native/runtime/model boundaries, UI packaging gap, Unix assumptions, platform qualification targets and operational requirements. Current host is darwin/arm64; no other platform smoke or native installation is claimed. Read-only audit only; Phase A behavior remains in progress.
- [x] LOCAL-02 — Package native server and web UI with simple install/start/stop and browser entry. Initialize SQLite, filesystem queues and OS-appropriate private data directories without additional database/broker services. Handle occupied ports, unwritable directories, missing dependencies and repeated startup clearly. Test clean disposable installation without Docker.
  - Evidence (2026-10-19): host-native bundle (`make build-native`) contains weftd/weft/UI; explicit `--init-local` creates a new private OS data directory, SQLite/fs queue configuration and file-referenced admin credential without printing its value. `--local` ignores configuration environment overrides, enforces private/loopback/state boundaries, locks the directory and preserves state on Ctrl-C. Listener reservation now precedes agent startup. Unknown profile formats, duplicate instances, missing UI and occupied ports are rejected. Native unit/API restart tests and the production-binary Chromium worktree journey passed on macOS arm64. Full tests (303 frontend plus Go/Python), lint, build, native bundle, 85+1+1 existing browser cases and 1 native browser case passed. No existing installation was activated. Advanced Phase A and LOCAL-03–07 remain open; this baseline makes the user's first local test possible without claiming full roadmap completion.
  - [x] LOCAL-02a — Seed a native Software Factory as the first saved workflow, without automatic execution, external Python services or automatic commit/push. Preserve user edits and deletion across restart; test initial presence, seed failures and recovery. Unit and SQLite/native HTTP integration cover first seed, marker read/write failures, partial-seed retry, preserved edits/deletion and restored-profile restart. Production native browser verifies the initial stopped graph and workflow selector. `CI=true make test` (312 frontend plus Go/Python), lint/build and two native browser journeys passed. Real-provider validation remains deferred.
- [x] LOCAL-03 — Complete local lifecycle and operational safety: initial access, credential storage, graceful shutdown, restart/reconciliation, backup/restore and versioned upgrade failure handling. Validate pending messages, approvals and token reservations across restarts. Never silently reset data or migrate the existing Docker installation; document separately approved migration work.
  - [x] LOCAL-03a — Offline native backup/restore and durable restart recovery. New archives are private, outside the profile and atomically published without overwrite; restore requires an absent original path and validates format, paths and integrity. Unit and production native HTTP integration tests cover retained drafts, queued messages and approvals, active locks, malformed archives and failure preservation. `CI=true make test` (303 frontend plus Go/Python), `make lint`, `make build` and the production native browser journey passed. Native token recovery and format-1 upgrade qualification are recorded in LOCAL-03b/d. No existing installation data was read or changed.
  - [x] LOCAL-03b — Native token-budget recovery qualification. Disposable native SQLite/backup/restore/HTTP integration verifies abandoned-worker reconciliation, release before dispatch, conservative charge after uncertain dispatch, preservation of active foreign workers and idempotent totals across a second restart. `go test ./internal/localinstall` passed; existing budget unit/error tests remain intact. Format-1 cross-version upgrade qualification is recorded in LOCAL-03d.
  - [x] LOCAL-03c — Revoke runtime callback admission before terminal execution persistence/publication. Unit coverage verifies scoped/idempotent revocation; deterministic engine/store integration checks both success and failure at the terminal write boundary. This fixes a race observed in the external-delegation callback integration test during native qualification.
  - [x] LOCAL-03d — Qualify a real format-1 binary upgrade from the first native launcher (`faf4644`) to the current version, using only disposable profile state. `make test-native-upgrade` passed: custom graph/authenticated access preserved, Software Factory seeded once, second restart stable, no unexpected execution, unsupported format rejected without changing persistent bytes. Existing unit/profile tests cover invalid formats and initialization errors; earlier native integration covers queued messages, approvals, backup/restore and token recovery. Future format migrations require separate qualification; no live profile was upgraded.
- [x] LOCAL-04 — Implement registered local repositories and task-owned worktree lifecycle using host execution. Preserve dirty checkouts, validate paths/base revisions and concurrent tasks, avoid branch collisions and expose changes in VS Code. Document OS-level authority and enforce applicable gateway permissions/approvals without claiming sandbox confinement. Cleanup requires explicit review and preserves user changes.
  - [x] LOCAL-04a — Reviewed cleanup of stopped registered-project task worktrees. Administrator preview binds path, branch, HEAD and cleanliness; removal rechecks the fingerprint and rejects modified, untracked, ignored, locked, redirected or unavailable checkouts. No force, branch deletion or source cleanup. Unit, real Git/storage/API integration and six production native browser journeys passed; `CI=true make test` passed (327 frontend plus Go/Python), lint/build passed. External writers must be stopped by the user; no user installation data was removed.
- [x] LOCAL-05 — Support explicitly registered local MCP stdio processes and reusable HTTP connections. Review commands/environment, manage process lifecycle and failures, discover tools and enforce per-agent grants on each call. Any config import requires explicit selection/review; do not copy or expose OS secrets automatically. Test process failure, restart, discovery errors and denied calls with fixtures.
  - [x] LOCAL-05a — Implement the internal stdio transport foundation with explicit executable/directory/environment, bounded JSON-RPC framing, shared catalogue validation, fresh processes per operation and Unix process-group cleanup. Unit and real-subprocess integration fixtures cover success, restart, malformed/oversized responses, unexpected exit, cancellation and environment echo rejection. Registration, encrypted environment storage, gateway integration and browser coverage are completed separately in LOCAL-05b. Windows process lifecycle remains unsupported.
  - [x] LOCAL-05b — Native-only connection registration and UI review, encrypted write-only process environment, discovery and per-call gateway authorization. Real subprocess/storage/gateway tests cover explicit grants, stale catalogue, disabled connection, quota, environment replacement and non-native rejection. Native Chromium covers save/discovery failure, reviewed repair, reload, secret absence and unchanged graph grants. HTTP connections remain reusable; no implicit configuration import is provided. `CI=true make test` (318 frontend plus Go/Python), lint/build, four native browser journeys and 11 HTTP connection browser regressions passed. External MCP/vendor credentials and Windows qualification remain deferred.
- [x] LOCAL-06 — Connect the Phase A environment selector to native execution and optional sandbox runtime capabilities. Detect unavailable Docker/Colima without blocking local work; explain setup only for a selected sandbox. Preserve sandbox policies and Docker/server flows; never interpret a bind mount as host execution or silently fall back from sandbox to native.
- [ ] LOCAL-07 — Complete native unit/integration/browser coverage and platform smoke checks: install → configure model → register repo → select workflow → worktree task → review in VS Code. Cover parallel tasks, cancellation, restart recovery and preservation of data. Exercise optional real sandbox separately and retain Docker regressions. Update installation, security, backup and troubleshooting docs with exact supported limits; leave external vendor and human acceptance for the final round.
  - [x] LOCAL-07a — Automated macOS arm64 native qualification: eight production-binary Chromium journeys cover first-graph Software Factory, browser model configuration/default selection, unchanged factory execution through its approval gate, retained results, project/worktree inspection, MCP stdio repair, parallel task isolation, scoped cancellation and reviewed cleanup. The HTTP model is synthetic; source files and graph definitions remain unchanged. Native lifecycle/storage unit/integration tests and format-1 upgrade/restart qualification passed. Native/API documentation now records delivered routes and external limits.
  - [x] LOCAL-07c — Readable native terminal logs, startup URL/data location and shutdown stage/duration feedback; JSON remains selectable without rewriting profiles. Close browser event streams after agent draining and propagate HTTP shutdown failures. Unit tests cover formatting, scoped attributes, control characters and writer errors; real HTTP/native lifecycle integration covers stream closure, console output and persisted restart state.
  - [ ] LOCAL-07b — Partial (2026-11-03): an opt-in native browser journey (`e2e-native/ollama.spec.ts`, `WEFT_E2E_OLLAMA_URL`) passed against a real local Ollama (`llama3.2`): plugin enabled and provider tested in the browser, a task's model wrote a file through the gateway, cost recorded at the local zero price. Final external acceptance: actual model/MCP accounts, VS Code application launch/user review, optional real sandbox on the chosen daemon, and other OS/architecture runtime smoke checks. No live installation activation or external-account change is implied by LOCAL-07a.

### 10c. Phase C — From Issue to Pull Request Under the Gateway (after Phase B)

Goal: a labelled issue becomes a reviewed pull request, end to end, with every
model call, tool call, push and tracker update passing through Weft's gateways.
Decisions recorded on 2026-10-30:

- Runs only on the user's machine (native or local Compose). Live tests may use
  personal or test accounts against real public APIs, opt-in; no company
  infrastructure, credentials or approvals are assumed or changed.
- Providers stay vendor-neutral: built-in providers first, an out-of-process
  provider plugin protocol afterwards.
- No GitHub CLI or other SCM CLI inside agent runs. Push and pull requests are
  gateway tools with server-held credentials.
- Issue triggers start with polling on the existing scheduled loops and leases;
  webhooks come later.
- Browser OIDC login is deferred (see AUTH-OIDC below).

Proposed designs: [cost control](../docs/cost-control.md),
[native coding runtime](../docs/native-coding-runtime.md),
[Atlassian and Bitbucket Data Center setup](../docs/atlassian-bitbucket-setup.md).
Every task needs unit and integration tests, and browser tests where it has UI,
as required by `AGENTS.md`.

#### Inference provider plugins (added 2026-10-30, before PROV-04)

Providers become plugins: a plugin is a provider *type* that is installed
(available to the server), enabled by an administrator, and then configured as
one or more provider instances. This is a POC with no users: no compatibility
or migration with the earlier provider configuration is kept. See
[local-first plan](./OLLAMA-PLAN.md).

- [x] PLUG-01 — Plugin manifest for every built-in provider type (id, label, description, local/remote, default base URL, key requirement, capabilities, configuration fields), registered with the adapter and served at `GET /api/v1/provider-plugins`; the console form uses it instead of a hard-coded type list.
- [x] PLUG-02 — Plugin activation: every plugin is disabled by default; administrators enable/disable it with optimistic concurrency and audit events. Creating or testing instances of a disabled plugin is rejected; disabling a plugin in use keeps its instances but marks them unavailable with the reason, so nodes fail visibly instead of being rerouted.
- [x] PLUG-03 — Remove provider environment variables (`OLLAMA_*`, `OPENAI_*`, `ANTHROPIC_*`, `OPENROUTER_*`, `HF_*` for Weft, `WEFT_DEFAULT_PROVIDER`) and real providers from configuration files. Only the development `mock` plugin can come from configuration (`llm.devMock` / `WEFT_DEV_MOCK=true`), and it is hidden otherwise. The installation encryption key, addresses and data paths stay outside the console.
- [x] PLUG-04 — Console: "Inference plugins" list with capabilities and enable/disable (confirmation lists affected instances); provider form generated from the manifest; only enabled plugins can be chosen.
- [x] PLUG-05 — With no ready provider, Tasks explains it and points administrators to enabling an inference plugin; running (Run task, Prepare environment, Start task) is blocked for workflows whose agents need a model, while drafts and echo-only workflows still work (the native first-use example needs no model). A provider list that cannot be loaded does not block.

#### Model providers

- [x] PROV-01 — Extend `llm.Usage` with optional cache-read and cache-write tokens; map them in providers that report them. Keep existing fields and wire contracts compatible.
- [ ] PROV-02 — Built-in AWS Bedrock provider (`llm/bedrock`) using the Converse API through `aws-sdk-go-v2`: tool use, JSON mode, usage, model listing, region and inference-profile IDs (including EU profiles). Credentials come only from the server-side AWS default chain (profile, SSO, environment); none reach runtimes, prompts, events or the browser. Map throttling, access-denied and model-not-enabled errors to actionable states. Unit tests with an HTTP fixture that verifies SigV4 signing; opt-in live test with a personal account.
- [x] PROV-03 — Built-in Google provider for Gemini (Gemini API key and Vertex AI with Application Default Credentials), with the same contract and tests as PROV-02. — `llm/gemini` speaks native `generateContent`: thought signatures round-trip through `ToolCall.ProviderData`, JSON mode, usage with thinking and cached tokens, model listing, configured context window, and actionable quota/permission/model/safety errors. Vertex AI is selected by the provider's project (and region) and uses the server's ADC (key file, gcloud user credentials or metadata server) with the standard library. Fixture unit tests, the conformance suite and a console integration test pass; `make test-llm-gemini` passed live against the Gemini API (gemini-3.5-flash). Vertex AI has not been run live: no Google Cloud project was used.
- [x] PROV-04 — Provider conformance suite: one shared test set (chat, tool calls, tool results, JSON mode, usage, errors, cancellation, timeouts, credential redaction) that every built-in provider passes, documented in `docs/extensibility.md`.
- [x] PROV-05 — Design Provider Protocol v1 for out-of-process provider plugins, derived from `llm.Provider` and PROV-04: transport (HTTP or stdio), manifest (name, version, checksum, declared outbound hosts), lifecycle and error contract. Document the trust model: a plugin receives prompts and code, runs as a separate process, receives only its own credential, and requires administrator review to install. Design only; no implementation in this task.

**Done when:** Bedrock and Gemini work through the UI like existing providers,
pass the conformance suite, and never expose credentials; the plugin protocol is
documented before any plugin code exists.

#### Push and pull requests through the gateway

- [x] SCM-01 — Define an `SCMProvider` interface (`scm.Provider`) (repository identity, push, pull request create/get, comment) and an SCM connection model with encrypted server-side credentials, following the provider/MCP credential patterns. Credentials never enter Git configuration, remote URLs, process arguments, agent-visible environment, events or logs.
- [x] SCM-02 — `git.push` gateway tool: pushes only the task's working branch, with a mandatory branch prefix; refuses the base branch, force pushes, tags and other refs; grant constrained by repository. Runs in Weft's own Git process, not in the agent sandbox.
- [x] SCM-03 — `scm.pull_request.create` gateway tool: bounded title/body, base fixed by the task, idempotent per branch (returns the existing PR), PR URL recorded as task evidence. Requires approval by default in every authorization mode until a policy explicitly relaxes it.
- [x] SCM-04 — GitHub adapter over the REST API (fine-grained token or OAuth from the existing connection model). Fixture-based integration tests; opt-in live test against a personal test repository. — Fixture REST API plus a local bare remote cover identity, push, pull request create/find/duplicate, comments and actionable failures through the real gateway. The live test (`WEFT_TEST_GITHUB_TOKEN`/`WEFT_TEST_GITHUB_REPO`) exists but has not been run: no account was used.
- [ ] SCM-05 — Bitbucket Data Center adapter: base URL, repository or project HTTP access token, optional custom CA, HTTPS clone/fetch/push and REST pull requests, "Test connection" that reads token identity and visible repositories. Integration tests against a local Bitbucket Data Center container with an evaluation licence; mark the proposed section in `docs/atlassian-bitbucket-setup.md` as delivered only after they pass.
- [x] SCM-06 — Connect the Software Factory hand-off and the task flow to SCM-02/03 so a completed task can push and open a PR after its human gate, with failures (rejected push, existing PR, missing permission) shown on the task. — Tasks choose an optional SCM connection; the result agent (the factory's Review handoff, after its approval channel) gets connection-scoped push/PR grants and hand-off steps; the task shows the pushed branch, PR link and failures. Covered by unit, API, component and browser tests with fixture providers; a live model completing the hand-off is part of the final local validation.

**Done when:** a native task ends with a pushed branch and an open pull request on
GitHub and on a local Bitbucket Data Center, created only through gateway tools,
with no SCM credential visible to agents.

#### Issue triggers by label (Jira and GitHub)

- [x] TRIG-01 — Normalized issue model and source adapters: Jira through the Atlassian MCP connection (JQL) and GitHub through its connection (issue search). Issue content is passed to agents as data, never as instructions. — `trigger.Source` maps search, relabel/assign, comment and Jira transitions to MCP tools (GitHub MCP `search_issues`/`update_issue`/`add_issue_comment`; Atlassian `searchJiraIssuesUsingJql`/`editJiraIssue`/`addCommentToJiraIssue`/transitions), invoked through the gateway with exactly those grants. Tested with fixture tools; live tracker accounts are deferred.
- [x] TRIG-02 — Trigger entity on a workflow: source, label and query, schedule, pre-recorded task consent (actor, workflow version, project, environment, authorization mode, policy version). Any change to those invalidates the consent and pauses the trigger instead of running.
- [x] TRIG-03 — Polling on the existing scheduled loops with leases so only one instance runs a trigger; one issue at a time per trigger; missed times run once, not once per missed time.
- [x] TRIG-04 — Eligibility rules: has the label, open/To Do, unassigned or assigned to the trigger owner, not blocked, never run by this workflow (deduplication keyed by source, issue and workflow). A "Matching issues" preview lists next, waiting and skipped issues with the reason.
- [x] TRIG-05 — Claim and start write-back through gateway tools: assign, replace the label with `weft:in-progress`, add a comment saying Weft started with a link to the task, and in Jira transition to "In Progress" when that transition exists. A missing transition is recorded on the task and does not fail the run.
- [x] TRIG-06 — Completion write-back: on success comment with the PR link, label `weft:review` and transition to "In Review" when available; on failure or stop comment with the reason and label `weft:failed`, and pause the trigger.
- [x] TRIG-07 — Trigger UI: create/edit on a workflow, state (active, paused with reason, finished), last run and skipped-time reasons, resume after review. Browser tests with fixture MCP/GitHub servers. — Graph settings list triggers with state, consent, pause reason, current task, skipped issues and notes; administrators add, pause, resume (recording consent) and delete; operators preview matching issues. Component tests and a browser journey with fixture GitHub MCP tools in the e2e harness. Editing an existing trigger is API-only for now.
- [ ] Later: webhooks for Jira and GitHub with authenticated event validation, as an alternative to polling.

**Done when:** labelling a test issue in a personal Jira site or GitHub repository
produces, without manual start, a task that comments, transitions where possible,
and ends with a PR or a visible failure, under the pre-recorded consent.

#### Monetary cost control ([design](../docs/cost-control.md))

- [x] COST-01 — Cost records written by the LLM gateway for every call (native, external callback, delegation), with reported or estimated usage; external `TurnResult.usage` recorded separately.
- [x] COST-02 — Versioned price catalogue in installation settings, editable by administrators; local providers default to zero; unpriced models visible and governed by policy. No vendor prices shipped.
- [x] COST-03 — Task-run budget with reservation before dispatch (reusing the delegation CAS ledger pattern), settlement after, pause at the hard level and audited human raise.
- [x] COST-04 — User, project, workflow and installation budgets with monthly periods, warning and hard thresholds, and the optional interrupt level.
- [x] COST-05 — Console price editing, spend views, budget bars, remaining budget on New task and task views, events and bounded metrics.

**Done when:** a run that reaches its budget pauses before overspending, a human
raises it and the run continues, and spend per task, user and model is visible.

#### Native coding runtime ([design](../docs/native-coding-runtime.md))

Decision 2026-11-10: OpenCode is not used. Weft's own native runtime
becomes the coding agent, inside the gateways it already runs in, tuned for
local models first. Each step is accepted only when the evaluation suite
(NAT-01) improves or holds.

- [x] NAT-01 — Evaluation suite: fixture repositories with small real coding tasks (fix a failing test, implement a function, multi-file rename, bug described in issue text), each with a verification command, run through the real engine and native runtime. Record success, rounds, tokens, time and tool errors per model; scripted model in CI, opt-in local Ollama runs; baseline recorded in the design page.
- [x] NAT-02 — Turn context management: budget the conversation against the provider context window; elide older tool results to receipts, cap single results with paging hints, compact earlier conversation with a model call when still over budget; never drop the task, system prompt, plan or latest results; compaction events and metrics.
- [x] NAT-03 — Edit tools: `filesystem.edit` reports the closest region when a snippet is not found; `filesystem.multi_edit` (atomic snippet edits in one file); `filesystem.apply_patch` (atomic unified diff across files, each path checked against grants).
- [x] NAT-04 — Code navigation: `code.outline` (Go via go/parser; TS/JS, Python, Java heuristics), token-bounded `code.map` usable in planning prompts, and `filesystem.search` with literal mode, context lines, limits and default ignores.
- [x] NAT-05 — Verification loop: declared project/workflow checks (test, lint, build); `check.run` runs one in the sandbox/worktree under shell grants and returns exit status plus concise parsed failures (Go test, Vitest/Jest, pytest, generic); results recorded as task evidence.
- [x] NAT-06 — `plan.update` platform tool: per-execution checklist shown live on the task.
- [x] NAT-07 — Worktree checkpoints on hidden Git refs before edits; restore the last good checkpoint from the task (and, with a grant, by the agent).
- [x] NAT-08 — Role prompt templates (planner, implementer, reviewer, verifier) as built-in skills; compact tool schemas; per-provider guidance for small models; accepted only on evaluation improvement.
- [x] NAT-09 — Provider streaming (Ollama, OpenAI-compatible, Gemini) for live output; accounting unchanged.
- [x] NAT-10 — Software Factory v2: Plan → Implement → Review → Verify → SCM hand-off on the new tools, measured on the evaluation suite with a local model.

**Done when:** the Software Factory v2 solves the evaluation suite with a local
model at a success rate set after the NAT-01 baseline, every change and check
goes through the gateway, and checks appear as task evidence.

#### Activity view (added 2026-11-11)

A console page, reachable from the side menu, that shows at a glance what is
running across workflows and what needs a person, linking to the existing
node, run and approval views for details.

- [x] OBS-01 — Activity view MVP: side-menu "Activity" page with live counters (agents running, waiting for a person, failed and finished in the window); "Needs you" (pending tool approvals, agent questions, human reviews, hand-offs, oldest first); "Running now" grouped by workflow/task with each agent replica's state, current plan step, elapsed time, tokens and cost; and a filterable recent-executions history. Backed by one bounded, role-filtered aggregation endpoint (`GET /api/v1/activity?window=`) and live updates from the existing event stream; unit, integration, component and browser tests. — Delivered 2026-11-11: `#/activity` (Work menu) and `GET /api/v1/activity` (viewer; reviews follow the operator/reviewer gate and are reported as hidden, not empty). Reads are bounded (window 5m–7d, 1000 executions, 1000 pending approvals, 50 active graphs, 200 replicas, 200 plan events per graph, 5000 cost records) and flagged when truncated; failed sections are null with an explicit error. Counters focus the page; window, filters and focus live in the URL; live refetch throttled to 1.5 s from execution/replica/approval/graph/agent.plan events. Spans per section, `weft_activity_sections_total{section,outcome}` and `weft_activity_build_duration_seconds`. Go unit tests (windows, limits, truncation, plan step, phases, cost, roles, section failures) and an API integration test with the real store and engine pass; 430 Vitest tests and typecheck pass; Chromium `e2e/work-activity.spec.ts` (live decision into history, counter focus across reload, viewer and failure recovery) plus navigation, activity and visual baselines pass. Plan steps are unit-tested only: the echo runtime used by integration and browser tests records no plans. Developer usability validation remains separate.
- [x] OBS-02 — Activity health: per agent and workflow failure rate, most common errors, duration and cost rankings over 24 h / 7 d, linked from OBS-01. — Delivered 2026-11-11: Health tab in `#/activity` (`?tab=health`) and `GET /api/v1/activity/health?window=24h|7d&ranking=` (viewer). Per agent (workflow/node; task runs under their workflow) and per workflow: runs, failure rate (failed or interrupted ÷ finished), top 5 normalized errors with their latest run, nearest-rank median/p90 of finished runs, tokens and cost; rankings fails most / slowest / most expensive. Same bounds as OBS-01 (newest 1000 executions, 5000 cost records), 200 rows per list keeping every ranked row, flagged when truncated; executions or cost failures are explicit and null. Error normalization keeps the first line, redacts credentials and replaces long quoted text, URLs, e-mails, ids, paths and numbers. Spans per section, `weft_activity_sections_total{section=health_*}` and `weft_activity_health_build_duration_seconds`. Rows link to the filtered Activity history and to run details.

#### Deferred

- [ ] PROV-06 (Future idea) — Implement provider plugin installation and execution per PROV-05, after PROV-02 is delivered: explicit administrator install with checksum verification, isolated process, per-plugin encrypted credential, console label marking plugin providers, conformance suite run before enablement. Deferred on 2026-11-11 by the user: running third-party provider code opens security risks they do not want to take on now. The PROV-05 design stays as reference.
- [ ] AUTH-OIDC (Later) — Browser login with Authorization Code + PKCE, session cookie, refresh and logout; configurable role claim path and group-to-role mapping; issuer/audience validation. Qualify against a local Keycloak container first.


### 11. Integrated Validation of Release 1

Run the consolidated acceptance round after the remaining Release 1 implementation. User direction on 2026-10-18: keep external vendor validation for the final phase; continue local implementation and fixture-based checks first, without starting Release 2. Per-delivery automated tests continue throughout development. Include the deferred UX walkthrough; do not mark acceptance complete based only on automated tests.

- [ ] Create an end-to-end test flow: installation → login → provider → MCP connection → permissions → visual editor → environment build → task → execution → files/diff → approval, when enabled. — A consolidated disposable-browser journey now covers login, MCP save/discovery, explicit grants, environment discovery/review/build, native execution/delegation, write approval and file/diff inspection. It uses a seeded graph/model, synthetic MCP and simulated Docker; clean installation, provider registration and real sandbox acceptance remain pending.
- [ ] Repeat the flow with both approval-required and automatic delegation policies. — Both consolidated fixture journeys pass. Automatic delegation still requires the separately configured filesystem-write approval; the baseline file remains unchanged before approval, and temporary children do not become permanent graph nodes. Repeat against the complete clean-install flow before closing acceptance.
- [ ] Validate direct first-use navigation on a clean installation, including real test MCPs and providers; the global onboarding panel has been withdrawn.
- [x] Verify that no interface path allows users to directly edit workspace files. — File/Git inspection exposes only reads; API integration and Chromium reject POST/PUT/PATCH/DELETE even for administrators and verify unchanged contents. Component tests confirm read-only controls and GET-only requests. Agent tools remain the explicit write path.
- [ ] Verify that no secret reaches the browser after saving, the runtime, a prompt, or logs. — Provider browser acceptance now saves and tests a synthetic key through the real backend and a loopback HTTP model server, checks successful inference, edit/reload, and a 401 response that deliberately echoes the key. API/UI/localStorage/sessionStorage and captured model requests contain no key. Existing provider API/storage integration and component tests pass; broader all-credential/runtime/log acceptance remains pending.
- [ ] Verify sandbox-mode toolchains/images/containers stay on the configured Weft sandbox daemon; reset preserves unrelated and persistent data. Separately validate explicitly selected native worktree execution on the host without claiming sandbox isolation or automatically installing project dependencies.
- [ ] Verify that data and instructions from one graph/workspace do not appear in another scope. — File inspection now discards late reads/errors across selections and graph/workspace changes, with component and browser regression coverage. A real engine/store/gateway integration now runs two graphs with identical node IDs and distinct skills, project steering, operator tasks and files; runtime inputs and persisted instruction snapshots contain only their graph markers, and traversal, absolute cross-graph paths and symlink reads are denied. Broader Docker/external-runtime and final manual acceptance remain pending.
- [ ] Run relevant backend, frontend, SDK/runtime, and Docker image tests; record known limitations. — Provider-management browser checks use their own mandatory disposable backend because provider CRUD reloads the fixture model registry. Main journeys, hosted CIMD and provider management remain chained under make test-e2e; full installed/Docker acceptance remains for the final phase.
- [ ] Update user, operations, and troubleshooting guides to reflect delivered behavior. — Delegation contracts now distinguish delivered bounded recursion/external-parent callbacks from proposed alternate APIs; MCP warehouse qualification and operations guidance are updated. Final integrated acceptance documentation remains pending.

**Done when:** Release 1 acceptance criteria in `PLAN.md` are covered by automated tests and a documented manual test.

## Production readiness (added 2026-11-11)

What must be true before Weft has real users, beyond finishing the planned
work. Blockers come first; the rest can run in parallel. Details:
[production hardening](../docs/production-hardening.md) and
[deployment](../docs/deployment.md).

**Blockers**

- [ ] PROD-01 — Real sign-in: OIDC/SSO login with roles from the identity provider's groups (completes AUTH-OIDC); static tokens only for automation.
- [ ] PROD-02 — End of POC mode: versioned database migrations, a tested upgrade path between releases, and no more breaking changes without migration.
- [ ] PROD-03 — Production model providers under a data-protection agreement (paid Vertex AI, Bedrock or paid API; never a free tier that trains on inputs), with real prices in the catalogue and budgets per project and user.
- [ ] PROD-04 — Final acceptance (section 11 of Release 1): end-to-end journey, no secret reaching the browser, logs, prompts or runtimes, sandbox isolation validated; browser suites green.

**Security**

- [ ] PROD-05 — Security review or pentest focused on sandbox isolation, network egress, approvals, and prompt injection from repositories, issues and tool output.
- [ ] PROD-06 — Secrets in a managed vault or KMS instead of the local key file; key rotation documented and tested.
- [ ] PROD-07 — Conservative defaults: human approval on write tools, push and pull requests for new workflows; documented threat model.

**Operations**

- [ ] PROD-08 — Chosen topology (single node or Kubernetes HA) with managed or replicated PostgreSQL and NATS.
- [ ] PROD-09 — Backups with a restore drill actually performed and timed.
  - Workspace journal backups now cover the complete segment chain and its cursor.
    The SQLite integration tests resumed import after restoring a missing
    archive. A timed installation restore drill is still open.
- [ ] PROD-10 — Alerts and SLOs on the existing metrics, runbooks, and an on-call owner.
  - The workspace journal exposes size, rotation, gap and import metrics.
    `promtool test rules` verified that the capacity alert fires and clears.
    The [journal runbook](../docs/operations.md#workspace-journal) covers rotation
    and recovery. Production SLOs, alert routing and on-call ownership are open.

**Quality and delivery**

- [ ] PROD-11 — CI on GitHub (`woliveiras/weft`): Go, Python and web tests, lint, build and browser suites on every change; known failing tests (`internal/repositoryimport`, `e2e/mcp.spec.ts:18`) fixed so the suite is green.
- [ ] PROD-12 — Supply chain: signed images, SBOM, dependency and image scanning, pinned versions.
- [ ] PROD-13 — Coding agent acceptance bar: the production model and a minimum solve rate on the evaluation suite (with repetitions) chosen and met.

**Integrations and organisation**

- [ ] PROD-14 — Real-account validation of the integrations in use (GitHub, Jira; Bitbucket Data Center and Bedrock if adopted).
- [ ] PROD-15 — Organisational approval when used inside a company: sending source code to an external model provider, data processing terms, and the acceptable-use policy.

## Release 2 — Reusable Components

Start only after Release 1 is complete and stable.

- [ ] Define a versioned format for reusable agent and subgraph components.
- [ ] Define component parameters, defaults, validation, and version compatibility.
- [ ] Implement permission-aware component storage, listing, reading, updating, and deletion.
- [ ] Implement saving an existing agent/subgraph as a component.
- [ ] Implement inserting a component into a graph through the visual editor.
- [ ] Implement parameter resolution on insertion without inadvertently overwriting local settings.
- [ ] Show the version in use, available updates, and diffs before updating an instance.
- [ ] Implement component sharing according to installation permissions.
- [ ] Add round-trip, version, parameter, permission, and update-without-configuration-loss tests.
- [ ] Add documentation and end-to-end tests for the reuse workflow.

**Done when:** Components can be created, versioned, shared, and reused through the interface without manual copying or loss of local configuration.

## UX Reorganization — Approved Direction

- [x] Deliver separate Connections, Models and Administration destinations with stable navigation, scoped loading, compatible legacy MCP/template/OAuth links and unchanged role gates. — Route unit tests and real-client component integration cover destination isolation, viewer controls and health failure/recovery; disposable-backend Chromium covers navigation, reload/back, legacy links and narrow viewer access. The complete standard and hosted-CIMD browser suites pass. This first slice retains existing forms; no installation activation is implied.
- [x] Add a scalable graph selector and agent list alongside the canvas, preserving links and draft state. — Search graph/agent names and IDs, restore visited destinations, mark unsaved graph drafts and keep inactive editor sessions in memory with effects paused. Preserve original draft revisions across remote refreshes and retain local drafts when a graph becomes unavailable. Canvas links and explicit agent permissions remain intact; narrow layouts move the list above the canvas. Unit/component integration covers search, errors/reload, selection, cached editor selection and revision conflicts. Real-controller Chromium verifies independent drafts, conflicts, persisted grants and viewer navigation. All 226 frontend tests and 44 standard/hosted-CIMD browser scenarios pass; six navigation/editor scenarios were repeated after the final selection fix. make test, make lint and make build pass. Drafts do not survive page reload; installation activation and developer usability validation remain separate.
- [x] Deliver MCP connection list/detail screens (Overview, Tools, Usage & history, Settings), explicit creation, authorization/revocation/error recovery and grant visibility. — Search/filter and route unit tests; real-client component integration for failures, revision conflicts, credential preservation, scoped history and inherited/explicit grants; disposable-backend browser journeys for creation, discovery, edit/disable, reload, history scoping, viewer restrictions and missing records. Existing OAuth/DCR/CIMD suites remain passing. Legacy setup links retain their compatibility flow. Installation activation and developer usability validation remain separate.
- [x] Deliver dedicated agent configuration sections with effective permission/isolation summaries and a whole-graph change review before save. — General, Instructions, Tools & permissions and Advanced share the graph draft; the summary explicitly describes configured boundaries rather than claiming runtime authorization. Review lists changed fields across the entire graph without copying values. Unit and real-client component tests cover inherited/explicit grants, isolation, routes, multiple-agent/graph changes, validation failures, dependency errors, revision conflicts and viewers. Disposable-backend Chromium verifies reviewed persistence, constrained and MCP grants, reload/back navigation, invalid-input recovery and narrow read-only access. All 207 frontend tests and 39 standard/hosted-CIMD browser scenarios pass; make test, make lint and make build pass. Existing canvas/Definition editing remains compatible; activation and developer usability validation remain separate.
- [x] Deliver Runs list/detail screens over existing execution records, highlighting blockers and distinguishing execution outcome from individual tool outcomes. — Search/filter the latest 100 executions; Result, Timeline, Tool calls, Messages and Diagnostics use existing authorized APIs. Details verify graph identity, correlate records by persisted IDs, label bounded history and current-workspace links, and reuse approval role gates. Unit/real-client integration tests cover filters, scope, independent tool outcomes, polling, missing records, failures and recovery. Chromium covers approval-to-completion, deep links/reload/back, viewer restrictions, graph mismatch and failed-history retry. All 219 frontend tests and 42 standard/hosted-CIMD E2E scenarios pass; the six Runs/Activity scenarios were repeated after final changes. make test, make lint and make build pass. Installation activation and developer usability validation remain separate.
- [x] Consolidate typography, spacing, button hierarchy, contextual help and accessible responsive components across the remaining screens. — Shared page/card/form styles cover Models, Administration, Approvals and Skills; canvas guidance is a native disclosure. Skills adopts shared action buttons with unchanged handlers and disabled fieldsets. Security notices and errors remain visible; focus and reduced-motion styles are explicit. Unit tests cover disclosure and disabled actions; real-client integration proves help does not invoke providers or expose viewer actions. Chromium covers keyboard disclosure, narrow layouts, viewer permissions and unchanged canvas editing. make test (229 frontend tests), make lint, make build and make test-e2e (45 standard plus one hosted-CIMD scenario) pass. A canvas-edge rendering failure after reload occurred in an earlier suite attempt; its isolated repeat and the final full suite passed without changing assertions. Preview was inspected at narrow and desktop widths. This slice was subsequently activated as commit 7ef1178 on 2026-10-17; see the activation evidence below. Developer usability acceptance remains pending.
- [ ] Final Release 1 acceptance: validate navigation and the three redesigned journeys with developers; record evidence separately from proposed usability targets. — Deferred by user decision on 2026-10-18 until remaining implementation is ready; not a blocker to development.
- [x] Remove the global onboarding panel following user-reported layout breakage; preserve direct navigation and contextual help. The removal was activated and browser-validated on 2026-10-18.


**Navigation baseline activation (2026-10-17):** Commit b0f7670 and preceding UX
slices were activated and checked in the local installation. weftd was rebuilt
and recreated using the existing volumes; health was ok, all three graphs and
nine saved versions remained present, and the browser checks covered connections,
agent configuration, draft switching/review/discard, saved Runs, Models and system
health. Earlier slice notes describe evidence before that activation. Developer
usability acceptance remains pending; later visual changes are not yet activated.


**Visual consolidation activation (2026-10-17):** Commit 7ef1178 was built with
docker compose build weftd and activated with docker compose up -d --no-deps
--force-recreate --wait weftd. Health returned ok and the existing four weftd
volume mounts were retained. The three graphs and nine saved versions remained
unchanged. Authenticated browser checks covered Models and its four providers,
keyboard-opened help, Skills guidance, Approvals, Administration health and canvas
guidance. Software Factory rendered its ten edges before and after reload;
Save graph remained disabled. No configuration, grant or credential edits were
submitted. Developer usability acceptance remains pending; global onboarding has since been withdrawn.


**Developer usability round (2026-10-17, in progress):** The first task asks the
developer to locate reader in MCP installation validation and identify its model
and grants without saving changes. Feedback is pending; MCP connection and
execution-tracking tasks have not started. Keep the usability checkbox open.
Record confirmed adjustments after developer observations; do not treat the
previous assistant-operated installation checks as developer acceptance.

**First-round feedback:** The developer described the Graphs screen as outdated and supplied a screenshot with the new navigation and graph selector. The served HTML references the activated visual build assets, matching the running container. Whether the concern is visual fidelity, graph data, or task instructions remains unconfirmed; clarification is pending. Do not classify this as a cache defect or accept the agent journey yet.


- [x] UX-01: Make graph cards clickable and agent discovery explicit. — Card
  surfaces open the graph through the accessible title button; action controls
  stay independent. Named Configure links open agent settings. Unit and browser
  tests cover navigation, keyboard activation, independent Start and retained
  drafts. Developer revalidation in the installation remains pending.


**Agent journey progress:** Developer screenshots confirm General and Instructions
were reached with assistance; ollama-local / llama3.2:latest is visible. Exact tool
grants are not yet demonstrated. An execution start is visible in Events, but its
outcome is not established. Continue with the existing run rather than starting
another. MCP connection journey and overall usability acceptance remain pending.

- [x] UX-02: Prevent long event types from overlapping actors. — Grid tracks
  and children allow wrapping; narrow layouts stack event details. Component and
  real-browser tests cover long names, separate actor bounds and expandable data
  at 1280 and 580 pixels. Developer revalidation remains pending.


**Developer round evidence update (19:28 local time):** The developer explicitly
confirmed that GitHub get_me and get_file_contents are selected in Tools &
permissions. Agent model and grant identification is complete with navigation
assistance; independent discoverability is not yet accepted. Subsequent screenshots
show the existing reader execution (graph version 7, started 19:23:57) as Completed,
with completion at 19:25:15 in Diagnostics. Tool calls shows one invoked/completed
pair each for get_me (2010 ms) and get_file_contents (1712 ms). Result, Timeline,
Tool calls, Messages and Diagnostics were reached. Messages reports no linked
messages in the recent window; this alone is not a failure. Screenshots establish
the displayed outcomes, not an inspection of raw MCP responses. The developer's
assessment of clarity and the MCP connection journey remain pending.

**UX-03 — tool-call count clarity:** Result says “4 tool calls recorded”, whereas
Tool calls displays four lifecycle events for two calls. Investigate the counting
source before implementation; distinguish actual calls from lifecycle events and
make summary/detail counts consistent. Preserve separate execution and tool
outcomes, including partial histories and failures. This is a screenshot-confirmed
presentation discrepancy; its implementation cause has not been established.

- [x] UX-03: Reconcile recorded tool attempts and lifecycle records. — Native
  runtimes no longer report gateway-counted calls a second time. Unit and engine
  integration tests cover counting, including failed attempts; browser coverage
  checks a real native/gateway execution and its summary. Runs distinguishes
  observed invocation events from lifecycle events and explains partial history
  and preserved historical totals.


**Confirmed finding UX-04 — connection validation versus agent execution:** The
developer explicitly reported losing context between Connections / Personal GitHub
and Graph, and had not understood that reader belongs to the dedicated MCP
installation validation graph. The guided round mixed connection setup validation
with an agent execution check without explaining their different purposes. The
existing connection Test and Refresh tools actions do not require creating an
agent. The dedicated validation agent was configured during the earlier assisted
gateway validation; it is not an automatic consequence of connecting an MCP.

Proposed adjustment: keep connection health/discovery checks and their results in
connection detail; explain separately that using tools requires explicit grants to
an agent. Make optional end-to-end agent checks clearly labeled, with the target
graph/agent and a return path to the connection. Do not create agents or grant tools
automatically. Retest whether the developer can distinguish connection readiness
from successful agent tool execution without facilitator explanations. This finding
also concerns the validation instructions, not only the product navigation.

- [x] UX-04: Clarify connection checks versus optional agent execution. —
  Connection detail explains discovery without agents and separate explicit
  grants. Agent-access links name the graph and open Tools & permissions, with
  a return banner retained across tabs and reloads. Unit/API-client integration
  and browser tests verify context, unchanged grants and no automatic execution.
  Developer usability acceptance remains pending.


- [x] UX-05: Automatically discover tools after saving in the console. — Enabled
  token-based and unauthenticated connections attempt discovery; pending OAuth
  waits for consent and authorized OAuth can refresh. Disabled connections stay
  inactive. Save and discovery states are distinct; saved connections survive
  discovery failures, with retry and explicit agent-grant guidance. Unit and
  API-client integration cover eligibility, empty catalogues, failures and no
  implicit grants; real-backend browser journeys cover automatic discovery,
  authentication failure, recovery, OAuth and disabled connections. API clients
  retain separate save and refresh endpoints.

**UX-01–UX-05 verification (2026-10-17):** make test passed (Go unit/integration,
Python and 241 frontend tests), make lint passed, make build passed, and
make test-e2e passed (48 standard Chromium scenarios plus one hosted CIMD case).
Desktop graph cards and narrow Events screenshots were inspected. Earlier targeted
runs exposed outdated test expectations for automatic discovery and missing test
setup; these were corrected with assertions retained. The existing sandbox journal
supervision test failed in the first full run and one isolated repeat, then passed
in a diagnostic run and the final full run without source or assertion changes.
Treat that intermittent test behavior as unresolved; it is not an established UX
regression or a claimed fix. No installation rebuild/restart, deployment, secret
change, data migration or commit was performed. Activation and another developer
usability round remain pending; the overall usability checkbox stays open.


**UX-01–UX-05 installation activation (2026-10-17):** Implementation commit
322f3bd was activated using docker compose build weftd and docker compose up -d
--no-deps --force-recreate --wait weftd. Only weftd was rebuilt/recreated. Health
returned ok; the existing workspace, data, journal and client-certificate volumes
were reused. PostgreSQL retained three graphs and nine graph versions. Other
services, including Ollama and the sandbox daemon, remained running.

Authenticated Chrome validation on localhost:8080 confirmed graph opening by
clicking the card description outside its title, explicit Configure reader links,
unchanged saved graph version/grants, connection-to-agent context and return links
through Runs and reload, and separate invocation/lifecycle counts. Saving the
existing public DeepWiki validation connection without changing its fields showed
“Connection saved. Discovering tools…” and automatically discovered three tools.
It remained credential-free and had no saved agent grants. Its connection revision,
catalogue revision and discovery timestamp advanced as expected.

A single new read-only reader execution (exe_mvcoynbnbb657e8bc131ce1afd81, graph v7)
completed using ollama-local / llama3.2:latest. Runs recorded two tool attempts and
two observed invocations with four lifecycle events; get_me and get_file_contents
both completed through the GitHub gateway. No graph or tool grants were edited and
no repository-writing tool was used. The fresh execution.instructions.loaded event
wrapped without overlapping reader n0 in Events. The earlier event was outside the
general recent-event window, so validation used this new execution.

Screenshots of installed graph cards, automatic discovery, Events and tool outcomes
were inspected. This is installation activation evidence, not developer usability
acceptance. The broader developer usability round and the previously recorded
intermittent sandbox supervision test remain open. No secret values, authentication
configuration, external OAuth applications or persistent volumes were changed.


**Historical resumable onboarding evidence (withdrawn 2026-10-18):** Implemented an optional, initially
collapsed Getting started panel without route redirects or background mutations.
Six topics cover concepts, health, models, MCP discovery/grants, canvas editing
and execution inspection. Live prerequisite reads report errors and retry explicitly;
topic acknowledgements are never inference, tool-access or installation-health
certification. Progress is versioned, local to the browser origin and sign-in
identity name/role, with no credentials or configuration payloads. Skipping does
not complete a topic; revisiting completion preserves progress; confirmed restart
only resets the guide. Storage failure/corruption is visible. The explicit example
uses the existing validation/create APIs, a finite echo runtime, no tools or model,
and no automatic start; viewers cannot create it. Echo forwards messages and does
not validate model inference or MCP access. The native-only read-only workspace
flag is intentionally absent from echo, following existing graph validation.

Validation: make test passed (Go unit/integration, Python suites and 256 frontend
tests across 43 files), make lint passed, make build passed, make test-e2e passed
(50 standard Chromium scenarios plus one hosted-CIMD scenario), and git diff --check
passed. New unit/real-client component tests cover progress parsing, invalid data,
skip/review/resume/restart, storage failures, prerequisite errors/retry, rejected
validation/create attempts, stable retry IDs and viewer restrictions. New real
disposable-backend browser tests create the example, preserve an unsaved agent
draft across guide/navigation changes, save it, execute echo and inspect its real
result, reload completed progress, confirm restart without deleting the graph,
and retry failed prerequisites as a viewer without mutations. Existing canvas
agent/channel creation, MCP, grant, OAuth and execution journeys also pass.
Desktop and narrow browser captures were inspected.

Earlier focused E2E failures exposed the native-only read-only workspace rule
and an incorrect expectation that echo returns its prompt; the example and test
were corrected to the actual runtime contract before the full passing suite.
README, getting-started and interface-navigation documentation describe the
browser journey and persistence limitations. No installation activation, external
OAuth changes, real model inference, fresh Docker installation, or developer
usability acceptance is claimed. First-class tasks and delegation remain in their
existing workstreams. Activation and the final developer usability round are pending.


**Historical onboarding installation activation (before removal):** Activated commit 9b8d1e8
with docker compose build weftd and docker compose up -d --no-deps
--force-recreate --wait weftd; both completed successfully. Only weftd was
recreated, without removing volumes, running migrations, or changing authentication.
The health endpoint returns ok; weftd, agents, NATS, PostgreSQL and the sandbox
daemon are healthy. Graph/version counts remained 3/9 before and after activation.

Browser validation on localhost:8080 confirmed the initially collapsed guide,
reviewing one topic, skipping health without marking it reviewed, pausing on
Choose a model, reloading, and resuming the same topic with 1/6 reviewed.
Navigation to Models and back to Graphs also preserved the topic/progress.
The health observation showed ok and the model observation showed four configured
providers without claiming successful inference. No graph was created, no agent
was started, and no connection, grant, credential or model configuration was
modified during this browser check. Only browser-local guide progress changed.
The implementation's passing unit/integration and 51 E2E scenarios remain the
automated evidence; this activation adds installed-browser pause/resume evidence.
Developer usability acceptance and a fresh-install exercise remain separate.

### Release 1 validation correction: paused input admission

- [x] Consult durable pause state before input consumption and sandbox resumption.
  Unit cases cover stale local state, resume, node pause, stop and storage failure;
  repeated integration/race tests cover an immediate message after pause and
  verify it remains unconsumed until resume. Browser recovery coverage pauses,
  sends input, resumes and checks the preserved workspace.

**Canvas regression follow-up (2026-10-18):** The previously intermittent missing
edges after reload/reopening were reproduced in two of five isolated browser runs.
Explicit post-render handle measurement now restores connections after structural
rebuilds and cached workspace reveal. Component integration verifies rendered
paths, removal and restoration; the unchanged browser behavior assertions pass in
five independently seeded repetitions. This is local code/test evidence, not a
new installation activation or developer homologation.

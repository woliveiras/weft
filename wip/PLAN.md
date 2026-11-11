# Weft Evolution Plan

**Goal:** Let users start useful tasks, choose existing workflows, follow progress and review results through the web interface, while keeping graph construction, configuration, security and diagnostics accessible. LangGraph remains supported; reusable components are deferred to the second release.

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
The Phase A (task-oriented UX) and Phase B (standalone installation) implementation sequence is recorded below, followed by deferred external acceptance. Other Release 1 gaps remain open; Release 2 is not authorized. Reuse existing graph/agent execution and message controls where possible. Define the task-to-execution contract before deciding whether additional persistent task metadata is necessary; a navigation rename alone does not justify a parallel execution engine.

## Task-oriented UX and Standalone Delivery Plan (2026-10-19)

**Status: Phase A task-flow implementation and the Phase B native bundle are delivered within the documented capability limits. macOS arm64 automated native qualification passed; external accounts, actual editor launch, optional real native sandbox and other platform smoke checks remain pending. No existing installation activation is claimed.**
**New decision, 2026-10-19: prompt-first New task and task authorization modes
are tracked as the next user-facing Release 1 increment. PROMPT-01 now delivers
the prompt-first editor, PROMPT-02 adds durable request interpretation, and
PROMPT-03 adds explicitly confirmed public repository imports and PROMPT-04 adds
explicit Git authentication (including native host credentials). PROMPT-05 adds
conditional administrator-confirmed prompt admission; task authorization modes remain planned. The current explicit review/start flow remains in force. See the decision below and PROMPT/AUTH tasks; no new runtime
behavior or installation permission change is activated by this documentation.**

Earlier UX delivery evidence remains historical evidence for existing screens, not
acceptance of this new flow. Do not restore the removed global onboarding panel.

### Phase A — Daily work first

FLOW-01 design completed on 2026-10-19: see [task workflow contracts](../docs/task-workflows.md). Inspection confirms that graph runs and replica worktrees cannot serve as independent user tasks. Durable task metadata, launch/completion bindings and shared-scope admission are required before enabling the new flow; none is claimed implemented by the design delivery.

Separate navigation into Work (Tasks, Projects, Approvals) and Configuration
(Workflows, Connections, Models, Skills & instructions, Administration). This is
presentation for the same user, not new roles or relaxed backend authorization.
Workflows exposes existing graph/agent editing; existing links and drafts survive.

New task starts with an outcome: understand a repository, implement a change,
investigate an issue, review changes, or a freeform request. Select a project and
an existing graph such as Software Factory, then review model, required tools,
effective permissions and execution environment before starting. Outcome presets
are editable instructions, never implicit grants. Validate graph input/entry-point
compatibility and preserve independent task/run context when a graph is reused.

Investigate an issue can use explicitly connected Jira/GitHub MCP tools, with a
URL or issue ID as input. Missing connections or grants have direct repair links
that return to the draft. Installed local MCPs are not automatically available to
a Docker server; local stdio support belongs to Phase B. No automatic credential
import, authorization expansion or provider write is implied.

The task page keeps the request, current progress, required decisions, errors,
result and changes together. Show approvals in context and in the global inbox.
Keep events, tool calls, traces, usage and execution-tree controls accessible as
details. Distinguish task completion from individual tool outcomes and missing
telemetry. Repair links preserve task context and draft state.

Present Local worktree and Sandbox as explicit capability-aware options. Phase A
must not offer unsupported host execution as working functionality. A container
mount shares files, not host execution. Remember a project's choice only while
it remains available and authorized. Use short English help explaining that a
worktree separates source changes, not processes/network/credentials, linking to
[Why Git Worktrees Matter](https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/).

FLOW-02 implementation evidence (2026-10-19): the shell now groups Work and
Configuration, with collapsible configuration, active destinations and responsive
navigation. At that delivery, Workflows retained graph routes and Tasks/Projects were disabled;
the later FLOW-03 deliveries below enabled their implemented behavior. No role or API changes.
Unit/App integration and 11 disposable-backend browser scenarios passed, including
legacy links, drafts and viewer restrictions. Full test/lint/build checks passed;
this does not constitute installed activation or final human acceptance.

### Phase B — Independent standalone installation

Offer a native Weft installation with a browser UI and execution as the OS user,
without requiring Docker or a companion executor connected to a container.
Keep the Docker installation independently supported for local/remote servers.
Deployment location and execution isolation remain separate choices. Optional
sandbox execution may require an explicitly configured compatible runtime.

Verified baseline: config defaults already use SQLite and the filesystem queue;
these adapters run without separate PostgreSQL or NATS services. Reuse storage,
queue, engine and permission contracts. Do not claim the packaged native journey
is complete merely because these adapters exist. Audit UI assets, runtime/provider
dependencies, OS support and container assumptions before selecting packaging.

Manage repository registration and per-task worktrees without overwriting dirty
checkouts, crossing task ownership or silently deleting data. Support VS Code
handoff and manual review. Commit/push remain separate explicit decisions.
Local execution uses actual OS permissions; it is not a sandbox and must not imply
filesystem or network confinement that the implementation cannot enforce.

Register local MCP processes explicitly, with reviewed commands/environment and
per-agent tool grants. Reuse suitable HTTP connections. Do not automatically
consume another application's MCP configuration or OS credential store. Keep
secrets out of API responses, browser storage, prompts and logs.

Package startup, local data paths, secure initial access, restart recovery,
backup/restore and safe upgrades. PostgreSQL-to-SQLite migration is a separate
explicitly approved operation, not an installation side effect. Preserve existing
installation data and require approval for activation or external changes.

### Evidence and acceptance

Each behavior delivery needs unit, integration and relevant browser coverage,
including invalid input, dependencies failing, permission denial and recovery.
Run make test, make lint and make build; record E2E results separately. Keep all
new tasks unchecked until proven. Final human walkthrough, clean-install and
external vendor acceptance remain deferred to the final Release 1 stage. Phase A
can be tested against current execution capabilities; Phase B adds real native
worktree/OS and optional sandbox validation without replacing Docker regression
coverage. No Release 2 reusable-component work is included.

## Product Principles

- **The web interface is the primary path:** Normal workflows must not require server access or CLI usage.
- **Configurable models:** Use the provider and credentials configured by the user, without coupling the platform to a vendor.
- **Bounded autonomy:** Agents may delegate work, but they cannot expand their own permissions or consume unlimited resources.
- **Security independent of instructions:** Skills and steering guide agents but do not replace platform-enforced permissions.
- **Genuine visual construction:** The canvas must allow users to edit graphs, not just view those already defined.
- **Explicit execution choices (planned UX):** Local worktrees and sandboxes are first-class choices. Standalone installation is planned after the task-oriented UX. Existing managed container behavior remains supported: Project environments run on Weft's dedicated daemon. Native `process` execution remains supported as an explicit trust choice with the platform user's OS rights; it does not gain container isolation. Deployment location and execution isolation are separate choices (see the README). Resource cleanup must preserve persistent data and respect ownership and approval policy.
- **Review in the preferred editor:** The browser workspace inspector remains read-only. In the planned standalone workflow, users can open worktrees in VS Code, edit and review locally, and decide separately whether to commit and push. Completion never implies permission to commit or push.

## Release 1 — Build and Operate Graphs Through the Interface

### 1. Remove Out-of-Scope Product Functionality

Remove compatibility features and references that are not part of the planned product:

- converter and import endpoint;
- conversion button, examples, and related tests;
- remaining documentation, references, and dependencies for that compatibility.

**Acceptance criterion:** The repository and application contain no compatibility flows, references, or documentation for out-of-scope external products.

### 2. Complete Model Configuration

Preserve support for LangGraph and the existing model provider system. Make provider configuration available in the interface so users can:

- add and edit providers;
- store credentials securely without displaying them again after saving;
- choose the provider and model for each agent;
- validate the connection and show actionable errors.

**Acceptance criterion:** Users configure credentials through the interface and run an agent using the selected provider, without configuring secrets directly on the server during normal use.

**Verified implementation:** Browser configuration and the Python SDK’s real LangGraph adapter are connected through the managed gateway. Tests cover model override, failures and credential isolation using an artificial-key deterministic model endpoint; live vendor inference quality remains outside this evidence. See [validation guide](../docs/provider-langgraph-validation.md).

### 3. Turn the Canvas into a Graph Editor

Evolve the existing `GraphCanvas.tsx` view to allow users to:

- create, move, configure, and remove agents;
- create, edit, and remove channels connecting agents;
- configure agent and channel properties in panels;
- validate the graph before saving;
- identify unsaved changes, errors, and version conflicts;
- keep the YAML editor as an advanced option.

**Acceptance criterion:** Users can create, configure, save, and start a complete graph in the browser without editing YAML or accessing the machine running the service.

**Verified implementation:** Component/API tests cover editing, validation, discard and version conflicts; Chromium now covers real connection dragging, Delete cancellation/confirmation and persisted agent/channel edits.

### 4. Provide Containerized Project Environments

Agents need the project's real toolchain (for example Go, Node, and pnpm) to install dependencies, build, and run tests. The supported sandbox scope is implemented: dedicated daemon, repository discovery, approved initial proposals, compatible reuse, capacity, idle resume and reference-safe cleanup. No Weft-specific file is required. See [acceptance evidence and explicit limits](../docs/sandbox-acceptance.md); this does not complete the other Release 1 workstreams.

Give each graph a managed **environment**:

- run every environment build and sandbox container on a Docker daemon dedicated to Weft (a Docker-in-Docker service in the Compose stack, rootless where the host supports it) instead of the host daemon; images, containers, and caches live in that service's volumes, so removing them resets everything and nothing is installed on the host;
- keep sandboxes on a network separate from platform services (database, broker, control plane); only the control plane talks to both;
- preserve an explicit environment configured by the operator in the interface; otherwise discover repository definitions in this order:
  1. `.weft/environment.yaml` (highest repository priority, optional);
  2. `.devcontainer/devcontainer.json`;
  3. Dockerfiles;
  4. Docker Compose files (`compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`, and explicitly selected variants);
- show the discovered source and effective definition for operator acceptance. When multiple Dockerfiles, build targets, or Compose services are candidates, ask the operator to select the development environment; do not start every service automatically. Report invalid or unsupported definitions rather than silently falling through to a lower-priority source;
- when no supported definition file is found, use an agent with bounded, read-only repository access to analyze manifests, lockfiles and toolchain declarations such as `mise.toml`, `go.mod`, and `package.json`. This analysis must work before a project environment exists. Propose the image, tools, setup commands, caches, network needs and rationale; a developer approval gate blocks building and task execution until the proposal is accepted. Rejection keeps the task blocked; approval permits building, and execution continues only once the environment is ready;
- build images and run setup steps in Weft, never through agent access to Docker; agents get `environment.propose` and `environment.status`, and keep running commands only through `shell.exec` inside the ready environment;
- apply a network policy per phase: setup may reach the internet to download dependencies; execution defaults to no network, with an optional host allowlist enforced by an egress proxy;
- reuse prepared images and compatible dependency caches across tasks for the same authorized repository and effective environment. The delivered implementation shares prepared base/user layers and immutable cache seeds, copies seeds into private writable caches, and reruns setup in each isolated graph workspace; Identify compatibility from the definition, relevant setup inputs (including lockfiles), platform and policy; reuse unchanged environments and rebuild when those inputs change or the operator requests it;
- keep task workspaces/worktrees and active execution containers isolated. Share only caches that support concurrent use, otherwise serialize access or isolate them. Never share task outputs, credentials or private repository data across unauthorized scopes;
- bound memory and CPU per sandbox and enforce a global concurrency/resource budget, queueing tasks when capacity is exhausted. The delivered budget reserves memory and slots across execution sandboxes and environment pipelines, with persistent accounting and cancellation. BuildKit workers, setup and cache copies run sequentially under one reservation; global CPU accounting now shares those atomic durable reservations and applies kernel caps to execution, BuildKit, setup and cache copies. Delivered idle suspension stops execution containers between turns after a configurable timeout (5m by default, 0s disables), preserves their files/workspaces/caches, and readmits the same container before new work. Active turns are excluded; process memory is not checkpointed. Image/cache reuse saves build time and disk, while active-container limits control memory consumption;
- expose environment state (`absent`, `building`, `ready`, `failed`), structured build phases and reason categories (delivered in settings and the agent panel), approval and capacity waiting states, reuse decisions, build logs, **Rebuild**, and **Reset** in the console. Reset and graph deletion must respect shared-resource ownership and active references: never remove images or caches another task still needs; delivered Integrations UI previews exact resources, requires selection/confirmation, and reports partial outcomes; admin preview/cleanup reclaims unused registered generations after rechecking environment/build/container references under reuse leases. Keep historical resources without ownership records; never force removal or delete workspaces/private caches.

Environment definitions coming from repositories or agents are untrusted content: they cannot change grants, credentials, limits, or network policy beyond what the installation allows.

**Acceptance criterion (sandbox mode):** An operator connects a repository without requiring a Weft-specific file, accepts a discovered definition or approves an agent proposal, and runs tasks only after the environment is ready. Later tasks reuse compatible images and caches while keeping workspaces isolated, with bounded active resource consumption. Rebuild and reset preserve resources referenced by other tasks. Nothing is installed on the host and agents never receive Docker access.

### 5. Create the File and Interaction Panel

Add a per-graph workspace to the console, including:

- a file tree and content viewer;
- Git diff, branch, directory, and responsible replica views;
- links between files, messages, and executions;
- an interface for sending tasks/commands to agents and tracking progress;
- a history of agent-made changes, with review and approval according to the configured policy.

Users may read and inspect files, but cannot edit, delete, or upload them through the console. Only authorized agents may modify the workspace; every operation must respect workspace boundaries and permissions without exposing the server's filesystem.

**Acceptance criterion:** Users can start and direct tasks, monitor agents, and inspect changes entirely through the browser, without server access or CLI usage.

**Delivered inspection:** Files now exposes read-only Git status, tracked diff against HEAD and bounded history, plus branch/directory/replica metadata and recent execution/tool-operation context. Real repository/API tests cover confinement and unsafe Git configuration; browser coverage starts a task and inspects its result. Native agents can explicitly opt into `workspace.readOnly`, enforced before tool execution regardless of grants; Docker also mounts their workspace read-only. The event log and Files panel include bounded before/after workspace observations for filesystem, Git-write and shell calls, associated with agent and execution, including failed calls. A controller-lifetime monitor also scans authorized shared workspaces and known replica worktrees after tasks stop, recording net changes from external/background processes and concurrent writers without inventing an execution or author. Baseline/restart, discovery failures and incomplete scans expose coverage gaps. The dedicated Linux daemon additionally supports a fanotify collector with a private durable journal: kernel PID evidence, create/delete events for transient files, concurrent writers, and idempotent replay after controller downtime. PID namespace and boot identity are recorded; process start time is included only when verified through a live pidfd. No agent/execution identity is inferred from a PID. Collector outages, kernel overflow, unresolved paths and unsupported runtimes remain explicit gaps; the collector must remain active during controller downtime. See the kernel-journal contract in [tool permissions](../docs/tool-permissions.md#kernel-journal-in-the-dedicated-linux-daemon). See [tool permissions](../docs/tool-permissions.md#workspace-write-access-and-observations) for the authorization boundary and capture limits. Durable candidate review is delivered independently in section 5a.

**Delivered task activity:** The graph's **Activity** tab combines current graph/replica state and wait reasons with recent executions, summaries/errors, messages, approvals, human-review state and events. Execution selection uses persisted execution/message IDs, never agent-name guesses; reviews and replica progress remain explicitly graph-wide. Existing approval endpoints enforce permissions; the view offers decisions to operators/admins and environment decisions only to admins. File/diff inspection and agent controls remain linked from the flow. Independent API failures are visible and retryable; a five-second poll and event-triggered refresh recover missed updates. Windows are bounded to 100 executions, 100 messages and 300 events; this is not a new task entity, a full historical query, or a percentage-complete estimate. Unit/component integration tests and real-controller Chromium journeys cover correlation, errors, permissions, approval/rejection, results, messages and file inspection.

### 5a. Human Review with Managed or External Previews

**Status: implemented for Docker sandboxes.** Weft ships its own local-first
preview gateway, supervised HTTP processes, durable review gates and console
review actions. Browser and cURL acceptance use real containers on the dedicated
daemon. Setup and limits are in `docs/sandbox-runtime.md`; API/tool/runtime
contracts are documented alongside the existing contracts. Native process
supervision and automatic provider-specific PR/CI adapters remain later work.

The implementation uses a scoped Docker exec stdio relay to a registered
container/port, without opening sandbox ports or changing egress policy. Review
records hold a SHA-256 workspace snapshot, generation, reviewer/method policy,
decision timestamp and durable continuation outbox. Runtime cache copies are
private but are not included in the source digest. Snapshots and stopped
containers are retained; offline-controller cleanup waits for reconciliation.
Private tunnels are administrator-configured, never an external-service
requirement for local operation.

Make human review a durable workflow step tied to the exact proposed change.
The agent prepares the change, runs required checks, supplies review instructions,
and requests review. The platform persists the request and yields execution;
no model call, tool invocation or input-message lease stays open for the human's
entire review. Approval queues an idempotent continuation through insert-once delivery.
Retried business operations still require their own idempotency controls.
Reuse approval storage/decision conventions, but add a durable continuation:
`human.ask` alone is not a lifecycle manager for this workflow.

#### Review choices and evidence

- Managed preview: Weft runs the candidate and supplies a protected URL for
  browser testing or an HTTP base URL for cURL/Postman.
- External preview: attach a CI-provided environment URL and its build/revision
  evidence. Weft does not imply that it hosts or controls that environment.
- Local branch review: show repository, branch and commit so the human can
  fetch and test it independently, then record the decision in Weft.
- Review without manual execution: explicitly record that the human accepted
  based on the diff, checks or other evidence. Do not label this as a successful
  manual test. Operator policy can require a specific review method; the agent
  cannot waive it. The default product flow permits explicit acceptance without
  a preview when policy allows it.

A PR on GitHub, GitLab, Bitbucket or another provider is an optional handoff and
evidence source, not a prerequisite for review. PR creation may trigger an
external CI preview. Creating a PR, passing CI, approving a review, merging and
deploying are distinct actions with separate authorization. A provider event
may satisfy a gate only under an explicitly configured policy and after actor,
repository and exact revision verification. Start with manually attached
external links; provider-specific automation is a later increment.

Each review records graph, task/execution, requester, authorized reviewers,
candidate identity (commit plus any uncommitted-content snapshot), checks,
instructions, preview generation or external evidence, expiry, decision,
review method, actor and timestamp. Freeze the candidate used by a preview;
editing the working branch must not silently change what the reviewer sees.
New candidate content supersedes the prior review and invalidates its approval
for the new candidate. The continuation verifies the approved identity again.

#### Managed application sessions

Introduce a platform-managed service lifecycle separate from `shell.exec`.
An authorized agent requests a command/argv, workspace-relative directory,
HTTP port, readiness path and bounded lifetime under operator policy. These
are proposed API/tool inputs, not existing supported configuration. The platform
validates them and starts a supervised process using the project's prepared
environment and frozen candidate, with required approved helper services.

The first increment targets Docker environments on local or remote installations.
Native execution can use the same review contract, but a native supervisor and
local binding policy are a later backend; it must clearly report the weaker
isolation. Neither implementation may turn arbitrary user-supplied URLs, host
paths or network addresses into forwarding targets.

Track session states independently from review decisions: queued, starting,
ready, failed, expired and stopped. Expose readiness failures and bounded logs;
a process starting successfully is not proof that HTTP is ready. Unexpected
exit removes routing immediately and leaves the review pending or unavailable,
never approved. A restart creates a new session generation for the same frozen
candidate and requires readiness again.

Account for preview CPU, memory, slots and helper services in the installation's
shared capacity budget. While a preview lease is active, ordinary sandbox idle
suspension must not stop it. Leases have a maximum lifetime and bounded renewal;
expiry releases compute and revokes access while preserving the candidate and
review record. Users can restart an expired preview after capacity admission.
Cancellation, supersession and review completion revoke routing and stop owned
processes. Retain workspace and persistent test data unless separately authorized
for deletion. Cleanup failures remain visible and retain resource accounting.
Controller restarts reconcile leases, sessions and pending continuations.

#### Access from browsers and HTTP clients

The preview gateway is a component shipped and managed as part of Weft,
not an external service. It works locally by default: browser, gateway and
sandbox can all run on the user's machine, without a cloud account, third-party
tunnel, public domain or external preview service. Bind its host-facing listener
to loopback only by default, including Compose port publication. Provide local
preview addressing without requiring public DNS; test supported browsers and
HTTP clients. This does not remove network requirements of the application,
dependency downloads or the selected model provider.

Use authentication and a separate preview origin from the console. Loopback-only
HTTP is supported for the default local workflow; require TLS for remote access
unless HTTP is carried entirely through an explicitly configured secure private
tunnel. Keep local authentication enabled and use appropriate cookie settings
for each transport. Prefer a dedicated host per session so applications using
absolute paths, redirects, cookies and WebSockets do not need a URL prefix.
The URL routes only to a registered session's approved HTTP port. A scoped
Docker exec stdio relay connects the registered container loopback port to the
gateway without publishing application ports or opening general network access.
Inbound review access and outbound application egress are separate policies;
the existing egress proxy does not provide this ingress feature.

Browser access uses an authenticated exchange into a session-scoped cookie on
the preview origin. Never forward the console cookie or platform authorization
headers to the application. cURL/Postman use a short-lived, revocable credential
scoped to one preview, issued explicitly to the human through a secure UI flow;
the gateway consumes it in a dedicated header so application `Authorization`
headers remain usable. Do not put credentials in URLs, prompts, exported
collections, logs or events. Application authentication remains independent.

Enforce review/session authorization on every HTTP request and WebSocket
handshake; bound and terminate existing connections on revocation or expiry.
Define CSRF protection for browser cookie access, exact CORS origins, request
size/time limits and WebSocket limits. HTTP methods needed for API testing may
mutate isolated test data; they must not expose production credentials or data.
Provide copyable base URLs and credential-free cURL/Postman templates, plus
test-account instructions without embedding secret values.

For remote hosting, the same Weft gateway must be reachable through an administrator-
configured domain/TLS route or private tunnel. The agent cannot create public
exposure itself. A local browser must receive a reachable gateway URL, never
the sandbox's internal address or the remote server's `localhost`.

#### Gate outcomes and delivery

The console presents the candidate, check results, instructions, review options,
preview state and URL. Actions are approve, request changes, reject/cancel, and
restart/extend preview within policy. Request changes returns feedback to the
agent and requires a new review for the next candidate. Decision and continuation
creation must be atomic or use a transactional outbox; duplicate decisions and
replayed deliveries must not duplicate continuation messages; downstream effects
retain their own idempotency requirements. Review expiration
never auto-approves this gate; preview expiry alone is not review rejection.

Record session lifecycle, readiness, access denials, review decisions and
continuation outcomes with correlation IDs, bounded metrics and traces. Exclude
credentials and request/response bodies from access logs. Distinguish technical
readiness, automated test success and a human's explicit approval in the UI.

Deliver in order: durable review/candidate contract with manual external/local
evidence; Docker service supervisor and resource leases; scoped HTTP gateway
and browser/client authentication; integrated console flow and recovery tests;
then native supervision and automated SCM/CI adapters. End-to-end acceptance
requires the gateway and UI increments, not only a stored URL.

**Acceptance criterion:** On a local installation, the bundled gateway exposes
browser and cURL/Postman previews through loopback without public DNS, cloud
services or third-party tunnels; other machines cannot reach that listener.
On a remotely hosted Weft installation, a React
candidate in an isolated Docker environment is accessible in the reviewer's
browser, and an HTTP backend is testable using cURL/Postman. The graph stays
pending until an authorized decision, survives controller restart, rejects
stale approval, and releases access/compute at the end of the session. Tests
demonstrate isolation from other sessions and platform services. External CI,
local branch review and explicit acceptance without manual execution remain
available according to policy, without requiring a managed preview or a PR.

### 6. Add MCP Configuration

**Connection quotas and history:** Configurable `callsPerMinute` (0/unset is
unlimited, maximum 60,000) uses persisted CAS admission in fixed UTC-minute
windows, shared across agents and controllers using the same storage. It is
charged after approval and validation, before dispatch; admitted failures count,
refusals do not. Restart and limit edits do not reset stored usage. This is a
rate quota, not an in-flight concurrency or billing limit. Existing timeouts and
payload limits remain. The console offers a viewer-readable, newest-first MCP
history filtered by connection, graph, agent, tool, type, outcome and time, with
exclusive sequence cursors and bounded scans. Tool/approval events are projected
to safe summaries without raw arguments/results/comments. Deleted connection
history remains available. Automated coverage uses disposable stores and TLS
fixtures. `make test` (179 frontend tests), lint/build, 32 Chromium E2E cases
and targeted Go race checks passed. This increment does not deploy or modify
live connections.

**Installed GitHub acceptance (2026-10-17):** A dedicated manually registered
GitHub OAuth App completed browser consent, backend callback and encrypted
connection persistence. Discovery succeeded. An agent with only `get_me` and
`get_file_contents` grants executed both through the gateway, first with a
controlled model and then with local Ollama `llama3.2:latest` (4,096-token
context). The real-model run read the authenticated profile and public
`octocat/Hello-World` README, completed successfully and was stopped afterward.
No repository writes were performed. Private repository/issue/PR permissions,
expiry/renewal, revocation and organization/SSO restrictions remain unqualified
against the live GitHub service. Atlassian's historical uncertain DCR outcome
still requires provider evidence before another registration; GitLab and live
CIMD acceptance remain pending.

**Connection removal and session recovery:** Admin deletion requires a disabled
connection, completed local OAuth revocation when applicable, current revision
and explicit UI confirmation. It removes local configuration/catalogue/secrets,
retains a minimal reserved-ID tombstone and audit history, and leaves graph
references visible but unusable. Reusing a deleted ID is prohibited. Remote
applications/registrations are never deleted implicitly. Stale writers and OAuth
callbacks cannot restore the connection. A session-bearing HTTP 404 triggers
one reinitialization within the same operation deadline. Discovery restarts from
its first page; pre-dispatch initialization can recover. A dispatched tool call
is never replayed, even after reinitialization. Other HTTP/network failures and
broken SSE streams remain explicit failures; persistent SSE cursor resumption
is outside this increment.

**Delivered first increment:** The console registers revisioned remote HTTPS MCP
connections, protects bearer credentials, discovers bounded JSON/SSE tool catalogues,
and saves exact per-agent grants in the graph editor. Discovery uses the
2025-06-18 Streamable HTTP protocol and public network destinations only. Unit,
SQLite/real TLS integration and Chromium tests cover the supported flow.
**Gateway increment:** Remote tools are offered only to explicitly granted agents
and invoked through the shared gateway. Each call checks agent grants and current
connection state, including revalidation after approval waits. Existing per-agent
grant rate limits and approvals apply; bounded JSON/SSE calls use fresh sessions,
no replay of dispatched tool calls, redacted audit metadata and capped runtime results. Unit,
real TLS/API integration and Chromium cover success, denial and connection disable.
Private-network policy and full live
vendor-specific acceptance remain pending; service names are metadata.
See [MCP scope and limits](../docs/mcp.md).

Implement MCP connection management in the interface, starting with the integrations used by the team:

- Bitbucket;
- GitHub;
- Confluence;
- Jira.

Users must be able to add multiple connections for each service, edit, test, disable, and remove connections; inspect available tools; and grant access to specific agents by connection/tool.

Use connections and transports that can be reached securely from the Docker environment. Store tokens and credentials encrypted, hide them after saving, and allow revocation/rotation. Calls go through the Weft gateway, with per-agent authorization, call/error auditing, limits, and approvals when configured. Do not execute arbitrary user commands in the main process to start local MCP servers.

**Contract survey (2026-10-17):** [Priority vendor contracts](../docs/mcp-integrations.md)
record official endpoints, authentication, scopes and remaining compatibility
qualifications. This is documentary evidence, not live vendor acceptance.
[Additional template candidates](../docs/mcp-templates.md) cover software, data,
ML and AI engineering; they do not expand Release 1 acceptance automatically.

Deliver the next increments in this order:

1. **Versioned templates and GitHub:** reviewed endpoint/credential/profile fields,
   exact grants, github.com PAT setup and read-only validation; map selected tools
   to minimum token permissions. Add GitHub OAuth with host-owned app registration.
2. **Shared Atlassian foundation:** qualify v2 Streamable HTTP and flat paginated
   discovery with a narrowly validated `tools=all` setting. Add OAuth and encrypted
   Basic credentials alongside service-account Bearer support. Prevent dispatcher
   tools from bypassing per-operation grants; validate site context and catalogue
   bounds without silently changing network or authorization policy.
3. **Bitbucket Cloud:** organization-linked workspace and repository selection,
   read/write scope guidance, repository/PR tests and cross-workspace denials.
4. **Jira Cloud:** site/project selection, read/search/write profiles, issue/JQL and
   approved mutation tests; keep delete/manage out of initial defaults.
5. **Confluence Cloud:** site/space selection, read/search/write profiles, content
   access/restriction tests and approved updates including version conflicts.
6. **Operational completion:** token renewal/reauthorization, deletion, aggregate
   connection quotas and filterable call/approval/outcome history. For each vendor,
   require unit, real-boundary integration, browser E2E and authorized live tests.

**Delivered template/link increment:** The authenticated backend catalogue now
publishes five immutable v1 presets (DeepWiki, GitHub read-only, Context7, Hugging
Face and W&B). Browser links pin ID/version, survive login and show a review before
an administrator fills and saves the connection. The persisted reference is
validated against endpoint/service; custom endpoints require detachment. Invalid
links/versions fail explicitly. Opening a link does not create, discover or grant.
Unit/API integration and Chromium cover this increment; live vendor acceptance,
template upgrade previews and provider-specific OAuth qualification remain pending.

**Delivered generic OAuth increment:** Pre-registered public or confidential clients
can authorize through the browser, return to a configured backend callback,
complete with their original Weft admin session, discover tools and renew/revoke
credentials. PKCE, cookie/session binding, encrypted token persistence, serialized
refresh and revocation races are covered by unit/TLS integration tests and the race
detector. Chromium covers consent, renewal, explicit agent execution and revocation,
including provider failures. Distributed pending-flow
storage and real-vendor qualification remain pending. See [OAuth scope](../docs/mcp.md#browser-oauth-pre-registered-clients).

**GitHub.com OAuth compatibility:** A pinned issuer/endpoint profile supports the documented confidential POST exchange and renewal despite omitted authentication-method metadata, normalizes comma-separated scopes, and rejects OAuth errors without leaking upstream payloads. Unit, TLS API integration and browser fixtures cover this contract; live consent and public profile/file reads are now verified as recorded above; broader repository permissions and organization/SSO acceptance remain pending. Manual registration is documented in `docs/mcp.md`.

**Service setup and multiple connections:** GitHub, Atlassian Cloud MCP v2,
GitLab instance and custom profiles prepare reviewed defaults and manual client
registration guidance. Existing connection IDs remain the installation-owned
credential/authorization boundary; repeated services are supported without
per-user credentials or social login. GitLab's documented public pre-registration
uses a narrowly scoped PKCE compatibility rule. API/TLS/gateway and browser
fixtures cover concurrent pending authorizations, independent revocation and
explicit agent grants. Live Atlassian registration/corporate approval and GitLab
instance qualification remain pending; see `docs/mcp.md`. This increment does not
deploy itself or change external OAuth applications.

**Delivered automatic OAuth client setup (DCR/CIMD; fixture-qualified):** Keep
pre-registered clients as an explicit, always available alternative. Reuse an
existing registration first; otherwise prefer CIMD when advertised and usable,
then DCR when advertised, then guided manual setup. Show the selected mechanism
and reason before the administrator starts it. Opening a template or inspecting
metadata must not register an application. Registration does not replace provider
consent or explicit per-agent tool grants.

The UI now offers manual, automatic, DCR and CIMD modes and a read-only preview
before explicit confirmation. Persistent CAS claims prevent duplicate concurrent
registration and automatic replay of uncertain POST outcomes. Completed clients
survive restart; manual clients remain the default for existing connections.
CIMD publishes a minimal public client document only for prepared enabled
connections. Public CIMD (`none`) and DCR `none`/basic/post clients are supported;
expiring registration secrets, initial registration access tokens, private-key JWT
and remote client deletion remain unsupported. See [operational limits](../docs/mcp.md#automatic-client-setup-dcr-and-cimd).

Verified: unit and real TLS/filesystem tests for selection, response validation,
secret encryption, concurrency and restart; SQLite/API tests for both mechanisms
through consent, discovery, renewal and revoke; multiple-service DCR gateway tests
for exact grants and independent revocation. Chromium covers DCR review, reuse,
denial, uncertain registration/manual recovery and hosted CIMD through a real TLS
fixture proxy. `make test`, `make lint`, `make build` and `make test-e2e` passed
(171 frontend tests; 28 standard browser cases plus 1 hosted CIMD case). Live
GitLab/Atlassian registration, consent and corporate acceptance remain pending.
No production installation or external application was changed by this increment.

**Capability verification, 2026-10-17:** Public documentation and read-only metadata
GETs establish the following advertised capabilities, not end-to-end acceptance:

| Provider | Evidence | Planned behavior |
| --- | --- | --- |
| GitLab.com / chosen GitLab instance | [Official MCP guide](https://docs.gitlab.com/user/model_context_protocol/mcp_server/#reuse-a-single-oauth-application) documents DCR and administrator disablement. [GitLab.com metadata](https://gitlab.com/.well-known/oauth-authorization-server) advertises `https://gitlab.com/oauth/register`; it does not advertise CIMD. | Discover each instance independently; support DCR with `mcp` scope when enabled. Keep pre-registration when disabled or unavailable. Do not infer CIMD support from DCR or general MCP support. |
| Atlassian Cloud MCP v2 | [Resource metadata](https://mcp.atlassian.com/.well-known/oauth-protected-resource/v2/mcp) identifies the issuer `https://auth.atlassian.com/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3`. Its [authorization metadata](https://auth.atlassian.com/.well-known/oauth-authorization-server/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3) advertises `client_id_metadata_document_supported: true` and `https://auth.atlassian.com/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3/dcr/register`. | Prefer usable CIMD; offer DCR and compatible pre-registration. Qualify callback/domain/site/IP policies and real consent separately; do not substitute REST 3LO registration. |
| GitHub remote MCP | [Official host guide](https://github.com/github/github-mcp-server/blob/main/docs/host-integration.md) states that DCR is unsupported. | Preserve the existing dedicated, pre-registered application flow; do not promise automatic registration. |

The backend implements capability-driven selection following the
[MCP registration approaches](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization#client-registration-approaches),
with the supported specification revision recorded during implementation. DCR
runs on the backend against a validated advertised registration endpoint, sends
reviewed client metadata and the exact configured callback, and validates the
returned registration/authentication method before use. Persist registration
identity and encrypted credentials per installation-owned connection and issuer;
reuse after restart, serialize concurrent registration, and avoid blindly retrying
an ambiguous registration POST. Explain denied registration, invalid metadata,
rate limits and unsupported client methods without leaking provider payloads.

DCR failure diagnostics retain only locally authored categories and the provider
HTTP status, survive reloads/restarts, and do not permit automatic retries.
Historical uncertain attempts without diagnostics require external reconciliation;
HTTP localhost is supported by Atlassian's documented domain policy, subject to
organization restrictions. Neither a browser 403 nor an uncertain marker proves
whether a remote client was created. See the recovery guidance in `docs/mcp.md`.

CIMD identifies the client through a stable public HTTPS JSON document; it does
not create an application by POST. Publish only non-secret client metadata,
including an exact matching URL-form client ID and approved callback URIs derived
from `server.publicUrl`/`WEFT_PUBLIC_URL`. The provider must be able to retrieve
that document. The current `http://localhost:8080` installation cannot itself host
a usable public HTTPS CIMD document: offer DCR/manual setup and explain the
prerequisite, without opening a tunnel or changing installation configuration.
Do not silently change mechanism after a failed authorization or overwrite an
existing manual registration. Changing issuer, callback or registration mode
requires explicit review and must invalidate incompatible pending flows.

Preserve public-only transport, resource/issuer binding, PKCE S256, state/cookie
protection, encrypted token storage and independent renewal/revocation. Separate
local connection revocation from provider grant revocation and remote client
registration deletion; never delete a shared remote registration implicitly.
Report bounded registration outcomes through existing audit mechanisms without
secrets or unbounded labels. Add unit, real TLS/API/storage integration and browser
E2E coverage for both automatic paths, manual alternatives, failures, concurrency,
restart and isolation between connections. Require separately authorized live
GitLab and Atlassian registration/consent checks before claiming vendor acceptance;
no external registration was performed in this survey.

**Browser-link and OAuth journey:** A link to the user's Weft
installation opens a reviewed template in the connection screen, preserving the
pending selection through Weft login. Links contain only public template
identifiers/version and validated configuration; opening one never creates a
connection, starts a server or grants agent permissions. Support configured local
and hosted installation URLs, with a registered backend OAuth callback separate
from the frontend hash route.

After reviewing the endpoint and requested scopes, the authorized operator starts
OAuth, consents at the provider and returns to the same Weft connection flow.
The backend validates a short-lived, single-use state bound to the initiating
user/session, installation and connection, verifies PKCE/issuer/callback, exchanges
the code and stores tokens encrypted. Access/refresh tokens never enter shared
links, frontend storage, model context or logs; callback codes are redacted and
removed from the visible URL after handling. Test/discovery follows successful
authorization, then the operator explicitly assigns tools to agents. Show denied,
cancelled, expired and failed authorization without claiming a working connection.

Renew tokens server-side when the provider supports refresh, serialize concurrent
refresh and persist rotated credentials atomically. If renewal is unavailable or
fails permanently, require reauthorization without expanding grants or replaying
an ambiguous remote mutation. Revocation blocks subsequent calls locally,
invalidates pending authorization/refresh work and removes locally stored tokens;
request provider-side revocation when supported and show its result separately.
If remote revocation fails or is unavailable, explain the remaining provider-side
action. Already dispatched calls cannot be rolled back. Unit, integration and
browser E2E must cover the complete journey, callbacks/replay, renewal races and
revocation, plus opt-in live-provider validation.

The additional template backlog can reuse the registry: DeepWiki, Context7, Hugging
Face and W&B first; Sentry, dbt, LangSmith and Databricks after their authentication
and header prerequisites; BigQuery/Snowflake after protocol qualification;
Playwright/MLflow/Qdrant after isolated MCP hosting design. Discover current tools
and require explicit grants; installing a template must never authorize an agent.

**Acceptance criterion:** Users can open a template by browser link, review and connect the four priority integrations through the interface, complete OAuth authorization and return where supported, renew or reauthorize credentials, revoke access, test connections, grant tools to specific agents, and inspect calls and their results. Credential modes remain provider-specific; the complete browser journey requires automated and live-provider acceptance.

### 7. Add Skills and Steering

Use hybrid storage with separate responsibilities:

- **Application:** Installation-wide instructions, user-managed skills, and precedence/permission configuration.
- **User repository:** Project-specific steering and instructions, versioned with the code and loaded only for graphs authorized to use that workspace.

Define clear precedence: global, graph, agent, and task instructions; repository instructions belong to the project scope and cannot override security policies. Show the source and effective version/content of instructions loaded in each execution. Treat repository content as untrusted input: it may guide the agent, but must never grant tools, credentials, or permissions.

**Acceptance criterion:** Operators can manage global instructions through the application; project-specific instructions travel with the repository; every execution records the loaded instruction set; no instruction can elevate privileges.

**Delivered scope:** Admin-managed revisioned Markdown skills, explicit graph/default/project opt-ins and narrower agent selections are available in **Skills & steering**. Project sources are immediate `.weft/steering/*.md` files in the authorized workspace. Immutable execution manifests show included content, revision/hash and omission reasons. Bounded composition preserves mandatory agent/task text; graphs without opt-in retain their previous prompt semantics. This does not execute skill scripts, discover arbitrary instruction formats, grant permissions or implement delegation. Limits and verification are recorded in [instructions.md](../docs/instructions.md).

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
and output allowances while retaining the parent's model and no tools/workspace.
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

Give orchestrator agents the ability to delegate subtasks when they determine it is necessary, without requiring users to pre-draw every subagent in the graph.

**Execution audit (2026-10-18):** Native delegated executions retain their enforced
access/output bounds and approval status, actor and timestamp independently of
mutable settings. Automatic admission is identified explicitly. Runs exposes the
snapshot with objective, resolved model and parent link; historical missing data
is labelled unknown. Approval free text is not duplicated into the snapshot.
Unit, real-engine integration and approve/reject browser tests cover this slice.
Installation activation and final acceptance remain pending.

Start with **temporary child executions**, not unrestricted edits to the permanent graph. Each delegation records its parent, objective, agent/configuration used, result, state, and resource consumption.

Allow administrators to configure the delegation policy; operators decide pending approvals:

- **Approval required:** Each new delegation waits for human approval before starting.
- **Automatic:** Delegations start without individual approval, subject to configured global limits.

Set global limits for depth, number of children, concurrency, duration, and cost. Child permissions must be the same as or more restrictive than the delegating agent's permissions. Configuration and approval policy are controlled by the operator, never by the agent.

**Acceptance criterion:** An agent can delegate, receive the result, and continue; the interface displays the execution tree and can stop executions; approval and limits are enforced by the server.

### 9. Prepare Local Docker Operation

Review deployment and documentation so the primary workflow is:

1. start the application on local infrastructure;
2. open the console in a browser;
3. configure providers, integrations, and permissions;
4. create or open a graph;
5. send tasks/commands to agents and monitor executions, approvals, and changes through the interface.

**Acceptance criterion:** Setting up and operating a new installation does not depend on editing files on the server or submitting tasks through the CLI.

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

## Release 2 — Reusable Components

After graph creation and operation have stabilized:

- allow users to save agents and subgraphs as components;
- insert components into other graphs through the visual editor;
- define editable parameters when inserting a component;
- version components and indicate available updates;
- allow sharing according to installation permissions.

**Acceptance criterion:** Users can reuse a component across graphs without manually copying definitions or breaking existing configurations.

## Suggested Implementation Sequence

1. **Scope cleanup** and confirmation of the flows that remain.
2. **API contracts and data model** for the visual editor, workspace, providers, MCP integrations, instructions, and child executions.
3. **Visual editor**, as the central experience and a dependency for the graph model.
4. **Containerized project environments**, on a Docker daemon dedicated to Weft, so agents run the project's toolchain without touching the host.
5. **File and interaction panel**, read-only for users, with tasks and changes tracked by execution.
   Follow with **durable human review and preview sessions** (section 5a):
   candidate identity, supervised services, protected HTTP access and resumable gates.
6. **Web configuration for models and MCPs**, with protected credentials and Bitbucket, GitHub, Confluence, and Jira integrations.
7. **Skills and steering**, with hybrid storage, precedence, traceability, and trust boundaries.
8. **Dynamic delegation**, with a configurable choice between individual approval and bounded automatic execution.
9. **Contextual guidance and direct navigation**; the global onboarding panel has been withdrawn.
10. **End-to-end tests** for the full interface workflow, including direct navigation, authorization, and isolation.
11. **Component library** as the second release.

Compose delivery note: development-service selection and reviewed list-form
`depends_on` groups are implemented for up to three image-based helpers per
replica. Variants are standalone (no override merging). Runtime-only helpers use
private internal networks and atomic CPU/memory/count admission; idle suspension
preserves their data. Advanced Compose fields remain explicitly unsupported; see
`docs/environments.md` for the accepted subset and lifecycle.

Installation policy delivery note: `isolation.docker.environmentPolicy` now
validates allowed preparation/execution networks and feature restrictions across
manual, repository and agent definitions, including cached builds and persisted
approvals. Dockerfile RUN obeys the preparation network. Node overrides cannot
widen permitted execution networks. Configuration takes effect after restart for
new admissions; no automatic revocation/deletion of running resources. Registry
egress restrictions remain planned; runtime HTTP/HTTPS host allowlists via a
managed per-replica proxy are implemented. Setup/build proxying with a separate approved host list, isolated BuildKit worker, aggregate capacity and real sandbox tests is implemented.
Installation network administration is delivered in Integrations: revisioned shared settings, admin preview/confirmation, graph host usage and current-policy checks at new admission and idle resume. Existing running work requires explicit stop for revocation. Per-environment proxy process state and sampled denial diagnostics are delivered in environment/agent panels, with bounded reads and a retained preparation snapshot. Collected denials now persist for seven days/up to 1,000 per graph by default, with admin-configurable installation bounds in Integrations (1–90 days, 1–10,000 blocks, confirmation and revision checks), through background and lifecycle collection, with an independent history UI/API and explicit sampling limitations. Daemon provisioning and client-side registry authentication remain outside it. See `docs/environments.md`.

Agent environment tools delivery note: explicit grants expose own-graph
`environment.status` and `environment.propose`. The latter submits a complete
validated definition from a Docker node using the graph environment, waits for
administrator approval/build, and prepares the approved sandbox before commands
continue with the same workspace. Existing repository definitions take priority.
Timeout/restart semantics follow the turn and persisted proposal lifecycle; no
in-memory call restoration or arbitrary graph targeting is implied.

## UX Reorganization — Incremental Delivery

The reviewed three-screen prototype establishes the direction for agent configuration,
MCP connection detail and execution inspection. It is a design reference with fictional
data, not an implementation or vendor-validation claim.

Preserve graph ownership, whole-document draft/version semantics, explicit per-agent
permissions, OAuth gates, redacted history, read-only workspace inspection and API role
checks. Keep Graphs as the product entity; first-class projects/tasks and dynamic delegation
remain separate planned work. All interface copy remains in English.

1. Separate installation destinations: Connections, Models and Administration, with
   stable global navigation. Administration separates health, network policies, shared
   resources, retention and the built-in tool catalogue. Existing integration/template/
   OAuth URLs remain compatible. Mount only the selected area; navigation must not test
   providers, grant tools, or mutate settings. Delivered as the first implementation slice, with route, real-client component and disposable-backend browser coverage. Existing MCP/provider forms remain unchanged within their new destinations; installation activation is not part of this evidence.
2. Replace graph tabs in global navigation with a scalable graph selector; preserve
   deep links, history and unsaved drafts. Add a list alternative to the canvas.
   Implemented with searchable graph/agent navigation and per-graph editor sessions
   retained in memory while inactive effects pause. Drafts retain their base revision
   across server refreshes; missing graphs retain their local editor with an error.
   Unit/component integration and disposable-backend Chromium verify search, failures,
   cached selection, independent drafts, revision conflicts and viewer restrictions.
   No browser-storage draft persistence or installation activation is implied.
3. Build connection list/detail routes with Overview, Tools, Usage & history and Settings;
   show connection creation only on request, preserve manual/DCR/CIMD and uncertain states.
   Delivered with searchable/filterable connections, searchable tools, scoped history and
   saved graph grant visibility. Existing setup handlers and legacy OAuth links remain
   compatible. Unit, real-client component integration and disposable-backend Chromium
   coverage verify creation, discovery, editing, missing records, errors and role gates.
   No installation activation or additional external provider validation is implied.
4. Give agents a dedicated configuration page: General, Instructions, Tools & permissions
   and Advanced. Keep effective isolation/grants visible; review the complete graph draft
   before saving. Keep operational controls separate from editing. Delivered through the
   dedicated agent route with shared graph drafts, section/agent deep links, configured
   permission/isolation summaries and a complete changed-field review (values stay in
   their existing editors). Existing canvas and Definition editors remain compatible.
   Server validation, revision conflicts and role authorization are preserved; no
   installation activation or runtime permission changes are implied.
5. Introduce Runs list/detail navigation over existing execution records. Show results and
   actionable blockers first, with timeline, calls, messages and diagnostics on demand.
   Do not infer task completion, percentage progress or record relationships. Delivered
   as Runs list/detail routes over existing APIs with explicit recent-window limits,
   persisted execution/message links, independent error recovery and existing approval
   role gates. Results distinguish execution status from blocked tool calls; workspace
   links explicitly refer to current graph files. Full-history pagination and replay
   are outside this interface slice.
6. Consolidate typography, spacing, contextual help, focus and responsive behavior.
   The global onboarding panel has been withdrawn. Prototype/user research does not establish
   usability success: validate with developers before claiming the proposed task-time goals.

Each slice requires unit tests, real-client/component integration tests and disposable-
backend browser coverage for success, error recovery, permissions, deep links and relevant
narrow-window interactions. Installation activation is a separate approved operation.


### Installation validation of the navigation baseline

On 2026-10-17, commit b0f7670 and the preceding navigation, connection, agent and
Runs slices were activated on the local Compose installation. Only weftd was
rebuilt and recreated with --no-deps and --wait; the existing data, workspace,
journal and certificate volumes were reused. Health returned ok. The three graphs
and nine saved versions remained present; the execution history continued growing.
Authenticated browser checks covered connection/tool search, agent sections,
independent in-memory drafts, review/discard without saving, saved run results,
Models and Administration health. This updates the earlier slice-local activation
disclaimers; it does not establish usability acceptance or activate later changes.

### Visual consolidation

The remaining pages now share heading, spacing, form, card, button and focus
styles, including Skills, Approvals, Models, Administration and canvas controls.
Optional guidance uses native disclosures; security/error context stays visible.
Existing handlers, role checks and draft semantics are retained. Developer journey
validation remains pending; global onboarding has since been withdrawn. This visual slice
was subsequently activated with approval; see the evidence below.


Validation: make test, make lint, make build and make test-e2e pass, including
229 frontend tests and 46 browser scenarios. The new keyboard/disclosure and
viewer journeys use the real disposable backend. An earlier canvas reload scenario
failed to render edges once; the isolated repeat and final full suite passed with
the original assertions. No claim of resolving an intermittent canvas issue is made.


### Visual consolidation installation validation

Commit 7ef1178 was activated on the local Compose installation on 2026-10-17.
Only weftd was rebuilt and recreated; existing data/workspace/journal/certificate
volumes were reused. The service became healthy, with three graphs and nine saved
versions retained. Authenticated browser validation covered Models, contextual
keyboard help, Skills, Approvals, Administration and canvas navigation. The saved
Software Factory canvas displayed ten edges both before and after reload without
dirtying the graph. No configuration, credential or grant changes were submitted.
This is installation smoke-test evidence, not developer usability acceptance.


### Developer usability validation — historical round (2026-10-17; continuation deferred to final acceptance on 2026-10-18)

The developer-led round uses the current local installation with visual baseline
7ef1178. Automated checks and assistant-operated smoke tests remain separate
evidence. Present one task at a time before offering navigation hints; record the
developer's route, outcome, confusion and assistance required. Do not infer
completion or timing from silence or from assistant navigation.

1. Agent configuration: locate reader in MCP installation validation and identify
   its model and explicit tool grants without saving changes. First task issued;
   developer feedback pending.
2. MCP connection: assess finding the connection flow and understanding setup,
   authorization and the separate agent-grant step. Actual connection creation or
   external consent must use a reviewed target and the applicable approval gates.
   Not started.
3. Execution tracking: locate an existing execution, explain its outcome and
   distinguish it from individual tool outcomes. Assess a new execution only if
   its target and effects have been agreed. Not started.

Log confirmed friction as concrete follow-up work with affected screen, expected
behavior and validation criterion. No usability adjustments have been confirmed
yet; no journey is accepted based on this initial task alone.

**First-round feedback:** The developer described the Graphs screen as outdated and supplied a screenshot with the new navigation and graph selector. The served HTML references the activated visual build assets, matching the running container. Whether the concern is visual fidelity, graph data, or task instructions remains unconfirmed; clarification is pending. Do not classify this as a cache defect or accept the agent journey yet.


**Confirmed finding UX-01 — locating an agent from Graphs:** The developer clarified
that “outdated” meant they could not find reader. Their screenshot shows the graph
selector open on Graphs; it lists graphs, while the graph card only reports the
agent count. The first journey required assistance explaining the graph/agent
hierarchy and opening MCP installation validation. Model/grant identification is
still pending, so the journey is not accepted.

Proposed adjustment: make the route from a graph to its agents explicit in Graphs,
for example a labeled “View agents” action or an agent preview that opens the
existing workspace. Keep graph selection distinct from agent selection and avoid
implying that the graph selector searches agents. Validate the design with the
developer: they should locate a named agent and its configuration from Graphs
without a navigation hint, with unchanged permissions and draft behavior.


**Developer round follow-up:** Screenshots confirm the developer reached reader's
General and Instructions sections after navigation assistance. General shows
provider ollama-local and model llama3.2:latest. The summary reports two agent-level
grants, but the screenshots do not show the selected tools; instructions naming
tools are not evidence of authorization. Grant identification is still pending.
The Events screenshot shows graph.started and execution.started at 19:23:57 local
time, not completion or successful tool calls. The developer has reached execution
tracking; no additional execution needs to be started for this round.

**Confirmed visual finding UX-02 — Events column overlap:** In the supplied Events
screenshot, execution.instructions.loaded overlaps the adjacent reader label.
Adjust event-type column sizing or wrapping so long event types and actor links
remain legible without overlap. Validate long event names at desktop and narrow
widths while preserving access to event details. Whether Events versus Runs is
confusing remains a question for the developer, not an established finding.


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


**Planned adjustment UX-05 — automatic tool discovery after saving:** Requested
by the developer during the usability round. After successfully saving an enabled
MCP connection, automatically discover its tools when authentication is available,
including token-based and unauthenticated connections. For OAuth connections that
still require consent, show authorization as the next step and perform discovery
after authorization succeeds. Disabled connections remain inactive. Keep this flow
inside connection detail; no validation agent is required or created.

Present saving, discovery in progress, tools discovered (including an empty
catalogue), authorization required and discovery failure as distinct states. A
failed discovery must not discard the saved connection or suggest that saving
failed; show a useful error and retain Test / Refresh tools for retry and later
catalogue updates. Preserve existing endpoint validation, credential protection,
role restrictions and revision/concurrency checks.

Explicitly state after discovery: “Tools discovered. Grant access to agents before
they can use them.” Discovery must never create or broaden agent grants, including
when a refreshed catalogue contains new tools. Validate this behavior with unit,
integration and browser tests for each authentication mode, failures, disabled
connections and unchanged permissions. This is planned work, not implemented or
validated behavior.


### UX-01–UX-05 implementation evidence (2026-10-17)

The preceding developer-round notes describe observations and plans at their time;
this section records the resulting implementation, without claiming installation
activation or developer acceptance.

- UX-01: Graph card surfaces open their graph, with native button keyboard/focus
  behavior, independent action controls, and explicit per-agent Configure links.
  Existing in-memory graph drafts and permission gates are preserved.
- UX-02: Events wraps long types and actors in bounded grid tracks, stacking details
  at narrow widths. Browser geometry checks and expandable data checks pass.
- UX-03: Investigation found native runtime and TurnEnv both counted gateway calls;
  the engine then added them. Native TurnResult no longer repeats those counts.
  Runtime-local counts remain additive under the documented runtime contract.
  Runs separates recorded attempts, observed invocations and lifecycle events.
  Historical records are unchanged and their possible duplicate totals and bounded
  event windows are explained in contextual help; no migration was performed.
- UX-04: Connection checks and optional agent execution are explained separately.
  Connection agent links open Tools & permissions in the named graph, with a
  return-to-connection banner preserved through graph navigation and reload.
  Navigating does not run agents or grant tools.
- UX-05: Console save orchestrates existing authorized save and discovery APIs.
  Enabled connections discover automatically when authentication is available;
  OAuth consent still gates discovery and disabled connections are not contacted.
  Progress distinguishes save from discovery, errors preserve saved connections,
  and discovery results explicitly require agent grants. Direct API clients retain
  the separate save/refresh contract. No automatic agent or grant creation occurs.

Validation: make test passed (including Go integration and 241 frontend tests),
make lint and make build passed, and make test-e2e passed with 48 standard Chromium
scenarios plus one hosted CIMD scenario. Browser coverage exercises actual fixture
MCP protocol exchanges and gateway execution, role gates, recovery, graph draft
retention, card hit targets, keyboard activation, connection return paths and event
layout at desktop/narrow widths. Screenshots were visually inspected.

The existing sandbox journal supervision test was intermittent: the first full
run and an isolated repeat failed on its daemon-stopped marker; a diagnostic run
and final make test passed without modifying that test or daemon source. This
remains an observation to investigate, not a resolved defect. Installation
activation and developer usability revalidation remain pending. No persistent data,
installation secrets or OAuth applications were changed.


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

### Release 1 operations runbook (2026-10-18)

The [operations runbook](../docs/operations.md) now covers the full persistence and
key inventory, quiescence, isolated restore, version-fenced upgrades, rollback and
failure diagnosis. Model-provider re-sealing must not be mistaken for a complete
MCP credential migration; previous keys remain necessary. This is documentation
verified against the implementation, not a completed restore drill or deployment.

### Compose installation contract (2026-10-18)

Reviewed persistence, required credentials, restart behavior and network exposure.
Published services default to loopback; `WEFT_BIND_ADDRESS` is the explicit
remote-host override, while previews stay local. Real Compose-render tests use
synthetic credentials and no `.env`, covering network/volume/restart contracts and
missing-secret rejection. No existing installation was recreated. Live reachability
and clean-install acceptance remain separate from configuration validation.

### Managed MCP hosting design (2026-10-18)

[Managed MCP hosting](../docs/mcp-hosting.md) defines the isolation/lifecycle,
transport authentication, upstream credential, resource admission, private-network
and qualification contracts for candidate Playwright, MLflow and Qdrant profiles.
Only the design task is complete; implementation and vendor qualification are not
implied, and no service has been installed or launched.

### Native child grant inheritance (2026-10-18)

Installation profiles can select exact capabilities already explicitly granted to
the parent. The server copies every constraint and reuses the gateway for child
calls, including workspace restrictions, approvals, shared rate/connection limits
and attribution. The child has no independent workspace, memory, emit, recursive
or environment/review continuation capability. A bounded tool/model loop charges
parent tool limits and root-tree token reservations. Profile review and execution
audit expose the effective access. Unit/integration/browser coverage includes
out-of-path denial, ungranted writes, bounded repeat calls and per-tool approval.
No installation activation or live-provider qualification is claimed.

### Dependency health contract (2026-10-18)

Administration already exposes liveness and store/queue/sandbox readiness. The
public readiness endpoint now returns bounded states instead of raw dependency
errors that could contain credentials or internal endpoints. Unit and real HTTP
integration cover failure, redaction and recovery; sandbox failure still does not
make non-sandbox work globally unready. Provider/MCP qualification remains separate.

### Delegation entity/state contract (2026-10-18)

The [state-machine reference](../docs/state-machines.md#dynamic-delegation-entities-and-states)
now defines the existing native parent/child, approval, cancellation, result and
consumption entities without inventing new graph nodes or task records. It explains
persisted approval waits, terminal outcomes, per-tool approvals, cancellation races,
inclusive reported usage and ledger recovery. The implementation suites validate
these boundaries; external runtimes and recursion are still pending.

### Initial installation identity (2026-10-18)

The bootstrap contract now explicitly distinguishes the initial admin token,
named operator provisioning through installation tokenFile/OIDC configuration,
and provider OAuth used only for MCP access. New-install instructions refuse to
overwrite existing `.env` credentials. No browser account-management feature or
live authentication/secret change is claimed. Routine model/MCP/graph operation
continues through the console after infrastructure bootstrap.

### Bounded native recursive delegation (2026-10-18)

The default depth remains one. Administrators can review a depth of up to four;
profiles must explicitly inherit delegation.request at every generation. Native
children share the root token ledger, preserve grant constraints and consume
ancestor tool/output allowances. Tree admission remains bounded to 256 nodes and
shared capacity; exhausted capacity rejects rather than waiting on an ancestor.
Child cancellation propagates to active descendants and is shown throughout that
subtree. Unit/integration/race tests cover depth denial, exact inclusive usage,
shared-ledger settlement and cancellation during grandchild approval; browser
coverage reviews the depth and displays a completed three-generation tree.
External runtimes and a guaranteed monetary cap remain separate pending work.

### Pause admission race correction (2026-10-18)

A successful pause is consulted from durable graph state before claiming operator
input and again after waiting for queue deliveries. A wake-up no longer relies on
the asynchronous reconciler's cached state. Storage failures defer execution;
paused deliveries wait for resume or are returned on cancellation. This preserves
sandbox suspension and queued input when a message arrives immediately after pause.

### Shared pending MCP OAuth transactions (2026-10-18)

Pending browser authorizations now use encrypted shared storage with compare-and-set
updates, preserving PKCE and the original session/browser binding across controller
restart and routing. Capture and consumption are single-winner operations, with a
shared 128-entry limit and ten-minute expiry. Storage/key failures refuse progress;
expired entries are reclaimed on successful updates. A claimed exchange is never
retried automatically after interruption. Live vendor consent remains separately
pending; this delivery proves the protocol with TLS fixtures and durable storage.

### Parameterized MCP templates and reviewed upgrades (2026-10-18)

Immutable legacy templates remain available. GitHub v2 adds a bounded read-only
toolset parameter and PAT/OAuth modes; GitLab v1 adds an HTTPS instance origin and
OAuth configuration. Parameters are non-secret and validated against the pinned
version. Settings show an upgrade preview, support cancellation and apply changes
only to the draft. Explicit saving retains optimistic concurrency, endpoint-change
credential handling and per-agent grants. Unit/API integration/browser tests cover
resolution, invalid parameters, stale writes, preview cancellation and persisted
updates. Live vendor/tool permission qualification remains pending.

### Atlassian v2 flat catalogue foundation (2026-10-18)

Only the exact reviewed tools=all query is accepted on the canonical Atlassian v2
endpoint. The advertised issuer and exact or query-free resource remain validated;
OAuth requests retain the accepted resource identifier. Existing transport limits,
revision checks and explicit grants apply to the paginated flat catalogue. TLS
fixtures and browser coverage prove routing, schema discovery, catalogue drift,
stale/new-tool denial and failed discovery. Live corporate qualification remains
pending; dispatcher protection and product/site policy are separate work below.

### Atlassian operation authorization (2026-10-18)

The generic executeRead, executeWrite and executeDestructive dispatchers are
excluded from discovery results and previously saved catalogues. Exact grants
cannot resolve them, and invocation rejects retained tool instances before
credential access or transport dispatch. Identification uses either the service
family or official hostname, preventing custom labels from bypassing the guard.
Individual operations remain available with explicit grants. Unit, persistence/
gateway integration and paginated TLS/browser fixtures cover these boundaries.
Product-specific selection and site context remain separate pending work.

Full-suite validation also reproduced an environment start/stop race: a stale
reconciler graph-record write can replace the operator desired state. The MCP
unit/integration and browser checks pass; the lifecycle race is a separate
follow-up before final full-suite acceptance.

### Graph state reconciliation preserves operator commands (2026-10-18)

A full-suite run reproduced stale observed-state writes overwriting a newer stop
command. Observed-state persistence now compares the complete graph-record
snapshot atomically in filesystem and SQL storage before updating state. It never
replaces desired state, graph version or run identity. A changed snapshot is
retried by the next reconciliation; storage failures are reported without
publishing a successful state event. Old runs do not publish running state after
a stop, new version or new run. Storage conformance, focused engine tests, the
repeated environment API lifecycle and browser start/stop journeys cover this fix.

### Workspace inspection response isolation (2026-10-18)

File inspection resets local state when switching graphs and invalidates pending
content reads when changing directory/workspace or selecting another file. Late
responses, failures and loading completions cannot replace the latest selection.
Tests exercise the real API client with controlled responses; browser integration
delays an actual backend response and verifies the newer file remains visible.
The file/Git inspection routes are read-only for every role: POST, PUT, PATCH and
DELETE are rejected, and the file remains unchanged. No editor or mutation action
is offered in the inspection components. Broader graph/instruction isolation
acceptance remains tracked separately.

### Atlassian Basic credentials (2026-10-18)

Personal email/token pairs now have an explicit Basic mode, separate from Bearer
service-account credentials and OAuth. Both values are sealed with the existing
connection-bound encryption; views expose only mode/configured status. The manager
selects the transport scheme, including initialization, pagination and cleanup;
agent arguments and remote metadata cannot select it. Result/catalogue echo guards
check the encoded pair, email and token. Replacement/clear, endpoint/mode changes,
failed discovery and exact per-agent grants retain their existing boundaries.
Unit, TLS manager/storage integration and browser fixtures cover the contract.
Real corporate access and scope/site qualification remain pending.

### Typed MCP headers for dbt and standalone LangSmith (2026-10-18)

Custom connections support an encrypted standalone LangSmith API-key mode and
bounded, named metadata settings. dbt accepts numeric production/development/user
IDs with Bearer/OAuth; standalone LangSmith accepts a workspace UUID and reviewed
US/EU API endpoints. Arbitrary names, Host/protocol headers, malformed values and
unreviewed upstream URLs are rejected. Metadata changes invalidate discovery and
require token replacement/clear or renewed OAuth authorization. The UI uses typed
fields without exposing stored keys. Unit, real TLS transport/storage integration
and browser fixtures cover persistence, calls, discovery and invalid settings.
This profile does not qualify hosted LangSmith OAuth, self-hosted private URLs,
provider tool permissions, Sentry or Databricks; those remain separate work.

### Sentry MCP configuration profile (2026-10-18)

The immutable Sentry v1 template and service selector provide the public endpoint,
issuer, minimal advertised org:read scope and provider setup guidance. Saving a
connection neither registers a client nor authorizes/grants tools. Existing PKCE,
resource/issuer discovery, registration review, token sealing and revocation
flows remain unchanged; there is no Sentry compatibility bypass. Unit, API/storage
integration and browser configuration tests cover pending authorization and invalid
Bearer template use. Live consent, dispatcher/catalogue qualification and actual
per-tool scope requirements remain pending despite advertised DCR/CIMD support.

### Databricks workspace/service configuration (2026-10-18)

A versioned profile constructs the reviewed Unity Gateway MCP service path from
an HTTPS workspace origin and bounded three-part name. Typed template parameters
render URL and text fields appropriately. Invalid paths, query strings and service
identifiers fail before persistence. Bearer testing and pre-registered user OAuth
remain separate; OAuth defaults request ai-gateway/offline_access but deliberately
leave the issuer empty until supplied from workspace MCP metadata. Workspace
changes reset draft client/issuer identity. Existing encrypted credentials,
resource binding and per-agent grants are preserved. Unit, API/storage and browser
configuration tests cover valid/invalid parameters, safe persistence and pending
consent. Live workspace acceptance, service-principal renewal, downstream provider
consent and service-specific data/cost controls remain unproven or unimplemented.

### External parents requesting native delegation (2026-10-18)

Runtime Protocol callbacks now accept delegation.request from active external
parents with the exact grant, a managed model and the existing reviewed policy.
Children remain native server-managed executions; no external child launch or
runtime/model override is accepted. Parent callback inference and descendants share
the root token ledger. Approval, cancellation, grants and expiry use the same
server paths. The Python helper blocks for the result without automatic replay;
gateway usage/tool attempts are no longer duplicated in runtime-local totals.
Unit and real HTTP runtime → callback → engine → provider/storage tests cover
approval/automatic success, rejection, invalid inputs, missing grants, provider
failure, budget denial and expired tokens. Browser tests cover approval, rejection,
tree cancellation and unchanged graph/grants. External inference outside the
gateway, hard monetary guarantees and idempotent request keys remain outside scope.

### Canvas connection measurement recovery (2026-10-18)

Full browser acceptance exposed missing rendered edges after reload/reopening a
cached graph, despite preserved nodes/channels. An isolated repeated scenario
reproduced two failures in five runs. Structural node rebuilds discard React Flow
handle bounds without necessarily changing DOM dimensions. The canvas now requests
handle measurement after the structure render and when Activity restarts effects
on reveal; cleanup cancels pending frames. Graph data, selection, permissions and
saved node positions are unchanged. Component integration exercises actual rendered
paths through rebuild, hide/reveal, removal and restoration. The original browser
journey retains all assertions and uses independent IDs for repeat runs; all five
repetitions pass after the change.

### Public MCP live qualification (2026-10-18)

Opt-in live tests exercise production DNS/TLS/protocol discovery against anonymous
DeepWiki (three tools) and Hugging Face (four tools). A DeepWiki public documentation
read uses the real manager/store/gateway, proves denial without an exact grant and
success after explicit authorization. Tests never access installation credentials,
create OAuth clients, mutate repositories or execute Hugging Face Space compute.
The normal offline suite skips network checks; explicit local execution passed.
Authenticated vendor journeys and the rest of the first-wave catalogue remain
pending rather than inferred from public discovery.

### Warehouse MCP qualification boundary (2026-10-18)

Reviewed official BigQuery/Snowflake contracts against the existing stateful
transport. A public BigQuery stateless discovery probe returned nine tools with
no authentication or query invocation. Its read-query schema lacks a maximum
billed-bytes input; neither dry-run estimates nor client timeouts provide a hard
cost ceiling. Snowflake documents lifecycle/notification/version exclusions and
account-specific issuer/role/warehouse requirements; no live account was tested.
The template guide now separates documented contracts, observed public evidence
and pending transport/data/cost controls. No compatibility fallback or template
is enabled from documentation alone. Documentation-only change; no behavior or
installation changes and no new unit/integration tests required.

### Concurrent graph scope acceptance (2026-10-18)

Added a real engine/SQLite/instruction manager/filesystem gateway acceptance test
for two graphs sharing the same worker ID. Each receives its selected skill,
workspace steering, prompt, operator task and file content; runtime input and
persisted snapshot are checked for the other graph's marker. Relative traversal,
absolute sibling paths and symlink escapes are denied. Existing instruction and
filesystem unit tests cover lower-level validation/error behavior. This adds
integration evidence without changing behavior or making claims about arbitrary
host processes, Docker/external-runtime acceptance or the deferred UX walkthrough.

### Delegation contract reconciliation (2026-10-18)

Removed stale statements that recursion/external parents were unimplemented and
explicitly separated the proposed alternate delegation API from the delivered
execution-scoped tools callback. Profile deletion, alternate fields and idempotent
request keys remain proposals. This documentation-only correction adds no API,
permission or runtime behavior; the tested delegation guide remains authoritative.

### Context7 public discovery evidence (2026-10-18)

Extended the opt-in public production-transport test to Context7, whose official
guide permits anonymous basic access. Live discovery passed with two tools and
no credentials or tool calls. Existing immutable template and permissions remain
unchanged; authenticated/private/OAuth and tool-execution acceptance are pending.

### Release 1 verification checkpoint (2026-10-18)

After the implementation and acceptance commits, `make test`, `make lint` and
`make build` passed again. Frontend coverage is 53 files / 290 tests; backend,
Python SDK/example and script suites passed. The latest full browser run passed
73 standard scenarios plus one hosted-CIMD scenario. Subsequent changes only
added Go acceptance tests and documentation; concurrent scope acceptance passed
five race-detector repetitions and the affected instructions/tools/engine suites.
Public DeepWiki/Hugging Face/Context7 discovery and a granted DeepWiki read passed
explicit opt-in checks. BigQuery public discovery was a separate protocol probe.

This is not final Release 1 acceptance. Remaining implementation includes explicit
warehouse transport/data-cost profiles, broader vendor resource constraints and
monetary admission semantics, and the consolidated clean-install journey. Live
vendor credentials/registration, approved writes, installation operations and
human UX acceptance require their separately authorized targets. Rootless remains
deferred and Release 2 remains untouched. No installation deployment or push was
performed by this work.

### Explicit stateless MCP transport (2026-10-18)

Custom connections may select the bounded 2026-07-28 stateless transport while
existing connections and pinned templates retain 2025-06-18 sessions. Requests
carry matching protocol/body metadata and method/name headers; no handshake,
session lifecycle or automatic tool replay occurs. Profile changes invalidate
discovery and OAuth authorization. Unit validation, TLS transport and real
API/storage/gateway tests cover exact grants, credential isolation, JSON/SSE,
invalid responses, upstream failures and unsupported input/header requirements.
The browser save/reload scenario passed; the two affected frontend suites passed
15 tests. The production stateless transport also discovered nine public BigQuery
tools without credentials or query execution in an opt-in live test. Warehouse data
and spending policies remain separate pending work, not implied by transport.

### Atlassian product/site read policy (2026-10-18)

Optional connection policy selects Jira/Confluence and exact site UUIDs. A finite
six-operation read allowlist requires a discovered string cloudId field; unknown,
cross-product, dispatcher, broad search, write and Bitbucket operations are excluded.
The gateway checks current policy/arguments before credential access and dispatch,
and policy changes invalidate catalogues and stale references. This is an operation
and site-argument boundary, not a JQL/project/space or returned-link data filter.
Existing connections are unchanged unless configured. Corporate live acceptance
and broader product resource constraints remain pending.

Validation for the read policy: unit tests cover invalid products/sites, finite
catalogue classification and required site schemas. Manager/store/gateway
integration verifies cross-product/site denial before dispatch, successful scoped
reads and stale-reference rejection after a policy change. Component tests cover
explicit opt-in and independent selections; browser/API acceptance proves
save/reload, invalid-site rejection and viewer denial using disposable fixtures.
No external Atlassian connection or credential was changed.

### Consolidated Release 1 browser journey (2026-10-18)

Two disposable-backend journeys cover login, MCP save/automatic discovery, exact
agent grants, repository environment review/build, native execution, child
delegation, approved file writing and read-only file/diff inspection. Required
and automatic delegation modes both pass, with independent write approval in
both modes. The fixture verifies tool-result order and rejects error results;
unit tests cover both modes, invalid order, failed results and missing input.
The test confirms that the baseline file is unchanged before approval, temporary
children do not alter the permanent graph, existing write constraints survive,
and connection reads do not return the synthetic credential.

This uses a seeded graph/model, synthetic MCP and simulated Docker. It does not
prove clean installation, provider registration, live vendor acceptance or real
sandbox isolation. Those acceptance items remain open. The preceding complete
browser checkpoint passed 75 standard scenarios plus one hosted-CIMD scenario;
the two new consolidated journeys passed separately. No installed services,
credentials or persistent workspaces were changed.

Checks for this journey commit: `make test` passed (54 frontend files / 292 tests
plus backend, SDK and script suites), `make lint` and `make build` passed, and
`go test -tags e2e ./cmd/weft-e2e` passed. Both targeted Chromium journeys passed.

### Context7 public lookup acceptance (2026-10-18)

An opt-in live integration test discovers the pinned Context7 template and invokes
`resolve-library-id` for public React documentation through the production manager
and gateway. Missing exact grants are denied before the permitted lookup. The
live test passed; response content is not logged and no credential, account,
installed connection or repository is changed. This is test-only work; existing
transport/grant units remain unchanged. `query-docs`, authenticated access and
private library qualification remain pending.

### MCP contract documentation reconciliation (2026-10-18)

The priority integration guide now identifies delivered typed credentials,
stateless selection, flat Atlassian discovery/dispatcher protection, bounded
product/site policy, quotas/history and OAuth lifecycle. A source-linked GitHub
read-permission worksheet separates underlying REST requirements from live MCP
qualification and defines positive/negative resource checks. No credential or
external application was changed; a source review is not a tested permission
matrix. Documentation-only change; `git diff --check` passed.

### Provider browser configuration and credential acceptance (2026-10-18)

A Chromium test configures a managed OpenAI-compatible provider through Models,
saves its synthetic key and executes real backend model-list/inference HTTP
requests against a disposable loopback server. It verifies blank credential
fields on edit, persistence after reload, and no key in provider reads, model
request bodies, rendered errors or browser local/session storage. An upstream
401 deliberately echoes the synthetic key; the client-facing test response
remains sanitized. No real provider, credential or installed configuration is used.

Validation: the browser journey passed, the existing 12 provider component tests
passed, provider API/storage integration passed and frontend type checking passed.
This test-only addition complements existing unit and integration coverage; it
does not prove every credential type, external runtime or log sink is isolated.

### Release 1 continuation verification checkpoint (2026-10-18)

`make test` passed with 54 frontend files / 292 tests plus backend, SDK and script
suites; `make lint` and `make build` passed. The complete `make test-e2e` run
passed 77 standard scenarios plus one hosted-CIMD scenario. The subsequently
added provider browser journey passed separately. Opt-in Context7 public lookup
and BigQuery stateless discovery also passed without credentials or paid jobs.

Release 1 is not complete. Remaining implementation includes broader vendor
resource boundaries, warehouse data/cost enforcement and a hard monetary
ceiling; integrated clean-install/real-sandbox acceptance is also open. Live
credential qualification, approved external writes and corporate consent require
authorized external targets. Human UX acceptance and rootless remain deferred.
Release 2, installation services, authentication configuration and remote Git
state were not changed. Checklist entries retain their actual partial status.

### Release 1 exact MCP argument restrictions (2026-10-18)

Optional admin-managed connection rules constrain top-level string arguments to
explicit case-sensitive values. Nonempty policies also exclude unlisted tools.
Multiple fields for a tool must all match; discovery excludes incompatible
schemas. Policy edits clear the catalogue and stale references are denied.
The gateway checks arguments before accessing credentials or dispatching a call,
and forwards the normalized checked object to avoid duplicate-key ambiguity.
Existing exact agent grants, approvals and host restrictions remain mandatory.
The editor supports independent rule addition/removal and preserves saved rules.

This is a generic request boundary, not a claim that resource identifiers prove
Jira project/Confluence space ownership, that independent lists represent paired
tuples, or that query costs are limited. External vendor validation remains for
the final acceptance phase. No installed configuration or external account changed.

Checks: `make test` passed (55 frontend files / 293 tests plus Go, Python and
script suites), `make lint` and `make build` passed. The targeted Chromium
journey passed with real backend/store/gateway and TLS MCP fixtures, covering
save/reopen, discovery filtering, denied and allowed execution, invalid policy
and viewer rejection. Unit tests cover rule validation, combined fields and
schema matching; manager/store/gateway integration covers pre-dispatch denial,
normalization, persisted policy, stale revisions and failed discovery.

### Provider management E2E isolation (2026-10-18)

The full combined browser run exposed test interference: managing providers
atomically reloads the model registry, replacing the deterministic provider
injected only into the main fixture's memory. Subsequent delegated journeys then
failed with a missing provider. The provider-management scenario now runs in a
fresh disposable backend through its own Playwright configuration, chained into
the mandatory `test:e2e` script. Its assertions are unchanged; it is not skipped
or weakened. E2E type checking includes the new directory/configuration. No
production registry behavior or installation configuration changes are needed.

Verification after isolation: the complete `make test-e2e` command passed
78 main scenarios, one hosted-CIMD scenario and one provider-management scenario.
The registry replacement unit tests and provider API/storage integration passed
again. The provider browser test was moved unchanged, preserving every assertion.
The earlier failed combined run is superseded by this complete successful run;
no security check, test assertion or production authorization was relaxed.

### FLOW-03a — Durable task drafts (2026-10-19)

The first FLOW-03 increment implements request drafts and requirement previews,
not execution admission. Settings CAS persists explicit graph versions/entry
agents, pagination and audit intent without duplicating the scheduler. The browser
supports save/reload/edit/review and denies viewer writes. SQLite/filesystem
reopen tests, API tests, component tests and four browser cases passed. Full
make test/lint/build checks passed (300 frontend tests); no installed deployment
was performed. Independent task execution, launch deduplication/reconciliation,
channel bindings and standalone work remain pending; FLOW-03 is not complete.

### Task execution admission evidence — 2026-10-19

FLOW-03b now starts an explicitly reviewed, version-pinned execution copy of a
workflow rather than serializing tasks against its mutable source graph. This
reuses the engine's graph-scoped memory, queues, children and managed workspace.
The UI explains the copy and whole-workflow start; original graphs and grants are
preserved. Admission is CAS-protected, repeated start never redispatches, uncertain
preparation remains inspectable, and task controls cannot stop unrelated graphs.
Progress derives from owned executions and unsettled work; generated summaries
remain distinct from tool outcomes. Required Go/Python/frontend tests, lint/build
and the 83-case main browser suite passed. Hosted OAuth/provider browser results
are recorded separately. This is an incremental FLOW-03 delivery, not completion
of all task/project flows or standalone readiness. See docs/task-workflows.md for
current restrictions. The existing installation was not changed.

### Project/worktree foundation evidence — 2026-10-19

FLOW-03c connects explicit project registration to task review and admission.
Repository paths are resolved on the server, full base commits are verified, and
unique task branches/worktrees preserve source dirty files. Two-worktree isolation,
SQLite reopen, API role/error cases and the browser journey passed. All unit and
integration suites, lint and build passed (303 frontend tests); six focused browser
cases passed. Native-only UX remains gated by the forthcoming installation work;
container mounts are never described as host execution. Project editing/deletion,
environment preferences and automatic editor handoff remain pending.
The preceding execution-copy increment additionally passed hosted CIMD and isolated
provider-management browser suites. No existing installation/data was changed.

### Structured task ingress evidence — 2026-10-19

FLOW-03d adds reviewed external queue/type/JSON binding while preserving existing
message approval gates and no-replay task admission. Progress distinguishes retained
consumerless output from queued work and shows rejected/expired message gates as
failure after settlement. Runtime preflight errors remain blocking while ordinary
graph warnings stay informational. Unit/engine/API tests, five browser cases,
`CI=true make test`, lint and build passed. Software Factory payload compatibility
is documented; no external runtime or provider validation is inferred from it.

### Standalone dependency audit — 2026-10-19

LOCAL-01 is documented in docs/standalone.md: reuse SQLite and filesystem queues,
package UI assets with the native server, require Git only for repository work,
and keep inference/external runtimes/sandbox infrastructure explicit and optional.
macOS arm64 is the initial qualification target; other target builds need their
own smoke evidence. Native initialization, locking, occupied-port handling,
operational recovery and stdio MCP remain implementation tasks. This was a read-only
architecture audit alongside Phase A, not installation activation or completion.

### Initial native test baseline — 2026-10-19

LOCAL-02 now packages the host binary and UI, initializes a separate private
SQLite/filesystem-queue profile, and starts/stops it in the foreground without
Docker. Startup locks the data directory and reserves listeners before starting
agents. Configuration does not silently import another installation's environment.
The native production-binary browser journey proved login, repository registration,
reviewed task/worktree execution, reload and file inspection with a synthetic echo
runtime. Tests, lint, standard/native builds and browser regressions all passed
(303 frontend tests, Go/Python, 85 main browser cases plus hosted CIMD/provider and
native cases). The user's first native test can now use docs/standalone.md.
This baseline was delivered after the core Phase A task/project/channel flow;
advanced Phase A items and LOCAL-03–07 remain explicitly open. Neither full roadmap
completion nor real-provider/stdio qualification is claimed. No current installation
or persistent user data was changed, and no push or release was performed.


### Native offline recovery delivery (2026-10-19)

LOCAL-03a adds explicit stopped-profile backup and non-overwriting restore at the
original absolute path. Archives include private credentials and must be protected;
external source Git repositories remain a separate backup obligation. Unit and
native production HTTP integration evidence covers queue/approval/draft recovery,
archive integrity and failure preservation. This does not close cross-version
upgrade qualification or native token-reservation recovery. No existing Docker
installation or user profile is migrated or activated.


### Native editor handoff

FLOW-07a exposes the worktree path and an explicit VS Code handoff for a native
loopback session after same-computer confirmation. Server/container profiles do
not advertise host editor links. No editor installation detection is claimed;
browser automation validates the link without launching VS Code. Diff/test
aggregation and the final human review remain open.


### Synthetic first-use path

FLOW-03e supplies a deliberate example from Tasks, using only the existing echo
runtime and authorized graph/task APIs. Preparation saves records after consent;
execution still requires review. The example's scope is explicit, with no account,
model or repository dependency. This enables an immediate native plumbing test
while real-provider, issue-source and advanced environment journeys remain open.


### Task-local review evidence

FLOW-07b reuses authorized Git status/diff/history inspection inside task progress,
loaded only after the user's request. Execution links point to recorded tool calls
for test evidence. No automatic test-success, commit or push inference is made.
Native integration verifies a changed disposable worktree and unchanged source.
The final human editor check and broader workspace/test aggregation remain open.


### First native workflow — user decision, 2026-10-19

The local installation must provide Software Factory on first launch so users do
not need to create their initial graph. LOCAL-02a seeds a versioned native variant
once, stopped, and preserves saved customizations and intentional deletion.
Native agents replace the Docker example's external Python runtime. Explicit
model configuration, scoped grants and human handoff approval remain required;
Git writes, pushes and shell commands are not granted by this template. QA reports
outstanding checks without claiming synthetic evidence as real test success.
The existing Docker example remains unchanged. External model acceptance stays
at the end of the plan, separately from automated fixture coverage.


### Short first-task path

FLOW-03f adds editable goal instructions and derives unambiguous entry/channel,
literal message type and terminal-result suggestions from the chosen graph.
Ambiguous fields remain user choices. Generated JSON follows title/request edits
only until customized. The native Software Factory first-task test persists and
reloads these inputs through the production API without executing the workflow
or mutating grants. External model/vendor validation remains deferred.


### Native token recovery evidence

LOCAL-03b qualifies the existing budget reconciler through the production native
launcher and restored SQLite profile, including a second restart and a foreign
worker whose reservations must remain intact. No model calls or real billing were
used. Cross-version upgrade qualification remains distinct and open.


### Local MCP transport foundation

LOCAL-05a provides an internal session-based MCP 2025-06-18 stdio transport.
Executable and working directory must be absolute; no shell expansion or parent
environment inheritance occurs. Processes are scoped to an operation, with a
15-second deadline, bounded frames/catalogues and Unix process-group teardown.
Child stderr is discarded and environment echoes are rejected. HTTP discovery
shares the same protocol/catalogue validator. The transport itself grants no tools. LOCAL-05b adds native-only reviewed
registration, encrypted environment values, gateway authorization and browser
coverage.
Windows is rejected until process-tree teardown is qualified.


### Callback completion boundary

LOCAL-03c removes callback admission as soon as the runtime returns, before the
terminal execution is persisted or published. Deferred cleanup still covers
earlier exits. Unit and deterministic storage-boundary integration tests cover
success/failure and keep other executions authorized independently. This closes
the completion/revocation race observed by the existing external callback test.


### Reviewed native MCP registration

LOCAL-05b connects the stdio transport to native-only administrator registration,
write-only encrypted environment values and the existing discovery/gateway paths.
The interface reviews host execution before save/discovery; process edits cannot
inherit old environment credentials without explicit replacement or clearing.
Each call rechecks exact agent grants, revision, status and quota. No tools are
automatically granted and no external application configuration is imported.
Real-subprocess/storage/gateway integration and production-native browser fixtures
cover failure, repair, revocation and credential visibility. HTTP and Docker
behavior remain separate; vendor acceptance stays in the final validation round.


### Native format-1 upgrade qualification

LOCAL-03d runs an actual old/new binary journey from the initial native launcher
commit faf4644, including backup, retained graph, factory seeding, second restart
and unsupported-format rejection without persistent changes. The native lifecycle
contract is qualified for this format-1 path; unknown future migrations remain
fail-closed and require their own approved procedure. No user installation was
upgraded. The automated qualifier is `make test-native-upgrade`.


### Contextual task decisions

FLOW-06a projects pending approvals from the owned execution graph into task
progress and renders the existing approval cards in context. The inbox and
server-side decision permissions are unchanged; viewers cannot decide and
environment decisions remain administrator-only. Missing/error telemetry is
not presented as an empty approval list. Activity and diagnostics are loaded
on demand within the task page, retaining access to detailed runs and evidence.
Dependency repair and broader child/test aggregation remain separate follow-ups.


### Inline task execution evidence

FLOW-07c reuses the existing run-event and execution-tree components within the
task review page after an explicit execution selection. It preserves event bounds,
error states, token accounting caveats, interruption confirmation and roles.
Tests verify denied tool outcomes are distinct from completed executions and
that terminal-tree controls cannot restart or interrupt completed work.


### Task requirement repair before admission

FLOW-06b checks the current MCP catalogue during workflow preflight, before a
task claims its execution copy. Configuration links open separately so the saved
request remains available; returning users review requirements again and provide
fresh start consent. Server-side save and per-call gateway checks remain in place.
Real MCP/API and browser tests cover failure, repair and retained draft identity.


### Task workspace review scopes

FLOW-07d exposes prepared agent/replica workspaces from engine status in task
progress, scoped to known nodes of the owned execution graph. Users select the
shared task workspace or an agent worktree for read-only Git inspection and
explicit native editor handoff. Missing snapshots do not silently switch scope.
Task replica branches use `weft-replicas/<graph>/<node>-<replica>` to avoid
Git ref collisions with the task root branch `weft/<graph>`. Ordinary workflow
branch names and existing files are unchanged. No cleanup, commit or push occurs.


### Preparation failure visibility

FLOW-06c distinguishes a failed replica setup before any execution from a running
task. The task becomes visibly blocked, retains its data and directs users to
Activity and diagnostics. It does not infer terminal completion or replay work.
Unit, API and native browser fixtures cover the real filesystem failure boundary.


### Reviewed native task worktree cleanup

LOCAL-04a completes the registered-project worktree lifecycle with explicit
administrator preview/confirmation after task cancellation. Cleanup retains
branches and history, rejects dirty/untracked/ignored files and revalidates the
checkout binding and review fingerprint before non-force Git removal. Locked
checkouts fail safely. The user must stop independent external writers first.
Only disposable repositories were removed in automated validation.


### Explicit task environment selection

FLOW-05a / LOCAL-06a persist the requested workflow/local/sandbox choice in the
draft and validate it again at admission. Local worktree requires native launch
identity, a registered project and process isolation; sandbox requires Docker
isolation for each enabled agent and a successful bounded read-only daemon probe.
Selection never changes grants or downgrades isolation. Local work does not probe
or require Docker. Project preference persistence and task-owned environment
build preparation remain separate implementation tasks.


### Remembered project environment preference

FLOW-05b adds an explicit administrator setting on each registered project.
Optimistic concurrency preserves competing edits; project path and base commit
remain unchanged. Selecting a project suggests the saved mode for a new draft,
without rewriting existing drafts or granting execution. Native-only local
preferences and all task admission checks remain enforced server-side.


### Separate task environment build and execution admission

FLOW-05c / LOCAL-06b prepare a stopped task-owned graph and workspace after
explicit environment review, reusing the environment manager and its policy,
capacity, provenance and build checks. Durable CAS transitions separate build
intent, pending environment and execution start; build readiness never injects
the task input. A second review and confirmation are required to start, and
repeated start requests do not dispatch twice. Explicit build retry and
cancellation retain data, images, caches and diagnostics. Remote-instance builds
cannot be cancelled locally. Agent-origin approvals remain graph-bound and cannot
be transferred implicitly to a task copy. Interrupted preparation is retained
for inspection without automatic replay.

### FLOW-04 — Issue references through existing MCP grants (2026-10-19)

Task drafts now retain a bounded GitHub/Jira issue reference and explicit connection;
Jira also requires the selected site's cloud UUID. Save/review never fetch content.
Preparation adds the read request only to the task-owned entry agent, with no new
grants. Exact `issue_read`/`getJiraIssue` grants, current catalogue validation and
normal per-call gateway controls apply. UI retains unavailable grants for repair.
URL hostnames do not verify Jira cloud ownership; GitHub Enterprise URL mapping
is outside the current selector. Issue content is untrusted data. Real vendor
qualification remains in final external acceptance, separate from TLS fixtures.

### FLOW-07e — Explicit delivery evidence states (2026-10-19)

The task review now labels test, commit and push verification separately. It points
to real local history and recorded tool results without upgrading an agent's
completion claim into passing tests or remote synchronization. Native browser
qualification covers the labels while inspecting actual disposable-worktree diffs;
no remote query or mutation was introduced. Full Phase A regression and external
editor/human acceptance remain separate checks.

### LOCAL-07a — Native production-binary qualification (2026-10-19)

Eight disposable-profile Chromium journeys now qualify the standalone flow on
macOS arm64. Software Factory is the first and only seeded graph, initially stopped.
The browser configures a loopback HTTP model fixture as default; the existing factory
then processes the selected repository task, waits for its handoff approval and
retains its final result. The user does not create a graph. Independent parallel
tasks and cancellation preserve both task worktrees and the original dirty source.
MCP stdio repair, preparation failures, cleanup review and editor URI confirmation
remain covered. Native lifecycle/storage unit/integration and the format-1 upgrade
qualification also passed. The fixture model is not real-model acceptance.

Production installation data was not read, migrated or activated. Actual model/MCP
accounts, VS Code application launch/user review, optional real native sandbox and
other platform runtime checks remain explicitly pending under LOCAL-07b. The native
bundle is suitable for a separately initialized local test, not a claim that all
unrelated Release 1 backlog or external acceptance is complete.

### FLOW-08 — Final automated flow regression (2026-10-19)

Phase A implementation is complete within its documented contracts: task drafts,
workflow selection, issue references, capability-aware environments, contextual
approvals/progress and review evidence preserve existing grants and configuration.
`CI=true make test` passed (332 frontend tests plus Go/Python), as did `make lint`
and `make build`. `make test-e2e` passed 93 main journeys, one hosted CIMD case and
one isolated provider case. Eight native Chromium journeys and format-1 upgrade
qualification passed separately. A stale project-browser assertion was corrected
from the former worktree label to the delivered workspace label; data preservation
checks remained intact. No tests were skipped or security gates weakened.

The native bundle can be initialized as an independent local test profile with
Software Factory already present. Existing Docker services/data were not changed.
LOCAL-07b and final developer/vendor acceptance remain open. Other Release 1
backlog (including separately scoped provider boundaries and monetary limits) is
not silently declared complete by this task/native delivery; Release 2 is untouched.


### Native terminal feedback and browser-stream shutdown (2026-10-19)

Native startup defaults to a readable console view with the browser URL and data
location. Shutdown reports agent draining, HTTP stream closure and persistent
resource cleanup with elapsed times. Explicit JSON/text presentation remains
available without rewriting saved profiles; server defaults are unchanged.
An active browser SSE stream reproduced a shutdown timeout in the native lifecycle
integration test. Cancelling request contexts after agents drain fixes that case;
HTTP shutdown failures close remaining connections and are returned to the caller.
The user's earlier process exited before a stack sample could be collected, so its
specific blocking stack was not confirmed. No live user profile was changed.

Validation: host `go test ./...`, `go vet ./...`, `go build ./...`,
`CI=true make test-python test-web` (332 web tests), `make test-native-browser`
(eight journeys), formatting/diff checks and `make build-native` passed.
A production-binary disposable-profile smoke confirmed readiness, readable logs
and successful SIGINT shutdown with retained data. Standard `make test`,
`make lint` and `make build` were attempted but could not reach the Docker socket;
the host equivalents above were run instead. No installation was activated.


### Paired MCP resource arguments (2026-10-19)

Extend the existing exact argument policy with optional complete combinations.
An administrator can constrain a reviewed tool to specific workspace/repository
(or other string-argument) tuples without allowing cross-pairs. All independent
rules, exact agent grants, approvals and connection revision checks continue to
apply. No vendor ownership, query parsing, returned-content filtering or live
qualification is inferred from a tuple match. External provider validation stays
in the final acceptance phase; no live connection is changed by development.

Delivered as a backward-compatible optional rule with 2–8 named arguments and
1–32 complete distinct combinations. Tests cover both allowed pairs, cross-pair
and malformed-input denial, conjunction with independent rules, schema drift,
persistence, stale invocation authority, ungranted calls and discovery failure.
Two Chromium journeys verify existing rules and the new editor through real
backend/gateway execution; the first browser attempt exposed a textarea locator
issue, fixed by selecting its accessible textbox role without weakening assertions.
Host `go test ./...`, `go test -race ./internal/mcpconfig`, `go vet ./...`,
`go build ./...`, tagged E2E harness tests, TypeScript checks and
`CI=true make test-python test-web` passed (333 web tests). `make test`,
`make lint` and `make build` were attempted but the Docker socket was unavailable;
no daemon or user installation was started. External validation remains pending.


### MCP remote access diagnostics (2026-10-19)

The remaining vendor authorization work now includes safe, actionable transport
failure guidance in connection discovery and gateway tool results. Only exact
platform-authored HTTP status diagnostics select a message: 401 requests credential
review, 403 suggests checking resource/scopes/corporate policy without claiming a
specific cause, and 429 distinguishes the provider limit from Weft admission.
No upstream descriptions/challenges/redirects are echoed, no failed tool call is
replayed, and no scope or grant is automatically expanded. Existing OAuth 401
reauthorization remains intact; 403 does not revoke authorization or affect other
connections. TLS fixtures deliberately echo synthetic credential material to verify
redaction through persistence, gateway and browser boundaries. External provider
consent, scope matrices and corporate-policy qualification remain for the final
acceptance phase; this implementation changes no installed connection.

Validation passed: host `go test ./...`, `go vet ./...`, `go build ./...`,
`go test -tags e2e ./cmd/weft-e2e`, TypeScript E2E checks,
`CI=true make test-python test-web` (333 web tests), and seven Chromium access/
OAuth journeys. `make test`, `make lint` and `make build` were attempted but the
Docker socket was unavailable; the host checks above ran instead. No installation
activation or external validation was performed.


### Prompt-first New task and authorization modes — agreed, not implemented (2026-10-19)

Replace mandatory detailed task setup with a primary prompt accepting the
repository URL, requested work, optional workflow and constraints. Example:
"Implement GitHub authentication in https://github.com/example/project using
Software Factory; use a local worktree and leave changes for VS Code review;
do not commit or push." Resolve existing configuration and supported graph input/
result bindings; retain Review settings collapsed by default. Work/Configuration
remain interface groups for the same user, with existing backend permissions.

The desired flow is prompt → agent progress → review. Sending the prompt may
start work only when prerequisites and applicable prior authority are satisfied.
Reuse an existing project or import the URL, prepare a task worktree, and dispatch
to the chosen/default compatible workflow. Software Factory is the initial default
candidate; missing models/access or ambiguous bindings require contextual repair,
not guessed configuration. Preserve request/context and resume after resolution.
Repository import from New task and private Git authentication are still missing;
MCP OAuth is not automatically a Git clone credential. No shared graph mutation
or duplicate execution is implied by prompt interpretation.

Expose Ask for approval, Workspace access and Full access beside the prompt.
Full access is an explicitly reviewed task capability scope that avoids repeated
individual approvals; it does not add MCP grants/accounts or bypass mandatory
installation policy. Confirmation must state execution location, file/network
access, external actions and whether commit/push are included. It can be remembered
for a project after explicit confirmation. Mode and environment are independent;
local commands have OS-user authority, while sandbox controls continue to apply.
Workspace access must not claim containment merely from a worktree directory.
Define enforceable restrictions or explicit escalation before shipping that mode.

The same approval is visible in the task and global Approvals inbox, with one
decision/history. Approval permits resumption of an eligible blocked action;
mandatory prohibitions remain blocked and link to configuration review. Keep
all action/audit/diagnostic evidence even for preauthorized execution. Users can
reduce authorization or stop a task; define effects on pending/in-flight work
without pretending external side effects can be undone. Child agents cannot
expand the parent's permissions. A risk-based Approve for me mode is deferred.

Implementation sequence: prompt UI/resolution; managed public URL imports; private
Git authentication contract; task authorization contract/enforcement/preferences;
automatic admission and contextual continuation; integrated fixture qualification.
The detailed work is tracked in PROMPT-01–07 and AUTH-01–03 in TASKS.md. External
account/platform/user acceptance stays last; Release 2 is not included.
The canonical planned behavior is in [task workflow contracts](../docs/task-workflows.md#prompt-first-tasks-and-authorization-modes--incremental-delivery).
This update is documentation only; existing implementation evidence is preserved.


### PROMPT-01 — Prompt-first editor (2026-10-19)

New task now starts with Request. Workflow and execution environment remain
visible; Goal, editable title, project, input payload/agents and issue source are
under collapsed Review settings. The first prompt line supplies a display title
bounded to 200 UTF-8 bytes for a new draft; explicit/loaded titles are preserved.
Existing generated channel payload synchronization remains intact. Invalid hidden
required controls open their disclosure, and project-loading/save errors remain
visible without losing the prompt. Existing viewer/operator/admin checks and
explicit review/confirmation/start are unchanged. No new backend privilege or API
was added. Existing authorization policies are described, not replaced by a fake
mode selector. URLs are retained as text; interpretation, import, default workflow
resolution and automatic start remain PROMPT-02 onward / AUTH tasks.

Unit tests cover title derivation, Unicode limits and unchanged input semantics;
API-client component integration covers retained requests on dependency failure,
manual titles and no launch/grant side effects. Browser coverage exercises draft
save/reload, keyboard disclosure, narrow-screen invalid-field recovery, existing
project/channel/issue/approval/environment flows and native Software Factory.

Verification passed: 336 frontend unit/component integration tests via
`CI=true make test-web`; Python suites via `CI=true make test-python test-web`;
host `go test ./...`, `go vet ./...` and `go build ./...`; TypeScript/E2E checks;
15 Chromium task/project journeys and eight production native browser journeys.
Existing browser tests were adapted to open the new disclosure before inspecting
advanced fields, including after reload; no assertions or security checks were
removed. `make test`, `make lint` and `make build` were attempted but the Docker
socket was unavailable, so host checks were used. Formatting/diff checks passed.
No installation was activated; remote accounts and final human acceptance remain
pending. PROMPT-02 resolution is the next implementation slice.

### PROMPT-02 — Durable request interpretation (2026-10-19)

Implemented operator-only `POST /api/v1/tasks/prompt` and Resolve request in New
task. Persist the original prompt before a bounded default-model call with no tools;
retain pending/error/clarification states on the existing task draft. Strict proposal
validation accepts only goal, workflow reference and a verbatim repository URL.
An explicit selector wins, then a named workflow, then the existing compatible
Software Factory. Structural binding never guesses between entry/result agents or
message types. CAS prevents stale model responses from overwriting newer edits.

Repository URLs remain unfetched references requiring an explicitly selected
registered project; import is PROMPT-03. Manual repair uses existing draft saving;
normal requirement review handles model/MCP/access/environment repairs. Interpretation
never edits shared graphs, grants tools or starts agents. Unresolved drafts block
admission. Current project/environment/title/issue settings survive reinterpretation and
normal review revalidates compatibility; entry/result/channel bindings are rebuilt.
Only whitelisted settings accompany resolution. No generic default-workflow
preference setting was added: selector/named workflow/Software Factory is the current
precedence. Live interpretation quality remains unqualified by synthetic fixtures.

Verification: host `go test ./...`, `go vet ./...`, `go build ./...` passed;
`go test -tags e2e ./cmd/weft-e2e` passed; `CI=true make test-python test-web`
passed (339 frontend tests after the final list-state test; Python SDK, agents and scripts). TypeScript and E2E
TypeScript checks passed. Eight native production browser journeys passed via `make test-native-browser`.
Fourteen existing task browser journeys and the project/worktree browser journey
passed; the two new prompt journeys passed after correcting their accessible-role
locators. The final rerun also checks retention of the selected environment.
Run the new deterministic provider journeys with
`WEFT_E2E_PROMPT_MODEL=1 pnpm exec playwright test e2e/task-prompts.spec.ts`
from web. SQLite close/reopen, model failure/recovery, malicious/invalid proposals,
named/explicit/default selection, repository repair, CAS conflicts and unchanged
workflow/grants have automated coverage. `make test`, `make lint`, `make build`
were attempted and blocked by the unavailable Docker socket; host equivalents
were used. No installation activation, external credentials or live repository
qualification is claimed. PROMPT-03 public repository import is next.

### PROMPT-03 — Public repository snapshots (2026-10-19)

Implemented explicit public URL import in Projects and task drafts, including a
Prepare repository handoff from unresolved prompt drafts. Administrators confirm
a canonical HTTPS clone URL and HEAD/full branch/tag/commit reference; import
does not save/start a task or broaden grants. Ready projects select the pinned
commit for the existing review/start/worktree flow. Shared workflows are unchanged.

A dedicated persisted import record tracks fetching, verifying, publishing, ready,
cancellation and failures. Same canonical URL/ref reuses one snapshot, including
concurrent starts. Retry requires the current revision and creates a fresh directory;
partial attempts and user checkouts are retained. Expired attempts reconcile after
the stored deadline, including recovery of uncertain project publication without
duplicate projects. Import shuts down before storage closes.

The bounded transport validates public DNS addresses and pins the dial target,
rejects redirects and credentials, and exposes only Git upload-pack via a private
loopback bridge. Git inherits neither host configuration nor credential helpers.
Import downloads shallow objects/metadata without checkout, hooks, submodules or
LFS; managed task checkout also ignores host filters. Limits are two imports per
process, three minutes and 128 MiB incoming transfer per attempt, not a global disk
or expanded-worktree quota. Existing installation permissions are preserved.

Evidence:
- Go unit and real smart-HTTP Git/storage/API integration tests cover malformed
  URLs/refs, public address filtering, transport/header restrictions, concurrent
  reuse, capacity, audit failure, partial directories, cancellation, restart/retry,
  uncertain publication and distinct worktrees. A checkout-filter regression
  proves imported worktrees do not invoke host-configured filters.
- `go test -p 2 ./...`, `go test -race ./internal/repositoryimport
  ./internal/projectconfig`, `go vet ./...` and `go build ./...` passed on the host.
- Web TypeScript checks, all 343 Vitest tests and `pnpm build` passed.
- Chromium import/projects/task-draft journeys passed (18 tests); the final
  import-only rerun passed all three journeys: import/reuse/reviewed execution,
  persisted failure/retry, and cancellation/viewer denial. The two opt-in
  deterministic prompt-interpretation browser journeys also passed.
- `make test-native-browser` passed all eight disposable standalone journeys,
  including the preinstalled Software Factory and independent task worktrees.
- `make test-python` passed (15 SDK, 14 agent and 8 script tests).
- `make test`, `make lint` and `make build` were attempted but their Docker
  wrappers could not connect to the daemon. Initial simultaneous full suites
  hit existing environment-test timeouts; unchanged tests passed on rerun with
  reduced Go package concurrency and separate web execution.

OpenAPI, workflow/product contracts and event/metric documentation describe the
implemented boundaries. Live public hosts, corporate networking and platform
qualification remain external acceptance. No installation activation, credential
change, push or external account operation occurred. Ready HEAD reuse deliberately
does not fetch a newer branch tip; choose another explicit ref/commit. Shared-store
servers need access to the same managed project paths. Private Git authentication
is PROMPT-04; automatic admission remains PROMPT-05.

### PROMPT-04 — Explicit private Git authentication (2026-10-19)

Implemented per-attempt authentication selection in repository imports. Administrators
can provide an HTTPS username/token, or explicitly choose existing Git credential
helpers / GitHub CLI login on the native profile. Server/container profiles reject
native modes in the API as well as hiding them in the UI. No actual user credential
was inspected, copied or changed during implementation. Synthetic fixtures only.

Native Git credential lookup executes from the empty attempt directory with parent
repository discovery blocked, preserving selected host helper configuration but
removing Git routing and GH/GITHUB token environment overrides. The Git path calls
credential fill only; the GH path calls auth token for the selected hostname.
Prompting is disabled, resolution is limited to 15 seconds and stdout to 16 KiB,
stderr is discarded and failures are sanitized. A configured helper may run local
commands under the native OS user; the UI requires explicit confirmation. Weft
does not invoke login, setup-git, approve/store or erase.

Resolved credentials are attached only inside the fixed-endpoint HTTPS bridge,
never to fetch/checkout processes, Git config, URLs, persisted records or browser
storage. The bridge refuses redirects even with an injected client and ignores
cookie jars. Tokens remain in worker memory for one attempt; the form clears them
after submission and on target changes. Only authentication mode is persisted/audited.
Anonymous and authenticated URL/ref reuse keys differ; authenticated snapshots
retain the existing installation ownership model, with no per-user isolation or
credential revalidation when reusing cached files. Existing projects, partial
attempts and graph/tool permissions remain unchanged.

Tests and checks:
- `go test -p 2 ./...`, `go vet ./...` and `go build ./...` passed.
- Race tests passed for repository imports/project configuration and imports/API.
- Unit/integration coverage includes malformed selections, server denial of native
  modes, fixed-endpoint/redirect handling, real synthetic Git helper and GH CLI
  subprocesses, cancellation/output bounds, sanitized helper failures, refused
  private Git access, retry after restart, retained attempts and absence of secrets
  in settings, Git config, API responses and audit payloads.
- TypeScript checks, 345 Vitest tests and the web production build passed.
- Four Chromium import journeys passed, including private authentication refusal,
  reload, token re-entry, successful retry and server rejection of native modes.
- `make test-native-browser` passed nine standalone journeys, including explicit
  host-authentication selection without invoking credentials on selection.
- `make test-python` passed (15 SDK, 14 agent and 8 script tests).
- `make test`, `make lint` and `make build` were attempted; their Docker wrappers
  could not reach the unavailable daemon. Host Go equivalents above passed.

Docs and OpenAPI describe the implemented choices and limits. HTTPS repositories
on public DNS endpoints are supported; SSH/private network access, persistent
credential vaults, automatic refresh/revocation and push credentials are outside
this import flow. Live provider/enterprise SSO/keychain and OS-specific acceptance
remain external validation. No activation, authentication configuration changes,
remote push or real-account test occurred. PROMPT-05 automatic admission is next.

### PROMPT-05 — Conditional prompt admission (2026-10-19)

Implemented administrator-only Run task / Continue task alongside the prompt.
The action explicitly authorizes existing workflow permissions; the server saves
the original request, performs the existing bounded interpretation and reviews
effective requirements before invoking the existing task runner. No new execution
engine, permission grants, approval acceptance or implicit repository authentication
is introduced. Save draft / Resolve request and separate reviewed start remain.

Blocked submissions retain their ID/request and reload their requirement review.
Repair model/configuration, clarify the request, import/select a project or use the
existing environment preparation approval flow before continuing. Continue task
reinterprets bindings; manual bindings still use Save draft and reviewed Start task.
Runtime tool/message approvals remain in task progress and the global inbox.

A durable identity-scoped client key binds the exact input fingerprint and task ID
before model/launch side effects. Replays read current state only; changed input
conflicts. CAS protects concurrent edits and existing admission protects dispatch.
Uncertain reservations are retained and never retried automatically; inspect Tasks
before creating a distinct new submission. After repair a new key targets the same
draft/revision. Existing interrupted/preparing diagnostics remain authoritative.

Evidence:
- Unit and SQLite integration tests cover key validation, concurrent claims, one
  draft, restart replay, changed-input conflict and uncertain save failure.
- API integration covers admin-only admission, explicit confirmation, missing-model
  persistence/repair, same-ID launch, replay without another execution, unchanged
  source workflow and blocking clarification/missing repository/invalid proposals.
- `go test -p 2 ./...`, race tests for taskconfig/taskrun/API, `go vet ./...` and
  `go build ./...` passed. Focused new blocking cases also passed after addition.
- TypeScript checks, all 347 Vitest tests and `pnpm build` passed. UI tests verify
  the same key survives failed submissions and operators cannot run tasks.
- Four opt-in deterministic Chromium prompt journeys passed: existing interpretation
  and repair, direct ready-task execution/replay/reload, and clarification/continuation
  without creating another task.
- `make test-native-browser` passed all nine disposable standalone regressions,
  including Software Factory and independent task worktrees.
- `make test-python` passed (15 SDK, 14 agent and 8 script tests).
- `make test`, `make lint` and `make build` were attempted but their Docker wrappers
  could not reach the daemon; host equivalents above passed.

OpenAPI and task/product contracts describe this boundary. Live model quality,
external accounts and human acceptance remain deferred. No installation activation,
secret changes, data deletion or remote push occurred. Authorization modes remain
separate AUTH tasks; prompt submission does not implement Full access.


### AUTH-01 — Task authorization contract

Defined precedence and three modes in docs/task-authorization.md and a pure server policy evaluator. Mandatory approvals and exact grants remain ceilings; native shell is denied in Workspace access. Runtime binding and UI remain AUTH-02. Validation: `go test ./internal/taskauth` (policy success/denial/mandatory-gate cases). Documentation/pure policy only; gateway integration coverage follows with wiring in AUTH-02.


### AUTH-02 — Gateway and task consent

Added admin-only revision-fenced task consent with reviewed capability digest and
execution-copy seal. New tasks default to Ask; existing copies retain their grants.
Gateway checks consent after grants and rechecks after waiting/preparation, without
bypassing mandatory gates. UI confirms scope explicitly; Workspace rejects native
shell. Source/workflow changes invalidate consent. No installation activation.

Verified: `go test ./internal/taskauth ./internal/taskrun ./internal/tools ./internal/api ./internal/app`;
TypeScript; 21 focused Vitest tests; one Chromium task-authorization journey covering
all three modes, reload and start. Initial browser test found revision-zero parsing
in the shared parser; a strict endpoint-specific parser fixes it. One unrelated full
Vitest resource-cleanup test timed out under parallel load; focused rerun passed.


### AUTH-03 — Suggestions and live reduction

Project preferences are explicit CAS-protected suggestions, never reused consent.
Running task modes can only reduce automatic authorization; execution seals survive
reduction. Waiting approvals revalidate the consent revision, subsequent calls see
new limits and cancellation denies new calls. Existing stop cancels execution,
without a rollback claim for in-flight effects. Audit intents/automatic decisions
retain task and revision evidence without request payloads.

Verified: focused Go policy, storage/gateway and API tests; 15 focused Vitest tests;
Chromium mode/reload/start/live reduction journey including denied expansion.
Live credentials and installation activation remain deferred.


### PROMPT-06 — Task journey and decision history

Task tracking now explains the current stage and next action, prioritizes pending
decisions, distinguishes cancellation/failure/review, and suppresses stale progress
claims after refresh failure. Durable decision history includes rejected requests;
the task and global inbox share approval IDs. Existing in-page result, workspace
diffs, tool evidence, execution tree and diagnostics remain available. Mandatory
denials point to configuration, without a bypass. Stale consent is a review blocker.

Verified: taskrun/API Go suites; 22 focused UI tests; two Chromium journeys approve
and reject the same durable workflow gate and verify reload/history/result states.

### PROMPT-07 — Integrated qualification

Qualified prompt interpretation → public Git import → selected/default Software
Factory fixture → isolated task worktree → native agent tool execution → code
review. The model is deterministic, but Git smart HTTP, storage, engine, gateway,
approvals, file writes and browser/API boundaries are real. All three authorization
modes preserve original Git objects/source files and distinct worktrees. Full access
still denies tools without grants. Project suggestions are not inherited consent.
The production native profile separately exercises the preinstalled Software Factory
through its human gate; this does not qualify live model output.

Qualification also covers private authentication refusal/repair with synthetic
credentials, slow-import cancellation, reload/resume, stale scopes, denied live
expansion, durable decision identities, child grant denial, native shell consent,
storage restart, and rejection without inferred success. Findings fixed during
qualification: stale prepared-environment starts now validate revision/requirements
before creating consent; consent seals propagate serialization errors; legacy
executions are labeled accurately; environment-pending tasks can review consent;
browser selectors identify the intended confirmation when multiple checkboxes exist.
No tests or policy checks were removed or weakened.

Checks: `make test`, `make lint`, `make build` attempted and blocked by the unavailable
Docker socket. Host equivalents `go test -p 2 ./...`, `go vet ./...`, `go build ./...`
passed; `gofmt -l .` returned no paths. Race checks cover taskauth/taskconfig/taskrun,
tools and API. `go test -tags e2e ./cmd/weft-e2e` passed. Python suites passed
(15 SDK, 14 agent, 8 script tests). Web TypeScript/build, Vitest and Chromium
qualification results are recorded with the final verification below.

External validation remains deferred: live models/MCP/private repository accounts,
OS keychains/corporate restrictions, real Docker sandbox and final user acceptance.
No live installation activation, secret changes or push was performed.

Final verification: 364 Vitest tests across 73 files passed with two workers;
TypeScript and the production web build passed. Thirty Chromium journeys passed
across prompt qualification, prompt submission, authorization, task decisions/drafts,
repository imports and projects; seven affected journeys were rerun successfully
following final consent-display fixes. `make test-native-browser` passed all nine
journeys against a disposable production native profile. Initial failures were
limited to an incorrect test assumption about imported repositories being checked
out, ambiguous checkbox selectors after adding consent, and legacy tests needing
the now-required Ask-mode decision; assertions were made more explicit and passed.
OpenAPI YAML parsed successfully and `git diff --check` is clean.

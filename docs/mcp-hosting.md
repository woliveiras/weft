# Managed MCP hosting design

Status: design only. Weft currently connects to configured remote MCP servers;
this document does not add a managed-server launcher or validate a vendor image.
Playwright, MLflow and Qdrant are candidate profiles, not installed services.

## Boundary

Run each managed MCP server in a dedicated container on the existing dedicated
sandbox daemon, never as a command, shell child or plugin inside `weftd`. An
installation-owned, versioned profile defines the immutable image digest,
entrypoint, supported transport, upstream connection fields, health probe and
resource bounds. Repository/agent text cannot define arbitrary launch commands,
image references, host mounts, Docker options or new credentials.

A managed MCP deployment is separate from a graph task sandbox. It must not mount
another graph's workspace or reuse its process namespace. It can be shared by
connections only when profile version, credential ownership, network policy and
state-isolation requirements are compatible. Sharing a server does not imply
sharing client sessions, authorization, tool grants or upstream data access.

## Admission and lifecycle

1. An administrator selects a reviewed profile and supplies configuration through
   the existing secret-entry boundary. A preview shows image/version, CPU/memory,
   storage, outbound destinations, credential references and affected connections.
2. The server validates the profile and installation policy, reserves capacity
   durably and records an immutable deployment request before creating resources.
   Use a deployment ID as the idempotency key and label every owned resource.
3. Create an isolated network and bounded container with non-root execution,
   dropped capabilities, no-new-privileges, a read-only root filesystem and
   explicit temporary/storage mounts. Browser profiles need a reviewed seccomp
   and shared-memory policy; never silently enable privileged mode to make them
   run. Reserve these resources within installation-wide limits.
4. A successful authenticated protocol health check moves the deployment from
   `starting` to `ready`. Protocol failure, image failure, denied network access
   and capacity waits remain distinct, actionable states. Discovering tools does
   not grant them to an agent.
5. Connection invocations use the existing gateway, exact per-agent grants,
   approvals, quotas, timeouts and bounded history. Resolve server-side connection
   credentials per call. Reject an unhealthy/retiring generation before dispatch.
6. Updates create a new generation. Show configuration and catalogue differences;
   drain active sessions before switching references. New tools remain ungranted.
   Preserve the prior generation for rollback until it has no users.
7. Stop/restart/delete requires explicit administrator action. Reconcile resources
   by deployment ID after a controller crash before retrying creation. Never
   infer nonexistence from a timeout. Delete only owned, unreferenced generations;
   persistent upstream/server data requires a separate explicit deletion choice.

Persist states `pending`, `waiting_capacity`, `starting`, `ready`, `failed`,
`draining`, `stopped` and `deleting` with a revision, last transition, bounded error
code and resource ownership references. A failed request must release only its own
capacity reservation. Active/uncertain operations retain a reconciliable hold.

## Transport and credentials

Inbound access is private to the gateway and protected with TLS plus a
server-specific credential or mutual TLS. A container IP is not authentication.
Do not publish an unauthenticated MCP port on the host. Validate the peer and
advertised protocol metadata before exposing a connection as ready. Profiles must
qualify the actual MCP transport; a product REST endpoint is not an MCP server.
A stdio-only server needs a separately reviewed adapter container, not a new
process runner in the controller.

Upstream secrets stay encrypted at rest and are projected only to the owning
server through a protected file mount with minimum permissions. Do not put secret
values in image arguments, labels, URLs, browser responses, model prompts or
normal logs. Reject profiles that require an unsafe secret channel. Connections
retain the current installation ownership model; personal-user credential
ownership is not introduced here. Rotation drains/restarts the appropriate
server generation without changing unrelated connections.

Outbound policy explicitly lists approved upstream hosts and ports. Internal
addresses require a reviewed installation exception for that profile; do not
reuse a general private-network bypass. DNS resolution, redirects and rebinding
must pass the same destination checks. No Docker API, controller admin endpoint,
cloud instance metadata or unapproved local service is reachable from the hosted
server. Images and browser binaries are prepared by a reviewed build/pull path;
never run an arbitrary package installer on first agent invocation.

## Candidate qualification profiles

| Candidate | Isolation and authorization questions required before shipping |
|---|---|
| Playwright MCP | Pin server and browser versions; isolate browser contexts and cookie/download state; block internal targets, redirects and downloaded-file escapes; account for browser subprocesses and shared memory; do not share authenticated contexts between connections. |
| MLflow MCP | Identify and review the actual MCP adapter separately from the tracking service; restrict tracking URI and artifact-store destinations; separate experiment/model registry reads from writes; test artifact links, large results and denied projects without auto-fetching returned links. |
| Qdrant MCP | Identify and review the actual MCP adapter separately from the database; bind the allowed endpoint and collection scope; use upstream least-privilege credentials; separate search from collection/point mutations; cap result sizes and query resources. |

No profile may advertise a supported upstream scope until tested against that
server/version. Tool annotations are descriptive, not authorization. An adapter
with a generic dispatcher must expose enforceable per-operation permissions or
remain unsupported for fine-grained grants.

## Evidence required for implementation

- Unit tests: profile validation, secret-field separation, admission and state
  transitions, reference counting, generation selection and malformed metadata.
- Integration tests: real protocol adapter/container boundary, authenticated TLS,
  gateway permission denial, credential separation, cancellation, startup failure,
  concurrent admission and idempotent restart recovery.
- Browser E2E: reviewed creation, waiting/failure/retry, tool discovery without
  grants, explicit grant, read invocation, update preview and safe retirement.
- Dedicated-daemon acceptance: no host socket/process escape, no cross-connection
  state, blocked private-network access, resource enforcement and preservation of
  in-use generations/data during cleanup.

Telemetry records deployment/generation/connection correlation and bounded state,
outcome and latency. It excludes prompts, credentials, cookies, tool payloads and
upstream personal data. The UI distinguishes protocol readiness, upstream
credential validity and per-agent tool permission. A successful container start
alone cannot satisfy any of those three checks.

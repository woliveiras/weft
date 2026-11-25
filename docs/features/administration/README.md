# Administration

**Configuration → Administration** opens installation-wide settings. Sections
load independently; changing a section is not saving it. Administrative writes
require admin permission, including when ordinary configuration can be viewed
by other roles. Dirty-form protection and explicit review/save boundaries apply
to the relevant policy editors.

## System health (`#/administration/health`)

1. Open the section and select **Check services**.
2. Inspect progress, completion time, connected count and individual Control
   API, storage, queue and sandbox-runtime outcomes.
3. Investigate the failing service using its diagnostic context and the
   operations guide. These checks do not call model providers; four connected
   services do not prove a provider, repository or task is ready.

## Network policies (`#/administration/network`)

1. Load the saved source/revision and distinguish build/setup networks from
   agent networks.
2. Edit permitted modes and exact public DNS hosts. `bridge` allows unrestricted
   internet within its execution contract; `egress` restricts supported traffic
   to allowed hosts. Host lists do not accept arbitrary URLs, wildcard names,
   ports or IPs as substitutes for exact DNS names.
3. Review the environment usage/admission table and policy preview. Environment
   requested lists remain separate and must fit the installation ceiling.
4. Explicitly confirm/save the reviewed revision. Invalid inputs/conflicts
   require correction/reload, not a successful-looking fallback.
5. Inspect new admissions. Running work retains its network until the documented
   stop/re-admission boundary; suspended work is checked when it resumes.

## Shared images and caches (`#/administration/resources`)

1. **Refresh preview** to inspect Weft-registered sandbox resources.
2. Review references and eligibility, then select only the resources offered for
   cleanup. Referenced tasks/builds/containers, including stopped/suspended
   sandboxes, can keep resources ineligible.
3. Review and explicitly confirm cleanup. The server checks eligibility again;
   a changed reference can prevent deletion after preview.
4. Inspect per-resource outcomes. Workspaces/private caches are preserved by
   this flow. It is not a global Docker prune or permission to delete volumes.

## Blocked access history (`#/administration/retention`)

Load the current revision, edit retention days and maximum blocks per
environment, review the consequences and explicitly save. This is blocked-access
history retention, not universal task/event retention. Reductions can discard
records and therefore have their own review boundary. Current UI constraints
are 1–90 days and 1–10,000 blocks per environment.

## Tool catalogue (`#/administration/tools`)

Search/filter registered built-in tools and inspect capabilities/descriptions.
This is an installation inventory, not an agent permissions editor. MCP tools
are discovered through Connections; availability in either catalogue does not
grant invocation rights. Continue to the agent/workflow permissions editor to
configure explicit grants.

## Delegation (`#/administration/delegation`)

1. Load approval/limits policy and inspect its revision and current maximum
   depth. Decide whether delegation is enabled and whether approval is required.
2. Configure children/concurrency, time/token and objective/result size limits.
   Review and save the policy explicitly.
3. Review analysis profiles or open a profile creation/edit page. Profiles
   customize guidance, allowances and a subset of the parent's explicit
   capabilities; they do not select a different model or grant new access.
4. Save/enable the chosen profile. Agents still require `delegation.request` and
   applicable inherited access/depth to create children.
5. Inspect actual child executions and decisions. Disabling a profile prevents
   new requests without deleting execution history. Running children retain
   their snapshots; changed settings can invalidate waiting requests.

Delegation token accounting and reservations are not a guaranteed vendor
billing cap. Consult [delegation](../../delegation.md) before changing limits.

## Implementation and verification map

- [InstallationPages](../../../web/src/components/InstallationPages.tsx),
  [AdministrationUI](../../../web/src/components/AdministrationUI.tsx) and
  [administration UI tests](../../../web/src/components/AdministrationUI.test.tsx).
- [API routes](../../../server/internal/api/server.go),
  [environment/network guide](../../environments.md),
  [operations](../../operations.md), [delegation](../../delegation.md).

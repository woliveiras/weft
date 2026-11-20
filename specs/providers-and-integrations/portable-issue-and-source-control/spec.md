---
id: INT-009
title: "Portable Issue and Source Control Integrations"
summary: "Compose independent issue and Git providers with portable review and explicitly mapped tracker workflows."
status: in-progress
scope: [Jira Cloud, Jira Data Center, Bitbucket Cloud, Bitbucket Data Center, cross-provider delivery, portable pull request review]
risk: external writes and automatic merge
risk_domains: [authorization, secrets, external access, persistence, concurrency]
change_surfaces: [tracker adapters, SCM gateway, task configuration, event routing, console, installation YAML]
navigation: [specs/README.md, specs/providers-and-integrations/portable-issue-and-source-control/matrix.md]
documentation: [docs/atlassian-bitbucket-setup.md, docs/task-workflows.md, docs/portable-provider-rehearsal.md]
dependencies: [TASK-006, TASK-007, TASK-008, INT-005, INT-007, RUN-005, RUN-006, RUN-007, OPS-003]
authority:
  granted: [specification, implementation, local fixture verification, subagent review, local commits, personal SCRUM rehearsal issue creation, local Compose weftd update, personal Cloud integration setup and credential entry]
  withheld: [connection and secret changes outside the named personal rehearsal, unrelated live writes, merge, push, other deployment, production qualification]
---

# Portable Issue and Source Control Integrations

Allow an operator to combine an issue tracker and a Git service without making
agents or workflows depend on GitHub. Edition-specific adapters, portable review
operations/routes and optional Jira mappings now have local implementation and
fixture coverage. This specification remains in progress: local evidence does
not qualify real accounts, provider combinations or Data Center versions, and
automatic portable merge remains unavailable.

## Maintainer decisions and current boundary

- 2026-11-20: after MCP tool discovery succeeded but Jira REST rejected the same
  MCP V2 token with HTTP 401/scope failure, the maintainer authorized separating
  the credentials, tests, subagent review, local deployment and personal Jira /
  Bitbucket integration setup. This is authority for the named disposable Cloud
  rehearsal, not corporate resources or arbitrary permission expansion.

- 2026-11-20: for the personal Jira Cloud/Bitbucket Cloud rehearsal, the
  maintainer requires usable Bitbucket authentication rather than a manually
  acquired OAuth-only prerequisite. Add explicit email-plus-scoped-API-token
  authentication alongside existing Bearer connections, expose it in console
  and installation YAML, and keep credentials server-held. The maintainer also
  authorized creating rehearsal issues in the personal SCRUM project and updating
  the local Compose weftd service. This does not authorize credential extraction,
  automatic permission changes, a remote push, or running the rehearsal trigger.

- 2026-11-20: the maintainer also requires a technical account with write
  permissions restricted to the selected project for automatic Jira Cloud
  transitions. This extends the Data Center prerequisite; a local project
  preflight is not an atomic remote project fence. Existing unmapped Cloud
  comments/labels retain their current contract.

- 2026-11-20: Jira Data Center automatic writes require a technical account
  whose remote write permissions are restricted to the selected project. The
  maintainer explicitly chose this prerequisite instead of unrestricted PAT
  writes. Connections without that reviewed confirmation remain read-only;
  local preflight does not make a broad PAT project-restricted.
- 2026-11-20: the maintainer authorized sequential implementation, subagent code
  review after each batch and one local commit per important change. No target
  Data Center version is selected; begin with contract/fixture coverage and retain
  version-specific/live qualification as pending. Optional transitions must not
  gain an implicit mandatory policy.
- 2026-11-20: document the integration plan before further setup questions or
  implementation. Support both Cloud and Data Center editions rather than
  choosing one edition as the product's only target. Coverage includes Jira and
  Bitbucket; each product/edition has separate implementation and qualification.
- 2026-11-20: accommodate custom tracker workflows. The maintainer previously
  encountered a board column named `Review` instead of `In Review`; this naming
  difference must not prevent Weft from working.
- Current local boundary: Jira Cloud and Data Center provide discovered mapping
  definitions, optional runtime/PR lifecycle synchronization, recovery, API,
  console and installation YAML. Cloud adoption uses a credential-free profile
  pinned separately from its static first-party MCP connection; native DC uses
  its own connection/PAT. Legacy unmapped Cloud behavior remains intact. Required
  transition-field updates and confirmed-merge synchronization remain unavailable.
- GitHub and both Bitbucket editions provide native delivery, immutable bounded
  review evidence and durable review summaries. Internal PR facts can be routed
  through explicit route API/YAML configuration to a pinned running event
  workflow, with durable deduplication and read-only uncertain-outcome recovery.
  A separate portable reviewer example is available; the existing GitHub MCP
  pilot is not migrated or granted permissions automatically. A console option
  or successful fixture is not real-account/version qualification.
- Portable automatic merge is unavailable: no adapter has a qualified atomic
  guarantee covering both reviewed source and destination revisions. Reported
  checks do not establish required branch/review rules. Provider-specific
  evidence limits remain explicit rather than being reported as successful tests.
- RUN-005 owns bounded push, idempotent PR creation and adapter identity.
  RUN-006 owns existing polling, gateway write-back and GitHub closing references;
  RUN-007 owns readiness. This specification owns provider composition, optional
  tracker mappings and portable PR review. Existing behavior stays in force
  unless an operator explicitly adopts the portable configuration; historical
  executions, version pins and grants are not rewritten by these local batches.

## Behavior and invariants

- Source issue identity and destination repository identity are independent,
  explicit and retained with the task/execution snapshot. Identity includes the
  connection/instance, product/edition and provider resource identifiers. Equal
  issue keys or repository names on different instances are different resources.
- Credentials remain server-held. Adapters cannot turn discovery, connection
  creation or a workflow instruction into an agent grant or merge authorization.
  TASK-006 approvals, repository/branch bounds and installation policies remain
  effective. Private Data Center access must retain TLS verification and a
  reviewed network/trust configuration; no automatic insecure fallback.
- Provider capabilities are explicit. Unsupported operations fail visibly;
  Weft does not substitute another account, provider, tool or operation.
- Display names and board columns are not stable workflow identifiers. Weft
  never assumes that a column or status called `In Review` exists.
- Delivery, review, merge and tracker synchronization are distinct outcomes.
  A successful PR delivery is not a merged PR or a closed Jira issue.

## Acceptance criteria

### AC-001 — Independent tracker and repository selection

An operator can select the issue connection and destination project/SCM
connection independently, including Jira with GitHub or Bitbucket, and GitHub
Issues with Bitbucket. Reviewed configuration identifies each exact instance and
resource. Missing, inaccessible or incompatible selections produce a field-level
error without dispatch. Execution snapshots preserve the reviewed pair even if
project defaults later change. Issue keys and PR links are associated explicitly;
GitHub `Closes` references are used only when supported by the actual issue/PR
provider pairing. Other pairings retain an ordinary source issue link without
claiming automatic closure.

### AC-002 — Separate edition capabilities and credentials

Jira Cloud, Jira Data Center, Bitbucket Cloud and Bitbucket Data Center have
independently identified adapters, configuration, capability declarations and
qualification results. Cloud endpoints, cloud IDs, credentials and schemas are
not reused implicitly for Data Center. The console and YAML expose actual
availability and edition-specific requirements. A planned adapter cannot appear
as a working connection solely because its kind exists in a selector.
Repository import/clone/fetch, bounded push, PR lookup/create/comment and
connection testing are covered according to each adapter's supported contract;
RUN-005 continues to own delivery invariants.

### AC-003 — Explicit tracker workflow mappings

Optional mappings bind Weft lifecycle events (work started, PR delivered and
confirmed merge) to discovered Jira statuses/transitions in a reviewed
connection/project/issue-workflow scope. A board column is never sent as a REST
transition or assumed to represent one status. The UI displays names for people
and stores scoped provider identifiers. Discovery uses the actual issue/current
state to determine currently available transitions and required fields; a
project-level template must be validated for each applicable issue workflow.
Custom names such as `Review`, `In Review`, localized names and duplicate names
work without string aliases or automatic guessing. Renaming a status with the
same identifier does not break the mapping. No mapping means no automatic
transition and preserves the existing comment/label lifecycle. Selecting eligible
work must likewise use explicit configured status/category/JQL semantics rather
than introducing hard-coded display names.

### AC-004 — Visible, recoverable tracker synchronization

Before a mapped transition, validate current state, availability, fields and
permissions. If the issue is already in the configured target status, record an
idempotent no-op. A missing/stale mapping, unavailable transition or denied write
does not erase delivery evidence, duplicate a task/PR or silently report tracker
success. Show the source issue, intended target, failure and recovery action;
retain a bounded retryable synchronization record. A display-name difference
alone never blocks execution. Revalidate after configuration/workflow changes
and reconcile unknown write outcomes before retrying. The policy for making a
transition mandatory remains an open decision; do not invent a blocking default.

### AC-005 — Portable pull request review and guarded merge

The code-review workflow uses provider-neutral Weft operations to read PR
metadata, diff/files and checks, publish a review summary/findings and request
merge through the selected SCM connection. Preserve the pilot's strict security,
scalability, performance, accessibility and architecture criteria and explicitly
granted internet research. Publish a summary even when no findings are present;
if publishing fails, do not proceed to automatic merge. A failed/unknown check
or unsupported capability must not be reported as passing.

Automatic merge requires explicit grants/policy, an open eligible PR, required
checks/review rules and confirmation of the exact reviewed head revision at the
mutation boundary. A newer head invalidates the decision. Each adapter must
document its remote concurrency guarantee; if it cannot prevent merging an
unreviewed revision, automatic merge remains unavailable for that adapter.
Reconcile uncertain comment/merge outcomes before replay; never report a merge
based only on an accepted asynchronous request. Existing approvals remain in
force and migration does not grant new tools automatically.

### AC-006 — Provider-independent internal events

PR delivery emits the existing `scm.pull_request_opened` lifecycle event with
enough source information to resolve the exact SCM connection, provider instance,
repository, PR identity/URL and head revision. Review routing must not infer
GitHub from a URL or repository string. Duplicate delivery/events do not produce
duplicate review summaries or merges for the same reviewed revision. Durable
merge confirmation can request the explicitly configured tracker synchronization
without relying on GitHub closing syntax. External authenticated webhooks are a
separate capability and are not required for Weft-created PR review.

### AC-007 — Compatible configuration and staged adoption

Existing GitHub connections, triggers, workflows, version pins and saved tasks
remain readable and preserve their effective behavior. Console and installation
YAML describe the same provider/edition selection, capabilities and mappings;
dry-run exposes changes before apply. Adopting portable tools or transition
mappings requires an explicit reviewed change. It does not rewrite historical
executions, migrate secrets or grant permissions automatically. Failures retain
drafts and identify unsupported capabilities or scope conflicts.

### AC-008 — Independent adapter and end-to-end qualification

For each supported product/edition, record unit and integration coverage for
valid operations, invalid configuration, unauthorized/wrong-scope resources,
pagination/rate limits, dependency failures, stale revisions, retries and unknown
write outcomes. Cross-provider scenarios prove that issue write-back and Git/PR
delivery use their own connections. Fixture/local Git results and authorized
real-account results are separate; PROD-14 owns the latter's acceptance.
An end-to-end rehearsal covers issue readiness, claim, isolated work/tests,
PR delivery, review summary, guarded merge and configured tracker write-back.
Evidence identifies which editions/combinations were actually exercised.

### AC-009 — Usable Bitbucket Cloud API-token authentication

An operator can explicitly select `authType: api-token` and supply an Atlassian
account `username` (email) plus a scoped API token for Bitbucket Cloud. REST uses
Basic authentication with that email and token; Git HTTPS uses the documented
API-token username `x-bitbucket-api-token-auth` and the same token. Omitted
`authType` retains existing Bearer behavior. The console offers both modes and
installation YAML preserves the explicit choice without embedding secret values.
Other provider kinds reject API-token mode and irrelevant usernames. Invalid or
missing emails, unsupported modes, header-injection input and missing credentials
fail before any remote call. Changing an existing credential's mode or username
requires explicitly supplying a replacement token. API-token credentials also
require replacement when changing provider, repository or remote destination;
ordinary unchanged updates may preserve the encrypted token. Existing Bearer-to-
Bearer destination update semantics remain compatible with AC-007. No connection is
silently migrated and no authentication fallback is attempted after failure.

Tokens and derived Basic credentials must not appear in API views, errors,
events, diffs, process arguments, persisted plaintext or agent outputs. Both
REST and Git use the selected mode and retain existing redirect, repository,
branch, approval and TLS boundaries. Read-only connection checks report denied,
expired or insufficiently scoped credentials without claiming successful writes.
Unit and integration checks cover encrypted save/reload, API/YAML/console paths,
Git and PR delivery/review, compatibility and dependency failures; real-account
authentication remains separately qualified by the live rehearsal.

### AC-010 — Independent Jira Cloud REST authentication

An Atlassian MCP connection may explicitly configure `jiraRest: {cloudId}` and a
separate write-only `jiraRestCredential: {username, value}` using an Atlassian
email and a Jira-scoped API token. `clear: true` clears only that REST credential.
The REST secret is independently encrypted with purpose, connection and Cloud ID
bound authenticated data. API views expose the Cloud ID and a separate configured
flag, never either credential value or REST email. MCP discovery/invocation uses
only the MCP credential; typed Jira REST calls use only the explicit REST slot.
OAuth MCP may coexist with an explicit REST slot without sharing OAuth tokens.

Explicit REST configuration is limited to supported first-party Atlassian MCP
connections and the fixed Jira Cloud gateway. Before decryption, quota admission
or dispatch, its REST path must begin with exactly
`/ex/jira/<configuredCloudId>/rest/api/3/`; malformed, escaped, traversal or cross-cloud
URL path components, fragments and alternate authorities are rejected. Ordinary
encoded query parameters remain supported. Missing/malformed Cloud IDs, malformed
emails (maximum 320 bytes), header controls, oversized (over 16 KiB) or empty
replacement tokens, mixed clear/replacement and unsupported endpoint /
service/policy combinations fail without network access. Credential rotation and
metadata updates use existing connection CAS. Changing the bound Cloud ID
requires a replacement REST credential or an explicit clear. Omitted REST input
preserves a previously explicit configuration; clearing its secret leaves that
mode configured but unavailable, never reverting to MCP credential reuse.
Incompatible endpoint/service changes cannot retain the REST slot implicitly.

Configurations that have never selected explicit REST retain their existing
legacy static first-party behavior; no credential migration occurs. Once selected,
missing/cleared/corrupt credentials or REST 401/403 never fall back to the MCP
credential. A REST change advances the existing connection revision, invalidating
reviewed trigger/profile pins; discovery and transient writes retain the REST slot.
Existing redirect, TLS, quota, response bounds and credential-reflection protections
remain effective. Neither raw nor encoded REST credentials may appear in errors,
API responses, events, exports, plaintext storage, tool output or process arguments.

The console explains the two credentials and exposes replace/clear/preserve states
without returning saved secrets. MCP Ready means tool discovery only. REST access
must be qualified by a separate scoped read before trigger activation. YAML uses a
separate `jiraRestSecretRef` secret reference slot with the email supplied via
`jiraRestUsernameRef` alongside the replacement; export emits only a preserve
reference for the credential. Dry-run checks the same
validation and retained-scope rules as save. Profiles remain credential-free and
pin the existing connection revision; no automatic grant, transition or task start
is implied by configuration or a read-only connection test.

## Implementation batches

These are local delivery units, not remotely created issue records. Keep them
sequential; each requires unit/integration checks and subagent code review before
its own commit. A completed batch does not qualify the remaining capability.

| Batch | Scope | Status |
| --- | --- | --- |
| INT-009-01 | Bitbucket Cloud delivery adapter and HTTP boundary | Implemented with local HTTP/Git fixtures; reviewed |
| INT-009-02 | Bitbucket Data Center delivery adapter | Implemented with local REST 1.0/Git fixtures; reviewed |
| INT-009-03 | Edition-aware connection configuration and capabilities in console/YAML | Implemented with API, YAML and browser fixtures; reviewed |
| INT-009-04 | Portable PR read/review/guarded merge operations and internal events | Bounded PR metadata/diff/files/reported checks, durable summaries and portable delivery identity implemented with local fixtures; provider coverage limits remain explicit; merge unavailable pending atomic head-and-destination guarantee |
| INT-009-05 | Independent tracker/SCM identity and Jira Data Center tracker adapter | Trusted origins/cross-provider links and Jira Cloud scoped history implemented; native DC client, connection API/YAML, write gate and scoped operational triggers implemented; console configuration covered by browser/API fixtures |
| INT-009-06 | Discovered Jira mappings and recoverable optional transitions | Shared durable mapping/sync core with separate DC and Cloud namespaces; Cloud profiles, edition-specific discovery/mapping API/UI/YAML and optional runtime/PR lifecycle/recovery implemented with local fixtures; account/workflow/version qualification remains pending |
| INT-009-07 | Integrated cross-provider regression and code-review adoption path | Local cross-provider fixtures and separate pinned portable reviewer example implemented; review-route API/YAML, activation barriers, deduplication and recovery implemented; installed pilot migration and real-account/model/combination qualification remain separate |
| INT-009-08 | Real-account/target-version qualification under approved resources | Personal Cloud site/workspace/repository and rehearsal issues available; credentials and live execution pending |
| INT-009-09 | Explicit Bitbucket Cloud email/API-token setup | API, encrypted manager, Git/REST, YAML and console implemented; independent review and local fixtures passed; real credential qualification pending |
| INT-009-10 | Separate Jira Cloud MCP/REST credentials | Independent encrypted slots, API/YAML/console and TLS/profile fixtures implemented and reviewed; personal Cloud MCP discovery and REST transition inspection passed; complete trigger/delivery qualification pending |

## Delivery sequence

1. Inventory and document current boundaries; settle remaining contract choices
   below before implementing dependent behavior.
2. Define common adapter conformance and explicit instance/capability identity;
   preserve existing GitHub behavior.
3. Implement and fixture-test Bitbucket Cloud and Data Center separately, and
   qualify existing Jira Cloud plus a separate Jira Data Center adapter.
4. Add independently selected tracker/SCM configuration and portable review
   operations; migrate code-review through an explicitly selected agent/workflow
   version, retaining its summary and safety requirements.
5. Add optional discovered tracker mappings and recoverable synchronization.
6. Rehearse each selected real edition/combination under separately authorized
   resources, then document its qualification status. A pilot order does not
   remove either edition from product scope.

## Explicit exclusions

- Live setup, secret entry, external writes, merges or deployments without
  their applicable authorization.
- Authenticated external Jira/Git/PR webhooks, generic CI execution, GitLab
  implementation, agent-side credential access or automatic permission expansion.
- Automatic equivalence between board columns, statuses, status categories and
  transitions; editing a company's Jira workflow to fit Weft's vocabulary.

## Open decisions and assumptions

- Live pilot order, supported Data Center versions, qualification of documented
  authentication modes, approved test instances/repositories/issues and corporate
  network/CA requirements. Additional authentication flows remain separate work;
  no real credentials are required for local fixture verification.
- Whether transitions may be explicitly mandatory and, if so, which lifecycle
  actions may wait, with what operator recovery. Until decided, no mandatory
  transition behavior is authorized by this plan.
- Mapping profiles for required transition-field updates and retention/recovery
  extensions beyond the documented implementation. Current API/YAML/tool names,
  explicit revision pins and retry boundaries are implemented and documented;
  proposed extensions must not masquerade as available capabilities.
- Qualification of documented review/check capability subsets and safe atomic
  source-and-destination revision protection per edition. Required merge rules
  and automatic portable merge remain unavailable; incomplete evidence cannot
  weaken the reviewed-revision requirement.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md), [documentation evidence](./evidence.md).
- [RUN-005 delivery](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md),
  [RUN-006 triggers](../../runtime-and-delivery/issue-references-and-polling-triggers/spec.md),
  [INT-007 vendor profiles](../mcp-vendor-profiles-and-qualification/spec.md) and
  [PROD-14 live qualification](../../production-readiness/prod-14/spec.md).
- [Atlassian and Bitbucket setup](../../../docs/atlassian-bitbucket-setup.md),
  [portable provider rehearsal](../../../docs/portable-provider-rehearsal.md) and
  [separate portable reviewer example](../../../examples/portable-code-review/README.md).
- Implementation is authorized as of the maintainer decision above. Record
  batch-level tests/reviews in evidence; unexercised adapters and live
  qualification remain pending.

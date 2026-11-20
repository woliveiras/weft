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
documentation: [docs/atlassian-bitbucket-setup.md, docs/task-workflows.md]
dependencies: [TASK-006, TASK-007, TASK-008, INT-005, INT-007, RUN-005, RUN-006, RUN-007, OPS-003]
authority:
  granted: [specification, implementation, local fixture verification, subagent review, local commits]
  withheld: [live connection changes, secret changes, live writes, merge, push, deployment, production qualification]
---

# Portable Issue and Source Control Integrations

Allow an operator to combine an issue tracker and a Git service without making
agents or workflows depend on GitHub. This document records the next capability;
it does not declare that the adapters or portable review tools already exist.

## Maintainer decisions and current boundary

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
- Jira Cloud issue references, polling/readiness, comments and labels already
  have implementation paths. They require separate real-account qualification.
  Jira Data Center remains pending. Bitbucket Cloud and Data Center delivery
  now have local HTTP/Git fixture coverage (batches 01/02).
- The SCM registry now registers GitHub and both Bitbucket editions. The console's
  Bitbucket Data Center option is not evidence of adapter availability.
  The code-review pilot uses GitHub-specific MCP tools.
- RUN-005 owns bounded push, idempotent PR creation and adapter identity.
  RUN-006 owns existing polling, gateway write-back and GitHub closing references;
  RUN-007 owns readiness. This specification owns provider composition, optional
  tracker mappings and portable PR review. Existing behavior stays in force
  until the planned capability is implemented and explicitly adopted.

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

## Implementation batches

These are local delivery units, not remotely created issue records. Keep them
sequential; each requires unit/integration checks and subagent code review before
its own commit. A completed batch does not qualify the remaining capability.

| Batch | Scope | Status |
| --- | --- | --- |
| INT-009-01 | Bitbucket Cloud delivery adapter and HTTP boundary | Implemented with local HTTP/Git fixtures; reviewed |
| INT-009-02 | Bitbucket Data Center delivery adapter | Implemented with local REST 1.0/Git fixtures; reviewed |
| INT-009-03 | Edition-aware connection configuration and capabilities in console/YAML | Implemented with API, YAML and browser fixtures; reviewed |
| INT-009-04 | Portable PR read/review/guarded merge operations and internal events | PR read/diff, summaries and portable delivery identity implemented; merge unavailable pending atomic destination guarantee; routing adoption pending |
| INT-009-05 | Independent tracker/SCM identity and Jira Data Center tracker adapter | Trusted origins/cross-provider links and Jira Cloud scoped history implemented; native DC client, connection API/YAML, write gate and scoped operational triggers implemented; console configuration covered by browser/API fixtures |
| INT-009-06 | Discovered Jira mappings and recoverable optional transitions | Read-only DC issue transition metadata and API/console inspector implemented; mapping editor, Cloud adoption and synchronization recovery pending |
| INT-009-07 | Integrated cross-provider regression and code-review adoption path | Pending |
| INT-009-08 | Real-account/target-version qualification under approved resources | Pending external resources |

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

- Initial pilot order, supported Data Center versions and exact authentication
  modes; approved test instances/repositories/issues and corporate network/CA
  requirements. No credentials are needed to finish this documentation phase.
- Whether transitions may be explicitly mandatory and, if so, which lifecycle
  actions may wait, with what operator recovery. Until decided, no mandatory
  transition behavior is authorized by this plan.
- Exact API/YAML/tool field names, representation of per-issue-type workflow
  mappings/required transition fields and retry bounds/retention. Criteria above
  govern their design; proposed shapes must not masquerade as implemented APIs.
- Supported review/check/merge capability subsets and safe atomic revision
  protection per edition; unavailable guarantees disable automatic merge rather
  than weakening its reviewed-revision requirement.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md), [documentation evidence](./evidence.md).
- [RUN-005 delivery](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md),
  [RUN-006 triggers](../../runtime-and-delivery/issue-references-and-polling-triggers/spec.md),
  [INT-007 vendor profiles](../mcp-vendor-profiles-and-qualification/spec.md) and
  [PROD-14 live qualification](../../production-readiness/prod-14/spec.md).
- [Atlassian and Bitbucket setup](../../../docs/atlassian-bitbucket-setup.md).
- Implementation is authorized as of the maintainer decision above. Record
  batch-level tests/reviews in evidence; unexercised adapters and live
  qualification remain pending.

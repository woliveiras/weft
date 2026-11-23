---
id: RUN-009
title: Manual Dependency Security Remediation
summary: Read scoped dependency alerts and manually run a bounded sandbox workflow that prepares a verified security pull request.
status: in-progress
scope: [manual task dispatch, dependency security alerts, GitHub Dependabot, remediation workflow]
risk: server-held credential access and agent-generated repository changes
risk_domains: [authorization, external access, untrusted content]
reversibility: additive tool and opt-in workflow
change_surfaces: [SCM gateway, GitHub adapter, workflow example, operator documentation]
contracts: [scm.security.alerts]
review_policy: independent specification, test-oracle and implementation review
navigation: [specs/README.md, specs/runtime-and-delivery/manual-dependency-security-remediation/matrix.md]
documentation: [examples/dependency-security/README.md, docs/tool-permissions.md]
authority:
  granted: [implementation, tests, subagent review, local commits, local stack rebuild]
  withheld: [automatic task dispatch, schedule creation, credential changes, unrelated policy changes, remote push by the coding agent, alert dismissal, automatic merge]
dependencies: [TASK-006, TASK-007, ENV-005, RUN-005, INT-009]
---

# Intent

Let an operator manually start dependency security maintenance using a reviewed
SCM connection, sandbox and task budget. Installing the example must not run
remediation or create a schedule. The first supported alert source is GitHub
Dependabot; the gateway contract identifies unsupported adapters explicitly.

## Behavior and invariants

- Alert access is read-only, uses an exact SCM connection grant, and retains the
  encrypted credential on the server. Remote text is untrusted data.
- Successful discovery is a bounded, complete observation of open alerts and
  open same-repository PRs targeting the selected base. A partial or failed read
  is never an empty successful result. It is not an atomic remote snapshot.
- Alerts for the same ecosystem, manifest and package form one work group, with
  a stable connection-prefix branch. Matching open PRs are exposed so the agent
  can skip existing work. Concurrent observations are not a distributed lock;
  non-forced push conflicts must stop the run rather than overwrite work.
- Remediation, test evidence, PR delivery, review, merge and alert closure remain
  distinct outcomes. Existing gateway approvals and reviewer routes still apply.

## Acceptance criteria

- **AC-001** A granted `scm.security.alerts` call returns normalized open alerts,
  grouped by dependency location, a stable branch, and matching existing open PRs
  for an explicitly supplied base. Wrong connection grants are denied before
  dispatch. Unsupported adapters, malformed data, pagination/size limits,
  authentication, rate limiting and transport failures return an explicit error,
  with no credential reflection or partial success.
- **AC-002** The `dependency-security` example can be saved and selected for a
  manual task without a trigger or schedule. Its native sandbox agent reads
  current alerts before edits, skips existing PR groups, selects at most one
  fixable group per invocation, updates its plan, checks compatibility and uses
  the repository's package manager. No compatible fix, no alerts, discovery
  failure or failing verification ends with a factual result and no new PR.
- **AC-003** The agent records actual verification and advisory references,
  makes the smallest scoped update, commits and uses gateway delivery for the
  stable group branch. It rechecks alerts/duplicates immediately before delivery,
  never forces a push, never dismisses alerts or merges. Mandatory PR approval
  and existing code-review routing remain effective. The operator initiates
  another task for remaining groups; installing does not execute this behavior.

## Explicit exclusions

Scheduled execution, automatic alert dismissal, guaranteed vulnerability
elimination, automatic major upgrades, automatic merge, new credentials,
provider-independent advisory feeds, and production qualification. The agent's
instructions are guidance, not an additional authorization boundary; gateway
grants and task approvals enforce access. No guarantee that another actor cannot
open a PR after the last observation.

## Edge, failure, compatibility, and recovery scenarios

An unreadable alert or PR page blocks discovery. Closed/dismissed alerts are not
remediation work. Transitive fixes require inspecting the parent dependency and
runtime compatibility, not copying the first patched version into an override.
Sandbox commands require the reviewed environment and network policy; discovery
does not grant network or write access. Existing workflows are unchanged.
After a crash/uncertain delivery, read the stable branch's existing PR before
retrying; never claim that a local commit or successful test closed an alert.

## Open decisions and assumptions

- 2026-11-23: the maintainer approved implementation with manual dispatch instead
  of a schedule, following the Dependabot remediation proposal.
- Initial live target: `woliveiras/weft`, SCM `weft-github`, base `main`. These
  are reviewed example values, not implicit cross-repository authority.
- Unit and gateway/HTTP integration fixtures qualify the tool. Prompt behavior
  requires deterministic runtime integration coverage and a separately authorized
  real remediation run; model compliance is not proven by string assertions.

## Reconciliation

- [Behavior and oracle matrix](matrix.md).
- [Evidence](evidence.md).
- [Operator instructions](../../../examples/dependency-security/README.md).

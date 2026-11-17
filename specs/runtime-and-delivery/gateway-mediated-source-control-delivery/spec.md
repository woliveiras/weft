---
id: RUN-005
title: "Gateway-mediated Source Control Delivery"
summary: "Push and open pull requests through the reviewed SCM gateway with task-scoped credentials and evidence."
status: in-progress
scope: ["gateway mediated source control delivery"]
navigation: ["specs/README.md","specs/runtime-and-delivery/gateway-mediated-source-control-delivery/matrix.md"]
documentation: ["docs/task-workflows.md","docs/tool-permissions.md","docs/atlassian-bitbucket-setup.md"]
dependencies: ["TASK-006","ENV-002","ENV-004"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Gateway-mediated Source Control Delivery

Push and open pull requests through the reviewed SCM gateway with task-scoped credentials and evidence.

## Behavior and invariants

- An approved gateway push targets the task branch under the configured prefix and repository. Base branches, tags, force pushes and other refs are denied. Agents do not receive SCM credentials.

## Acceptance criteria

### AC-001 — Bounded push

An approved gateway push targets the task branch under the configured prefix and repository. Base branches, tags, force pushes and other refs are denied. Agents do not receive SCM credentials.

### AC-002 — Idempotent pull request and hand-off

After the reviewed gate, PR creation uses the task base and bounded metadata. Existing PRs are returned idempotently and the task records delivery or a visible failure.

### AC-003 — Adapter qualification

An adapter preserves repository identity and credentials across its supported Git and PR operations. A fixture or local bare remote result is recorded separately from a real account result.

## Explicit exclusions

- Automatic PR/CI review gates are ENV-004. Agent-side SCM CLIs are outside the delivery design.

## Open decisions

- SCM-04 is complete for fixtures and local push. Real GitHub validation and Bitbucket Data Center, which requires an evaluation license and authorized image acquisition, remain pending.

## History

- 2026-10-30. Phase C chose server-held credentials and gateway-only delivery after the human gate.
- 2026-11-11. SCM-01–04 and SCM-06 were recorded complete within the local/fixture scope.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [tool-permissions.md](../../../docs/tool-permissions.md), [atlassian-bitbucket-setup.md](../../../docs/atlassian-bitbucket-setup.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).

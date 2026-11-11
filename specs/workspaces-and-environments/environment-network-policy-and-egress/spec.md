---
id: ENV-009
title: "Environment Network Policy and Egress"
summary: "Enforce installation policy across environment preparation and execution and expose bounded proxy denial history."
status: implemented
scope: ["environment network policy and egress"]
navigation: ["specs/README.md","specs/workspaces-and-environments/environment-network-policy-and-egress/matrix.md"]
documentation: ["docs/environments.md","docs/security.md","docs/sandbox-runtime.md"]
dependencies: ["ENV-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Environment Network Policy and Egress

Enforce installation policy across environment preparation and execution and expose bounded proxy denial history.

## Behavior and invariants

- The effective policy checks manual, repository and agent definitions, stored approvals, cached builds and node overrides. New admission and idle resume use the applicable policy revision.
- Egress is opt-in for each phase. Omitted mode lists permit `none` and
  `bridge`; preparation defaults to `bridge` and execution to `none`.
  Preparation policy governs Dockerfile RUN and setup commands. The daemon
  firewall and registry provisioning have separate boundaries. Base images
  remain trusted installation inputs.

## Acceptance criteria

### AC-001 — Policy at admission

The effective policy checks manual, repository and agent definitions, stored approvals, cached builds and node overrides. New admission and idle resume use the applicable policy revision.

### AC-002 — Separate egress phases

Preparation and runtime use separately reviewed exact HTTP/HTTPS host lists within installation limits. Private destinations and paths bypassing the proxy are denied within the supported sandbox topology.

### AC-003 — Revisioned diagnostics and retention

Admins review revisioned network settings and see current proxy status. Collected denials persist under configured time/count limits and remain explicitly sampled.

## Explicit exclusions

- Daemon-wide registry egress and registry client authentication remain separate. Existing running resources require explicit stop for revocation.

## Open decisions

- Guaranteeing complete denial audit or expanding registry controls needs a separate decision. Dynamic host settings and static daemon restrictions have distinct rollout behavior.
- In `ad5cfd5:docs/security.md`, line 171 described egress allowlists as planned,
  and line 87 described Git hooks and fsmonitor protections as absent,
  contradicting lines 157–163. The review reconciled both descriptions.
  Daemon-wide registry controls and direct repository writes through arbitrary
  shell remain outside the implemented guarantees.

## History

- 2026-10-18 Release 1 checkpoint. The plan records policy checks, managed execution/preparation proxies, revisioned settings and bounded denial history.

## Reconciliation

- [environments.md](../../../docs/environments.md), [security.md](../../../docs/security.md), [sandbox-runtime.md](../../../docs/sandbox-runtime.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).

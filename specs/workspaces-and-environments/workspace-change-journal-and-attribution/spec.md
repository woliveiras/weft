---
id: ENV-003
title: "Workspace Change Journal and Attribution"
summary: "Record bounded change evidence, preserve honest attribution and recover the Linux workspace journal."
status: in-progress
scope: ["workspace change journal and attribution"]
navigation: ["specs/README.md","specs/workspaces-and-environments/workspace-change-journal-and-attribution/matrix.md"]
documentation: ["docs/operations.md","docs/tool-permissions.md","docs/events.md"]
dependencies: ["ENV-002","ENV-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Workspace Change Journal and Attribution

Record bounded change evidence, preserve honest attribution and recover the Linux workspace journal.

## Behavior and invariants

- File, Git-write and shell calls record bounded before/after change evidence with their execution identity, including failed calls. Incomplete scans report gaps.

## Acceptance criteria

### AC-001 — Bounded tool observations

File, Git-write and shell calls record bounded before/after change evidence with their execution identity, including failed calls. Incomplete scans report gaps.

### AC-002 — Continuous monitoring

Authorized shared workspaces and replica worktrees report net changes from concurrent or background writers beyond tool boundaries. Unproven author and execution remain unknown.

### AC-003 — Kernel observations and gaps

The dedicated Linux collector records transient operations and supported process evidence, including verified start time only where available. Unsupported namespaces, unresolved paths and overflow remain explicit gaps.

### AC-004 — Durable rotation and replay

A collector that stays active during controller downtime can replay its private segment chain idempotently. Rotation retains cursor continuity and replay reports unavailable required segments.

### AC-005 — Cursor-gated retention

Approved pruning removes only fully imported archives behind a verified anchor. Retries are safe and a store restored behind the anchor is refused.
If pruning changes the anchor between the importer's initial read and
validation, the importer retries once with a fresh anchor and durable cursor.
An unchanged invalid anchor or a second change stops the pass. The anchor
checks consistency with the cursor and segment chain, without authenticating
the volume's contents.

### AC-006 — Capacity and collector monitoring

Installed metrics expose journal capacity, write/rotation failures and collector progress. The corresponding alerts can evaluate without importer samples where their inputs are collector-side.
Monitoring unavailability covers scrape or filesystem metric failure.
Missing or stale collector heartbeat uses the collector alert.

## Explicit exclusions

- The journal does not infer agent/execution identity from PID alone or recover events while the collector is offline. Production filesystem migration, timed restore and alert ownership are PROD-08, PROD-09 and PROD-10.

## Open decisions

- Exact attribution for arbitrary processes and coverage for unsupported runtimes/PID namespaces remain unresolved. A distinct workspace filesystem is a production topology decision.

## History

- 2026-11-11. Local follow-ups added coalesced unresolved paths, 64 MiB rotation, archived-cursor recovery, verified pruning and collector/capacity alerts.

## Reconciliation

- [operations.md](../../../docs/operations.md), [tool-permissions.md](../../../docs/tool-permissions.md), [events.md](../../../docs/events.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).

# 0002 — Reconciler with replica leases instead of a central scheduler

**Status:** Accepted

## Context
Replicas must survive process restarts and instance failures, several
instances must share the fleet without double-running a replica, and we
want no extra coordination service (etcd, ZooKeeper, Temporal) to operate.

## Decision
Desired state (graph desired state, node overrides, replica counts) lives in
the store. Every instance runs the same reconcile loop: for each replica that
should exist it calls `AcquireLease(graph/node#n, instance, ttl)`; the holder
runs the replica and renews the lease every reconcile interval; replicas that
should not exist are stopped gracefully and their leases released. A new
holder marks the previous holder's in-flight executions `interrupted`; the
broker redelivers their unacknowledged inputs.

## Consequences
- No leader election, no scheduler process; any instance can die.
- Failover time ≈ lease TTL (default 20 s); tunable.
- Placement is first-come rather than bin-packed; acceptable because turns are
  I/O-bound (model latency). A placement policy can be added later as a
  pre-acquire filter.
- Correctness relies on the store's atomic lease compare-and-set (covered by
  the storage conformance race tests) and on synchronised clocks.

## Alternatives
- **Central scheduler with leader election:** better placement, more moving parts.
- **Temporal/Cadence workflows:** strong durability but a heavy dependency and
  an execution model at odds with free-running agent loops.
- **Kubernetes operator only:** excludes Docker Compose and bare-metal installs.

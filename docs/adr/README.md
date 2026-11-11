# Architecture decision records

| # | Decision | Status |
|---|---|---|
| [0001](0001-go-control-plane.md) | Go for the control plane | Accepted |
| [0002](0002-reconciler-and-leases.md) | Reconciler with replica leases instead of a central scheduler | Accepted |
| [0003](0003-pull-broker-abstraction.md) | Pull-based broker abstraction: subjects × consumer groups | Accepted |
| [0004](0004-runtime-protocol.md) | Agents via an HTTP runtime protocol with a callback gateway | Accepted |
| [0005](0005-default-deny-tool-gateway.md) | Default-deny capability grants enforced in a gateway | Accepted |
| [0006](0006-sqlite-default-postgres-production.md) | SQLite by default, PostgreSQL in production | Accepted |
| [0007](0007-nats-jetstream-default.md) | NATS JetStream as the default broker | Accepted |
| [0008](0008-offline-mock-provider.md) | Deterministic mock provider as the default model | Accepted |
| [0009](0009-dedicated-sandbox-daemon.md) | A Docker daemon dedicated to agent sandboxes | Proposed |

Format: context, decision, consequences, alternatives considered. New ADRs
take the next number with status *Proposed* in the pull request that
implements them.

# 0006 — SQLite by default, PostgreSQL in production

**Status:** Accepted

## Context
The platform must start with zero services on a laptop, and run highly
available in production. Storage holds graphs, versions, executions, message
history, approvals, events, KV state and leases.

## Decision
One `storage.Store` interface; a shared database/sql implementation with
SQLite (pure Go, WAL, single writer) as the binary default and PostgreSQL for
multi-instance deployments (Compose uses PostgreSQL to mirror production). A
filesystem store exists for zero-dependency, inspectable single-node use.
Filter columns are real indexed columns; full records live in a JSON column
so additive fields need no migration.

## Consequences
- Same code path and conformance suite for both SQL dialects.
- SQLite and fs stores are single-instance by design (lock / single writer).
- PostgreSQL event appends are serialised to keep sequence order equal to
  commit order (correct SSE resume) at some write-throughput cost.

## Alternatives
- **Event sourcing as the only model:** elegant, heavier for queries and operations.
- **Embedded KV (bbolt/Badger):** fast, but no multi-instance story.

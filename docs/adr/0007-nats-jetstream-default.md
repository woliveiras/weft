# 0007 — NATS JetStream as the default broker

**Status:** Accepted

## Context
The default broker ships in `docker compose up`, so it must be small, fast to
start, durable, clusterable, and support pull consumers with ack deadlines,
redelivery and delayed negative acks.

## Decision
Default to NATS JetStream: one stream per channel, durable pull consumers per
group with `AckWait` and `MaxDeliver`, `NakWithDelay` for retries,
`InProgress` for lease extension, dead letters republished to a per-graph DLQ
subject (also on max-delivery advisories). Redis Streams, RabbitMQ, Kafka and
filesystem adapters remain fully supported.

## Consequences
- A ~20 MB single binary for development, a 3-node R3 cluster for production.
- Operators already running Kafka or RabbitMQ can keep them via configuration.

## Alternatives
- **Kafka:** excellent at scale, heavy for a laptop; competing consumers bounded by partitions.
- **RabbitMQ:** mature, but delayed retries and lease semantics need workarounds.
- **Redis Streams:** light, but durability depends on persistence settings.

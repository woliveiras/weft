# 0003 — Pull-based broker abstraction: subjects × consumer groups

**Status:** Accepted

## Context
Channel semantics (queue, topic, fanout, broadcast, competing consumers) must
work identically on NATS, Redis Streams, RabbitMQ, Kafka and a plain
filesystem, with durable at-least-once delivery, retries and dead-lettering.

## Decision
Expose one primitive: durable **subjects** with named **consumer groups**.
Every group receives every message; inside a group each message is leased to
one consumer (`AckWait`), redelivered on expiry or `Nack(delay)`, and
dead-lettered after `MaxDeliver` or `Term`. Consumers **pull**
(`Fetch(max, wait)`). The engine maps channel types to groups:
queue → one group per channel; topic/fanout → one per consuming node;
broadcast → one per replica. Groups are declared before publishing.
Consumers deduplicate by message id (`MarkProcessed`) and ack only after
outputs are published.

## Consequences
- Adapters are small and testable against one conformance suite.
- Pull gives natural back-pressure and implements "wait for work" without
  push buffers holding messages hostage in a busy replica.
- Effectively-once processing; external side effects should use message ids
  as idempotency keys.
- Broker-specific limits are documented per adapter (e.g. Kafka competing
  consumers bounded by partitions; RabbitMQ delayed retries via TTL queues).

## Alternatives
- **Push subscriptions:** lower latency, but prefetch strands work in busy replicas.
- **Expose broker-native features (exchanges, partitions) in graphs:** more
  power, no portability.

# Multi-agent execution model

This document explains how agents run. Implementation: `engine/replica.go`, `engine/turn.go`, `engine/graphrun.go`, `engine/publish.go`, `graph/`.

## 1. Vocabulary

| Term | Meaning |
|---|---|
| **Node** | An agent definition in a graph. |
| **Replica** | One running instance of a node: one goroutine and one loop. A node runs 1..N replicas. |
| **Turn** | One iteration of a replica: claim inputs → runtime call → publish outputs → ack inputs. Every turn is a fresh session. |
| **Execution** | The persisted record of a turn. |
| **Run** | The period from a graph `start` to its `stop`, identified by `runId`. |

## 2. Loop modes and stop policies

`loop.mode` decides **when a turn starts**. `loop.stop.policy` decides **when a replica ends by itself**. The two are orthogonal, except that `event` and `auto` are rejected together (waiting is a claim that more work is coming; auto stop is a claim that the work can finish).

| Mode | Turn starts when | Input claiming |
|---|---|---|---|
| `once` | Immediately, exactly one turn per run, then `completed` — or `failed` when that turn failed (the reason is in the replica detail and a `log` event with level `error`). Neither is restarted during the run; start the node again to retry. | Non-blocking fetch. |
| `continuous` | Turn after turn. | Non-blocking: takes what is there, may run with no inputs. |
| `scheduled` | First turn at start, then on `schedule` (cron, `@hourly`, `@every 15m`). An operator message triggers an extra turn. | Non-blocking. |
| `event` | A message is waiting or an operator message arrives; otherwise `idle`. | Blocking fetch, 2s wait per cycle. A node with no inputs waits for operator messages only. |

| Stop policy | Ends when | Measured over |
|---|---|---|
| `never` | Never; only an operator stops it. | — |
| `auto` | A turn returns `confirm_stop` after the previous turn returned `no_further_work` (two-turn handshake). An operator message withdraws a pending declaration. | Per replica |
| `iterations` | `maxIterations` turns have run in this run. | Current run, not lifetime |
| `duration` | `maxDuration` has elapsed since the replica started. | Current run |
| `idle` | Event mode only: `idleTimeout` passed without input. | Current idle period |

Counted limits decide whether a turn *starts*; they never cut one short.

`loop.interval` paces turns in any mode: the replica waits that long after a turn ends. A poke (operator message, resume, stop) ends the wait early.

## 3. Anatomy of a turn

| Step | What happens | Failure handling |
|---|---|---|
| 1. Concurrency slot | Global semaphore `engine.maxConcurrentTurns` per instance. | Waits. |
| 2. Claim | Up to `batchSize` deliveries, rotating across input subscriptions so one busy input cannot starve another. Each delivery is checked against history; one already `acked` for this group is acked and skipped. | — |
| 3. Execution record | `running`; `agent.turn` span (parent = first input's trace, links = other inputs); `delivered` history rows; queue-latency metric. | — |
| 4. Lease extension | `Extend` every `min(ackWait)/3` (minimum 1s) while the turn runs. | — |
| 5. Turn gate | If `approval.beforeTurn`, block on a human. | Rejected → inputs dead-lettered, execution `cancelled`. |
| 6. Input build | Prompt with parameters substituted. Platform preamble: identity, directories, inputs framed as untrusted data, outputs and routing hints, granted tools, ring neighbours, operator messages, unset parameters, stop protocol. Callback URL and token for external runtimes. | — |
| 7. Runtime call | `Runtime.Turn(ctx, in, env)` under `limits.turnTimeout`. Tool calls are capped by `limits.maxToolCalls`, tokens by `limits.maxTokens`. | Error, timeout or `retry: true` → nack with exponential back-off; dead letter at `maxDeliver`. |
| 8. Publish | Every emit (from the result and from `env.Emit` or `/emit`) → `Child` message → hop check → route → publish, or hold at a gate. | The first publication error stops remaining emits and fails the turn. Inputs are nacked with back-off, or dead-lettered at `maxDeliver`; none are acked or marked processed. |
| 9. Ack | Ack each input, `MarkProcessed`, `acked` history row. | Ack failure → redelivery → dedupe. |
| 10. Signals | Ring broadcast stored; auto-stop handshake updated. | — |

## 4. Delivery guarantees

- **At-least-once delivery.** Every broker adapter leases deliveries for `ackWait` and redelivers unacked messages with `attempt+1`. After `maxDeliver` attempts, a message is dead-lettered: NATS writes it to a per-graph DLQ stream, other adapters to dead streams, queues or topics. Dead letters are counted in `Stats.Dead` where the adapter can tell.
- **Ack after successful publication.** All routed outputs must be published successfully (or durably held for human approval) before any input is acked or marked processed. An output construction or publication error fails the turn and follows the same retry/dead-letter policy as a runtime error. Intentional routing drops (no matching route or hop limit) remain successful dispositions. A crash between publication and acknowledgement redelivers the inputs.
- **Effectively-once processing.** Before a turn, a delivery already recorded as `acked` for the same group is skipped. After a turn, `MarkProcessed(group, id)` records the processing. NATS publishes also carry `Nats-Msg-Id` for broker-side duplicate suppression of producer retries.

Duplicates can still occur in these cases:

| Case | Why |
|---|---|
| The turn succeeded, outputs were published, then the process died before ack and history write. | The redelivered input is processed again and its outputs are re-emitted with *new* message ids. Downstream dedupe does not catch them. |
| A turn published some outputs before another publication failed. | The successful outputs remain delivered. Retrying the turn creates new message IDs, so downstream deduplication does not eliminate these repeated outputs. |
| A history write failed. | The pre-check reads history, not the `processed` table. |
| Lease overlap during failover. | An instance that stalled past `LeaseTTL` and the new owner may both run one turn. |
| Kafka rebalance or crash. | Records after an uncommitted offset are redelivered. |

**Consequence for agent authors:** make side effects idempotent. Write files deterministically (overwrite rather than append), and key state by message or correlation id. The example agents follow this rule.

Tool side effects are not transactional with the ack. Weft recovers by redelivery, not by deterministic replay.

## 5. Channels, clusters and competition

| Pattern | How to express it |
|---|---|
| Point-to-point work queue | `type: queue`. Every consumer (all target nodes, all replicas) competes. |
| N workers on one step | `cluster: {replicas: N}` (fixed) or `{mode: autoscale, maxReplicas: N}` on the consumer. |
| Copy per downstream agent | `type: fanout` with `to: [a, b]`. |
| Decoupled pub-sub | `type: topic, topic: review.audit`; consumers add `subscribe: ["review.*"]`. |
| Every replica must see it | `type: broadcast`. |
| Fan-in | Several channels into one node. The replica pulls from all of them, rotating its starting point. |

**Clusters versus separate nodes.** On a `topic` or `fanout`, a *cluster* is one subscriber: its replicas compete for one copy. Separate nodes each receive a copy.

**Fixed clusters.** Run exactly `replicas`. `PUT /nodes/{n}/scale` overrides the count, persisted in `_control/<graph>`. Broadcast groups for new replicas are declared at scale time.

**Autoscaled clusters.** On each reconcile, the engine computes `desired = clamp(Σ pending + inFlight over the node's groups, minReplicas, maxReplicas)`.

- Scale-up starts the missing replica indexes; any instance holding the lease can run them.
- Scale-down stops only replicas that have been `idle` or `paused` for `scaleDownAfter`, and never one that is mid-turn.
- `minReplicas ≥ 1` keeps a started node visibly alive while it waits for its first item.
- Autoscale needs `event` mode to shrink, because only idle replicas are shed.

**Comms.**

- `isolated`: replicas share only their queue, and the preamble says so explicitly.
- `ring`: each replica writes one broadcast line per turn (key `r<n>`; `finish{broadcast}` or a `BROADCAST:` line). Before each turn it reads its two neighbours' lines (radius 1, wrapping). Each replica writes only its own key: no locks, no read-modify-write. A replica that broadcast nothing still records "(no broadcast; …)", because silence would be indistinguishable from death.

## 6. Dynamic branching, cycles and the hop limit

The router applies three rules in order (`graph.Router.Route`):

1. **`emit.channel` set:** only that channel. It must be one of the node's outputs.
2. **`emit.target` set:** only outputs whose consumers include the target node.
3. **Otherwise:** every outgoing channel.

In every case, the channel's `types` filter (globs) and `when` condition must also pass. `when` is an expr-lang boolean over `type`, `source`, `target`, `payload` and `metadata`, for example:

```yaml
- {id: approved, from: reviewer, to: [qa], when: 'payload.verdict == "approved"'}
- {id: rework,   from: reviewer, to: [developer], when: 'payload.verdict != "approved" && metadata.hops < 30'}
```

Several matching channels mean fan-out; that is intentional. If none match, the message is recorded as `dropped` on the pseudo-channel `_unrouted`, with the reason (for example `rework: condition false`), and `weft_messages_dropped_total{reason="no_route"}` is incremented.

**Cycles are allowed.** Each `Child` message increments `metadata.hops`. A message whose hops exceed `spec.maxHops` (default 64) is dropped with `reason="hop_limit"`. Conditions can enforce tighter, domain-specific bounds, such as the example graph's 30.

## 7. Concurrency limits

| Limit | Scope | Setting |
|---|---|---|
| Turns in flight | per instance | `engine.maxConcurrentTurns` (compose: 32) |
| Replicas | per node | `cluster.replicas` / `maxReplicas` (≤ 256) |
| Model requests | per provider | `providers[].concurrency` |
| Tool invocations | per node and tool | `tools[].rateLimit` (per minute) |
| Tool calls / tokens | per turn | `limits.maxToolCalls`, `limits.maxTokens` |
| Turn duration | per turn | `limits.turnTimeout` |
| Emits | per turn | 1000 (hard) |
| Kafka competing consumers | per group | number of partitions (default 12) |

## 8. Isolation modes

| Mode | Where tools run | Where an external runtime runs | What is actually isolated |
|---|---|---|---|
| `process` (default) | Child processes of `weftd` (`ProcessSandbox`): working directory jailed to the replica workdir, scrubbed environment (no server secrets), process-group kill on timeout, capped output. | `runtime.command` as a child process on a free loopback port, with a minimal environment, or an existing `endpoint`. | Filesystem tools are jailed (resolved through symlinks). **`shell.exec` is not syscall-confined**: an unrestricted shell can read outside the workspace. Constrain it with `commands`, or use docker. |
| `docker` | One container per replica (`weft-sbx-<graph>-<node>-<n>`) from `isolation.image` or `weft/sandbox`: workspace bind-mounted at `/workspace`, `--network none` by default, `--cap-drop ALL`, `no-new-privileges`, CPU and memory limits. | `runtime.image` started as a container on `WEFT_RUNTIME_NETWORK`. | Process, filesystem and network isolation of tool execution. Containers run on the daemon in `isolation.docker.host` — with Compose, `sandbox-daemon`, dedicated to Weft and firewalled from private networks ([sandbox-runtime.md](sandbox-runtime.md)). `network: bridge` gives internet access only. |
| `kubernetes` | Currently the process sandbox of the `weftd` pod. | Must be an explicit `endpoint`. `weft k8s render` produces a hardened Deployment and Service (non-root, read-only rootfs, dropped capabilities, seccomp RuntimeDefault). | Agent code is isolated in its own pod. **Per-replica tool pods are not implemented** (extension point: implement `tools.Sandbox`). |

Filesystem tools always operate on the platform's view of the workspace and enforce the path jail. Git tools run through the replica's sandbox, so they work inside containers too.

## 9. Workspaces and worktrees

| Mode | Directory | Use when |
|---|---|---|
| `shared` (default) | `<root>/<graph>` (optional `subdir`) | Pipeline stages touch different files, or later stages must see earlier stages' files directly (the software-factory example). |
| `worktree` | `<root>/.worktrees/<graph>/<node>-<n>` on branch `weft/<graph>/<node>-<n>` | Parallel replicas edit the same code. Merging is done by git or an integrator agent, never by the platform. |

Each replica also has a scratch folder `<workdir>/.weft/<node>/r<n>` (`@loop`) for notes between turns. Durable memory that should be shared across replicas belongs in `state_set` (KV namespace `<graph>/<node>`).

## 10. Operator interaction

| Action | Effect |
|---|---|
| Operator message (`POST /nodes/{n}/chat`, `replica` −1 = any) | Persisted, then consumed atomically by the next turn of a matching replica. Wakes local event-mode replicas; remote replicas notice within one poll cycle. Withdraws a pending auto-stop declaration. Re-queued if that turn fails. |
| Inject (`POST /channels/{c}/messages`) | New message with `source = external:<user>` and `injected_by`; channel gates apply. |
| Pause / resume (graph or node) | No new turns; the current turn finishes. |
| Stop / kill (graph or node) | Graceful drain or immediate cancellation (inputs nacked). |
| Scale | Fixed: replica count override. Autoscale: edits `maxReplicas` (new graph version). |
| Edit graph | Applied live; see [state-machines.md](state-machines.md) and sequence 10 in [diagrams/sequences.md](diagrams/sequences.md). |

## Native analysis delegation

See [delegation](delegation.md) for the explicit grant, mandatory approval, child
execution records, event metadata, fixed limits and inclusive usage accounting.

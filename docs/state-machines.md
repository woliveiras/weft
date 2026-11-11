# State machines

Allowed transitions are declared in `server/internal/domain/state.go` (`CanTransition`). The engine drives them in `engine/` and `hitl/`. Every replica, execution and approval transition emits an event (`replica.state_changed`, `execution.*`, `approval.*`).

## Graph run

```mermaid
stateDiagram-v2
  [*] --> stopped
  stopped --> running : start (new runId)
  running --> paused : pause
  paused --> running : resume
  running --> stopping : stop / kill
  paused --> stopping : stop / kill
  stopping --> stopped : no live replica leases
  running --> failed
  paused --> failed
  stopping --> failed
  failed --> running : start
  failed --> stopped
```

`GraphRecord.desiredState` is what an operator asked for; `state` is what the reconciler observed.

| Trigger | Guard | Effects |
|---|---|---|
| `start` | desired is `stopped` (no-op if already `running`) | New `runId`, `startedAt`; node stop and pause overrides cleared; `graph.started`; reconciler acquires leases and starts replicas. |
| `start` | desired is `paused` | desired → `running`. |
| `pause` | desired is `running` | desired → `paused`; replicas finish the current turn and park in `paused`. |
| `resume` | desired is `paused` | desired → `running`; replicas are poked. |
| `stop` | — | desired → `stopped`, state → `stopping`; `graph.stopping`. Replicas drain their current turn, then release their leases. |
| `kill` | — | Same as stop (`graph.stopping`), plus in-flight turns on this instance are cancelled (inputs nacked). |
| reconcile | desired `stopped`, no live replica holds an unexpired lease | state → `stopped`; `graph.stopped`. Statuses of replicas whose instance vanished are set to `stopped`. |
| reconcile | desired `running`/`paused` | state mirrors desired. |
| reconcile error | desired `running`/`paused`, and the graph cannot be deployed (version not loadable, a `when` condition no longer compiles, the broker refuses the topology) | state → `failed`; `graph.failed` with the error. Reconciliation keeps retrying and the state returns to `running`/`paused` on the next successful deploy. |

## Replica

```mermaid
stateDiagram-v2
  [*] --> stopped
  stopped --> starting : lease acquired
  starting --> idle : setup ok
  starting --> failed : setup error
  idle --> running : inputs / tick / operator message
  running --> idle : turn done
  running --> awaiting_approval : beforeTurn / tool approval / human.ask
  awaiting_approval --> running : decided
  idle --> paused : graph or node paused
  paused --> idle : resumed
  running --> completed : stop policy met / confirm_stop / once
  running --> failed : once, and its only turn failed
  idle --> completed : idle timeout / iteration or duration limit
  idle --> stopping : stop
  running --> stopping : stop (turn finishes) / kill
  paused --> stopping : stop
  stopping --> stopped : loop exited
  completed --> starting : node start / new run
  failed --> starting : next reconcile
```

| Trigger | Guard | Effects |
|---|---|---|
| Lease acquired | replica wanted, not `completed` in this run | Previous owner's executions marked `interrupted`; goroutine started. |
| Setup | workspace, sandbox, `Runtime.Prepare`, subscriptions | On failure: `failed`, 10s back-off, then exit (restarted by a later reconcile). |
| Claim | depends on loop mode (see [execution-model.md](execution-model.md)) | `running`; execution created. |
| Turn ends | — | `idle`. Failures add exponential back-off (`retryBackoff · 2^(n-1)`, max 5m). |
| Pause | graph desired `paused` or node override `paused` | `paused`; no new turn starts. |
| Stop policy | `iterations` / `duration` / `idle` limit reached, `once` done, or `confirm_stop` after a declaration | `completed`; marker `done/<node>#<n> = runId`; not restarted during this run. |
| Once failed | `once` node whose only turn failed | `failed` with detail `its only turn failed: …` and a `log` event (`level: error`); same marker, so it is not retried in a loop; a new start runs it again. |
| Stop | graph or node stopped, surplus replica, lease lost | `stopping` → `stopped`; lease released. |
| Kill | — | Turn context cancelled, then as for stop. |

The UI reports a node's state as its "most alive" replica: running > awaiting_approval > starting > idle > paused > stopping > failed > completed > stopped.

## Execution (turn)

```mermaid
stateDiagram-v2
  [*] --> running : turn starts
  running --> awaiting_approval : (reflected on the replica)
  awaiting_approval --> running
  running --> succeeded : runtime ok
  running --> failed : runtime error / timeout / retry requested
  running --> cancelled : kill / beforeTurn rejected
  running --> interrupted : owner instance died (detected on recovery or lease takeover)
  succeeded --> [*]
  failed --> [*]
  cancelled --> [*]
  interrupted --> [*]
```

`pending` exists in the domain model, but the engine creates executions directly in `running`. While a turn waits for a human, the *replica* shows `awaiting_approval`; the execution record stays `running`. Delegated children are the exception: their
initial delegation-approval wait is persisted as `awaiting_approval` (see below).

| Outcome | Effect on inputs |
|---|---|
| `succeeded` | Emits routed and published, then inputs acked and `MarkProcessed`. |
| `failed` | Inputs nacked with delay `retryBackoff · 2^(attempt-1)`; `Term` (dead letter) once `attempt ≥ channel.maxDeliver`. Consumed operator messages are re-queued. |
| `cancelled` (kill) | Inputs nacked immediately. |
| `cancelled` (beforeTurn rejected) | Inputs dead-lettered with reason "turn rejected by operator". |
| `interrupted` | Nothing is acked; the broker redelivers after the lease expires. |

## Message delivery (history)

```mermaid
stateDiagram-v2
  [*] --> published
  [*] --> held : approval gate
  held --> published : approved
  held --> rejected : rejected / expired
  published --> delivered : fetched by a group
  delivered --> acked : turn succeeded
  delivered --> nacked : turn failed / killed
  nacked --> delivered : redelivery (attempt+1)
  delivered --> dead_lettered : attempt ≥ maxDeliver or turn rejected
  [*] --> dropped : no route / hop limit
```

History holds one row per (message, channel, group). The publish-side row has no group. A duplicate delivery (already `acked` for the group) is acked without a turn and counted as `outcome="duplicate"`.

## Approval

```mermaid
stateDiagram-v2
  [*] --> pending : Create / RequestApproval
  pending --> approved : Decide(approve)
  pending --> rejected : Decide(reject)
  pending --> expired : expiresAt passed (onTimeout = reject)
  pending --> approved : expiresAt passed (onTimeout = approve)
```

| Kind | Created by | On approval | On rejection / expiry |
|---|---|---|---|
| `tool` | Tool gateway (`requiresApproval`) | Tool executes | `ErrRejected` is returned to the agent |
| `turn` | `approval.beforeTurn` | Turn proceeds | Inputs dead-lettered; execution `cancelled` |
| `message` | `channel.approval`, `approval.beforeEmit` | Published (payload replaced if `answer` is valid JSON) | Recorded as `rejected` |
| `question` | `human.ask` tool, `AskHuman` callback | `answer` returned to the agent | Error returned to the agent |

`DecideApproval` succeeds only from `pending`; a second decision returns HTTP 409. Expiry is evaluated by the waiter and by the periodic `Sweep`.

## Dynamic delegation entities and states

Delegation uses existing execution and approval records, not temporary graph
nodes. This is the current native contract; other runtimes remain separate capabilities; native recursion requires an explicit
inherited grant and a configured depth of at most four.

| Entity | Identity and authoritative fields |
|---|---|
| Parent execution | Immutable execution ID, graph/run/version, node/replica; owns the tool request and inclusive reported consumption. |
| Child execution | Its own execution ID plus `parentExecutionId`; selected profile/model/objective, revisioned configuration, access/decision audit, outcome, result/error and child-only reported usage. |
| Delegation approval | Approval ID linked to the child execution, kind `delegation`, pending/approved/rejected/expired decision, actor and time. Automatic admission has no synthetic human approval. |
| Tool approval | Separate approval linked to the child for a granted tool requiring consent. Delegation approval does not replace it. |
| Cancellation request | Durable record addressed to an execution ID with child/tree scope, root ID, actor and time. It requests cooperative cancellation; it is not a terminal execution outcome. |
| Result | Child summary or bounded error persisted on the child and returned to the waiting parent as the delegation tool outcome. It does not add a graph channel or create a new task resource. |
| Consumption ledger | Root-execution key, bounded reservations and charged/estimated totals. Reported execution usage and conservative admission accounting have different meanings. |

```mermaid
stateDiagram-v2
  [*] --> awaiting_approval : admitted under required policy
  [*] --> running : admitted under automatic policy
  awaiting_approval --> running : approved and configuration/capacity revalidated
  awaiting_approval --> failed : rejected or stale configuration
  awaiting_approval --> cancelled : cancellation or deadline
  awaiting_approval --> interrupted : restart or lease takeover
  running --> succeeded : bounded complete result
  running --> failed : model/tool/limit/persistence failure
  running --> cancelled : cooperative cancellation or deadline
  running --> interrupted : restart or lease takeover
```

A rejected/expired decision is retained on the approval; it does not introduce
`rejected` or `expired` execution statuses. An expiry returned as an approval
decision produces failure; expiry through the request deadline produces a
cancelled child. Recovery interrupts abandoned children and expires their pending
approvals. A child's tool approval can suspend work without changing the child's
persisted execution status from `running`; inspect linked approvals for that wait.

The parent can handle a failed/rejected/cancelled child and finish successfully.
Stopping one child also cancels its active descendants, but not its parent or siblings. Tree cancellation
checks the root request in every active descendant context. A request racing with
completion can coexist with a terminal successful outcome; the UI must not turn
an acknowledged request into a claim that work was forcibly killed.

Reservations move from reserved to dispatched and then settled. Undispatched
failures release credit; dispatched unknown usage charges the reservation
conservatively. Settlement is idempotent. Recovery closes terminal-root ledgers,
retains active/unknown-root holds and later compacts per-call details while keeping
a closed aggregate tombstone. Do not sum parent and child execution usage: parent
reported usage is inclusive. This accounting is not a guaranteed monetary ceiling.

Transitions are covered by the delegation, execution-control and budget
unit/integration suites and browser approval, interruption and recovery journeys.
See [delegation](delegation.md) for limits and [operations](operations.md) for
restart/version compatibility. General event publication is best effort; persisted
execution/approval/ledger records are the source of truth.

# Sequence diagrams

Participants are named after the packages that implement them. "Replica" is the replica goroutine in `engine/replica.go` and `engine/turn.go`.

## 1. Injection → turn → emit → route → publish → ack

```mermaid
sequenceDiagram
  autonumber
  actor Op as Operator / CI
  participant API as api
  participant E as engine.Inject
  participant B as Broker
  participant S as Store
  participant R as Replica (consumer)
  participant RT as Runtime
  participant RO as graph.Router

  Op->>API: POST /graphs/{g}/channels/{c}/messages
  API->>E: Inject(type, payload)
  E->>E: NewMessage (correlation_id = id, hops 0, traceparent)
  E->>B: Declare(subject, groups) (idempotent)
  E->>B: Publish("<g>.<c>", msg)
  E->>S: RecordMessage(published), AppendEvent(message.published / injected)
  API-->>Op: 201 Message
  R->>B: Fetch(group, max=batchSize, wait=2s)
  B-->>R: Delivery (attempt 1, leased for ackWait)
  R->>S: GetMessage(id) — already acked by this group? (dedupe)
  R->>S: PutExecution(running), RecordMessage(delivered)
  R->>RT: Turn(TurnInput, TurnEnv)
  RT->>R: env.Emit({type, payload, target?})
  RT-->>R: TurnResult
  loop each emit, until the first error
    R->>R: parent.Child() → hops+1, causation_id, inherited correlation & trace
    R->>RO: Route(node, msg, channel)
    RO-->>R: matching channels (types filter, when, target)
    R->>B: Publish(each channel)
    R->>S: RecordMessage(published)
  end
  alt all outputs published or durably held for approval
    R->>B: Ack(delivery)
    R->>S: MarkProcessed(group, id), RecordMessage(acked), PutExecution(succeeded)
  else output construction or publication failed
    R->>B: Nack(back-off), or Term at maxDeliver
    R->>S: RecordMessage(nacked / dead_lettered), PutExecution(failed)
    Note over R,B: No ack or processed marker; partial outputs may repeat on retry
  end
```

## 2. External (Python) agent turn with callbacks

```mermaid
sequenceDiagram
  autonumber
  participant R as Replica
  participant X as execRegistry
  participant EXT as runtime/external
  participant PY as Python agent (weft.serve)
  participant CB as /internal/v1/executions/{id}
  participant TG as Tool gateway
  participant LG as LLM registry
  participant S as Store

  R->>X: register(execID) → random token
  R->>EXT: Turn(TurnInput{callback: url, token})
  EXT->>PY: POST /v1/turn (X-Weft-Runtime-Secret, traceparent)
  PY->>CB: POST /tools/filesystem.read (Bearer token)
  CB->>X: Callback(execID, token) (constant-time compare)
  CB->>TG: Invoke(principal = node grants, workdir, sandbox)
  TG-->>PY: {content, data} or 403 when denied
  PY->>CB: POST /llm/chat (model optional)
  CB->>LG: Resolve(node model) + Chat
  LG-->>PY: ChatResponse
  PY->>CB: PUT /state/plan%3A42 (raw bytes)
  CB->>S: KVPut("<graph>/<node>", "plan:42")
  PY-->>EXT: 200 TurnResult{emits, summary, signal}
  EXT-->>R: TurnResult
  R->>X: unregister(execID) — the token stops working
```

## 3. Tool call that requires approval

```mermaid
sequenceDiagram
  autonumber
  participant A as Agent (native loop)
  participant TE as turnEnv
  participant TG as tools.Gateway
  participant H as hitl.Service
  participant S as Store
  actor Ap as Approver
  participant API as api

  A->>TE: InvokeTool("shell.exec", args)
  TE->>TE: replica state → awaiting_approval
  TE->>TG: Invoke(principal, name, args)
  TG->>TG: Authorize (default deny) → merged grant requiresApproval
  TG->>H: RequestApproval(kind=tool, detail{tool,args})
  H->>S: PutApproval(pending), event approval.requested
  loop until decided / expired / ctx done
    H->>S: GetApproval (every 2s) or local signal
  end
  Ap->>API: POST /approvals/{id}/approve
  API->>H: Decide → S.DecideApproval (pending only)
  H-->>TG: approved
  TG->>TG: execute in sandbox, audit tool.invoked/completed
  TG-->>A: Result
  Note over TG,A: rejected → ErrRejected → the model sees "a human rejected this call"
```

## 4. Held message at a channel gate

```mermaid
sequenceDiagram
  autonumber
  participant R as Replica (producer)
  participant E as engine.publish
  participant H as hitl.Service
  participant S as Store
  actor Ap as Approver
  participant B as Broker
  participant C as Replica (consumer)

  R->>E: publish(channel.approval = true)
  E->>H: Create(kind=message, detail{graphId, channel, message, onTimeout})
  E->>S: RecordMessage(held), event message.held
  Note over R: The producer acks its inputs and continues — it does not wait
  Ap->>H: Decide(approve, answer = edited payload JSON?)
  H->>E: onApprovalDecided hook (on the deciding instance)
  E->>E: replace payload if answer is valid JSON (edited_by)
  E->>B: Publish(subject)
  E->>S: RecordMessage(published)
  B-->>C: Delivery
  Note over H,E: reject / expire → RecordMessage(rejected); never published
```

## 5. Auto-stop two-turn handshake

```mermaid
sequenceDiagram
  autonumber
  participant L as Replica loop
  participant RT as Runtime
  participant S as Store

  L->>RT: Turn(pendingStop=false)
  RT-->>L: signal no_further_work
  L->>L: pendStop = true (unless operator messages arrived this turn)
  L->>RT: Turn(pendingStop=true) — preamble: "check again, or confirm"
  alt finds work
    RT-->>L: no signal → pendStop = false (declaration withdrawn)
  else nothing to do
    RT-->>L: signal confirm_stop
    L->>S: KVPut(_control/<g>, done/<node>#<n>, runID)
    L->>S: ReplicaStatus(completed)
    Note over L: the reconciler will not restart it during this run
  end
```

An operator message to the node also clears `pendStop` (`SendOperatorMessage`).

## 6. Ring cluster broadcast

```mermaid
sequenceDiagram
  autonumber
  participant R0 as replica 0
  participant R1 as replica 1
  participant R2 as replica 2
  participant S as Store (KV <g>/<node>/_ring)

  R1->>S: KVList(prefix "r") → neighbours r0, r2 (radius 1, wrapping)
  R1->>R1: TurnInput.neighbors = latest lines of r0 and r2
  R1->>R1: turn … finish{broadcast: "covered speed records"}
  R1->>S: KVPut("r1", {text, at}) — each replica writes only its own key
  R1-->>S: event agent.broadcast
  R2->>S: KVList → sees r1's line next turn
```

## 7. Autoscale up and down

```mermaid
sequenceDiagram
  autonumber
  participant Rec as Reconciler (every interval)
  participant B as Broker
  participant S as Store
  participant Rn as New replica n

  Rec->>B: Stats(subject, group) for each input group
  B-->>Rec: pending + inFlight = 5
  Rec->>Rec: desired = clamp(5, min=1, max=4) = 4
  loop idx 0..3 not running locally
    Rec->>S: AcquireLease(<g>/<node>#idx, instance, ttl)
    S-->>Rec: ok (or held elsewhere → skip)
    Rec->>Rn: start replica goroutine
  end
  Note over Rec: later, backlog = 0 → desired = 1
  Rec->>Rec: replicas idx ≥ 1 idle ≥ scaleDownAfter? → stop gracefully + ReleaseLease
  Note over Rec: replicas mid-turn or recently busy are kept
```

## 8. Instance failover

```mermaid
sequenceDiagram
  autonumber
  participant I1 as weftd i1
  participant I2 as weftd i2
  participant S as Store
  participant B as Broker

  I1->>S: AcquireLease(<g>/<n>#0, "i1", 20s) (renewed every interval)
  I1->>B: Fetch → delivery D (leased for ackWait)
  Note over I1: crash — no release, no ack
  loop every reconcile interval
    I2->>S: AcquireLease(<g>/<n>#0, "i2") → false while i1's lease is valid
  end
  I2->>S: AcquireLease → true (lease expired)
  I2->>S: ListExecutions → mark i1's running execution "interrupted"
  I2->>I2: start replica 0
  Note over B: after ackWait, D becomes visible again (attempt 2)
  I2->>B: Fetch → D (attempt 2)
  I2->>S: GetMessage(D) — not acked by this group → process
```

A restarting instance also runs `recover()` on boot, which marks its own `running` executions `interrupted`.

## 9. Graceful stop vs kill

```mermaid
sequenceDiagram
  autonumber
  actor Op as Operator
  participant E as engine
  participant R as Replica
  participant RT as Runtime
  participant B as Broker

  Op->>E: POST /graphs/{g}/stop
  E->>E: record.desiredState = stopped
  E->>R: stop(kill=false): loopCtx cancelled (waits end)
  Note over R,RT: the turn context derives from WithoutCancel(loopCtx) — the turn finishes
  RT-->>R: TurnResult → publish, ack
  R-->>E: exit → status stopped, ReleaseLease

  Op->>E: POST /graphs/{g}/kill
  E->>R: stop(kill=true): turnCancel()
  RT-->>R: ctx canceled
  R->>B: Nack(delay 0) inputs
  R-->>E: execution cancelled, status stopped
```

## 10. Editing a running graph

```mermaid
sequenceDiagram
  autonumber
  actor D as Designer
  participant API as api
  participant E as engine
  participant S as Store
  participant Old as graphRun v1
  participant New as graphRun v2
  participant R as Replica

  D->>API: PUT /graphs/{g} (If-Match: 1)
  API->>E: SaveGraph(expectedVersion 1)
  E->>S: SaveGraph → v2 (409 if someone saved first)
  E->>S: record.version = 2; Declare topology
  E->>E: reconcile: run.version != record.version
  E->>New: newGraphRun(v2)
  New->>Old: adopt()
  alt same run and structuralKey unchanged (runtime, isolation, workspace, comms)
    Old->>New: move replica; rep.setRun(v2); poke
    Note over R: next turn uses the new prompt/model/tools/loop settings
  else structural change or node removed
    Old->>R: stop(false) — graceful
    E->>R: started again under v2 on the next reconcile
  end
```

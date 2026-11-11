# Production hardening

The defaults of `docker compose up` optimise for a working platform in one
command. This checklist turns a deployment into one you can run unattended
with real models, real code bases and real credentials. Each item names the
setting or file that implements it.

## Identity and access

- [ ] **Use a strong admin token.** Set `WEFT_ADMIN_TOKEN` (≥ 32 random
      bytes) only for break-glass access.
- [ ] **Use OIDC for people** (`auth.oidc`: issuer, audience, `roleClaim`);
      map groups to `viewer` / `operator` / `admin`. Admin (graph edits) should
      be a small group; approvers need `operator`.
- [ ] **Scoped tokens for automation** (`auth.tokens` with `tokenFile` from
      mounted secrets): CI gets `operator` or `admin` per need; dashboards get
      `viewer`.
- [ ] **Rotate tokens** by adding the new token, rolling clients, removing the
      old one (tokens are re-read at startup; roll instances).
- [ ] **Set `WEFT_RUNTIME_SECRET`** (`runtimes.sharedSecretEnv`) on weftd and
      every external runtime so only the platform can start turns.
- [ ] Never set `WEFT_AUTH_DISABLED` outside a laptop.

## Network

- [ ] **TLS termination** at the ingress/load balancer; weftd speaks plain
      HTTP inside the cluster/host network.
- [ ] **Do not expose `/internal/v1`** publicly — only agent runtimes need it,
      and they reach instances directly (`server.internalUrl` = pod IP).
- [ ] **Restrict `/metrics`** to the monitoring network.
- [ ] **NetworkPolicies** (`deploy/kubernetes/networkpolicies.yaml`): default
      deny; agents may only reach weftd; weftd egress limited to broker,
      database, telemetry, model servers and approved external APIs.
- [ ] **Egress control for `http.fetch`**: grant `hosts` allowlists in graphs;
      keep `tools.allowPrivateNetworks: false` (SSRF guard); add an egress
      proxy/FQDN policy for defence in depth.
- [ ] `server.corsOrigins` empty unless a separate console origin is required.

## Secrets

- [ ] Model keys only via `apiKeyEnv`/`apiKeyFile` from a secret manager
      (Kubernetes Secrets via External Secrets/Sealed Secrets/SOPS, Vault
      agent, Docker secrets). The gateway is the only holder; agents never see
      them.
- [ ] Start weftd with a **clean environment**: the process sandbox scrubs
      the environment for commands, but anything the server inherits is a
      liability.
- [ ] Database and broker credentials with least privilege (no superuser;
      NATS user limited to the `weft.>` subjects).

## Isolation of agent work

- [ ] **Grants are the first boundary**: default deny; narrow with `paths`,
      `commands`, `hosts`, `rateLimit`; avoid `*` (validation warns).
- [ ] **Treat `shell.exec` as code execution.** The process sandbox confines
      the working directory, environment and timeout but not system calls. For
      untrusted repositories or inputs use `isolation.mode: docker`
      (no network, all capabilities dropped, `no-new-privileges`, CPU/memory
      limits), or run weftd under a sandboxed runtime class
      (**gVisor** `runsc` or **Kata Containers**) on Kubernetes.
- [ ] Run docker isolation on a daemon dedicated to Weft — never mount the
      host's Docker socket into weftd (root on that host). Compose ships
      `sandbox-daemon` for this; it is privileged Docker-in-Docker, so run
      Weft on a dedicated host or VM. Keep agents on `network: none`
      unless they must download packages. See [sandbox runtime](sandbox-runtime.md).
- [ ] Containers: non-root (uid 10001), read-only root filesystem, seccomp
      `RuntimeDefault`, `automountServiceAccountToken: false` (the shipped
      manifests do this).
- [ ] Use `workspace.mode: worktree` for parallel writers; keep deliverables in
      git remotes with protected branches so agents cannot rewrite history
      that matters.

## LLM safety

- [ ] **Prompt injection posture**: inputs are framed as data in the preamble,
      but an autonomous agent can still be steered. Assume any agent with a
      tool can be made to use it; design grants accordingly.
- [ ] **Approval gates on destructive steps**: `requiresApproval: true` on
      grants such as `git.write`, `filesystem.delete`, deployment tools;
      `approval: true` on release channels; `approval.beforeTurn` on rarely-run
      high-impact nodes. Set `approval.timeout` + `onTimeout: reject`.
- [ ] Review third-party repositories before pointing agents at them
      (instructions in files are read by agents).
- [ ] Keep `maxHops` low enough to cut runaway cycles.

## Data and durability

- [ ] **PostgreSQL** with HA (operator or managed), continuous archiving and
      tested point-in-time restore.
- [ ] **NATS JetStream** as a 3-node cluster with `queue.options.replicas: "3"`
      (or Kafka RF 3 / RabbitMQ quorum queues / Redis with replication and AOF).
- [ ] **Retention**: set `channel.retention`; prune old events and message
      history on a schedule (database job) according to your audit policy;
      Loki/Tempo retention is configured in `deploy/observability/`.
- [ ] Backups for workspaces (or rely on git remotes); see [deployment](deployment.md#backup-and-restore).
- [ ] **Give workspaces a dedicated Linux filesystem (PROD-08).** On the
      selected sandbox host or VM, mount a dedicated block volume with a
      filesystem supporting fanotify file handles, such as ext4. Mount that
      filesystem at the existing workspace path in both weftd and the
      dedicated daemon, and keep the daemon's image layers, the journal and
      PostgreSQL on other filesystems. A named Docker volume backed by a
      directory on the same filesystem still shares the fanotify filesystem
      mark. A mount-only fanotify mark cannot report the file-handle events
      needed for creation and moves. [Linux fanotify marks](https://man7.org/linux/man-pages/man2/fanotify_mark.2.html)
      · [Docker volumes](https://docs.docker.com/engine/storage/volumes/)

      Plan the mount and data migration with the production topology. Preserve
      workspace ownership and Git metadata and verify that the daemon sees a
      distinct filesystem. In the chosen environment, test transient operations
      and PID attribution from nested sandboxes while generating writes on the
      other filesystems. Compare the unresolved-path and overflow rates before
      and after the migration.

## Reliability

- [ ] **≥ 2 weftd instances** with PostgreSQL + clustered broker; PDB
      `minAvailable: 1`; spread across nodes/zones.
- [ ] **Lease TTL**: `engine.leaseTTL` trades failover time against
      sensitivity to pauses (GC, network). 20–30 s is a good default;
      `reconcileInterval` ≤ TTL/4.
- [ ] **Clock sync (NTP/chrony)** on all instances: lease expiry is evaluated
      with the calling instance's clock.
- [ ] **Graceful termination** longer than typical turns
      (`terminationGracePeriodSeconds`, `stop_grace_period`); long turns that
      exceed it are cancelled and their inputs redelivered.
- [ ] **Bound concurrency**: `engine.maxConcurrentTurns` per instance;
      provider `concurrency` per model backend; `cluster.maxReplicas` per node.
- [ ] **Timeouts**: `limits.turnTimeout` per node; provider `timeoutSec` and
      `maxRetries`; channel `ackWait` larger than the longest expected turn
      (leases are extended during a turn, but `ackWait` bounds recovery time).
- [ ] **Idempotent agents**: delivery is at-least-once with dedupe by message
      id; external side effects (APIs, deployments) should use the message id
      as an idempotency key.

## Cost controls

- [ ] `limits.maxTokens` and `limits.maxToolCalls` per turn.
- [ ] Prefer `loop.mode: event` for consumers; set stop policies
      (`iterations`, `duration`, `auto`, `idle`) on producers and research loops.
- [ ] `loop.interval` / `scheduled` for loops polling external systems.
- [ ] Alert on token rate (`weft_llm_tokens_total`) per provider/model.

## Observability

- [ ] Ship OTLP to your collector (`observability.otlpEndpoint`); sample traces
      (`traceSampleRatio` 0.1–0.2) at high volume — parent-based sampling keeps
      whole agent chains together.
- [ ] Load `deploy/observability/alerts.yaml` (dead letters, growing backlog,
      turn failure rate, waiting approvals, LLM errors and journal segment
      capacity) into Prometheus / Alertmanager and route them.
- [ ] Define SLOs, e.g. *95 % of messages start processing within 2 min*
      (`weft_message_queue_latency_seconds`), *turn success ≥ 99 %*
      (`weft_executions_total`), *API availability ≥ 99.9 %*.
- [ ] Log retention per policy; logs are JSON on stdout and OTLP, correlated
      with traces by trace id.
- [ ] The event log is your audit trail (tool calls, approvals with
      decider, graph changes with author): keep it as long as compliance requires.

## Supply chain

- [ ] Build images in CI from a pinned base, generate an **SBOM** (Syft) and
      scan (Grype/Trivy); fail on critical vulnerabilities.
- [ ] **Sign images** (Cosign/Sigstore, or Notation) and enforce signatures at
      admission (Kyverno/Gatekeeper/Connaisseur).
- [ ] Deploy by **digest**, not tag; mirror third-party images (NATS,
      PostgreSQL, observability) into your registry.
- [ ] Pin Python agent dependencies and model versions; record the model in
      graph documents (`model.name`) so changes are reviewed.

## Capacity and resilience testing

- [ ] Load test with `runtime.kind: echo` graphs to measure broker/store
      throughput without model cost; then with a fixed local model.
- [ ] Chaos: kill a weftd instance mid-turn and verify takeover after the
      lease TTL and redelivery of its inputs; restart the broker and database.
- [ ] Verify backups by restoring into a staging environment regularly.

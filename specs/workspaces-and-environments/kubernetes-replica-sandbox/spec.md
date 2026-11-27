---
id: ENV-010
title: Kubernetes replica tool sandboxes
summary: Execute granted commands in a workspace-scoped Kubernetes pod for the addressed graph, node and replica.
status: in-progress
scope: [Kubernetes command execution, workload identity, workspace mounts, admission, recovery]
risk: high
risk_domains: [authorization, isolation, credentials, storage]
change_surfaces: [server/internal/app, server/internal/tools, deploy/kubernetes]
dependencies: [TASK-006, ENV-005, ENV-009, RUN-010]
navigation: [specs/README.md, specs/workspaces-and-environments/kubernetes-replica-sandbox/matrix.md]
authority:
  granted: [local implementation, scoped commits, automatic sandbox provisioning, namespace-scoped pod lifecycle and exec]
  withheld: [production deployment, remote push]
---

# Kubernetes replica tool sandboxes

The maintainer selected a sandbox per replica for Kubernetes. A turn still runs through Weft's existing native or HTTP runtime. The Eino service can serve many agents. A tool request crosses the Weft gateway before a command starts in the pod assigned to the requesting replica. This also applies to a declared project check. The controller's per-execution HTTP callback identity is independent of the identity that Kubernetes uses to start a command.

## Acceptance criteria

### AC-001 — A command has one execution location

A Kubernetes node's command must execute only in the sandbox assigned to its effective graph, node, replica and workspace. Missing configuration, unavailable capacity, lost identity or a failed API request returns a diagnosable error. The controller never substitutes its own OS process, including for existing graphs and confirmed actions. An explicitly configured process node continues to use its process sandbox. A turn with ordinary file or model callbacks can run without a shell.

### AC-002 — Workload permissions confine the command

An unprivileged sandbox receives only the effective workspace or worktree and its declared access mode, bounded scratch space and approved runtime image. Shared workspace nodes can see the same files by design. Distinct worktrees and unrelated graphs stay separate. Prevent access to controller credentials, Kubernetes API credentials, host mounts, Docker sockets and the platform network. Restrict CPU, memory and namespace resource use. Require a network-policy-enforcing cluster for qualification. A graph or model cannot choose its Kubernetes identity, namespace, volumes or security policy. Preserve existing task consent, tool grants and mandatory approvals.

### AC-003 — Cancellation, recovery and QA yield observable outcomes

Preserve command argv, working directory, bounded stdout/stderr, exit status, timeout and cancellation. Bind work to a pod identity across requests. A disappearing pod or lost exec connection produces an infrastructure failure. Reconcile by ownership and UID before reusing or removing a pod. Recreating a sandbox retains the workspace claim. Normal cleanup removes only owned disposable workloads and never the workspace. Record execution correlation and tool outcomes without publishing command credentials or arbitrary output as metric labels.

The acceptance journey uses a failing test followed by a passing test for the same task in the assigned pod. The first result returns to development. The second reaches the existing human release gate. Claim support only after observing consumer acknowledgement following approval.

## Provisioning and compatibility

Kubernetes namespace roles, pod admission and PVC binding are installation decisions. A claim is namespaced and binds to one PV. A separate sandbox namespace needs a separately provisioned view of the same workspace backing, or an explicit transfer design. The controller's current restricted namespace also hosts platform credentials. A general pod creation or exec permission in that namespace would cross the isolation boundary. The maintainer selected automatic provisioning for workflows created in Chat, the console or the API. A namespaced Role bound to the controller service account grants pod lifecycle and exec solely in `weft-sandboxes`. The installer configures the workspace storage and an enforced admission policy once. Starting a graph requires no manifest changes. In the Kubernetes installation profile the controller routes command tools into sandbox pods and requires an endpoint for external agent runtimes. The standalone profile continues to support local processes. [Kubernetes RBAC guidance](https://kubernetes.io/docs/concepts/security/rbac-good-practices/) · [Persistent volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)

Drain Kubernetes shell turns before cutover. Keep the controller callback Service and existing approvals during the rollout. If sandbox qualification fails, keep Kubernetes command execution unavailable and retain pending work for retry under normal Weft delivery semantics. Docker and explicit native-process sandboxes have their own installation and consent contracts. [Task consent](../../product-and-tasks/task-consent-and-gateway-authorization/spec.md) · [Qualification matrix](./matrix.md)
